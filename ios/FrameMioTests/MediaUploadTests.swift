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

    func testFailedUploadReauthorizesWhenMaterializedFileSizeChanged() async throws {
        let asset = makeAsset(id: "icloud-photo", kind: .image)
        let stagedURL = FileManager.default.temporaryDirectory
            .appendingPathComponent("FrameMio-stale-size-\(UUID().uuidString).jpg")
        try Data(repeating: 9, count: 200).write(to: stagedURL)
        defer { try? FileManager.default.removeItem(at: stagedURL) }

        let uploadID = MediaUpload.makeID(for: asset.id, optimization: .original)
        let staleSession = UploadSession(
            uploadID: "upload-stale",
            mediaID: "media-stale",
            s3Key: "users/user-1/media/media-stale/icloud-photo.jpg",
            strategy: .single,
            partSizeBytes: 100,
            s3MultipartUploadID: nil,
            expiresAt: Date().addingTimeInterval(3_600),
            completed: nil,
            parts: [
                UploadPart(
                    partNumber: 1,
                    offsetBytes: 0,
                    lengthBytes: 100,
                    presignedURL: URL(string: "https://s3.example.test/stale/1?X-Amz-SignedHeaders=host")!,
                    status: .completed,
                    attempts: 1,
                    eTag: "etag-stale",
                    lastError: nil
                )
            ]
        )
        let staleUpload = MediaUpload(
            id: uploadID,
            assetID: asset.id,
            localIdentifier: asset.localIdentifier,
            mediaID: staleSession.mediaID,
            mediaType: .photo,
            fileName: "icloud-photo.jpg",
            contentType: "image/jpeg",
            fileSizeBytes: 100,
            duration: nil,
            creationDate: asset.creationDate,
            pixelWidth: asset.pixelWidth,
            pixelHeight: asset.pixelHeight,
            orientation: .portrait,
            optimization: .original,
            status: .failed,
            uploadedBytes: 100,
            authorizationIdempotencyKey: nil,
            session: staleSession,
            stagedFileURL: stagedURL,
            lastError: "uploaded object size does not match authorization",
            updatedAt: Date()
        )
        let source = makeSource(asset: asset, byteCount: 999)
        let authorizer = MockMediaUploadAuthorizer()
        let storage = MockDirectObjectStorageClient()
        let store = MockMediaUploadStore(uploads: [staleUpload])
        let service = makeService(source: source, authorizer: authorizer, storage: storage, store: store)

        let uploads = try await service.resumePendingUploads(options: makeOptions(), progress: { _ in })

        let upload = try XCTUnwrap(uploads.first)
        let requestSizes = await storage.requests.map(\.fileSizeBytes)
        let requestedKeys = await authorizer.requestedIdempotencyKeys
        XCTAssertEqual(upload.status, .completed)
        XCTAssertEqual(upload.fileSizeBytes, 200)
        XCTAssertEqual(upload.uploadedBytes, 200)
        XCTAssertEqual(upload.session?.parts.first?.lengthBytes, 200)
        XCTAssertEqual(requestSizes, [200])
        XCTAssertEqual(requestedKeys, [MediaUpload.makeAuthorizationID(for: uploadID, fileSizeBytes: 200)])
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


    func testInvalidateCompletedUploadsForcesFreshUploadSession() async throws {
        let asset = makeAsset(id: "stale-photo", kind: .image)
        let source = makeSource(asset: asset, byteCount: 128 * 1_024)
        let authorizer = MockMediaUploadAuthorizer()
        let storage = MockDirectObjectStorageClient()
        let store = MockMediaUploadStore()
        let service = makeService(source: source, authorizer: authorizer, storage: storage, store: store)
        let selection = selection(for: [asset])
        let options = makeOptions()

        _ = try await service.upload(selection: selection, assets: [asset], options: options, progress: { _ in })
        try await service.invalidateCompletedUploads(for: [asset], optimization: .original)
        _ = try await service.upload(selection: selection, assets: [asset], options: options, progress: { _ in })

        let uploadID = MediaUpload.makeID(for: asset.id, optimization: .original)
        let refreshed = try await store.upload(for: uploadID)
        let materializationCount = await source.materializationCount
        let requestCount = await storage.requests.count
        let createSessionCallCount = await authorizer.createSessionCallCount
        let notificationCount = await authorizer.completedNotifications.count
        XCTAssertEqual(refreshed?.status, .completed)
        XCTAssertEqual(materializationCount, 2)
        XCTAssertEqual(requestCount, 2)
        XCTAssertEqual(createSessionCallCount, 2)
        XCTAssertEqual(notificationCount, 2)
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

    func testUploadRejectsSelectionOverTotalSizeLimitBeforeAuthorizing() async throws {
        let assets = [
            makeAsset(id: "small-video-1", kind: .video),
            makeAsset(id: "small-video-2", kind: .video)
        ]
        let fixtures = Dictionary(uniqueKeysWithValues: assets.map { asset in
            (asset.id, MockOriginalMediaSource.Fixture(data: Data(repeating: 1, count: 700)))
        })
        let source = MockOriginalMediaSource(fixtures: fixtures)
        let authorizer = MockMediaUploadAuthorizer()
        let storage = MockDirectObjectStorageClient()
        let store = MockMediaUploadStore()
        let service = makeService(source: source, authorizer: authorizer, storage: storage, store: store)

        do {
            _ = try await service.upload(
                selection: selection(for: assets),
                assets: assets,
                options: makeOptions(maximumTotalUploadBytes: 1_000),
                progress: { _ in }
            )
            XCTFail("Expected total-size limit failure")
        } catch let AppError.message(message) {
            XCTAssertEqual(message, "Selected media must be under 1 GB per story.")
        }

        let createSessionCallCount = await authorizer.createSessionCallCount
        let requestCount = await storage.requests.count
        let removedURLs = await source.removedURLs
        XCTAssertEqual(createSessionCallCount, 0)
        XCTAssertEqual(requestCount, 0)
        XCTAssertEqual(removedURLs.count, 2)
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
        partSizeBytes: Int = 8 * 1_024 * 1_024,
        maximumPartRetries: Int = 3,
        maximumTotalUploadBytes: Int64? = 1_000_000_000
    ) -> MediaUploadOptions {
        MediaUploadOptions(
            usesBackgroundURLSession: usesBackgroundURLSession,
            maximumConcurrentUploads: maximumConcurrentUploads,
            maximumPartRetries: maximumPartRetries,
            partSizeBytes: Int64(partSizeBytes),
            maximumTotalUploadBytes: maximumTotalUploadBytes
        )
    }
}
