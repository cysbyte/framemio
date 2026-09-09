import Foundation
@preconcurrency import Photos
import UIKit
import UniformTypeIdentifiers

private func photoLibraryDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][PhotoLibrary] \(message())")
#endif
}

final class PhotoKitPhotoLibraryService: PhotoLibraryService, MediaSelectionService, OriginalMediaSource, @unchecked Sendable {
    private let imageManager: PHCachingImageManager

    init(imageManager: PHCachingImageManager = PHCachingImageManager()) {
        self.imageManager = imageManager
    }

    func authorizationStatus() async -> PhotoLibraryAuthorization {
        Self.authorization(for: PHPhotoLibrary.authorizationStatus(for: .readWrite))
    }

    func requestAuthorization() async -> PhotoLibraryAuthorization {
        let status = await withCheckedContinuation { continuation in
            PHPhotoLibrary.requestAuthorization(for: .readWrite) { status in
                continuation.resume(returning: status)
            }
        }
        return Self.authorization(for: status)
    }

    func fetchAssets(for context: MomentContext, filter: PhotoLibraryMediaFilter) async throws -> [MediaAsset] {
        try await fetchAssets(in: .context(context), filter: filter)
    }

    func resolveAssets(localIdentifiers: [String]) async throws -> [MediaAsset] {
        let identifiers = localIdentifiers.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty }
        guard !identifiers.isEmpty else { return [] }
        let result = PHAsset.fetchAssets(withLocalIdentifiers: identifiers, options: nil)
        var assetsByIdentifier: [String: MediaAsset] = [:]
        result.enumerateObjects { asset, _, _ in
            assetsByIdentifier[asset.localIdentifier] = Self.makeMediaAsset(from: asset)
        }
        return identifiers.compactMap { assetsByIdentifier[$0] }
    }

    func fetchAssets(
        in scope: MediaSelectionScope,
        filter: PhotoLibraryMediaFilter
    ) async throws -> [MediaAsset] {
        try Task.checkCancellation()

        let authorization = await authorizationStatus()
        guard authorization == .authorized || authorization == .limited else {
            throw AppError.notAuthorized
        }

        let options = PHFetchOptions()
        options.sortDescriptors = [NSSortDescriptor(key: #keyPath(PHAsset.creationDate), ascending: false)]
        options.includeHiddenAssets = false
        options.includeAllBurstAssets = false

        var predicates: [NSPredicate] = []

        switch filter {
        case .all:
            break
        case .photos:
            predicates.append(NSPredicate(format: "mediaType == %d", PHAssetMediaType.image.rawValue))
        case .videos:
            predicates.append(NSPredicate(format: "mediaType == %d", PHAssetMediaType.video.rawValue))
        }

        switch scope.kind {
        case .all, .album:
            break
        case .context:
            if let context = scope.context {
                appendPredicates(for: context, to: &predicates)
            }
        case .timePeriod:
            if let startDate = scope.startDate {
                predicates.append(NSPredicate(format: "creationDate >= %@", startDate as NSDate))
            }
            if let endDate = scope.endDate {
                predicates.append(NSPredicate(format: "creationDate <= %@", endDate as NSDate))
            }
        }

        options.predicate = predicates.isEmpty ? nil : NSCompoundPredicate(andPredicateWithSubpredicates: predicates)
        let fetchResult: PHFetchResult<PHAsset>
        if scope.kind == .album {
            guard let albumIdentifier = scope.albumLocalIdentifier,
                  let album = PHAssetCollection.fetchAssetCollections(
                    withLocalIdentifiers: [albumIdentifier],
                    options: nil
                  ).firstObject else {
                return []
            }
            fetchResult = PHAsset.fetchAssets(in: album, options: options)
        } else {
            fetchResult = PHAsset.fetchAssets(with: options)
        }
        return try await map(fetchResult)
    }

    func fetchAlbums() async throws -> [MediaAlbum] {
        try Task.checkCancellation()
        let authorization = await authorizationStatus()
        guard authorization == .authorized || authorization == .limited else {
            throw AppError.notAuthorized
        }

        let result = PHAssetCollection.fetchAssetCollections(
            with: .album,
            subtype: .any,
            options: nil
        )
        var albums: [MediaAlbum] = []
        albums.reserveCapacity(result.count)
        for index in 0..<result.count {
            try Task.checkCancellation()
            let collection = result.object(at: index)
            albums.append(
                MediaAlbum(
                    id: collection.localIdentifier,
                    title: collection.localizedTitle ?? "Album",
                    estimatedAssetCount: collection.estimatedAssetCount
                )
            )
        }
        return albums
    }

    func materializeOriginalMedia(
        for asset: MediaAsset,
        allowsNetworkAccess: Bool,
        progress: (@Sendable (Double) -> Void)?
    ) async throws -> OriginalMediaFile {
        try Task.checkCancellation()
        guard let localIdentifier = asset.localIdentifier,
              let phAsset = PHAsset.fetchAssets(
                withLocalIdentifiers: [localIdentifier],
                options: nil
              ).firstObject else {
            throw AppError.unsupportedMedia
        }

        guard let resource = Self.originalResource(for: phAsset) else {
            throw AppError.unsupportedMedia
        }

        let fileManager = FileManager.default
        let directory = (fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? fileManager.temporaryDirectory)
            .appendingPathComponent("FrameMio", isDirectory: true)
            .appendingPathComponent("Uploads", isDirectory: true)
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)

        if !asset.isVideo {
            return try await materializePhotoAsJPEG(
                phAsset: phAsset,
                resource: resource,
                asset: asset,
                directory: directory,
                allowsNetworkAccess: allowsNetworkAccess,
                progress: progress
            )
        }

        let type = UTType(resource.uniformTypeIdentifier)
        let fileExtension = type?.preferredFilenameExtension ?? "mov"
        let fileURL = directory
            .appendingPathComponent("\(asset.id.hashValue)-\(UUID().uuidString).\(fileExtension)")

        guard fileManager.createFile(atPath: fileURL.path, contents: nil) else {
            throw AppError.persistenceFailure
        }

        let requestOptions = PHAssetResourceRequestOptions()
        requestOptions.isNetworkAccessAllowed = allowsNetworkAccess
        requestOptions.progressHandler = { value in
            progress?(value)
        }
        let requestState = try AssetResourceFileRequestState(fileURL: fileURL)
        let manager = PHAssetResourceManager.default()

        do {
            try await withTaskCancellationHandler {
                try await withCheckedThrowingContinuation { continuation in
                    requestState.install(continuation)
                    let requestID = manager.requestData(
                        for: resource,
                        options: requestOptions,
                        dataReceivedHandler: { data in
                            requestState.append(data)
                        },
                        completionHandler: { error in
                            requestState.finish(error: error)
                        }
                    )
                    requestState.setRequestID(requestID, manager: manager)
                }
            } onCancel: {
                requestState.cancel(manager: manager)
            }
        } catch {
            try? fileManager.removeItem(at: fileURL)
            throw error
        }

        let fileSize = (try? fileURL.resourceValues(forKeys: [.fileSizeKey]).fileSize)
            .flatMap { $0.map(Int64.init) } ?? 0
        return OriginalMediaFile(
            url: fileURL,
            fileName: resource.originalFilename,
            contentType: type?.preferredMIMEType ?? "video/quicktime",
            fileSizeBytes: fileSize
        )
    }


    private func materializePhotoAsJPEG(
        phAsset: PHAsset,
        resource: PHAssetResource,
        asset: MediaAsset,
        directory: URL,
        allowsNetworkAccess: Bool,
        progress: (@Sendable (Double) -> Void)?
    ) async throws -> OriginalMediaFile {
        let options = PHImageRequestOptions()
        options.deliveryMode = .highQualityFormat
        options.resizeMode = .none
        options.isNetworkAccessAllowed = allowsNetworkAccess
        options.version = .current
        options.progressHandler = { value, _, _, _ in
            progress?(value)
        }

        let requestIDBox = ImageRequestIDBox()
        let jpegData = try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                let requestID = imageManager.requestImage(
                    for: phAsset,
                    targetSize: PHImageManagerMaximumSize,
                    contentMode: .default,
                    options: options
                ) { image, info in
                    if let cancelled = info?[PHImageCancelledKey] as? Bool, cancelled {
                        requestIDBox.resume(.failure(CancellationError()), continuation: continuation)
                    } else if let error = info?[PHImageErrorKey] as? Error {
                        requestIDBox.resume(.failure(error), continuation: continuation)
                    } else if let image, let data = image.jpegData(compressionQuality: 0.9) {
                        requestIDBox.resume(.success(data), continuation: continuation)
                    } else {
                        requestIDBox.resume(.failure(AppError.unsupportedMedia), continuation: continuation)
                    }
                }
                requestIDBox.set(requestID, imageManager: imageManager)
            }
        } onCancel: {
            requestIDBox.cancel(imageManager: imageManager)
        }

        let fileURL = directory
            .appendingPathComponent("\(asset.id.hashValue)-\(UUID().uuidString).jpg")
        do {
            try jpegData.write(to: fileURL, options: .atomic)
        } catch {
            try? FileManager.default.removeItem(at: fileURL)
            throw error
        }

        let baseName = (resource.originalFilename as NSString).deletingPathExtension
        let fileName = (baseName.isEmpty ? "photo" : baseName) + ".jpg"
        return OriginalMediaFile(
            url: fileURL,
            fileName: fileName,
            contentType: "image/jpeg",
            fileSizeBytes: Int64(jpegData.count)
        )
    }

    func removeMaterializedMedia(at url: URL) async {
        try? FileManager.default.removeItem(at: url)
    }

    func requestThumbnail(for asset: MediaAsset, targetSize: CGSize) async throws -> Data {
        try Task.checkCancellation()

        guard let localIdentifier = asset.localIdentifier,
              let phAsset = PHAsset.fetchAssets(withLocalIdentifiers: [localIdentifier], options: nil).firstObject else {
            photoLibraryDebugLog("thumbnail request rejected asset=\(asset.id) local_identifier=\(asset.localIdentifier ?? "nil") reason=asset_not_found")
            throw AppError.unsupportedMedia
        }

        photoLibraryDebugLog("thumbnail request started asset=\(asset.id) local_identifier=\(localIdentifier) target=\(Int(targetSize.width))x\(Int(targetSize.height))")
        let options = PHImageRequestOptions()
        options.deliveryMode = .highQualityFormat
        options.resizeMode = .fast
        options.isNetworkAccessAllowed = true
        options.version = .current

        let requestIDBox = ImageRequestIDBox()
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                let requestID = imageManager.requestImage(
                    for: phAsset,
                    targetSize: targetSize,
                    contentMode: .aspectFill,
                    options: options
                ) { image, info in
                    if let cancelled = info?[PHImageCancelledKey] as? Bool, cancelled {
                        photoLibraryDebugLog("thumbnail callback cancelled asset=\(asset.id)")
                        requestIDBox.resume(.failure(CancellationError()), continuation: continuation)
                    } else if let error = info?[PHImageErrorKey] as? Error {
                        photoLibraryDebugLog("thumbnail callback failed asset=\(asset.id) error=\(error.localizedDescription)")
                        requestIDBox.resume(.failure(error), continuation: continuation)
                    } else if let image, let data = image.jpegData(compressionQuality: 0.82) {
                        let degraded = (info?[PHImageResultIsDegradedKey] as? Bool) ?? false
                        let inCloud = (info?[PHImageResultIsInCloudKey] as? Bool) ?? false
                        photoLibraryDebugLog("thumbnail callback image asset=\(asset.id) bytes=\(data.count) degraded=\(degraded) in_cloud=\(inCloud)")
                        requestIDBox.resume(.success(data), continuation: continuation)
                    } else {
                        let degraded = (info?[PHImageResultIsDegradedKey] as? Bool) ?? false
                        let inCloud = (info?[PHImageResultIsInCloudKey] as? Bool) ?? false
                        photoLibraryDebugLog("thumbnail callback missing_image asset=\(asset.id) degraded=\(degraded) in_cloud=\(inCloud)")
                        requestIDBox.resume(.failure(AppError.unsupportedMedia), continuation: continuation)
                    }
                }
                requestIDBox.set(requestID, imageManager: imageManager)
            }
        } onCancel: {
            requestIDBox.cancel(imageManager: imageManager)
        }
    }

    func startCachingThumbnails(for assets: [MediaAsset], targetSize: CGSize) {
        let phAssets = fetchPHAssets(for: assets)
        imageManager.startCachingImages(
            for: phAssets,
            targetSize: targetSize,
            contentMode: .aspectFill,
            options: thumbnailOptions()
        )
    }

    func stopCachingThumbnails(for assets: [MediaAsset], targetSize: CGSize) {
        let phAssets = fetchPHAssets(for: assets)
        imageManager.stopCachingImages(
            for: phAssets,
            targetSize: targetSize,
            contentMode: .aspectFill,
            options: thumbnailOptions()
        )
    }

    private func map(_ result: PHFetchResult<PHAsset>) async throws -> [MediaAsset] {
        var assets: [MediaAsset] = []
        assets.reserveCapacity(result.count)

        for index in 0..<result.count {
            try Task.checkCancellation()
            let asset = result.object(at: index)
            assets.append(Self.makeMediaAsset(from: asset))
        }
        return assets
    }

    private func appendPredicates(for context: MomentContext, to predicates: inout [NSPredicate]) {
        switch context {
        case .today:
            let start = Calendar.current.startOfDay(for: Date())
            let end = Calendar.current.date(byAdding: .day, value: 1, to: start) ?? Date.distantFuture
            predicates.append(
                NSCompoundPredicate(orPredicateWithSubpredicates: [
                    NSCompoundPredicate(andPredicateWithSubpredicates: [
                        NSPredicate(format: "creationDate >= %@", start as NSDate),
                        NSPredicate(format: "creationDate < %@", end as NSDate)
                    ]),
                    NSCompoundPredicate(andPredicateWithSubpredicates: [
                        NSPredicate(format: "modificationDate >= %@", start as NSDate),
                        NSPredicate(format: "modificationDate < %@", end as NSDate)
                    ])
                ])
            )
        case .weekend:
            let interval = Calendar.current.dateInterval(of: .weekOfYear, for: Date())
            let start = interval?.start ?? Calendar.current.startOfDay(for: Date())
            let end = interval?.end ?? Date()
            predicates.append(NSPredicate(format: "creationDate >= %@ AND creationDate <= %@", start as NSDate, end as NSDate))
        case .trip, .birthday, .friends, .family, .pets, .specialMoments:
            break
        }
    }

    private static func originalResource(for asset: PHAsset) -> PHAssetResource? {
        let resources = PHAssetResource.assetResources(for: asset)
        if asset.mediaType == .video {
            return resources.first(where: { $0.type == .fullSizeVideo })
                ?? resources.first(where: { $0.type == .video })
        }
        return resources.first(where: { $0.type == .fullSizePhoto })
            ?? resources.first(where: { $0.type == .photo })
    }

    private func fetchPHAssets(for assets: [MediaAsset]) -> [PHAsset] {
        let identifiers = assets.compactMap(\.localIdentifier)
        guard !identifiers.isEmpty else { return [] }
        let result = PHAsset.fetchAssets(withLocalIdentifiers: identifiers, options: nil)
        var phAssets: [PHAsset] = []
        phAssets.reserveCapacity(result.count)
        result.enumerateObjects { asset, _, _ in
            phAssets.append(asset)
        }
        return phAssets
    }

    private func thumbnailOptions() -> PHImageRequestOptions {
        let options = PHImageRequestOptions()
        options.deliveryMode = .highQualityFormat
        options.resizeMode = .fast
        options.isNetworkAccessAllowed = true
        options.version = .current
        return options
    }

    private static func makeMediaAsset(from asset: PHAsset) -> MediaAsset {
        MediaAsset(
            id: asset.localIdentifier,
            localIdentifier: asset.localIdentifier,
            kind: asset.mediaType == .video
                ? .video
                : asset.mediaSubtypes.contains(.photoLive) ? .livePhoto : .image,
            creationDate: asset.creationDate,
            duration: asset.duration,
            pixelWidth: asset.pixelWidth,
            pixelHeight: asset.pixelHeight
        )
    }

    private static func authorization(for status: PHAuthorizationStatus) -> PhotoLibraryAuthorization {
        switch status {
        case .notDetermined: .notDetermined
        case .restricted: .restricted
        case .denied: .denied
        case .authorized: .authorized
        case .limited: .limited
        @unknown default: .denied
        }
    }
}

