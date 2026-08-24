import Foundation

private func mediaUploadDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][MediaUpload] \(message())")
#endif
}

final class MediaUploadCoordinator: MediaUploadService, @unchecked Sendable {
    private let source: OriginalMediaSource
    private let authorizer: MediaUploadAuthorizer
    private let objectStorage: DirectObjectStorageClient
    private let store: MediaUploadStore

    init(
        source: OriginalMediaSource,
        authorizer: MediaUploadAuthorizer,
        objectStorage: DirectObjectStorageClient,
        store: MediaUploadStore
    ) {
        self.source = source
        self.authorizer = authorizer
        self.objectStorage = objectStorage
        self.store = store
    }

    func upload(
        selection: MediaSelection,
        assets: [MediaAsset],
        options: MediaUploadOptions = MediaUploadOptions(),
        progress: @escaping @Sendable (MediaUploadProgress) -> Void = { _ in }
    ) async throws -> [MediaUpload] {
        guard options.optimization == .original else {
            throw AppError.message("Media optimization is not implemented yet; the original is preserved.")
        }

        let eligibleIDs = Set(selection.uploadEligibleAssetIDs)
        let eligibleAssets = assets.filter { eligibleIDs.contains($0.id) }
        mediaUploadDebugLog("upload requested assets=\(assets.count) eligible=\(eligibleAssets.count) max_concurrent=\(options.maximumConcurrentUploads) part_size=\(options.partSizeBytes) background=\(options.usesBackgroundURLSession)")
        guard !eligibleAssets.isEmpty else {
            return []
        }
        let preparedAssets = try await prepareUploads(eligibleAssets, options: options)
        mediaUploadDebugLog("prepared uploads count=\(preparedAssets.count)")
        return try await uploadInBatches(
            preparedAssets,
            options: options,
            progress: progress
        )
    }

    func resumePendingUploads(
        options: MediaUploadOptions = MediaUploadOptions(),
        progress: @escaping @Sendable (MediaUploadProgress) -> Void = { _ in }
    ) async throws -> [MediaUpload] {
        let pending = try await store.allUploads().filter {
            $0.status != .completed && $0.status != .cancelled
        }
        let assets = pending.map(makeAsset(from:))
        return try await uploadInBatches(assets, options: options, progress: progress)
    }

    private func prepareUploads(
        _ assets: [MediaAsset],
        options: MediaUploadOptions
    ) async throws -> [MediaAsset] {
        var prepared: [PreparedMediaUpload] = []
        var totalBytes: Int64 = 0

        do {
            for asset in assets {
                let uploadID = MediaUpload.makeID(for: asset.id, optimization: options.optimization)
                let existing = try await store.upload(for: uploadID)

                if let existing, existing.status == .completed {
                    totalBytes += existing.fileSizeBytes
                    mediaUploadDebugLog("prepare existing completed upload=\(existing.id) asset=\(asset.id) media=\(existing.mediaID ?? "nil") bytes=\(existing.fileSizeBytes)")
                    prepared.append(PreparedMediaUpload(asset: asset, record: existing, mediaFile: nil, isNewMaterialization: false))
                } else {
                    let staged = try await materializePreparedFile(for: asset, existing: existing, options: options)
                    totalBytes += staged.file.fileSizeBytes
                    mediaUploadDebugLog("prepare staged upload=\(MediaUpload.makeID(for: asset.id, optimization: options.optimization)) asset=\(asset.id) bytes=\(staged.file.fileSizeBytes) new_materialization=\(staged.isNewMaterialization)")
                    prepared.append(PreparedMediaUpload(asset: asset, record: existing, mediaFile: staged.file, isNewMaterialization: staged.isNewMaterialization))
                }

                if let maximumTotalUploadBytes = options.maximumTotalUploadBytes,
                   totalBytes > maximumTotalUploadBytes {
                    throw AppError.message("Selected media must be under 1 GB per story.")
                }
            }

            for item in prepared {
                guard let mediaFile = item.mediaFile else { continue }
                var record = item.record ?? makeUploadRecord(
                    asset: item.asset,
                    mediaFile: mediaFile,
                    options: options
                )
                record.stagedFileURL = mediaFile.url
                if record.status == .failed || record.status == .cancelled {
                    record.status = .queued
                }
                record.lastError = nil
                record.updatedAt = Date()
                try await store.save(record)
            }
            return assets
        } catch is CancellationError {
            await removeNewMaterializations(prepared)
            throw CancellationError()
        } catch {
            await removeNewMaterializations(prepared)
            throw error
        }
    }

