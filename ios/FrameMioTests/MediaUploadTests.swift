import XCTest
@testable import FrameMio

final class MediaUploadTests: XCTestCase {
    private let mebibyte = 1_024 * 1_024

    func testInterruptedUploadPreservesCompletedPartsAndResumes() async throws {
        let asset = makeAsset(id: "large-video", kind: .video, duration: 42.3)
        let source = makeSource(asset: asset, byteCount: 11 * mebibyte)
        let authorizer = MockMediaUploadAuthorizer()
        let storage = MockDirectObjectStorageClient(cancellationRequestNumbers: [2])
        let store = MockMediaUploadStore()
        let service = makeService(source: source, authorizer: authorizer, storage: storage, store: store)
        let options = makeOptions(partSizeBytes: 5 * mebibyte, maximumPartRetries: 1)

        do {
            _ = try await service.upload(
                selection: selection(for: [asset]),
                assets: [asset],
                options: options,
                progress: { _ in }
            )
            XCTFail("Expected cancellation")
        } catch is CancellationError {
        }

        let uploadID = MediaUpload.makeID(for: asset.id, optimization: .original)
        let pausedRecord = try await store.upload(for: uploadID)
        let paused = try XCTUnwrap(pausedRecord)
        XCTAssertEqual(paused.status, .paused)
        XCTAssertEqual(paused.session?.parts.filter(\.isCompleted).count, 1)

        let resumed = try await service.resumePendingUploads(options: options, progress: { _ in })
        let materializationCount = await source.materializationCount
        let requestCount = await storage.requests.count
        let createSessionCallCount = await authorizer.createSessionCallCount
        XCTAssertEqual(resumed.first?.status, .completed)
        XCTAssertEqual(materializationCount, 1)
        XCTAssertEqual(requestCount, 4)
        XCTAssertEqual(createSessionCallCount, 1)
    }

    func testFailedPartRetriesWithoutRestartingCompletedParts() async throws {
        let asset = makeAsset(id: "retry-video", kind: .video)
        let source = makeSource(asset: asset, byteCount: 6 * mebibyte)
        let authorizer = MockMediaUploadAuthorizer()
        let storage = MockDirectObjectStorageClient(failuresRemainingByPartNumber: [1: 1])
        let store = MockMediaUploadStore()
        let service = makeService(source: source, authorizer: authorizer, storage: storage, store: store)

        let uploads = try await service.upload(
            selection: selection(for: [asset]),
            assets: [asset],
            options: makeOptions(partSizeBytes: 5 * mebibyte, maximumPartRetries: 2),
            progress: { _ in }
        )

        let upload = try XCTUnwrap(uploads.first)
        let requestCount = await storage.requests.count
        XCTAssertEqual(upload.status, .completed)
        XCTAssertEqual(upload.session?.parts.first?.attempts, 2)
        XCTAssertEqual(upload.session?.parts.last?.attempts, 1)
        XCTAssertEqual(requestCount, 3)
    }

    func testDuplicateUploadReturnsCompletedRecordWithoutUploadingAgain() async throws {
        let asset = makeAsset(id: "duplicate-photo", kind: .image)
        let source = makeSource(asset: asset, byteCount: 128 * 1_024)
        let authorizer = MockMediaUploadAuthorizer()
        let storage = MockDirectObjectStorageClient()
        let store = MockMediaUploadStore()
        let service = makeService(source: source, authorizer: authorizer, storage: storage, store: store)
        let options = makeOptions()
        let selection = selection(for: [asset])

        let first = try await service.upload(
            selection: selection,
            assets: [asset],
            options: options,
            progress: { _ in }
        )
        let second = try await service.upload(
            selection: selection,
            assets: [asset],
            options: options,
            progress: { _ in }
        )

        let materializationCount = await source.materializationCount
        let requestCount = await storage.requests.count
        let createSessionCallCount = await authorizer.createSessionCallCount
        let notificationCount = await authorizer.completedNotifications.count
        XCTAssertEqual(first, second)
        XCTAssertEqual(materializationCount, 1)
        XCTAssertEqual(requestCount, 1)
        XCTAssertEqual(createSessionCallCount, 1)
        XCTAssertEqual(notificationCount, 1)
    }