private final class ImageRequestIDBox: @unchecked Sendable {
    private let lock = NSLock()
    private var requestID: PHImageRequestID?
    private var isCancelled = false
    private var didResume = false

    func resume(
        _ result: Result<Data, Error>,
        continuation: CheckedContinuation<Data, Error>
    ) {
        lock.lock()
        guard !didResume else {
            lock.unlock()
            return
        }
        didResume = true
        lock.unlock()

        continuation.resume(with: result)
    }

    func set(_ requestID: PHImageRequestID, imageManager: PHImageManager) {
        lock.lock()
        self.requestID = requestID
        let shouldCancel = isCancelled
        lock.unlock()
        if shouldCancel {
            imageManager.cancelImageRequest(requestID)
        }
    }

    func cancel(imageManager: PHImageManager) {
        lock.lock()
        isCancelled = true
        let requestID = self.requestID
        lock.unlock()
        if let requestID {
            imageManager.cancelImageRequest(requestID)
        }
    }
}

private final class AssetResourceFileRequestState: @unchecked Sendable {
    private let lock = NSLock()
    private let fileHandle: FileHandle
    private var continuation: CheckedContinuation<Void, Error>?
    private var requestID: PHAssetResourceDataRequestID?
    private var isResolved = false
    private var isCancelled = false