    private func makeUploadRecord(
        asset: MediaAsset,
        mediaFile: OriginalMediaFile,
        options: MediaUploadOptions
    ) -> MediaUpload {
        MediaUpload(
            id: MediaUpload.makeID(for: asset.id, optimization: options.optimization),
            assetID: asset.id,
            localIdentifier: asset.localIdentifier,
            mediaID: nil,
            mediaType: uploadMediaType(for: asset),
            fileName: mediaFile.fileName,
            contentType: mediaFile.contentType,
            fileSizeBytes: mediaFile.fileSizeBytes,
            duration: asset.isVideo ? asset.duration : nil,
            creationDate: asset.creationDate,
            pixelWidth: asset.pixelWidth,
            pixelHeight: asset.pixelHeight,
            orientation: .from(pixelWidth: asset.pixelWidth, pixelHeight: asset.pixelHeight),
            optimization: options.optimization,
            status: .queued,
            uploadedBytes: 0,
            session: nil,
            stagedFileURL: mediaFile.url,
            lastError: nil,
            updatedAt: Date()
        )
    }

    private func removeNewMaterializations(_ prepared: [PreparedMediaUpload]) async {
        for item in prepared where item.isNewMaterialization {
            if let url = item.mediaFile?.url {
                await source.removeMaterializedMedia(at: url)
            }
        }
    }

    private func uploadInBatches(
        _ assets: [MediaAsset],
        options: MediaUploadOptions,
        progress: @escaping @Sendable (MediaUploadProgress) -> Void
    ) async throws -> [MediaUpload] {
        var results: [MediaUpload] = []
        var firstError: AppError?
        var wasCancelled = false
        let batchSize = options.maximumConcurrentUploads

        for start in stride(from: 0, to: assets.count, by: batchSize) {
            let end = min(start + batchSize, assets.count)
            let batch = Array(assets[start..<end])
            mediaUploadDebugLog("batch start range=\(start)..<\(end) size=\(batch.count)")
            await withTaskGroup(of: UploadOutcome.self) { group in
                for asset in batch {
                    group.addTask {
                        do {
                            return .success(
                                try await self.uploadOne(
                                    asset: asset,
                                    options: options,
                                    totalUploads: assets.count,
                                    progress: progress
                                )
                            )
                        } catch is CancellationError {
                            return .cancelled
                        } catch {
                            return .failure(AppError.from(error))
                        }
                    }
                }

                for await outcome in group {
                    switch outcome {
                    case let .success(upload):
                        results.append(upload)
                    case let .failure(error):
                        firstError = firstError ?? error
                    case .cancelled:
                        wasCancelled = true
                        group.cancelAll()
                    }
                }
            }
            if wasCancelled {
                throw CancellationError()
            }
            if let firstError {
                throw firstError
            }
        }
        return results.sorted { $0.assetID < $1.assetID }
    }

