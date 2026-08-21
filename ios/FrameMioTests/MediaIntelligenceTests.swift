import XCTest
@testable import FrameMio

final class MediaIntelligenceTests: XCTestCase {
    func testMediaSelectionOnlyIncludesAutomaticAssetsWhenEnabled() {
        let disabled = MediaSelection(
            explicitlySelectedAssetIDs: ["photo-1", "photo-1", "video-1"],
            automaticMemoryAssetIDs: ["automatic-1", "photo-1"],
            automaticMemoryEnabled: false
        )
        XCTAssertEqual(disabled.uploadEligibleAssetIDs, ["photo-1", "video-1"])
        XCTAssertFalse(disabled.permitsUpload(of: "automatic-1"))

        let enabled = MediaSelection(
            explicitlySelectedAssetIDs: ["photo-1", "video-1"],
            automaticMemoryAssetIDs: ["automatic-1", "photo-1"],
            automaticMemoryEnabled: true
        )
        XCTAssertEqual(enabled.uploadEligibleAssetIDs, ["photo-1", "video-1", "automatic-1"])
        XCTAssertTrue(enabled.permitsUpload(of: "automatic-1"))
    }

    func testPhotoAndVideoMetadataRoundTripThroughJSON() throws {
        let date = Date(timeIntervalSince1970: 1_700_000_000)
        let photo = MediaAssetIntelligence(
            assetID: "photo-1",
            analyzedAt: date,
            photoMetadata: makePhotoMetadata(
                assetID: "photo-1",
                date: date,
                hash: PerceptualHash(bits: 0x0f0f0f0f0f0f0f0f)
            ),
            videoMetadata: nil
        )
        let video = MediaAssetIntelligence(
            assetID: "video-1",
            analyzedAt: date,
            photoMetadata: nil,
            videoMetadata: VideoMetadata(
                assetID: "video-1",
                creationDate: date,
                location: GeoCoordinate(latitude: 31.2304, longitude: 121.4737),
                duration: 12.5,
                pixelWidth: 1_920,
                pixelHeight: 1_080,
                orientation: .landscape,
                framesPerSecond: 29.97,
                fileSizeBytes: 18_000_000,
                hasAudio: true,
                fileType: "public.mpeg-4",
                quality: VideoQualityIndicators(
                    resolutionScore: 1,
                    frameRateScore: 0.999,
                    overallScore: 0.9998
                )
            )
        )

        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let data = try encoder.encode([photo, video])
        let decoded = try decoder.decode([MediaAssetIntelligence].self, from: data)

        XCTAssertEqual(decoded, [photo, video])
    }

    func testPerceptualHashDistance() {
        let original = PerceptualHash(bits: 0)
        let identical = PerceptualHash(bits: 0)
        let nearby = PerceptualHash(bits: 0b1011)

        XCTAssertEqual(original.hammingDistance(to: identical), 0)
        XCTAssertEqual(original.hammingDistance(to: nearby), 3)
    }

    func testClustererGroupsNearDuplicatesAndSeparatesUnrelatedPhotos() async throws {
        let start = Date(timeIntervalSince1970: 1_700_000_000)
        let values = [
            makePhotoIntelligence(assetID: "photo-1", date: start, hash: PerceptualHash(bits: 0)),
            makePhotoIntelligence(
                assetID: "photo-2",
                date: start.addingTimeInterval(3),
                hash: PerceptualHash(bits: 0b101)
            ),
            makePhotoIntelligence(
                assetID: "photo-3",
                date: start.addingTimeInterval(5),
                hash: PerceptualHash(bits: UInt64.max)
            )
        ]

        let clusters = try await MediaClusterer().clusters(from: values)
        let sizes = clusters.map(\.assetIDs.count).sorted()

        XCTAssertEqual(sizes, [1, 2])
        XCTAssertTrue(clusters.contains { Set($0.assetIDs) == Set(["photo-1", "photo-2"]) })
    }

    func testMockIntelligenceServiceRespectsCancellation() async {
        let service = MockMediaIntelligenceService(perAssetDelay: .milliseconds(100))
        let assets = (0..<20).map(makeAsset)
        let task = Task {
            try await service.analyze(
                assets: assets,
                options: MediaIntelligenceOptions(usesCachedResults: false)
            )
        }

        try? await Task.sleep(for: .milliseconds(30))
        task.cancel()

        do {
            _ = try await task.value
            XCTFail("Expected the intelligence task to be cancelled")
        } catch is CancellationError {
        } catch {
            XCTFail("Expected CancellationError, received \(error)")
        }
    }

    func testMockIntelligenceServiceProcessesThousandsOfMetadataOnlyAssets() async throws {
        let service = MockMediaIntelligenceService(perAssetDelay: .zero)
        let assets = (0..<3_000).map(makeAsset)
        let results = try await service.analyze(
            assets: assets,
            options: MediaIntelligenceOptions(usesCachedResults: false)
        )

        XCTAssertEqual(results.count, assets.count)
        XCTAssertTrue(results.allSatisfy { $0.photoMetadata != nil })
    }

    private func makePhotoIntelligence(
        assetID: String,
        date: Date,
        hash: PerceptualHash
    ) -> MediaAssetIntelligence {
        MediaAssetIntelligence(
            assetID: assetID,
            analyzedAt: date,
            photoMetadata: makePhotoMetadata(assetID: assetID, date: date, hash: hash),
            videoMetadata: nil
        )
    }

    private func makePhotoMetadata(
        assetID: String,
        date: Date,
        hash: PerceptualHash
    ) -> PhotoMetadata {
        PhotoMetadata(
            assetID: assetID,
            creationDate: date,
            location: GeoCoordinate(latitude: 31.2304, longitude: 121.4737),
            pixelWidth: 3_024,
            pixelHeight: 4_032,
            orientation: .portrait,
            fileType: "public.jpeg",
            isLivePhoto: false,
            quality: PhotoQualityIndicators(
                blurScore: 0.1,
                sharpnessScore: 0.9,
                brightness: 0.52,
                exposureScore: 0.96,
                overallScore: 0.924
            ),
            faces: [],
            peopleCount: 0,
            classifications: [MediaClassification(identifier: "outdoor", confidence: 0.8)],
            perceptualHash: hash
        )
    }

    private func makeAsset(index: Int) -> MediaAsset {
        MediaAsset(
            id: "asset-\(index)",
            localIdentifier: nil,
            kind: .image,
            creationDate: Date(timeIntervalSince1970: Double(index)),
            duration: 0,
            pixelWidth: 3_024,
            pixelHeight: 4_032
        )
    }
}