    init(fileURL: URL) throws {
        self.fileHandle = try FileHandle(forWritingTo: fileURL)
    }

    func install(_ continuation: CheckedContinuation<Void, Error>) {
        lock.lock()
        self.continuation = continuation
        lock.unlock()
    }

    func setRequestID(_ requestID: PHAssetResourceDataRequestID, manager: PHAssetResourceManager) {
        lock.lock()
        self.requestID = requestID
        let shouldCancel = isCancelled
        lock.unlock()
        if shouldCancel {
            manager.cancelDataRequest(requestID)
        }
    }

    func append(_ data: Data) {
        lock.lock()
        guard !isResolved else {
            lock.unlock()
            return
        }
        do {
            try fileHandle.write(contentsOf: data)
            lock.unlock()
        } catch {
            lock.unlock()
            finish(error: error)
        }
    }

    func finish(error: Error?) {
        lock.lock()
        guard !isResolved else {
            lock.unlock()
            return
        }
        isResolved = true
        let continuation = self.continuation
        self.continuation = nil
        try? fileHandle.close()
        lock.unlock()

        if let error {
            continuation?.resume(throwing: error)
        } else {
            continuation?.resume()
        }
    }

    func cancel(manager: PHAssetResourceManager) {
        lock.lock()
        isCancelled = true
        let requestID = self.requestID
        lock.unlock()
        if let requestID {
            manager.cancelDataRequest(requestID)
        }
        finish(error: CancellationError())
    }
}