    private func uploadOne(
        asset: MediaAsset,
        options: MediaUploadOptions,
        totalUploads: Int,
        progress: @escaping @Sendable (MediaUploadProgress) -> Void
    ) async throws -> MediaUpload {
        let uploadID = MediaUpload.makeID(for: asset.id, optimization: options.optimization)
        var record = try await store.upload(for: uploadID)

        if let record, record.status == .completed {
            mediaUploadDebugLog("uploadOne skipped completed upload=\(record.id) asset=\(record.assetID) media=\(record.mediaID ?? "nil") bytes=\(record.fileSizeBytes)")
            progress(
                MediaUploadProgress(
                    uploadID: record.id,
                    assetID: record.assetID,
                    uploadedBytes: record.fileSizeBytes,
                    totalBytes: record.fileSizeBytes,
                    completedUploads: 1,
                    totalUploads: totalUploads
                )
            )
            return record
        }

        do {
            mediaUploadDebugLog("uploadOne start upload=\(uploadID) asset=\(asset.id) existing_status=\(record?.status.rawValue ?? "none")")
            let mediaFile = try await materializeFile(for: asset, existing: record, options: options)
            mediaUploadDebugLog("uploadOne materialized upload=\(uploadID) asset=\(asset.id) bytes=\(mediaFile.fileSizeBytes) file=\(mediaFile.fileName)")
            if record == nil {
                record = makeUploadRecord(asset: asset, mediaFile: mediaFile, options: options)
            } else if record?.stagedFileURL == nil {
                record?.stagedFileURL = mediaFile.url
            }

            guard var record else {
                throw AppError.persistenceFailure
            }
            record.status = .authorizing
            record.lastError = nil
            record.updatedAt = Date()
            try await store.save(record)

            var session = record.session
            if session == nil || session?.isUsable() == false {
                mediaUploadDebugLog("authorizing session upload=\(record.id) asset=\(record.assetID) bytes=\(record.fileSizeBytes)")
                session = try await authorizer.createUploadSession(
                    metadata: uploadMetadata(from: record, options: options),
                    idempotencyKey: record.id
                )
                mediaUploadDebugLog("authorized session upload=\(record.id) session=\(session?.uploadID ?? "nil") media=\(session?.mediaID ?? "nil") strategy=\(session?.strategy.rawValue ?? "nil") parts=\(session?.parts.count ?? 0) completed_parts=\(session?.parts.filter(\.isCompleted).count ?? 0) usable=\(session?.isUsable() ?? false)")
                record.session = session
                record.mediaID = session?.mediaID
                record.updatedAt = Date()
                try await store.save(record)
            }

            guard var session, !session.parts.isEmpty else {
                mediaUploadDebugLog("invalid session upload=\(record.id) has_session=\(record.session != nil)")
                throw AppError.invalidResponse
            }
            mediaUploadDebugLog("using session upload=\(record.id) session=\(session.uploadID) media=\(session.mediaID) strategy=\(session.strategy.rawValue) parts=\(session.parts.count) completed_parts=\(session.parts.filter(\.isCompleted).count) usable=\(session.isUsable())")

            record.status = .uploading
            try await store.save(record)
            try await uploadParts(
                session: &session,
                record: &record,
                options: options,
                totalUploads: totalUploads,
                progress: progress
            )

            record.status = .completing
            record.session = session
            try await store.save(record)
            mediaUploadDebugLog("all parts uploaded upload=\(record.id) session=\(session.uploadID) strategy=\(session.strategy.rawValue) completed_parts=\(session.parts.filter(\.isCompleted).count)/\(session.parts.count)")
            if session.strategy == .multipart {
                let completedParts = session.parts.compactMap { part -> MultipartPartCompletion? in
                    guard let eTag = part.eTag else { return nil }
                    return MultipartPartCompletion(partNumber: part.partNumber, eTag: eTag)
                }
                mediaUploadDebugLog("complete multipart start upload=\(record.id) session=\(session.uploadID) parts=\(completedParts.count)")
                try await authorizer.completeMultipartUpload(session: session, parts: completedParts)
                mediaUploadDebugLog("complete multipart done upload=\(record.id) session=\(session.uploadID)")
            }

            mediaUploadDebugLog("notify completion start upload=\(record.id) session=\(session.uploadID) media=\(session.mediaID)")
            try await authorizer.notifyUploadCompleted(
                UploadCompletionNotification(
                    mediaId: session.mediaID,
                    uploadId: session.uploadID,
                    s3Key: session.s3Key,
                    mediaType: record.mediaType,
                    duration: record.duration
                )
            )

            mediaUploadDebugLog("notify completion done upload=\(record.id) session=\(session.uploadID) media=\(session.mediaID)")
            record.status = .completed
            record.uploadedBytes = record.fileSizeBytes
            record.stagedFileURL = nil
            record.updatedAt = Date()
            try await store.save(record)
            await source.removeMaterializedMedia(at: mediaFile.url)
            progress(
                MediaUploadProgress(
                    uploadID: record.id,
                    assetID: record.assetID,
                    uploadedBytes: record.fileSizeBytes,
                    totalBytes: record.fileSizeBytes,
                    completedUploads: 1,
                    totalUploads: totalUploads
                )
            )
            return record
        } catch is CancellationError {
            if var record {
                record.status = .paused
                record.updatedAt = Date()
                try? await store.save(record)
            }
            throw CancellationError()
        } catch {
            if var record {
                record.status = .failed
                record.lastError = error.localizedDescription
                record.updatedAt = Date()
                mediaUploadDebugLog("uploadOne failed upload=\(record.id) asset=\(record.assetID) status=\(record.status.rawValue) error=\(error.localizedDescription)")
                try? await store.save(record)
            }
            throw error
        }
    }