    func testLargeVideoUsesMultipartPartSizedFiles() async throws {
        let asset = makeAsset(id: "multipart-video", kind: .video, duration: 42.3)
        let source = makeSource(asset: asset, byteCount: 11 * mebibyte)
        let authorizer = MockMediaUploadAuthorizer()
        let storage = MockDirectObjectStorageClient()
        let store = MockMediaUploadStore()
        let service = makeService(source: source, authorizer: authorizer, storage: storage, store: store)

        let uploads = try await service.upload(
            selection: selection(for: [asset]),
            assets: [asset],
            options: makeOptions(partSizeBytes: 5 * mebibyte),
            progress: { _ in }
        )

        let upload = try XCTUnwrap(uploads.first)
        let requestSizes = await storage.requests.map(\.fileSizeBytes)
        let multipartPartCount = await authorizer.completedMultipartUploads.first?.count
        let completedDuration = await authorizer.completedNotifications.first?.duration
        XCTAssertEqual(upload.session?.strategy, .multipart)
        XCTAssertEqual(requestSizes, [
            Int64(5 * mebibyte),
            Int64(5 * mebibyte),
            Int64(1 * mebibyte)
        ])
        XCTAssertEqual(multipartPartCount, 3)
        XCTAssertEqual(completedDuration, 42.3)
    }

    func testMultipleUploadsRespectConcurrencyLimit() async throws {
        let assets = (1...4).map { makeAsset(id: "photo-\($0)", kind: .image) }
        let fixtures = Dictionary(uniqueKeysWithValues: assets.map { asset in
            (asset.id, MockOriginalMediaSource.Fixture(data: Data(repeating: 1, count: 64 * 1_024)))
        })
        let source = MockOriginalMediaSource(fixtures: fixtures)
        let authorizer = MockMediaUploadAuthorizer()
        let storage = MockDirectObjectStorageClient(uploadDelay: .milliseconds(80))
        let store = MockMediaUploadStore()
        let service = makeService(source: source, authorizer: authorizer, storage: storage, store: store)

        let uploads = try await service.upload(
            selection: selection(for: assets),
            assets: assets,
            options: makeOptions(maximumConcurrentUploads: 2),
            progress: { _ in }
        )

        let maximumActiveUploads = await storage.maximumActiveUploads
        XCTAssertEqual(uploads.count, 4)
        XCTAssertEqual(maximumActiveUploads, 2)
    }

    func testBackgroundUploadOptionReachesDirectStorageClient() async throws {
        let asset = makeAsset(id: "background-video", kind: .video)
        let source = makeSource(asset: asset, byteCount: 256 * 1_024)
        let authorizer = MockMediaUploadAuthorizer()
        let storage = MockDirectObjectStorageClient()
        let store = MockMediaUploadStore()
        let service = makeService(source: source, authorizer: authorizer, storage: storage, store: store)

        _ = try await service.upload(
            selection: selection(for: [asset]),
            assets: [asset],
            options: makeOptions(usesBackgroundURLSession: true),
            progress: { _ in }
        )

        let backgroundFlags = await storage.requests.map(\.usesBackgroundSession)
        XCTAssertEqual(backgroundFlags, [true])
    }

    private func makeService(
        source: MockOriginalMediaSource,
        authorizer: MockMediaUploadAuthorizer,
        storage: MockDirectObjectStorageClient,
        store: MockMediaUploadStore
    ) -> MediaUploadCoordinator {
        MediaUploadCoordinator(
            source: source,
            authorizer: authorizer,
            objectStorage: storage,
            store: store
        )
    }

    private func makeSource(asset: MediaAsset, byteCount: Int) -> MockOriginalMediaSource {
        MockOriginalMediaSource(
            fixtures: [
                asset.id: MockOriginalMediaSource.Fixture(
                    data: Data(repeating: 7, count: byteCount),
                    fileName: asset.isVideo ? "\(asset.id).mov" : "\(asset.id).jpg",
                    contentType: asset.isVideo ? "video/quicktime" : "image/jpeg"
                )
            ]
        )
    }

    private func makeAsset(
        id: String,
        kind: MediaKind,
        duration: TimeInterval = 8
    ) -> MediaAsset {
        MediaAsset(
            id: id,
            localIdentifier: "local-\(id)",
            kind: kind,
            creationDate: Date(timeIntervalSince1970: 1_700_000_000),
            duration: duration,
            pixelWidth: kind == .video ? 1_920 : 3_024,
            pixelHeight: kind == .video ? 1_080 : 4_032
        )
    }

    private func selection(for assets: [MediaAsset]) -> MediaSelection {
        MediaSelection(explicitlySelectedAssetIDs: assets.map(\.id))
    }

    private func makeOptions(
        usesBackgroundURLSession: Bool = false,
        maximumConcurrentUploads: Int = 2,
        maximumPartRetries: Int = 3,
        partSizeBytes: Int = 8 * 1_024 * 1_024
    ) -> MediaUploadOptions {
        MediaUploadOptions(
            usesBackgroundURLSession: usesBackgroundURLSession,
            maximumConcurrentUploads: maximumConcurrentUploads,
            maximumPartRetries: maximumPartRetries,
            partSizeBytes: Int64(partSizeBytes)
        )
    }
}