final class MockPhotoLibraryService: PhotoLibraryService, MediaSelectionService, @unchecked Sendable {
    private let assets: [MediaAsset]

    init(assets: [MediaAsset] = MockPhotoLibraryService.defaultAssets) {
        self.assets = assets
    }

    func authorizationStatus() async -> PhotoLibraryAuthorization { .authorized }
    func requestAuthorization() async -> PhotoLibraryAuthorization { .authorized }

    func fetchAssets(for context: MomentContext, filter: PhotoLibraryMediaFilter) async throws -> [MediaAsset] {
        try Task.checkCancellation()
        switch filter {
        case .all:
            return assets
        case .photos:
            return assets.filter { !$0.isVideo }
        case .videos:
            return assets.filter(\.isVideo)
        }
    }

    func resolveAssets(localIdentifiers: [String]) async throws -> [MediaAsset] {
        try Task.checkCancellation()
        let requested = Set(localIdentifiers)
        return assets.filter { asset in
            guard let localIdentifier = asset.localIdentifier else { return requested.contains(asset.id) }
            return requested.contains(localIdentifier) || requested.contains(asset.id)
        }
    }

    func fetchAssets(
        in scope: MediaSelectionScope,
        filter: PhotoLibraryMediaFilter
    ) async throws -> [MediaAsset] {
        try Task.checkCancellation()
        let scopedAssets: [MediaAsset]
        switch scope.kind {
        case .all, .context:
            scopedAssets = assets
        case .album:
            scopedAssets = []
        case .timePeriod:
            scopedAssets = assets.filter { asset in
                guard let creationDate = asset.creationDate else { return false }
                if let startDate = scope.startDate, creationDate < startDate { return false }
                if let endDate = scope.endDate, creationDate > endDate { return false }
                return true
            }
        }
        switch filter {
        case .all:
            return scopedAssets
        case .photos:
            return scopedAssets.filter { !$0.isVideo }
        case .videos:
            return scopedAssets.filter(\.isVideo)
        }
    }

    func fetchAlbums() async throws -> [MediaAlbum] {
        try Task.checkCancellation()
        return []
    }

    func requestThumbnail(for asset: MediaAsset, targetSize: CGSize) async throws -> Data {
        try Task.checkCancellation()
        let renderer = UIGraphicsImageRenderer(size: targetSize)
        return renderer.jpegData(withCompressionQuality: 0.82) { context in
            UIColor(red: 0.55, green: 0.38, blue: 0.28, alpha: 1).setFill()
            context.fill(CGRect(origin: .zero, size: targetSize))
        }
    }

    func startCachingThumbnails(for assets: [MediaAsset], targetSize: CGSize) {}

    func stopCachingThumbnails(for assets: [MediaAsset], targetSize: CGSize) {}

    private static let defaultAssets: [MediaAsset] = (0..<8).map { index in
        MediaAsset(
            id: "preview-asset-\(index)",
            localIdentifier: nil,
            kind: index.isMultiple(of: 3) ? .video : .image,
            creationDate: Calendar.current.date(byAdding: .day, value: -index, to: Date()),
            duration: index.isMultiple(of: 3) ? 8 + Double(index) : 0,
            pixelWidth: 1200,
            pixelHeight: 1600
        )
    }
}