    private func uploadParts(
        session: inout UploadSession,
        record: inout MediaUpload,
        options: MediaUploadOptions,
        totalUploads: Int,
        progress: @escaping @Sendable (MediaUploadProgress) -> Void
    ) async throws {
        guard let fileURL = record.stagedFileURL else {
            throw AppError.unsupportedMedia
        }

        for index in session.parts.indices {
            try Task.checkCancellation()
            guard !session.parts[index].isCompleted else {
                mediaUploadDebugLog("part skipped completed session=\(session.uploadID) part=\(session.parts[index].partNumber)")
                continue
            }

            session = try await refreshSessionIfNeeded(session, record: record, options: options)
            record.session = session
            try await store.save(record)

            let uploadFileURL = try makePartFileIfNeeded(
                sourceURL: fileURL,
                part: session.parts[index],
                strategy: session.strategy
            )
            defer {
                if uploadFileURL != fileURL {
                    try? FileManager.default.removeItem(at: uploadFileURL)
                }
            }

            var attempt = session.parts[index].attempts
            while true {
                try Task.checkCancellation()
                attempt += 1
                session.parts[index].attempts = attempt
                session.parts[index].status = attempt == 1 ? .uploading : .retrying
                mediaUploadDebugLog("part upload attempt session=\(session.uploadID) upload=\(record.id) part=\(session.parts[index].partNumber) attempt=\(attempt) length=\(session.parts[index].lengthBytes) strategy=\(session.strategy.rawValue)")
                record.session = session
                record.status = attempt == 1 ? .uploading : .retrying
                try await store.save(record)

                let completedBefore = session.parts
                    .filter(\.isCompleted)
                    .reduce(0) { $0 + $1.lengthBytes }
                let progressUploadID = record.id
                let progressAssetID = record.assetID
                let progressTotalBytes = record.fileSizeBytes
                do {
                    let result = try await objectStorage.upload(
                        fileURL: uploadFileURL,
                        to: session.parts[index].presignedURL,
                        headers: ["Content-Type": record.contentType],
                        usesBackgroundSession: options.usesBackgroundURLSession,
                        progress: { sent, expected in
                            progress(
                                MediaUploadProgress(
                                    uploadID: progressUploadID,
                                    assetID: progressAssetID,
                                    uploadedBytes: completedBefore + sent,
                                    totalBytes: progressTotalBytes,
                                    completedUploads: 0,
                                    totalUploads: totalUploads
                                )
                            )
                            _ = expected
                        }
                    )
                    let eTag: String
                    if session.strategy == .multipart {
                        guard let resultETag = result.eTag else {
                            throw AppError.invalidResponse
                        }
                        eTag = resultETag
                    } else {
                        eTag = result.eTag ?? "single-object"
                    }
                    mediaUploadDebugLog("part upload complete session=\(session.uploadID) upload=\(record.id) part=\(session.parts[index].partNumber) status=\(result.statusCode) etag_present=\(!eTag.isEmpty)")
                    session.parts[index].eTag = eTag
                    session.parts[index].status = .completed
                    record.uploadedBytes = session.parts
                        .filter(\.isCompleted)
                        .reduce(0) { $0 + $1.lengthBytes }
                    record.session = session
                    record.status = .uploading
                    record.updatedAt = Date()
                    try await store.save(record)
                    break
                } catch is CancellationError {
                    throw CancellationError()
                } catch {
                    mediaUploadDebugLog("part upload failed session=\(session.uploadID) upload=\(record.id) part=\(session.parts[index].partNumber) attempt=\(attempt) error=\(error.localizedDescription)")
                    session.parts[index].lastError = error.localizedDescription
                    session.parts[index].status = .failed
                    record.session = session
                    record.lastError = error.localizedDescription
                    try? await store.save(record)
                    guard attempt < options.maximumPartRetries else {
                        throw error
                    }
                    try await Task.sleep(for: .milliseconds(250 * (1 << min(attempt - 1, 4))))
                }
            }
        }
    }

    private func uploadMetadata(from record: MediaUpload, options: MediaUploadOptions) -> MediaUploadMetadata {
        MediaUploadMetadata(
            assetID: record.assetID,
            mediaType: record.mediaType,
            contentType: record.contentType,
            fileName: record.fileName,
            fileSizeBytes: record.fileSizeBytes,
            duration: record.duration,
            creationDate: record.creationDate,
            pixelWidth: record.pixelWidth,
            pixelHeight: record.pixelHeight,
            orientation: record.orientation,
            optimization: record.optimization,
            preferredPartSizeBytes: options.partSizeBytes
        )
    }

    private func refreshSessionIfNeeded(
        _ session: UploadSession,
        record: MediaUpload,
        options: MediaUploadOptions
    ) async throws -> UploadSession {
        guard let expiresAt = session.expiresAt else {
            return session
        }
        guard expiresAt <= Date().addingTimeInterval(15 * 60) else {
            return session
        }

        mediaUploadDebugLog("refreshing upload session upload=\(record.id) session=\(session.uploadID) expires_at=\(expiresAt)")
        let refreshed = try await authorizer.createUploadSession(
            metadata: uploadMetadata(from: record, options: options),
            idempotencyKey: record.id
        )
        return mergeCompletedParts(from: session, into: refreshed)
    }

