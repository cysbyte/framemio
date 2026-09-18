import AVFoundation
import Combine
import CoreTransferable
import CryptoKit
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
    @Published private(set) var saveVideoState: StoryPreviewSaveVideoState?
    @Published var didDeleteStory = false
    @Published var isPlayingVideo = false
    @Published private(set) var momentAssets: [String: MediaAsset] = [:]
    @Published private(set) var momentThumbnails: [String: UIImage] = [:]
    @Published private(set) var thumbnailRevision = 0
    @Published var insufficientCredits: CreditIssue?
    @Published private(set) var creditBalance: Int?

    private var storyID: String?
    private let launchContext: MomentContext?
    private let launchDraft: VideoProjectDraft?
    private let launchAssets: [MediaAsset]
    private let launchGenerationOptions: StoryGenerationOptions
    private let launchID: UUID?
    private let storyLaunchCoordinator: StoryLaunchCoordinator?
    private let storyService: StoryService
    private let creditService: CreditService?
    private let onCreditsUpdated: (() -> Void)?
    private let photoLibrary: PhotoLibraryService
    private let mediaUploadStore: MediaUploadStore
    private let mediaUploadService: MediaUploadService
    private let analysisService: MediaAnalysisService
    let promptTranscriptionService: PromptTranscriptionService?
    private var loadTask: Task<Void, Never>?
    private var actionTask: Task<Void, Never>?
    private var thumbnailTasks: [String: Task<Void, Never>] = [:]
    private var momentVideoURLs: [String: URL] = [:]
    private var launchCancellable: AnyCancellable?
    private var cachedUploadAssetSignature: String?

    init(
        story: Story,
        storyService: StoryService,
        photoLibrary: PhotoLibraryService,
        mediaUploadStore: MediaUploadStore,
        mediaUploadService: MediaUploadService,
        analysisService: MediaAnalysisService,
        creditService: CreditService? = nil,
        promptTranscriptionService: PromptTranscriptionService? = nil,
        onCreditsUpdated: (() -> Void)? = nil
    ) {
        self.story = story
        storyID = story.id
        launchContext = nil
        launchDraft = nil
        launchAssets = []
        launchGenerationOptions = .automatic
        launchID = nil
        storyLaunchCoordinator = nil
        self.storyService = storyService
        self.creditService = creditService
        self.onCreditsUpdated = onCreditsUpdated
        self.photoLibrary = photoLibrary
        self.mediaUploadStore = mediaUploadStore
        self.mediaUploadService = mediaUploadService
        self.analysisService = analysisService
        self.promptTranscriptionService = promptTranscriptionService
    }

    init(
        storyID: String,
        storyService: StoryService,
        photoLibrary: PhotoLibraryService,
        mediaUploadStore: MediaUploadStore,
        mediaUploadService: MediaUploadService,
        analysisService: MediaAnalysisService,
        creditService: CreditService? = nil,
        promptTranscriptionService: PromptTranscriptionService? = nil,
        onCreditsUpdated: (() -> Void)? = nil
    ) {
        self.storyID = storyID
        launchContext = nil
        launchDraft = nil
        launchAssets = []
        launchGenerationOptions = .automatic
        launchID = nil
        storyLaunchCoordinator = nil
        self.storyService = storyService
        self.creditService = creditService
        self.onCreditsUpdated = onCreditsUpdated
        self.photoLibrary = photoLibrary
        self.mediaUploadStore = mediaUploadStore
        self.mediaUploadService = mediaUploadService
        self.analysisService = analysisService
        self.promptTranscriptionService = promptTranscriptionService
    }

    init(
        context: MomentContext,
        assets: [MediaAsset],
        generationOptions: StoryGenerationOptions = .automatic,
        launchID: UUID,
        storyLaunchCoordinator: StoryLaunchCoordinator,
        storyService: StoryService,
        photoLibrary: PhotoLibraryService,
        mediaUploadStore: MediaUploadStore,
        mediaUploadService: MediaUploadService,
        analysisService: MediaAnalysisService,
        creditService: CreditService? = nil,
        promptTranscriptionService: PromptTranscriptionService? = nil,
        onCreditsUpdated: (() -> Void)? = nil
    ) {
        storyID = nil
        launchContext = context
        launchDraft = .story(context: context, options: generationOptions)
        launchAssets = assets
        launchGenerationOptions = generationOptions
        self.launchID = launchID
        self.storyLaunchCoordinator = storyLaunchCoordinator
        self.storyService = storyService
        self.creditService = creditService
        self.onCreditsUpdated = onCreditsUpdated
        self.photoLibrary = photoLibrary
        self.mediaUploadStore = mediaUploadStore
        self.mediaUploadService = mediaUploadService
        self.analysisService = analysisService
        self.promptTranscriptionService = promptTranscriptionService
        launchCancellable = storyLaunchCoordinator.objectWillChange.sink { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.objectWillChange.send()
            }
        }
    }

    init(
        draft: VideoProjectDraft,
        assets: [MediaAsset],
        launchID: UUID,
        storyLaunchCoordinator: StoryLaunchCoordinator,
        storyService: StoryService,
        photoLibrary: PhotoLibraryService,
        mediaUploadStore: MediaUploadStore,
        mediaUploadService: MediaUploadService,
        analysisService: MediaAnalysisService,
        creditService: CreditService? = nil,
        promptTranscriptionService: PromptTranscriptionService? = nil,
        onCreditsUpdated: (() -> Void)? = nil
    ) {
        storyID = nil
        launchContext = draft.context
        launchDraft = draft
        launchAssets = assets
        launchGenerationOptions = draft.storyOptions
        self.launchID = launchID
        self.storyLaunchCoordinator = storyLaunchCoordinator
        self.storyService = storyService
        self.creditService = creditService
        self.onCreditsUpdated = onCreditsUpdated
        self.photoLibrary = photoLibrary
        self.mediaUploadStore = mediaUploadStore
        self.mediaUploadService = mediaUploadService
        self.analysisService = analysisService
        self.promptTranscriptionService = promptTranscriptionService
        launchCancellable = storyLaunchCoordinator.objectWillChange.sink { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.objectWillChange.send()
            }
        }
    }

    var isPerformingAction: Bool {
        activeAction != nil
    }

    var projectType: VideoProjectType {
        story?.projectType ?? launchDraft?.type ?? .story
    }

    var editorConfiguration: VideoProjectEditorConfiguration {
        .forType(projectType)
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

    func refreshCreditBalance() {
        guard let creditService else { return }
        Task { [weak self] in
            guard let self else { return }
            do {
                let balance = try await creditService.balance()
                creditBalance = balance.displayCredits
            } catch {
            }
        }
    }

    func playVideo() {
        guard canPlayVideo else { return }
        isPlayingVideo = true
    }

    func momentAsset(for storyAssetID: String) -> MediaAsset? {
        momentAssets[storyAssetID] ?? launchAssets.first { $0.id == storyAssetID }
    }

    func image(forStoryAssetID storyAssetID: String) -> Image? {
        if let image = momentThumbnails[storyAssetID] {
            return Image(uiImage: image)
        }
        if let cachedImage = cachedThumbnailImage(forStoryAssetID: storyAssetID) {
            return Image(uiImage: cachedImage)
        }
        guard let sourceID = sourceIdentity(forStoryAssetID: storyAssetID) else {
            return nil
        }
        for aliasID in thumbnailAliasIDs(forSourceID: sourceID) where aliasID != storyAssetID {
            if let image = momentThumbnails[aliasID] {
                storyPreviewDebugLog("thumbnail alias hit requested=\(storyAssetID) alias=\(aliasID) source=\(sourceID)")
                return Image(uiImage: image)
            }
            if let cachedImage = cachedThumbnailImage(forStoryAssetID: aliasID) {
                storyPreviewDebugLog("thumbnail cache hit requested=\(storyAssetID) alias=\(aliasID) source=\(sourceID)")
                return Image(uiImage: cachedImage)
            }
        }
        return nil
    }

    func playableMomentVideoURL(forStoryAssetID storyAssetID: String) async -> URL? {
        if let cachedURL = momentVideoURLs[storyAssetID], FileManager.default.fileExists(atPath: cachedURL.path) {
            return cachedURL
        }

        if let sourceID = sourceIdentity(forStoryAssetID: storyAssetID),
           let cachedURL = momentVideoURLs[sourceID],
           FileManager.default.fileExists(atPath: cachedURL.path) {
            momentVideoURLs[storyAssetID] = cachedURL
            return cachedURL
        }

        if let uploadURL = await stagedUploadVideoURL(forStoryAssetID: storyAssetID) {
            momentVideoURLs[storyAssetID] = uploadURL
            return uploadURL
        }

        guard let asset = await resolveMomentAsset(forStoryAssetID: storyAssetID), asset.isVideo else {
            storyPreviewDebugLog("moment video unavailable no_video_asset media=\(storyAssetID)")
            return nil
        }

        if let originalMediaSource = photoLibrary as? OriginalMediaSource {
            do {
                let file = try await originalMediaSource.materializeOriginalMedia(
                    for: asset,
                    allowsNetworkAccess: true,
                    progress: nil
                )
                momentVideoURLs[storyAssetID] = file.url
                storyPreviewDebugLog("moment video materialized media=\(storyAssetID) url=\(file.url.lastPathComponent)")
                return file.url
            } catch {
                storyPreviewDebugLog("moment video materialize failed media=\(storyAssetID) error=\(error.localizedDescription)")
            }
        }

        return nil
    }

    private func stagedUploadVideoURL(forStoryAssetID storyAssetID: String) async -> URL? {
        do {
            let sourceID = sourceIdentity(forStoryAssetID: storyAssetID)
            guard let upload = try await mediaUploadStore.allUploads().first(where: {
                $0.mediaID == storyAssetID || $0.assetID == storyAssetID || $0.localIdentifier == storyAssetID || (sourceID != nil && $0.localIdentifier == sourceID)
            }),
                  upload.mediaType == .video,
                  let url = upload.stagedFileURL,
                  FileManager.default.fileExists(atPath: url.path) else {
                return nil
            }
            storyPreviewDebugLog("moment video using staged upload media=\(storyAssetID) upload=\(upload.id)")
            return url
        } catch {
            storyPreviewDebugLog("moment video staged lookup failed media=\(storyAssetID) error=\(error.localizedDescription)")
            return nil
        }
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
                        cacheMomentThumbnail(image, forStoryAssetID: storyAssetID, reason: "local_asset")
                        return
                    }
                }
                guard let remoteURL = remoteThumbnailURL(forStoryAssetID: storyAssetID) else { return }
                let data = try await Self.downloadThumbnail(from: remoteURL)
                try Task.checkCancellation()
                if let image = UIImage(data: data) {
                    cacheMomentThumbnail(image, forStoryAssetID: storyAssetID, reason: "remote_url")
                }
            } catch is CancellationError {
            } catch {
                storyPreviewDebugLog("thumbnail load failed media=\(storyAssetID) error=\(error.localizedDescription)")
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

    private func cacheMomentThumbnail(_ image: UIImage, forStoryAssetID storyAssetID: String, reason: String) {
        var changed = false
        guard let storyID else { return }
        for aliasID in thumbnailAliasIDs(forStoryAssetID: storyAssetID) {
            if momentThumbnails[aliasID] == nil {
                momentThumbnails[aliasID] = image
                changed = true
            }
            StoryThumbnailCache.save(image, storyID: storyID, storyAssetID: aliasID)
        }
        if changed {
            thumbnailRevision += 1
        }
        storyPreviewDebugLog("thumbnail cached media=\(storyAssetID) reason=\(reason) aliases=\(thumbnailAliasIDs(forStoryAssetID: storyAssetID).joined(separator: ",")) revision=\(thumbnailRevision)")
    }

    private func cachedThumbnailImage(forStoryAssetID storyAssetID: String) -> UIImage? {
        guard let storyID else { return nil }
        for aliasID in thumbnailAliasIDs(forStoryAssetID: storyAssetID) {
            if let image = StoryThumbnailCache.load(storyID: storyID, storyAssetID: aliasID) {
                return image
            }
        }
        return nil
    }

    private func thumbnailAliasIDs(forStoryAssetID storyAssetID: String) -> [String] {
        guard let sourceID = sourceIdentity(forStoryAssetID: storyAssetID) else {
            return [storyAssetID]
        }
        return thumbnailAliasIDs(forSourceID: sourceID, including: storyAssetID)
    }

    private func thumbnailAliasIDs(forSourceID sourceID: String, including storyAssetID: String? = nil) -> [String] {
        var ids: [String] = []
        var seen = Set<String>()
        func append(_ value: String?) {
            let trimmed = value?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            guard !trimmed.isEmpty, seen.insert(trimmed).inserted else { return }
            ids.append(trimmed)
        }
        append(storyAssetID)
        for (mediaID, asset) in momentAssets where asset.localIdentifier == sourceID {
            append(mediaID)
        }
        for moment in (story?.moments ?? []) + (story?.availableMoments ?? []) where moment.sourceAssetID?.trimmingCharacters(in: .whitespacesAndNewlines) == sourceID {
            append(moment.mediaID)
            append(moment.id)
        }
        return ids.isEmpty ? [sourceID] : ids
    }

    private func sourceIdentity(forStoryAssetID storyAssetID: String) -> String? {
        if let sourceID = momentAssets[storyAssetID]?.localIdentifier?.trimmingCharacters(in: .whitespacesAndNewlines), !sourceID.isEmpty {
            return sourceID
        }
        let allMoments = (story?.moments ?? []) + (story?.availableMoments ?? [])
        if let sourceID = allMoments.first(where: { $0.mediaID == storyAssetID || $0.id == storyAssetID })?.sourceAssetID?.trimmingCharacters(in: .whitespacesAndNewlines), !sourceID.isEmpty {
            return sourceID
        }
        return nil
    }


    var progressTitle: String {
        if let storyLaunchCoordinator, story == nil {
            return storyLaunchCoordinator.phaseTitle
        }

        if let title = nonEmptyProgressText(story?.progressTitle) {
            return title
        }

        switch projectType {
        case .story:
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
        case .promo:
            return switch story?.status {
            case .queued:
                "Preparing your promo..."
            case .analyzing:
                "Building promo scenes..."
            case .rendering:
                "Rendering your promo..."
            case .ready:
                "Promo ready."
            case .failed:
                "This promo needs another try."
            case nil:
                "Preparing your promo..."
            }
        case .reel:
            return switch story?.status {
            case .queued:
                "Preparing your reel..."
            case .analyzing:
                "Finding short-form clips..."
            case .rendering:
                "Rendering your reel..."
            case .ready:
                "Reel ready."
            case .failed:
                "This reel needs another try."
            case nil:
                "Preparing your reel..."
            }
        }
    }

    var progressDetail: String? {
        if let storyLaunchCoordinator, story == nil {
            return nonEmptyProgressText(storyLaunchCoordinator.phaseDetail)
        }
        return nonEmptyProgressText(story?.progressDetail)
    }

    var progressValue: Double {
        if let storyLaunchCoordinator, story == nil {
            return storyLaunchCoordinator.progress
        }

        if let progressValue = story?.progressValue, progressValue.isFinite {
            return min(max(progressValue, 0), 1)
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

    private func nonEmptyProgressText(_ text: String?) -> String? {
        guard let text else { return nil }
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    func load() {
        loadTask?.cancel()
        loadTask = Task { [weak self] in
            await self?.loadStoryOrLaunch()
        }
    }

    private func loadStoryOrLaunch() async {
        if let launchDraft, let launchID, let storyLaunchCoordinator {
            let instruction = launchGenerationOptions.instruction?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            storyPreviewDebugLog("launch_bridge_start launch_id=\(launchID) type=\(launchDraft.type.rawValue) assets=\(launchAssets.count) style=\(launchGenerationOptions.style ?? "automatic") scene_detail=\(launchGenerationOptions.sceneDetailStyle ?? "balanced") target_duration=\(launchGenerationOptions.targetDuration.map { String(Int($0.rounded())) } ?? "auto") prompt_present=\(!instruction.isEmpty) prompt_length=\(instruction.count) story_id=\(storyID ?? "nil")")
            storyLaunchCoordinator.start(draft: launchDraft, assets: launchAssets, launchID: launchID)
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
                storyPreviewDebugLog("launch_bridge_story_ready story_id=\(launchedStoryID)")
                storyID = launchedStoryID
                return
            }

            if let issue = storyLaunchCoordinator.insufficientCredits {
                storyPreviewDebugLog("launch_bridge_insufficient_credits required=\(issue.required) available=\(issue.available)")
                insufficientCredits = issue
                return
            }

            if let launchErrorMessage = storyLaunchCoordinator.errorMessage {
                storyPreviewDebugLog("launch_bridge_error message=\(launchErrorMessage)")
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
                try await cacheUploadAssets(for: latest)
                cacheLaunchAssets(for: latest)
                story = latest
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
        progress: StoryImportProgressHandler? = nil,
        excludedSourceIDs: Set<String> = []
    ) async throws -> [ImportedStoryMedia] {
        let contentTypeSummary = items
            .map { $0.supportedContentTypes.map(\.identifier).joined(separator: "|") }
            .joined(separator: ",")
        progress?(0.04, "Reading your selection…")
        let normalizedExcludedSourceIDs = Set(excludedSourceIDs.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty })
        let filteredItems = items.filter { item in
            let identifier = item.itemIdentifier?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            guard !identifier.isEmpty, normalizedExcludedSourceIDs.contains(identifier) else {
                return true
            }
            storyPreviewDebugLog("edit import skipped duplicate picker source=\(identifier)")
            return false
        }
        storyPreviewDebugLog("edit import start picker_items=\(items.count) filtered_items=\(filteredItems.count) excluded_sources=\(normalizedExcludedSourceIDs.count) content_types=\(contentTypeSummary)")
        guard !filteredItems.isEmpty else {
            progress?(1.0, "Already in your story")
            storyPreviewDebugLog("edit import stopped all_picker_items_already_in_story")
            return []
        }
        do {
            let resolvedAssets = try await assetsForPickedItems(filteredItems)
            progress?(0.16, "Preparing media…")
            let filteredAssets = resolvedAssets.filter { stagedImport in
                let sourceID = stagedImport.asset.localIdentifier?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
                guard !sourceID.isEmpty, normalizedExcludedSourceIDs.contains(sourceID) else {
                    return true
                }
                storyPreviewDebugLog("edit import skipped duplicate resolved source=\(sourceID) asset=\(stagedImport.asset.id)")
                return false
            }
            let assets = try await importsWithMaterializedVideoFiles(filteredAssets)
            storyPreviewDebugLog("edit import assets ready resolved=\(resolvedAssets.count) filtered=\(assets.count) photos=\(assets.filter { !$0.asset.isVideo }.count) videos=\(assets.filter(\.asset.isVideo).count) staged=\(assets.filter { $0.file != nil }.count)")
            guard !assets.isEmpty else {
                progress?(1.0, "Already in your story")
                storyPreviewDebugLog("edit import stopped all_resolved_assets_already_in_story")
                return []
            }

            for asset in assets {
                if let file = asset.file {
                    if asset.asset.isVideo {
                        cacheLocalMomentVideoPreview(from: file.url, for: asset.asset)
                    }
                    progress?(0.22, "Preparing upload…")
                    try await mediaUploadStore.save(makeStagedUploadRecord(asset: asset.asset, file: file))
                    storyPreviewDebugLog("edit import staged picker asset=\(asset.asset.id) local_identifier=\(asset.asset.localIdentifier ?? "nil") file=\(file.fileName) bytes=\(file.fileSizeBytes) content_type=\(file.contentType)")
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
                if asset.asset.isVideo, let sourcePreviewURL = momentVideoURLs[asset.asset.id] {
                    momentVideoURLs[mediaID] = sourcePreviewURL
                    if let sourceID = asset.asset.localIdentifier {
                        momentVideoURLs[sourceID] = sourcePreviewURL
                    }
                }
                if let thumbnail = asset.thumbnail {
                    cacheMomentThumbnail(thumbnail, forStoryAssetID: mediaID, reason: "edit_import")
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
        0.28 + progress.fractionCompleted * 0.56
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
                storyPreviewDebugLog("edit import using resolved photo asset=\(asset.id) local_identifier=\(asset.localIdentifier ?? "nil") kind=\(asset.kind.rawValue) created=\(created)")
                let thumbnail = await thumbnailImage(for: asset)
                staged.append(StagedStoryImport(asset: asset, file: nil, thumbnail: thumbnail, fallbackAnalysis: nil))
                continue
            }
            staged.append(try await stagedImport(from: item))
        }
        return staged
    }

    private func stagedImport(from item: PhotosPickerItem) async throws -> StagedStoryImport {
        let trimmedIdentifier = item.itemIdentifier?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let sourceIdentifier = trimmedIdentifier.isEmpty ? nil : trimmedIdentifier
        if item.supportedContentTypes.contains(where: { $0.conforms(to: .movie) || $0.conforms(to: .video) }) {
            return try await stagedVideoImport(from: item, sourceIdentifier: sourceIdentifier)
        }
        if item.supportedContentTypes.contains(where: { $0.conforms(to: .image) }) {
            return try await stagedPhotoImport(from: item, sourceIdentifier: sourceIdentifier)
        }
        throw AppError.unsupportedMedia
    }

    private func stagedPhotoImport(from item: PhotosPickerItem, sourceIdentifier: String?) async throws -> StagedStoryImport {
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
        let fingerprintIdentifier = "picker-photo-\(Self.sha256Hex(for: jpegData).prefix(32))"
        let assetID = sourceIdentifier ?? fingerprintIdentifier
        let asset = MediaAsset(
            id: assetID,
            localIdentifier: sourceIdentifier ?? fingerprintIdentifier,
            kind: .image,
            creationDate: nil,
            duration: 0,
            pixelWidth: Int(image.size.width.rounded()),
            pixelHeight: Int(image.size.height.rounded())
        )
        let analysis = MediaAnalysis(assetID: assetID, labels: ["photo", "memory"], peopleCount: 0, confidence: 0.5)
        storyPreviewDebugLog("edit import staged photo asset=\(asset.id) source_identifier=\(sourceIdentifier ?? "nil") local_identifier=\(asset.localIdentifier ?? "nil") pixels=\(asset.pixelWidth)x\(asset.pixelHeight) bytes=\(file.fileSizeBytes)")
        return StagedStoryImport(asset: asset, file: file, thumbnail: image, fallbackAnalysis: analysis)
    }

    private func stagedVideoImport(from item: PhotosPickerItem, sourceIdentifier: String?) async throws -> StagedStoryImport {
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
        let fingerprintIdentifier = try await mediaFingerprintIdentifier(for: url, prefix: "picker-video")
        let asset = MediaAsset(
            id: sourceIdentifier ?? fingerprintIdentifier,
            localIdentifier: sourceIdentifier ?? fingerprintIdentifier,
            kind: .video,
            creationDate: creationDate,
            duration: metadata.duration,
            pixelWidth: metadata.pixelWidth,
            pixelHeight: metadata.pixelHeight
        )
        let thumbnail = await videoThumbnail(for: url)
        storyPreviewDebugLog("edit import staged video asset=\(asset.id) source_identifier=\(sourceIdentifier ?? "nil") local_identifier=\(asset.localIdentifier ?? "nil") duration=\(asset.duration) pixels=\(asset.pixelWidth)x\(asset.pixelHeight) bytes=\(file.fileSizeBytes) content_type=\(contentType)")
        return StagedStoryImport(asset: asset, file: file, thumbnail: thumbnail, fallbackAnalysis: nil)
    }

    nonisolated private static func sha256Hex(for data: Data) -> String {
        SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
    }

    nonisolated private func mediaFingerprintIdentifier(for url: URL, prefix: String) async throws -> String {
        let digest = try await Task.detached(priority: .utility) { () throws -> String in
            let handle = try FileHandle(forReadingFrom: url)
            defer { try? handle.close() }
            var hasher = SHA256()
            while true {
                let chunk = try handle.read(upToCount: 1_048_576) ?? Data()
                if chunk.isEmpty {
                    break
                }
                hasher.update(data: chunk)
            }
            return hasher.finalize().map { String(format: "%02x", $0) }.joined()
        }.value
        return "\(prefix)-\(digest.prefix(32))"
    }

    private func importsWithMaterializedVideoFiles(_ imports: [StagedStoryImport]) async throws -> [StagedStoryImport] {
        guard let originalMediaSource = photoLibrary as? OriginalMediaSource else { return imports }
        var prepared: [StagedStoryImport] = []
        prepared.reserveCapacity(imports.count)
        for item in imports {
            guard item.asset.isVideo, item.file == nil else {
                prepared.append(item)
                continue
            }
            let file = try await originalMediaSource.materializeOriginalMedia(
                for: item.asset,
                allowsNetworkAccess: true,
                progress: nil
            )
            cacheLocalMomentVideoPreview(from: file.url, for: item.asset)
            prepared.append(StagedStoryImport(asset: item.asset, file: file, thumbnail: item.thumbnail, fallbackAnalysis: item.fallbackAnalysis))
            storyPreviewDebugLog("edit import materialized resolved video asset=\(item.asset.id) file=\(file.fileName) bytes=\(file.fileSizeBytes)")
        }
        return prepared
    }

    private func thumbnailImage(for asset: MediaAsset) async -> UIImage? {
        do {
            let data = try await photoLibrary.requestThumbnail(for: asset, targetSize: FrameMioMetrics.mediaThumbnailRequestSize)
            return UIImage(data: data)
        } catch {
            return nil
        }
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

    nonisolated private func videoCreationDate(for url: URL) async throws -> Date? {
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

    private func cacheLocalMomentVideoPreview(from sourceURL: URL, for asset: MediaAsset) {
        do {
            let previewURL = try copyMomentVideoPreview(from: sourceURL)
            momentVideoURLs[asset.id] = previewURL
            if let sourceID = asset.localIdentifier {
                momentVideoURLs[sourceID] = previewURL
            }
            storyPreviewDebugLog("edit import cached local video preview asset=\(asset.id) url=\(previewURL.lastPathComponent)")
        } catch {
            storyPreviewDebugLog("edit import local video preview cache failed asset=\(asset.id) error=\(error.localizedDescription)")
        }
    }

    private func copyMomentVideoPreview(from sourceURL: URL) throws -> URL {
        let fileManager = FileManager.default
        let directory = (fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? fileManager.temporaryDirectory)
            .appendingPathComponent("FrameMio", isDirectory: true)
            .appendingPathComponent("MomentVideoPreviews", isDirectory: true)
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
        let fileExtension = sourceURL.pathExtension.isEmpty ? "mov" : sourceURL.pathExtension
        let destinationURL = directory.appendingPathComponent("moment-\(UUID().uuidString).\(fileExtension)")
        try fileManager.copyItem(at: sourceURL, to: destinationURL)
        return destinationURL
    }

    private func makeStagedUploadRecord(asset: MediaAsset, file: OriginalMediaFile) -> MediaUpload {
        let uploadID = MediaUpload.makeID(for: asset.id, optimization: .original)
        return MediaUpload(
            id: uploadID,
            assetID: asset.id,
            localIdentifier: asset.localIdentifier,
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
        let versionedRequest = request.withBaseVersion(story?.storyPlanVersion)
        storyPreviewDebugLog("edit save requested story=\(storyID) base_version=\(versionedRequest.baseVersion.map(String.init) ?? "unset") add=\(versionedRequest.addMediaIDs.joined(separator: ",")) remove_media=\(versionedRequest.removeMediaIDs.joined(separator: ",")) remove_moments=\(versionedRequest.removeMomentIDs.joined(separator: ",")) ordered=\(versionedRequest.orderedMediaIDs.joined(separator: ",")) replace=\(versionedRequest.replace.count) analyses=\(versionedRequest.analyses.count) scene_detail=\(versionedRequest.sceneDetailStyle ?? "balanced") has_instruction=\(versionedRequest.instruction?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false) credit_actions=\(versionedRequest.regenerationCreditActions.map(\.rawValue).joined(separator: ",")) required_credits=\(versionedRequest.regenerationCreditCost)")
        perform(.editStory, creditActions: versionedRequest.regenerationCreditActions, mapResult: { [weak self] updatedStory in
            self?.storyPreviewDebugLogEditResult(updatedStory, request: versionedRequest)
            return self?.storyWithPendingEdit(versionedRequest, appliedTo: updatedStory) ?? updatedStory
        }) { [storyService] in
            try await storyService.saveStoryEdits(storyID: storyID, request: versionedRequest)
        }
    }

    func saveVideoToPhotos() {
        guard let videoURL = playableVideoURL, activeAction == nil else { return }
        actionTask?.cancel()
        actionTask = Task { [weak self] in
            guard let self else { return }
            activeAction = .saveVideo
            errorMessage = nil
            saveVideoState = .downloading(progress: nil)
            defer { activeAction = nil }
            do {
                try await Self.withTimeout(seconds: 150) {
                    try await Self.saveVideoToPhotoLibrary(
                        from: videoURL,
                        progress: { [weak self] progress in
                            DispatchQueue.main.async { [weak self] in
                                self?.saveVideoState = .downloading(progress: progress)
                            }
                        }
                    )
                }
                saveVideoState = .completed(message: "Saved to your Photos library. Open Photos to find it in Recents.")
                FrameMioHaptics.success()
            } catch is CancellationError {
                saveVideoState = nil
            } catch {
                saveVideoState = nil
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    func dismissSaveVideoState() {
        saveVideoState = nil
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

    private func storyPreviewDebugLogEditResult(_ updatedStory: Story, request: StoryEditRequest) {
        let returnedMomentIDs = (updatedStory.moments ?? []).map(\.mediaID).joined(separator: ",")
        let returnedAvailableIDs = (updatedStory.availableMoments ?? []).map(\.mediaID).joined(separator: ",")
        storyPreviewDebugLog("edit save response story=\(updatedStory.id) status=\(updatedStory.status.rawValue) requested_add=\(request.addMediaIDs.joined(separator: ",")) returned_moments=\(returnedMomentIDs) returned_available=\(returnedAvailableIDs)")
    }

    private func storyWithPendingEdit(_ request: StoryEditRequest, appliedTo updatedStory: Story) -> Story {
        var merged = updatedStory
        var moments = merged.moments ?? []
        let removedMomentIDs = Set(request.removeMomentIDs)
        let removedMediaIDs = Set(request.removeMediaIDs)
        moments.removeAll { removedMomentIDs.contains($0.id) || removedMediaIDs.contains($0.mediaID) }
        var existingMediaIDs = Set(moments.map(\.mediaID))
        var availableByMediaID: [String: StoryMoment] = [:]
        for availableMoment in merged.availableMoments ?? [] {
            availableByMediaID[availableMoment.mediaID] = availableMoment
        }
        for mediaID in request.addMediaIDs where !existingMediaIDs.contains(mediaID) {
            existingMediaIDs.insert(mediaID)
            if let existing = availableByMediaID[mediaID] {
                moments.append(StoryMoment(
                    id: existing.id,
                    mediaID: existing.mediaID,
                    sourceAssetID: existing.sourceAssetID,
                    type: existing.type,
                    thumbnailURL: existing.thumbnailURL,
                    title: existing.title,
                    description: existing.description,
                    creationDate: existing.creationDate,
                    isSelected: true
                ))
                continue
            }
            let asset = momentAssets[mediaID]
            moments.append(StoryMoment(
                id: mediaID,
                mediaID: mediaID,
                sourceAssetID: asset?.localIdentifier,
                type: storyMomentType(for: asset?.kind),
                thumbnailURL: nil,
                title: nil,
                description: "Added to this version. FrameMio is rebuilding your story.",
                creationDate: asset?.creationDate,
                isSelected: true
            ))
        }
        if !request.orderedMediaIDs.isEmpty {
            let order = Dictionary(uniqueKeysWithValues: request.orderedMediaIDs.enumerated().map { ($0.element, $0.offset) })
            moments.sort { left, right in
                switch (order[left.mediaID], order[right.mediaID]) {
                case let (.some(leftIndex), .some(rightIndex)):
                    return leftIndex < rightIndex
                case (.some, .none):
                    return true
                case (.none, .some):
                    return false
                case (.none, .none):
                    return false
                }
            }
        }
        merged.moments = deduplicatedStoryMoments(moments)
        merged.assetIDs = merged.moments?.map(\.mediaID) ?? merged.assetIDs
        storyPreviewDebugLog("edit optimistic moments story=\(merged.id) status=\(merged.status.rawValue) moments=\((merged.moments ?? []).map(\.mediaID).joined(separator: ","))")
        return merged
    }

    private func deduplicatedStoryMoments(_ moments: [StoryMoment]) -> [StoryMoment] {
        var seen = Set<String>()
        return moments.filter { moment in
            let sourceKey = moment.sourceAssetID?.trimmingCharacters(in: .whitespacesAndNewlines)
            let key = sourceKey?.isEmpty == false ? sourceKey! : moment.mediaID
            return seen.insert(key).inserted
        }
    }

    private func storyMomentType(for kind: MediaKind?) -> StoryMomentType? {
        switch kind {
        case .image: return .photo
        case .video: return .video
        case .livePhoto: return .livePhoto
        case nil: return nil
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
                await invalidateUploadCacheAfterStoryDelete(storyID: storyID)
                story = nil
                didDeleteStory = true
                FrameMioHaptics.success()
            } catch is CancellationError {
            } catch {
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    private func invalidateUploadCacheAfterStoryDelete(storyID: String) async {
        guard !launchAssets.isEmpty else { return }
        do {
            try await mediaUploadService.invalidateCompletedUploads(for: launchAssets, optimization: .original)
            cachedUploadAssetSignature = nil
            storyPreviewDebugLog("delete invalidated upload cache story=\(storyID) assets=\(launchAssets.count)")
        } catch {
            storyPreviewDebugLog("delete upload cache invalidation failed story=\(storyID) error=\(error.localizedDescription)")
        }
    }

    private func cacheLaunchAssets(for story: Story) {
        guard !launchAssets.isEmpty else { return }
        let storyMediaIDs = Set(story.assetIDs + (story.availableMoments ?? []).map(\.mediaID))
        for asset in launchAssets where storyMediaIDs.contains(asset.id) {
            momentAssets[asset.id] = asset
        }
    }

    private func cacheUploadAssets(for story: Story) async throws {
        let storyMediaIDs = Set(
            story.assetIDs
                + (story.moments ?? []).map(\.mediaID)
                + (story.availableMoments ?? []).map(\.mediaID)
        )
        guard !storyMediaIDs.isEmpty else { return }
        let signature = ([story.id] + storyMediaIDs.sorted()).joined(separator: "|")
        if cachedUploadAssetSignature == signature,
           storyMediaIDs.allSatisfy({ momentAssets[$0] != nil }) {
            return
        }

        let uploads = try await mediaUploadStore.allUploads()
        var cachedCount = 0
        for upload in uploads {
            guard let mediaID = upload.mediaID, storyMediaIDs.contains(mediaID) else { continue }
            let isNewCacheEntry = momentAssets[mediaID] == nil
            momentAssets[mediaID] = MediaAsset(
                id: mediaID,
                localIdentifier: upload.localIdentifier,
                kind: mediaKind(for: upload.mediaType),
                creationDate: upload.creationDate,
                duration: upload.duration ?? 0,
                pixelWidth: upload.pixelWidth,
                pixelHeight: upload.pixelHeight
            )
            if isNewCacheEntry {
                cachedCount += 1
            }
        }
        cachedUploadAssetSignature = signature
        if cachedCount > 0 {
            storyPreviewDebugLog("cached upload source assets story=\(story.id) count=\(cachedCount)")
        }
    }

    private func resolveMomentAsset(forStoryAssetID storyAssetID: String) async -> MediaAsset? {
        if let asset = momentAsset(for: storyAssetID) {
            return asset
        }
        let sourceID = sourceIdentity(forStoryAssetID: storyAssetID)
        do {
            if let upload = try await mediaUploadStore.allUploads().first(where: {
                $0.mediaID == storyAssetID || $0.assetID == storyAssetID || $0.localIdentifier == storyAssetID || (sourceID != nil && $0.localIdentifier == sourceID)
            }) {
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
                if let localIdentifier = upload.localIdentifier?.trimmingCharacters(in: .whitespacesAndNewlines), !localIdentifier.isEmpty {
                    momentAssets[localIdentifier] = asset
                }
                if let sourceID {
                    momentAssets[sourceID] = asset
                }
                return asset
            }

            if let sourceID, !sourceID.isEmpty {
                let resolved = try await photoLibrary.resolveAssets(localIdentifiers: [sourceID])
                if let asset = resolved.first {
                    momentAssets[storyAssetID] = asset
                    momentAssets[sourceID] = asset
                    storyPreviewDebugLog("resolved moment asset from photo library story_asset=\(storyAssetID) source=\(sourceID)")
                    return asset
                }
            }
            return nil
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

    private nonisolated static func saveVideoToPhotoLibrary(
        from remoteURL: URL,
        progress: (@Sendable (Double?) -> Void)?
    ) async throws {
        let authorization = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
        guard authorization == .authorized || authorization == .limited else {
            throw AppError.message("Allow FrameMio to add videos to Photos to save your story.")
        }

        let localURL = try await downloadVideoForSaving(from: remoteURL, progress: progress)
        defer {
            if localURL != remoteURL {
                try? FileManager.default.removeItem(at: localURL)
            }
        }

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

    private nonisolated static func downloadVideoForSaving(
        from remoteURL: URL,
        progress: (@Sendable (Double?) -> Void)?
    ) async throws -> URL {
        if remoteURL.isFileURL {
            progress?(1)
            return remoteURL
        }

        let configuration = URLSessionConfiguration.ephemeral
        configuration.requestCachePolicy = .reloadIgnoringLocalCacheData
        configuration.urlCache = nil
        configuration.waitsForConnectivity = true
        configuration.timeoutIntervalForRequest = 30
        configuration.timeoutIntervalForResource = 120
        let destinationURL = FileManager.default.temporaryDirectory
            .appendingPathComponent("framemio-story-\(UUID().uuidString)")
            .appendingPathExtension(remoteURL.pathExtension.isEmpty ? "mp4" : remoteURL.pathExtension)
        let delegate = VideoDownloadProgressDelegate(destinationURL: destinationURL, progress: progress)
        let session = URLSession(configuration: configuration, delegate: delegate, delegateQueue: nil)
        defer { session.finishTasksAndInvalidate() }
        return try await delegate.download(from: remoteURL, session: session)
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

    private func perform(
        _ action: StoryPreviewAction,
        creditActions: [CreditAction]? = nil,
        mapResult: (@MainActor (Story) -> Story)? = nil,
        operation: @escaping @Sendable () async throws -> Story
    ) {
        guard activeAction == nil else { return }
        actionTask?.cancel()
        actionTask = Task { [weak self] in
            guard let self else { return }
            activeAction = action
            errorMessage = nil
            defer { activeAction = nil }
            do {
                let requiredCreditActions = creditActions ?? action.creditActions
                try await verifyCredits(for: requiredCreditActions, issueAction: action.creditAction)
                let operationStory = try await operation()
                let updatedStory = mapResult?(operationStory) ?? operationStory
                try await cacheUploadAssets(for: updatedStory)
                cacheLaunchAssets(for: updatedStory)
                story = updatedStory
                storyPreviewDebugLog("action completed action=\(action.title) story=\(updatedStory.id) status=\(updatedStory.status.rawValue) should_poll=\(updatedStory.status.shouldPoll)")
                if updatedStory.status.shouldPoll || action == .editStory {
                    load()
                }
                onCreditsUpdated?()
                refreshCreditBalance()
                FrameMioHaptics.success()
            } catch is CancellationError {
            } catch {
                let appError = AppError.from(error)
                if case let .insufficientCredits(required, available) = appError {
                    insufficientCredits = CreditIssue(
                        action: action.creditAction,
                        required: required,
                        available: available
                    )
                    errorMessage = nil
                } else {
                    errorMessage = appError.localizedDescription
                }
            }
        }
    }

    private func verifyCredits(for action: CreditAction) async throws {
        try await verifyCredits(for: [action], issueAction: action)
    }

    private func verifyCredits(for actions: [CreditAction], issueAction: CreditAction) async throws {
        guard let creditService else { return }
        let balance = try await creditService.balance()
        creditBalance = balance.displayCredits
        if let issue = CreditPreflight.issue(for: actions, issueAction: issueAction, balance: balance) {
            throw AppError.insufficientCredits(required: issue.required, available: issue.available)
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

    var creditAction: CreditAction {
        creditActions.first ?? .preview
    }

    var creditActions: [CreditAction] {
        switch self {
        case .regenerate:
            [.regenerateStory]
        case .editStory:
            [.regenerateStory]
        case .replaceMoment:
            [.regenerateStory, .addMoment]
        case .removeMoment:
            [.removeMoment]
        case .retry, .renderAgain, .saveVideo, .deleteStory:
            [.preview]
        }
    }
}

enum StoryPreviewSaveVideoState: Equatable, Sendable {
    case downloading(progress: Double?)
    case completed(message: String)

    var title: String {
        switch self {
        case .downloading:
            "Downloading video"
        case .completed:
            "Saved"
        }
    }

    var message: String {
        switch self {
        case .downloading:
            "FrameMio is preparing your story for Photos."
        case .completed(let message):
            message
        }
    }

    var progress: Double? {
        switch self {
        case .downloading(let progress):
            progress
        case .completed:
            nil
        }
    }

    var showsDoneButton: Bool {
        if case .completed = self {
            true
        } else {
            false
        }
    }
}

private final class VideoDownloadProgressDelegate: NSObject, URLSessionDownloadDelegate, URLSessionTaskDelegate {
    private let destinationURL: URL
    private let fileManager: FileManager
    private let progressHandler: (@Sendable (Double?) -> Void)?
    private var continuation: CheckedContinuation<URL, Error>?
    private var task: URLSessionDownloadTask?

    init(
        destinationURL: URL,
        fileManager: FileManager = .default,
        progress: (@Sendable (Double?) -> Void)?
    ) {
        self.destinationURL = destinationURL
        self.fileManager = fileManager
        progressHandler = progress
    }

    func download(from remoteURL: URL, session: URLSession) async throws -> URL {
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                self.continuation = continuation
                let task = session.downloadTask(with: remoteURL)
                self.task = task
                task.resume()
            }
        } onCancel: {
            task?.cancel()
        }
    }

    func urlSession(
        _ session: URLSession,
        downloadTask: URLSessionDownloadTask,
        didWriteData bytesWritten: Int64,
        totalBytesWritten: Int64,
        totalBytesExpectedToWrite: Int64
    ) {
        guard totalBytesExpectedToWrite > 0 else {
            progressHandler?(nil)
            return
        }
        let progress = min(max(Double(totalBytesWritten) / Double(totalBytesExpectedToWrite), 0), 1)
        progressHandler?(progress)
    }

    func urlSession(
        _ session: URLSession,
        downloadTask: URLSessionDownloadTask,
        didFinishDownloadingTo location: URL
    ) {
        do {
            if fileManager.fileExists(atPath: destinationURL.path) {
                try fileManager.removeItem(at: destinationURL)
            }
            try fileManager.moveItem(at: location, to: destinationURL)
            resume(returning: destinationURL)
        } catch {
            resume(throwing: error)
        }
    }

    func urlSession(
        _ session: URLSession,
        task: URLSessionTask,
        didCompleteWithError error: Error?
    ) {
        if let error {
            resume(throwing: error)
        }
    }

    private func resume(returning url: URL) {
        guard let continuation else { return }
        self.continuation = nil
        continuation.resume(returning: url)
    }

    private func resume(throwing error: Error) {
        guard let continuation else { return }
        self.continuation = nil
        continuation.resume(throwing: error)
    }
}

enum StoryThumbnailCache {
    private static let cacheDirectoryName = "story-thumbnails-v1"

    static func load(storyID: String, storyAssetID: String, fileManager: FileManager = .default) -> UIImage? {
        let fileURL = fileURL(storyID: storyID, storyAssetID: storyAssetID, fileManager: fileManager)
        guard let data = try? Data(contentsOf: fileURL) else { return nil }
        return UIImage(data: data)
    }

    static func save(
        _ image: UIImage,
        storyID: String,
        storyAssetID: String,
        fileManager: FileManager = .default
    ) {
        guard let data = image.jpegData(compressionQuality: 0.82) else { return }
        let directory = cacheDirectory(fileManager: fileManager)
        do {
            try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
            try data.write(to: fileURL(storyID: storyID, storyAssetID: storyAssetID, fileManager: fileManager), options: .atomic)
        } catch {
        }
    }

    private static func cacheDirectory(fileManager: FileManager) -> URL {
        let baseURL = fileManager.urls(for: .cachesDirectory, in: .userDomainMask).first
            ?? fileManager.temporaryDirectory
        return baseURL
            .appendingPathComponent("FrameMio", isDirectory: true)
            .appendingPathComponent(cacheDirectoryName, isDirectory: true)
    }

    private static func fileURL(storyID: String, storyAssetID: String, fileManager: FileManager) -> URL {
        let fileName = "\(safeCacheComponent(storyID))-\(fnv1a64Hex(storyAssetID)).jpg"
        return cacheDirectory(fileManager: fileManager).appendingPathComponent(fileName, isDirectory: false)
    }

    private static func safeCacheComponent(_ value: String) -> String {
        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "-_"))
        let scalars = value.unicodeScalars.map { allowed.contains($0) ? Character($0) : "_" }
        let sanitized = String(scalars)
        return sanitized.isEmpty ? "story" : sanitized
    }

    private static func fnv1a64Hex(_ value: String) -> String {
        var hash: UInt64 = 0xcbf29ce484222325
        for byte in value.utf8 {
            hash ^= UInt64(byte)
            hash &*= 0x100000001b3
        }
        return String(format: "%016llx", hash)
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
