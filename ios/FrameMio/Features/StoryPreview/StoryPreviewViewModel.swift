import AVFoundation
import Combine
import CoreTransferable
import Foundation
import Photos
import PhotosUI
import SwiftUI
import UIKit
import UniformTypeIdentifiers

private func storyPreviewDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][StoryPreview] \(message())")
#endif
}

typealias StoryImportProgressHandler = @MainActor @Sendable (Double?, String) -> Void


private struct StagedStoryImport: Sendable {
    let asset: MediaAsset
    let file: OriginalMediaFile?
    let thumbnail: UIImage?
    let fallbackAnalysis: MediaAnalysis?
}

private struct PickedStoryMovie: Transferable, Sendable {
    let url: URL

    static var transferRepresentation: some TransferRepresentation {
        FileRepresentation(contentType: .movie) { movie in
            SentTransferredFile(movie.url)
        } importing: { received in
            PickedStoryMovie(url: try PickerImportStager.copyTransferredFile(received.file, fallbackExtension: "mov"))
        }
    }
}

private enum PickerImportStager {
    static func directory() throws -> URL {
        let fileManager = FileManager.default
        let directory = (fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? fileManager.temporaryDirectory)
            .appendingPathComponent("FrameMio", isDirectory: true)
            .appendingPathComponent("PickerImports", isDirectory: true)
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
        return directory
    }

    static func writeData(_ data: Data, fileExtension: String) throws -> URL {
        let url = try directory().appendingPathComponent("picker-\(UUID().uuidString).\(fileExtension)")
        try data.write(to: url, options: .atomic)
        return url
    }

    static func copyTransferredFile(_ sourceURL: URL, fallbackExtension: String) throws -> URL {
        let fileExtension = sourceURL.pathExtension.isEmpty ? fallbackExtension : sourceURL.pathExtension
        let destinationURL = try directory().appendingPathComponent("picker-\(UUID().uuidString).\(fileExtension)")
        try FileManager.default.copyItem(at: sourceURL, to: destinationURL)
        return destinationURL
    }
}


struct ImportedStoryMedia: Hashable, Sendable {
    let mediaID: String
    let sourceAsset: MediaAsset
    let analysis: MediaAnalysis?
}

@MainActor
final class StoryPreviewViewModel: ObservableObject {
    @Published private(set) var story: Story?
    @Published private(set) var isLoading = false
    @Published private(set) var activeAction: StoryPreviewAction?
    @Published var errorMessage: String?
    @Published var confirmationMessage: String?
    @Published var didDeleteStory = false
    @Published var isPlayingVideo = false
    @Published private(set) var momentAssets: [String: MediaAsset] = [:]
    @Published private(set) var momentThumbnails: [String: UIImage] = [:]

    private var storyID: String?
    private let launchContext: MomentContext?
    private let launchAssets: [MediaAsset]
    private let launchID: UUID?
    private let storyLaunchCoordinator: StoryLaunchCoordinator?
    private let storyService: StoryService
    private let photoLibrary: PhotoLibraryService
    private let mediaUploadStore: MediaUploadStore
    private let mediaUploadService: MediaUploadService
    private let analysisService: MediaAnalysisService
    private var loadTask: Task<Void, Never>?
    private var actionTask: Task<Void, Never>?
    private var thumbnailTasks: [String: Task<Void, Never>] = [:]
    private var launchCancellable: AnyCancellable?

    init(
        story: Story,
        storyService: StoryService,
        photoLibrary: PhotoLibraryService,
        mediaUploadStore: MediaUploadStore,
        mediaUploadService: MediaUploadService,
        analysisService: MediaAnalysisService
    ) {
        self.story = story
        storyID = story.id
        launchContext = nil
        launchAssets = []
        launchID = nil
        storyLaunchCoordinator = nil
        self.storyService = storyService
        self.photoLibrary = photoLibrary
        self.mediaUploadStore = mediaUploadStore
        self.mediaUploadService = mediaUploadService
        self.analysisService = analysisService
    }

    init(
        storyID: String,
        storyService: StoryService,
        photoLibrary: PhotoLibraryService,
        mediaUploadStore: MediaUploadStore,
        mediaUploadService: MediaUploadService,
        analysisService: MediaAnalysisService
    ) {
        self.storyID = storyID
        launchContext = nil
        launchAssets = []
        launchID = nil
        storyLaunchCoordinator = nil
        self.storyService = storyService
        self.photoLibrary = photoLibrary
        self.mediaUploadStore = mediaUploadStore
        self.mediaUploadService = mediaUploadService
        self.analysisService = analysisService
    }