    private func mergeCompletedParts(from current: UploadSession, into refreshed: UploadSession) -> UploadSession {
        let completedParts = Dictionary(
            uniqueKeysWithValues: current.parts.compactMap { part -> (Int, UploadPart)? in
                guard part.isCompleted else { return nil }
                return (part.partNumber, part)
            }
        )
        guard !completedParts.isEmpty else {
            return refreshed
        }

        var merged = refreshed
        for index in merged.parts.indices {
            guard let completed = completedParts[merged.parts[index].partNumber] else { continue }
            merged.parts[index].status = .completed
            merged.parts[index].attempts = completed.attempts
            merged.parts[index].eTag = completed.eTag
            merged.parts[index].lastError = completed.lastError
        }
        return merged
    }

    private func materializeFile(
        for asset: MediaAsset,
        existing: MediaUpload?,
        options: MediaUploadOptions
    ) async throws -> OriginalMediaFile {
        try await materializePreparedFile(for: asset, existing: existing, options: options).file
    }

    private func materializePreparedFile(
        for asset: MediaAsset,
        existing: MediaUpload?,
        options: MediaUploadOptions
    ) async throws -> (file: OriginalMediaFile, isNewMaterialization: Bool) {
        if let stagedURL = existing?.stagedFileURL,
           FileManager.default.fileExists(atPath: stagedURL.path) {
            return (
                OriginalMediaFile(
                    url: stagedURL,
                    fileName: existing?.fileName ?? "media",
                    contentType: existing?.contentType ?? "application/octet-stream",
                    fileSizeBytes: existing?.fileSizeBytes ?? 0
                ),
                false
            )
        }
        let file = try await source.materializeOriginalMedia(
            for: asset,
            allowsNetworkAccess: options.allowsNetworkAccessToPhotos,
            progress: nil
        )
        return (file, true)
    }

    private func makePartFileIfNeeded(
        sourceURL: URL,
        part: UploadPart,
        strategy: UploadStrategy
    ) throws -> URL {
        guard strategy == .multipart else {
            return sourceURL
        }

        let fileManager = FileManager.default
        let partURL = sourceURL.deletingLastPathComponent()
            .appendingPathComponent("\(sourceURL.lastPathComponent).part-\(part.partNumber)")
        guard fileManager.createFile(atPath: partURL.path, contents: nil) else {
            throw AppError.persistenceFailure
        }

        do {
            let sourceHandle = try FileHandle(forReadingFrom: sourceURL)
            let destinationHandle = try FileHandle(forWritingTo: partURL)
            defer {
                try? sourceHandle.close()
                try? destinationHandle.close()
            }

            try sourceHandle.seek(toOffset: UInt64(part.offsetBytes))
            var remaining = part.lengthBytes
            let chunkSize = 1_024 * 1_024
            while remaining > 0 {
                try Task.checkCancellation()
                let requestedBytes = min(Int64(chunkSize), remaining)
                guard let data = try sourceHandle.read(upToCount: Int(requestedBytes)), !data.isEmpty else {
                    throw AppError.unsupportedMedia
                }
                try destinationHandle.write(contentsOf: data)
                remaining -= Int64(data.count)
            }
            return partURL
        } catch {
            try? fileManager.removeItem(at: partURL)
            throw error
        }
    }

    private func makeAsset(from upload: MediaUpload) -> MediaAsset {
        MediaAsset(
            id: upload.assetID,
            localIdentifier: upload.localIdentifier,
            kind: upload.mediaType == .video ? .video : upload.mediaType == .livePhoto ? .livePhoto : .image,
            creationDate: upload.creationDate,
            duration: upload.duration ?? 0,
            pixelWidth: upload.pixelWidth,
            pixelHeight: upload.pixelHeight
        )
    }

    private func uploadMediaType(for asset: MediaAsset) -> UploadMediaType {
        if asset.isVideo { return .video }
        if asset.isLivePhoto { return .livePhoto }
        return .photo
    }
}

private struct PreparedMediaUpload: Sendable {
    let asset: MediaAsset
    let record: MediaUpload?
    let mediaFile: OriginalMediaFile?
    let isNewMaterialization: Bool
}

private enum UploadOutcome: Sendable {
    case success(MediaUpload)
    case failure(AppError)
    case cancelled
}
