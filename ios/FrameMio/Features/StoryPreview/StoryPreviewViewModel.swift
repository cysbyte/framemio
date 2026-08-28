import Combine
import Foundation
import Photos
import PhotosUI
import SwiftUI
import UIKit


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

    func importMediaForEdit(_ items: [PhotosPickerItem]) async throws -> [ImportedStoryMedia] {
        let identifiers = items.compactMap(\.itemIdentifier)
        guard !identifiers.isEmpty else { return [] }
        let assets = try await photoLibrary.resolveAssets(localIdentifiers: identifiers)
        guard !assets.isEmpty else {
            throw AppError.message("We couldn't read those Photos items. Try selecting them again.")
        }
        let selection = MediaSelection(explicitlySelectedAssetIDs: assets.map(\.id))
        let uploads = try await mediaUploadService.upload(
            selection: selection,
            assets: assets,
            options: MediaUploadOptions(),
            progress: { _ in }
        )
        var uploadsByAssetID: [String: MediaUpload] = [:]
        for upload in uploads where upload.status == .completed && upload.mediaID != nil {
            uploadsByAssetID[upload.assetID] = upload
        }
        var imported: [ImportedStoryMedia] = []
        for asset in assets {
            guard let upload = uploadsByAssetID[asset.id], let mediaID = upload.mediaID else { continue }
            let storyAsset = MediaAsset(
                id: mediaID,
                localIdentifier: asset.localIdentifier,
                kind: asset.kind,
                creationDate: asset.creationDate,
                duration: asset.duration,
                pixelWidth: asset.pixelWidth,
                pixelHeight: asset.pixelHeight
            )
            momentAssets[mediaID] = storyAsset
            let analysis: MediaAnalysis?
            if asset.isVideo {
                analysis = nil
            } else {
                let localAnalysis = try await analysisService.analyze(asset: asset)
                analysis = MediaAnalysis(
                    assetID: mediaID,
                    labels: localAnalysis.labels,
                    peopleCount: localAnalysis.peopleCount,
                    confidence: localAnalysis.confidence
                )
            }
            imported.append(ImportedStoryMedia(mediaID: mediaID, sourceAsset: storyAsset, analysis: analysis))
        }
        return imported
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
        guard let storyID, activeAction == nil, story?.status.shouldPoll != true else { return }
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