    init(
        context: MomentContext,
        assets: [MediaAsset],
        launchID: UUID,
        storyLaunchCoordinator: StoryLaunchCoordinator,
        storyService: StoryService,
        photoLibrary: PhotoLibraryService,
        mediaUploadStore: MediaUploadStore,
        mediaUploadService: MediaUploadService,
        analysisService: MediaAnalysisService
    ) {
        storyID = nil
        launchContext = context
        launchAssets = assets
        self.launchID = launchID
        self.storyLaunchCoordinator = storyLaunchCoordinator
        self.storyService = storyService
        self.photoLibrary = photoLibrary
        self.mediaUploadStore = mediaUploadStore
        self.mediaUploadService = mediaUploadService
        self.analysisService = analysisService
        launchCancellable = storyLaunchCoordinator.objectWillChange.sink { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.objectWillChange.send()
            }
        }
    }

    var isPerformingAction: Bool {
        activeAction != nil
    }

    var shouldShowInitialProgress: Bool {
        story == nil && errorMessage == nil && (isLoading || storyLaunchCoordinator != nil)
    }

    /// The film can be watched: the render finished and the server handed back a
    /// playable URL. A ready story without a URL is not playable, so the hero
    /// must not offer a play affordance that would do nothing.
    var playableVideoURL: URL? {
        guard let story, story.status == .ready else { return nil }
        return story.videoURL
    }

    var previewVideoURL: URL? {
        story?.videoURL
    }

    var canPlayVideo: Bool {
        playableVideoURL != nil
    }

    func playVideo() {
        guard canPlayVideo else { return }
        isPlayingVideo = true
    }

    func momentAsset(for storyAssetID: String) -> MediaAsset? {
        momentAssets[storyAssetID] ?? launchAssets.first { $0.id == storyAssetID }
    }

    func image(forStoryAssetID storyAssetID: String) -> Image? {
        guard let image = momentThumbnails[storyAssetID] else { return nil }
        return Image(uiImage: image)
    }

    func loadThumbnail(forStoryAssetID storyAssetID: String, targetSize: CGSize) {
        guard momentThumbnails[storyAssetID] == nil, thumbnailTasks[storyAssetID] == nil else { return }

        thumbnailTasks[storyAssetID] = Task { [weak self, photoLibrary] in
            guard let self else { return }
            defer { thumbnailTasks[storyAssetID] = nil }
            do {
                if let asset = await resolveMomentAsset(forStoryAssetID: storyAssetID) {
                    let data = try await photoLibrary.requestThumbnail(for: asset, targetSize: targetSize)
                    try Task.checkCancellation()
                    if let image = UIImage(data: data) {
                        momentThumbnails[storyAssetID] = image
                        return
                    }
                }
                guard let remoteURL = remoteThumbnailURL(forStoryAssetID: storyAssetID) else { return }
                let data = try await Self.downloadThumbnail(from: remoteURL)
                try Task.checkCancellation()
                if let image = UIImage(data: data) {
                    momentThumbnails[storyAssetID] = image
                }
            } catch is CancellationError {
            } catch {
            }
        }
    }

    func cancelThumbnail(forStoryAssetID storyAssetID: String) {
        thumbnailTasks[storyAssetID]?.cancel()
        thumbnailTasks[storyAssetID] = nil
    }

    private func remoteThumbnailURL(forStoryAssetID storyAssetID: String) -> URL? {
        let allMoments = (story?.moments ?? []) + (story?.availableMoments ?? [])
        return allMoments.first { moment in
            moment.id == storyAssetID || moment.mediaID == storyAssetID
        }?.thumbnailURL
    }

    var progressTitle: String {
        if let storyLaunchCoordinator, story == nil {
            return storyLaunchCoordinator.phaseTitle
        }

        return switch story?.status {
        case .queued:
            "Preparing your memories..."
        case .analyzing:
            "Finding meaningful moments..."
        case .rendering:
            "Creating your film..."
        case .ready:
            "Ready to watch."
        case .failed:
            "This story needs another try."
        case nil:
            "Preparing your memories..."
        }
    }

    var progressValue: Double {
        if let storyLaunchCoordinator, story == nil {
            return storyLaunchCoordinator.progress
        }

        return switch story?.status {
        case .queued:
            0.18
        case .analyzing:
            0.58
        case .rendering:
            0.86
        case .ready:
            1
        case .failed:
            0
        case nil:
            0.08
        }
    }

    func load() {
        loadTask?.cancel()
        loadTask = Task { [weak self] in
            await self?.loadStoryOrLaunch()
        }
    }

    private func loadStoryOrLaunch() async {
        if let launchContext, let launchID, let storyLaunchCoordinator {
            storyLaunchCoordinator.start(context: launchContext, assets: launchAssets, launchID: launchID)
            await waitForLaunchedStoryID(from: storyLaunchCoordinator)
        }

        guard storyID != nil else { return }
        await pollStory()
    }

    private func waitForLaunchedStoryID(from storyLaunchCoordinator: StoryLaunchCoordinator) async {
        isLoading = true
        defer { if storyID == nil { isLoading = false } }

        while !Task.isCancelled {
            if let launchedStoryID = storyLaunchCoordinator.storyID {
                storyID = launchedStoryID
                return
            }

            if let launchErrorMessage = storyLaunchCoordinator.errorMessage {
                errorMessage = launchErrorMessage
                return
            }

            do {
                try await Task.sleep(nanoseconds: 300_000_000)
            } catch {
                return
            }
        }
    }

    private func pollStory() async {
        guard let storyID else { return }

        if story == nil {
            isLoading = true
        }
        defer { isLoading = false }

        while !Task.isCancelled {
            do {
                let latest = try await storyService.story(id: storyID)
                story = latest
                cacheLaunchAssets(for: latest)
                errorMessage = nil
                isLoading = false

                guard latest.status.shouldPoll else { return }
                try await Task.sleep(nanoseconds: 5_000_000_000)
            } catch is CancellationError {
                return
            } catch {
                let appError = AppError.from(error)
                guard story?.status.shouldPoll == true, appError.isTransientPollingError else {
                    errorMessage = appError.localizedDescription
                    return
                }
                isLoading = false
                do {
                    try await Task.sleep(nanoseconds: 5_000_000_000)
                } catch {
                    return
                }
            }
        }
    }

    func retryProcessing() {
        guard let storyID else { return }
        perform(.retry) { [storyService] in
            try await storyService.retryProcessing(id: storyID)
        }
    }

    func regenerateStory() {
        guard let storyID else { return }
        perform(.regenerate) { [storyService] in
            try await storyService.regenerateStory(id: storyID)
        }
    }

    func renderAgain() {
        guard let storyID else { return }
        perform(.renderAgain) { [storyService] in
            try await storyService.renderAgain(id: storyID)
        }
    }

    func importMediaForEdit(
        _ items: [PhotosPickerItem],
        progress: StoryImportProgressHandler? = nil
    ) async throws -> [ImportedStoryMedia] {
        let contentTypeSummary = items
            .map { $0.supportedContentTypes.map(\.identifier).joined(separator: "|") }
            .joined(separator: ",")
        progress?(0.04, "Reading your selection…")
        storyPreviewDebugLog("edit import start picker_items=\(items.count) content_types=\(contentTypeSummary)")
        do {
            let assets = try await assetsForPickedItems(items)
            progress?(0.16, "Preparing media…")
            storyPreviewDebugLog("edit import assets ready count=\(assets.count) photos=\(assets.filter { !$0.asset.isVideo }.count) videos=\(assets.filter(\.asset.isVideo).count) staged=\(assets.filter { $0.file != nil }.count)")
            guard !assets.isEmpty else {
                storyPreviewDebugLog("edit import stopped no_assets")
                return []
            }

            for asset in assets {
                if let file = asset.file {
                    progress?(0.22, "Preparing upload…")
                    try await mediaUploadStore.save(makeStagedUploadRecord(asset: asset.asset, file: file))
                    storyPreviewDebugLog("edit import staged picker asset=\(asset.asset.id) file=\(file.fileName) bytes=\(file.fileSizeBytes) content_type=\(file.contentType)")
                }
            }

            let sourceAssets = assets.map(\.asset)
            let selection = MediaSelection(explicitlySelectedAssetIDs: sourceAssets.map(\.id))
            progress?(0.28, "Uploading to FrameMio…")
            storyPreviewDebugLog("edit import upload start eligible_assets=\(selection.uploadEligibleAssetIDs.count)")
            let uploads = try await mediaUploadService.upload(
                selection: selection,
                assets: sourceAssets,
                options: MediaUploadOptions(),
                progress: { uploadProgress in
                    if let progress {
                        let value = Self.importProgressValue(for: uploadProgress)
                        Task { @MainActor in
                            progress(value, "Uploading to FrameMio…")
                        }
                    }
                    storyPreviewDebugLog("edit import upload progress upload=\(uploadProgress.uploadID) asset=\(uploadProgress.assetID) bytes=\(uploadProgress.uploadedBytes)/\(uploadProgress.totalBytes) completed=\(uploadProgress.completedUploads)/\(uploadProgress.totalUploads)")
                }
            )
            progress?(0.86, "Preparing new moments…")
            storyPreviewDebugLog("edit import upload finished uploads=\(uploads.count) completed=\(uploads.filter { $0.status == .completed }.count) with_media_id=\(uploads.filter { $0.mediaID != nil }.count)")

            var uploadsByAssetID: [String: MediaUpload] = [:]
            for upload in uploads where upload.status == .completed && upload.mediaID != nil {
                uploadsByAssetID[upload.assetID] = upload
            }
            var imported: [ImportedStoryMedia] = []
            for asset in assets {
                guard let upload = uploadsByAssetID[asset.asset.id], let mediaID = upload.mediaID else {
                    storyPreviewDebugLog("edit import skipping asset=\(asset.asset.id) missing_completed_upload")
                    continue
                }
                let storyAsset = MediaAsset(
                    id: mediaID,
                    localIdentifier: asset.asset.localIdentifier,
                    kind: asset.asset.kind,
                    creationDate: asset.asset.creationDate,
                    duration: asset.asset.duration,
                    pixelWidth: asset.asset.pixelWidth,
                    pixelHeight: asset.asset.pixelHeight
                )
                momentAssets[mediaID] = storyAsset
                if let thumbnail = asset.thumbnail {
                    momentThumbnails[mediaID] = thumbnail
                }

                let analysis: MediaAnalysis?
                if let fallbackAnalysis = asset.fallbackAnalysis {
                    analysis = MediaAnalysis(
                        assetID: mediaID,
                        labels: fallbackAnalysis.labels,
                        peopleCount: fallbackAnalysis.peopleCount,
                        confidence: fallbackAnalysis.confidence
                    )
                    storyPreviewDebugLog("edit import fallback analysis ready media=\(mediaID) labels=\(fallbackAnalysis.labels.count)")
                } else if asset.asset.isVideo {
                    storyPreviewDebugLog("edit import media ready media=\(mediaID) kind=video analysis=deferred_to_backend")
                    analysis = nil
                } else {
                    progress?(0.92, "Understanding new photos…")
                    storyPreviewDebugLog("edit import analyzing photo asset=\(asset.asset.id) media=\(mediaID)")
                    let localAnalysis = try await analysisService.analyze(asset: asset.asset)
                    analysis = MediaAnalysis(
                        assetID: mediaID,
                        labels: localAnalysis.labels,
                        peopleCount: localAnalysis.peopleCount,
                        confidence: localAnalysis.confidence
                    )
                    storyPreviewDebugLog("edit import photo analysis ready media=\(mediaID) labels=\(localAnalysis.labels.count) people=\(localAnalysis.peopleCount)")
                }
                imported.append(ImportedStoryMedia(mediaID: mediaID, sourceAsset: storyAsset, analysis: analysis))
            }
            progress?(1.0, "Added to your story")
            storyPreviewDebugLog("edit import complete imported=\(imported.count)")
            return imported
        } catch is CancellationError {
            storyPreviewDebugLog("edit import cancelled")
            throw CancellationError()
        } catch {
            storyPreviewDebugLog("edit import failed error=\(error.localizedDescription)")
            throw error
        }
    }

    nonisolated private static func importProgressValue(for progress: MediaUploadProgress) -> Double {
        guard progress.totalUploads > 0 else { return 0.42 }
        let currentUploadFraction: Double
        if progress.totalBytes > 0 {
            currentUploadFraction = min(1, max(0, Double(progress.uploadedBytes) / Double(progress.totalBytes)))
        } else {
            currentUploadFraction = 0
        }
        let completedUploads = max(0, progress.completedUploads - (currentUploadFraction >= 1 ? 1 : 0))
        let uploadFraction = min(1, max(0, (Double(completedUploads) + currentUploadFraction) / Double(progress.totalUploads)))
        return 0.28 + uploadFraction * 0.56
    }

    private func assetsForPickedItems(_ items: [PhotosPickerItem]) async throws -> [StagedStoryImport] {
        let identifiers = items.compactMap(\.itemIdentifier)
        storyPreviewDebugLog("edit import identifiers item_count=\(items.count) identifier_count=\(identifiers.count) nil_identifiers=\(items.count - identifiers.count)")
        var resolvedByIdentifier: [String: MediaAsset] = [:]
        if !identifiers.isEmpty {
            let resolved = try await photoLibrary.resolveAssets(localIdentifiers: identifiers)
            storyPreviewDebugLog("edit import resolved identifiers=\(identifiers.count) assets=\(resolved.count) photos=\(resolved.filter { !$0.isVideo }.count) videos=\(resolved.filter(\.isVideo).count)")
            resolvedByIdentifier = Dictionary(uniqueKeysWithValues: resolved.compactMap { asset in
                guard let localIdentifier = asset.localIdentifier else { return nil }
                return (localIdentifier, asset)
            })
            if resolved.count != identifiers.count {
                storyPreviewDebugLog("edit import partial identifier resolution resolved=\(resolved.count) expected=\(identifiers.count)")
            }
        } else {
            storyPreviewDebugLog("edit import using transferable loading no_photo_identifiers")
        }

        var staged: [StagedStoryImport] = []
        staged.reserveCapacity(items.count)
        for item in items {
            try Task.checkCancellation()
            if let identifier = item.itemIdentifier, let asset = resolvedByIdentifier[identifier] {
                let created = asset.creationDate?.description ?? "nil"
                storyPreviewDebugLog("edit import using resolved photo asset=\(asset.id) kind=\(asset.kind.rawValue) created=\(created)")
                staged.append(StagedStoryImport(asset: asset, file: nil, thumbnail: nil, fallbackAnalysis: nil))
                continue
            }
            staged.append(try await stagedImport(from: item))
        }
        return staged
    }

    private func stagedImport(from item: PhotosPickerItem) async throws -> StagedStoryImport {
        if item.supportedContentTypes.contains(where: { $0.conforms(to: .movie) || $0.conforms(to: .video) }) {
            return try await stagedVideoImport(from: item)
        }
        if item.supportedContentTypes.contains(where: { $0.conforms(to: .image) }) {
            return try await stagedPhotoImport(from: item)
        }
        throw AppError.unsupportedMedia
    }

    private func stagedPhotoImport(from item: PhotosPickerItem) async throws -> StagedStoryImport {
        guard let data = try await item.loadTransferable(type: Data.self),
              let image = UIImage(data: data),
              let jpegData = image.jpegData(compressionQuality: 0.92) else {
            throw AppError.message("We couldn't read that photo. Try selecting it again.")
        }
        let fileURL = try PickerImportStager.writeData(jpegData, fileExtension: "jpg")
        let file = OriginalMediaFile(
            url: fileURL,
            fileName: fileURL.lastPathComponent,
            contentType: "image/jpeg",
            fileSizeBytes: Int64(jpegData.count)
        )
        let assetID = "picker-\(UUID().uuidString)"
        let asset = MediaAsset(
            id: assetID,
            localIdentifier: nil,
            kind: .image,
            creationDate: nil,
            duration: 0,
            pixelWidth: Int(image.size.width.rounded()),
            pixelHeight: Int(image.size.height.rounded())
        )
        let analysis = MediaAnalysis(assetID: assetID, labels: ["photo", "memory"], peopleCount: 0, confidence: 0.5)
        storyPreviewDebugLog("edit import staged photo asset=\(asset.id) pixels=\(asset.pixelWidth)x\(asset.pixelHeight) bytes=\(file.fileSizeBytes)")
        return StagedStoryImport(asset: asset, file: file, thumbnail: image, fallbackAnalysis: analysis)
    }

    private func stagedVideoImport(from item: PhotosPickerItem) async throws -> StagedStoryImport {
        let url: URL
        if let movie = try await item.loadTransferable(type: PickedStoryMovie.self) {
            url = movie.url
        } else if let transferredURL = try await item.loadTransferable(type: URL.self) {
            url = try PickerImportStager.copyTransferredFile(transferredURL, fallbackExtension: "mov")
        } else {
            throw AppError.message("We couldn't read that video. Try selecting it again.")
        }

        let metadata = try await videoMetadata(for: url)
        let creationDate = try await videoCreationDate(for: url)
        let fileSizeBytes = Int64((try url.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0)
        guard fileSizeBytes > 0 else {
            throw AppError.unsupportedMedia
        }
        let contentType = UTType(filenameExtension: url.pathExtension)?.preferredMIMEType ?? "video/quicktime"
        let file = OriginalMediaFile(
            url: url,
            fileName: url.lastPathComponent,
            contentType: contentType,
            fileSizeBytes: fileSizeBytes
        )
        let asset = MediaAsset(
            id: "picker-\(UUID().uuidString)",
            localIdentifier: nil,
            kind: .video,
            creationDate: creationDate,
            duration: metadata.duration,
            pixelWidth: metadata.pixelWidth,
            pixelHeight: metadata.pixelHeight
        )
        let thumbnail = await videoThumbnail(for: url)
        storyPreviewDebugLog("edit import staged video asset=\(asset.id) duration=\(asset.duration) pixels=\(asset.pixelWidth)x\(asset.pixelHeight) bytes=\(file.fileSizeBytes) content_type=\(contentType)")
        return StagedStoryImport(asset: asset, file: file, thumbnail: thumbnail, fallbackAnalysis: nil)
    }

    private func videoMetadata(for url: URL) async throws -> VideoMetadata {
        let asset = AVURLAsset(url: url)
        let duration = try await asset.load(.duration)
        let videoTracks = try await asset.loadTracks(withMediaType: .video)
        guard let videoTrack = videoTracks.first else {
            throw AppError.unsupportedMedia
        }
        let naturalSize = try await videoTrack.load(.naturalSize)
        let preferredTransform = try await videoTrack.load(.preferredTransform)
        let displaySize = naturalSize.applying(preferredTransform)
        return VideoMetadata(
            assetID: nil,
            creationDate: nil,
            location: nil,
            duration: duration.seconds.isFinite ? max(0, duration.seconds) : 0,
            pixelWidth: Int(abs(displaySize.width).rounded()),
            pixelHeight: Int(abs(displaySize.height).rounded()),
            orientation: .from(pixelWidth: Int(abs(displaySize.width).rounded()), pixelHeight: Int(abs(displaySize.height).rounded())),
            framesPerSecond: nil,
            fileSizeBytes: nil,
            hasAudio: nil,
            fileType: nil,
            quality: nil
        )
    }

    private func videoCreationDate(for url: URL) async throws -> Date? {
        let asset = AVURLAsset(url: url)
        let metadataItem = try await asset.load(.creationDate)
        return try await metadataItem?.load(.dateValue)
    }

    private func videoThumbnail(for url: URL) async -> UIImage? {
        await Task.detached(priority: .userInitiated) {
            let asset = AVURLAsset(url: url)
            let generator = AVAssetImageGenerator(asset: asset)
            generator.appliesPreferredTrackTransform = true
            do {
                let cgImage = try generator.copyCGImage(at: CMTime(seconds: 0.2, preferredTimescale: 600), actualTime: nil)
                return UIImage(cgImage: cgImage)
            } catch {
                return nil
            }
        }.value
    }

    private func makeStagedUploadRecord(asset: MediaAsset, file: OriginalMediaFile) -> MediaUpload {
        let uploadID = MediaUpload.makeID(for: asset.id, optimization: .original)
        return MediaUpload(
            id: uploadID,
            assetID: asset.id,
            localIdentifier: nil,
            mediaID: nil,
            mediaType: asset.isVideo ? .video : .photo,
            fileName: file.fileName,
            contentType: file.contentType,
            fileSizeBytes: file.fileSizeBytes,
            duration: asset.isVideo ? asset.duration : nil,
            creationDate: asset.creationDate,
            pixelWidth: asset.pixelWidth,
            pixelHeight: asset.pixelHeight,
            orientation: .from(pixelWidth: asset.pixelWidth, pixelHeight: asset.pixelHeight),
            optimization: .original,
            status: .queued,
            uploadedBytes: 0,
            authorizationIdempotencyKey: MediaUpload.makeAuthorizationID(for: uploadID, fileSizeBytes: file.fileSizeBytes),
            session: nil,
            stagedFileURL: file.url,
            lastError: nil,
            updatedAt: Date()
        )
    }


    func saveStoryEdits(_ request: StoryEditRequest) {
        guard let storyID, request.hasChanges, story?.status.shouldPoll != true else { return }
        perform(.editStory) { [storyService] in
            try await storyService.saveStoryEdits(storyID: storyID, request: request)
        }
    }

    func saveVideoToPhotos() {
        guard let videoURL = playableVideoURL, activeAction == nil else { return }
        actionTask?.cancel()
        actionTask = Task { [weak self] in
            guard let self else { return }
            activeAction = .saveVideo
            errorMessage = nil
            confirmationMessage = nil
            defer { activeAction = nil }
            do {
                try await Self.withTimeout(seconds: 150) {
                    try await Self.saveVideoToPhotoLibrary(from: videoURL)
                }
                confirmationMessage = "Saved to Photos."
                FrameMioHaptics.success()
            } catch is CancellationError {
            } catch {
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    func replaceMoment(assetID: String) {
        guard let storyID else { return }
        perform(.replaceMoment) { [storyService] in
            let request = StoryEditRequest(
                removeMomentIDs: [],
                removeMediaIDs: [],
                addMediaIDs: [assetID],
                replace: [],
                style: nil,
                subtitlesEnabled: nil,
                subtitleStyle: nil,
                targetDuration: nil,
                instruction: "Use this moment in a new version."
            )
            return try await storyService.saveStoryEdits(storyID: storyID, request: request)
        }
    }

    func removeMoment(assetID: String) {
        guard let storyID else { return }
        perform(.removeMoment) { [storyService] in
            let request = StoryEditRequest(
                removeMomentIDs: [],
                removeMediaIDs: [assetID],
                addMediaIDs: [],
                replace: [],
                style: nil,
                subtitlesEnabled: nil,
                subtitleStyle: nil,
                targetDuration: nil,
                instruction: "Remove this moment from the story."
            )
            return try await storyService.saveStoryEdits(storyID: storyID, request: request)
        }
    }

    func deleteStory() {
        guard let storyID, activeAction == nil else { return }
        actionTask?.cancel()
        actionTask = Task { [weak self] in
            guard let self else { return }
            activeAction = .deleteStory
            errorMessage = nil
            defer { activeAction = nil }
            do {
                try await storyService.deleteStory(id: storyID)
                story = nil
                didDeleteStory = true
                FrameMioHaptics.success()
            } catch is CancellationError {
            } catch {
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    private func cacheLaunchAssets(for story: Story) {
        guard !launchAssets.isEmpty else { return }
        let storyMediaIDs = Set(story.assetIDs + (story.availableMoments ?? []).map(\.mediaID))
        for asset in launchAssets where storyMediaIDs.contains(asset.id) {
            momentAssets[asset.id] = asset
        }
    }

    private func resolveMomentAsset(forStoryAssetID storyAssetID: String) async -> MediaAsset? {
        if let asset = momentAsset(for: storyAssetID) {
            return asset
        }
        do {
            guard let upload = try await mediaUploadStore.allUploads().first(where: {
                $0.mediaID == storyAssetID || $0.assetID == storyAssetID
            }) else {
                return nil
            }
            let asset = MediaAsset(
                id: storyAssetID,
                localIdentifier: upload.localIdentifier,
                kind: mediaKind(for: upload.mediaType),
                creationDate: upload.creationDate,
                duration: upload.duration ?? 0,
                pixelWidth: upload.pixelWidth,
                pixelHeight: upload.pixelHeight
            )
            momentAssets[storyAssetID] = asset
            return asset
        } catch {
            return nil
        }
    }

    private func mediaKind(for uploadType: UploadMediaType) -> MediaKind {
        switch uploadType {
        case .photo:
            .image
        case .video:
            .video
        case .livePhoto:
            .livePhoto
        }
    }

    private nonisolated static func saveVideoToPhotoLibrary(from remoteURL: URL) async throws {
        let authorization = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
        guard authorization == .authorized || authorization == .limited else {
            throw AppError.message("Allow FrameMio to add videos to Photos to save your story.")
        }

        let localURL = try await downloadVideoForSaving(from: remoteURL)
        defer { try? FileManager.default.removeItem(at: localURL) }

        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            PHPhotoLibrary.shared().performChanges {
                PHAssetChangeRequest.creationRequestForAssetFromVideo(atFileURL: localURL)
            } completionHandler: { success, error in
                if let error {
                    continuation.resume(throwing: error)
                } else if success {
                    continuation.resume(returning: ())
                } else {
                    continuation.resume(throwing: AppError.persistenceFailure)
                }
            }
        }
    }

    private nonisolated static func downloadThumbnail(from remoteURL: URL) async throws -> Data {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.requestCachePolicy = .returnCacheDataElseLoad
        configuration.waitsForConnectivity = true
        configuration.timeoutIntervalForRequest = 20
        configuration.timeoutIntervalForResource = 30
        let session = URLSession(configuration: configuration)
        defer { session.finishTasksAndInvalidate() }

        let (data, response) = try await session.data(from: remoteURL)
        if let httpResponse = response as? HTTPURLResponse, !(200...299).contains(httpResponse.statusCode) {
            throw AppError.server(statusCode: httpResponse.statusCode)
        }
        return data
    }

    private nonisolated static func downloadVideoForSaving(from remoteURL: URL) async throws -> URL {
        if remoteURL.isFileURL {
            return remoteURL
        }

        let configuration = URLSessionConfiguration.ephemeral
        configuration.requestCachePolicy = .reloadIgnoringLocalCacheData
        configuration.urlCache = nil
        configuration.waitsForConnectivity = true
        configuration.timeoutIntervalForRequest = 30
        configuration.timeoutIntervalForResource = 120
        let session = URLSession(configuration: configuration)
        defer { session.finishTasksAndInvalidate() }

        let (downloadedURL, response) = try await session.download(from: remoteURL)
        if let httpResponse = response as? HTTPURLResponse, !(200...299).contains(httpResponse.statusCode) {
            throw AppError.server(statusCode: httpResponse.statusCode)
        }

        let localURL = FileManager.default.temporaryDirectory
            .appendingPathComponent("framemio-story-\(UUID().uuidString)")
            .appendingPathExtension("mp4")
        try FileManager.default.moveItem(at: downloadedURL, to: localURL)
        return localURL
    }

    private nonisolated static func withTimeout<T: Sendable>(
        seconds: TimeInterval,
        operation: @escaping @Sendable () async throws -> T
    ) async throws -> T {
        try await withThrowingTaskGroup(of: T.self) { group in
            group.addTask {
                try await operation()
            }
            group.addTask {
                try await Task.sleep(nanoseconds: UInt64(seconds * 1_000_000_000))
                throw AppError.message("Saving took too long. Check your connection and try again.")
            }

            guard let result = try await group.next() else {
                throw AppError.cancelled
            }
            group.cancelAll()
            return result
        }
    }

    private func perform(_ action: StoryPreviewAction, operation: @escaping @Sendable () async throws -> Story) {
        guard activeAction == nil else { return }
        actionTask?.cancel()
        actionTask = Task { [weak self] in
            guard let self else { return }
            activeAction = action
            errorMessage = nil
            defer { activeAction = nil }
            do {
                let updatedStory = try await operation()
                story = updatedStory
                cacheLaunchAssets(for: updatedStory)
                if updatedStory.status.shouldPoll {
                    load()
                }
                FrameMioHaptics.success()
            } catch is CancellationError {
            } catch {
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    deinit {
        loadTask?.cancel()
        actionTask?.cancel()
        thumbnailTasks.values.forEach { $0.cancel() }
    }
}

enum StoryPreviewAction: Hashable, Sendable {
    case retry
    case regenerate
    case renderAgain
    case saveVideo
    case deleteStory
    case editStory
    case replaceMoment
    case removeMoment

    var title: String {
        switch self {
        case .retry:
            "Retrying"
        case .regenerate:
            "Regenerating"
        case .renderAgain:
            "Rendering again"
        case .saveVideo:
            "Saving"
        case .deleteStory:
            "Removing story"
        case .editStory:
            "Saving changes"
        case .replaceMoment:
            "Replacing moment"
        case .removeMoment:
            "Removing moment"
        }
    }
}

private extension AppError {
    var isTransientPollingError: Bool {
        switch self {
        case .networkUnavailable, .server(statusCode: 408), .server(statusCode: 429), .server(statusCode: 500...599):
            true
        default:
            false
        }
    }
}

private extension StoryStatus {
    var shouldPoll: Bool {
        switch self {
        case .queued, .analyzing, .rendering:
            true
        case .ready, .failed:
            false
        }
    }
}
