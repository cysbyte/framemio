import Combine
import Foundation

private func storyLaunchDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][StoryLaunch] \(message())")
#endif
}

private enum StoryLaunchTimeoutError: Error {
    case timedOut
}

private final class StoryLaunchAnalysisRace: @unchecked Sendable {
    private let lock = NSLock()
    private var didResume = false
    private var analysisTask: Task<Void, Never>?
    private var timeoutTask: Task<Void, Never>?

    func setTasks(analysisTask: Task<Void, Never>, timeoutTask: Task<Void, Never>) {
        lock.lock()
        if didResume {
            lock.unlock()
            analysisTask.cancel()
            timeoutTask.cancel()
            return
        }
        self.analysisTask = analysisTask
        self.timeoutTask = timeoutTask
        lock.unlock()
    }

    func resume(
        _ continuation: CheckedContinuation<MediaAnalysis, Error>,
        with result: Result<MediaAnalysis, Error>
    ) {
        let tasks: [Task<Void, Never>]
        lock.lock()
        guard !didResume else {
            lock.unlock()
            return
        }
        didResume = true
        tasks = [analysisTask, timeoutTask].compactMap { $0 }
        lock.unlock()

        tasks.forEach { $0.cancel() }
        continuation.resume(with: result)
    }

    func cancel() {
        let tasks: [Task<Void, Never>]
        lock.lock()
        didResume = true
        tasks = [analysisTask, timeoutTask].compactMap { $0 }
        lock.unlock()
        tasks.forEach { $0.cancel() }
    }
}

/// Owns the work between tapping Create Story and the server accepting the
/// story: uploading media, analysing it on device, then creating the story.
///
/// This lives above the navigation stack on purpose. The upload and analysis
/// used to run on the create screen, which pinned the viewer there while
/// iCloud assets downloaded. Now the app navigates first and this keeps
/// running, so all progress is visible in one place.
@MainActor
final class StoryLaunchCoordinator: ObservableObject {
    /// Local preparation stage, before the server knows about the story.
    @Published private(set) var phaseTitle = "Preparing your memories..."
    @Published private(set) var phaseDetail = "Uploading and local analysis happen first; server rendering starts right after."
    @Published private(set) var progress: Double = 0
    @Published private(set) var isPreparing = false
    /// Set once the server accepts the story; the view then polls it.
    @Published private(set) var storyID: String?
    @Published var insufficientCredits: CreditIssue?
    @Published var errorMessage: String?

    private let analysisService: MediaAnalysisService
    private let mediaUploadService: MediaUploadService
    private let storyService: StoryService
    private let creditService: CreditService?
    private let localReelRenderer: LocalReelRendering
    private var launchTask: Task<Void, Never>?
    private var activeLaunchID: UUID?

    init(
        analysisService: MediaAnalysisService,
        mediaUploadService: MediaUploadService,
        storyService: StoryService,
        creditService: CreditService? = nil,
        localReelRenderer: LocalReelRendering
    ) {
        self.analysisService = analysisService
        self.mediaUploadService = mediaUploadService
        self.storyService = storyService
        self.creditService = creditService
        self.localReelRenderer = localReelRenderer
    }

    func start(
        context: MomentContext,
        assets: [MediaAsset],
        options: StoryGenerationOptions = .automatic,
        launchID: UUID
    ) {
        start(draft: .story(context: context, options: options), assets: assets, launchID: launchID)
    }

    func start(
        draft: VideoProjectDraft,
        assets: [MediaAsset],
        launchID: UUID
    ) {
        guard activeLaunchID != launchID || (!isPreparing && storyID == nil) else {
            storyLaunchDebugLog("ignored duplicate launch id=\(launchID)")
            return
        }
        guard !assets.isEmpty else {
            errorMessage = AppError.unsupportedMedia.localizedDescription
            return
        }

        let instruction = draft.prompt?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let totalVideoSourceDuration = assets.reduce(0.0) { total, asset in
            asset.isVideo ? total + max(0, asset.duration) : total
        }
        storyLaunchDebugLog("launch requested id=\(launchID) type=\(draft.type.rawValue) assets=\(assets.count) videos=\(assets.filter(\.isVideo).count) total_video_source_seconds=\(totalVideoSourceDuration) duration_mode=\(draft.storyOptions.durationMode.rawValue) target_duration=\(draft.storyOptions.targetDuration.map { String(Int($0.rounded())) } ?? "auto") template_id=\(draft.templateID ?? "none") prompt_present=\(!instruction.isEmpty) prompt_length=\(instruction.count)")
        launchTask?.cancel()
        activeLaunchID = launchID
        isPreparing = true
        progress = 0
        storyID = nil
        insufficientCredits = nil
        errorMessage = nil
        phaseTitle = preparingTitle(for: draft.type)
        phaseDetail = "Preparing selected media before upload starts."
        launchTask = Task { [weak self] in
            guard let self else { return }
            defer { isPreparing = false }

            do {
                if LocalReelPolicy.shouldRenderLocally(draft: draft, assets: assets) {
                    phaseTitle = "Creating your reel on this iPhone..."
                    phaseDetail = "Using the selected template without uploading media."
                    progress = 0.12
                    storyLaunchDebugLog("phase=local_reel_start assets=\(assets.count) template_id=\(draft.templateID ?? "none")")
                    let rendered = try await localReelRenderer.renderReel(
                        draft: draft,
                        assets: assets,
                        progress: { [weak self] value in
                            Task { @MainActor [weak self] in
                                self?.progress = max(self?.progress ?? 0, value)
                            }
                        }
                    )
                    phaseTitle = "Saving your reel securely..."
                    phaseDetail = "Uploading the finished video to your private cloud storage."
                    let uploaded = try await mediaUploadService.uploadGeneratedVideo(
                        GeneratedVideoUploadRequest(
                            fileURL: rendered.videoURL,
                            fileName: "framemio-\(UUID().uuidString).mp4",
                            contentType: "video/mp4",
                            duration: rendered.duration,
                            pixelWidth: 540,
                            pixelHeight: 960
                        ),
                        progress: { [weak self] value in
                            Task { @MainActor [weak self] in
                                self?.progress = max(self?.progress ?? 0, 0.42 + value * 0.42)
                            }
                        }
                    )
                    phaseTitle = "Finalizing your reel..."
                    phaseDetail = "Preparing a secure download link."
                    let story = try await storyService.createLocalReel(
                        request: LocalReelPublishRequest(
                            context: draft.context.rawValue,
                            title: rendered.templateName,
                            mediaID: uploaded.mediaID,
                            duration: rendered.duration,
                            templateID: draft.templateID,
                            templateVersion: draft.templateVersion,
                            templateConfiguration: draft.templateConfiguration,
                            sourceAssets: assets.map {
                                LocalReelSourceAsset(
                                    assetID: $0.id,
                                    sourceAssetID: $0.localIdentifier ?? $0.id,
                                    kind: $0.kind.rawValue,
                                    duration: $0.duration,
                                    creationDate: $0.creationDate,
                                    pixelWidth: $0.pixelWidth,
                                    pixelHeight: $0.pixelHeight
                                )
                            }
                        )
                    )
                    try? FileManager.default.removeItem(at: rendered.videoURL)
                    storyID = story.id
                    progress = 1
                    phaseTitle = "Reel ready."
                    phaseDetail = "Saved securely and ready to download."
                    storyLaunchDebugLog("phase=local_reel_complete story=\(story.id) media=\(uploaded.mediaID) local_file_removed=true")
                    FrameMioHaptics.success()
                    return
                }

                try await verifyCredits(for: .createStory, assets: assets)
                let selection = MediaSelection(explicitlySelectedAssetIDs: assets.map(\.id))
                phaseTitle = "Uploading your photos and videos..."
                phaseDetail = "Uploading originals to FrameMio. Large videos and iCloud downloads can take longer."
                storyLaunchDebugLog("phase=upload_start progress=\(progress)")
                let uploads = try await mediaUploadService.upload(
                    selection: selection,
                    assets: assets,
                    options: MediaUploadOptions(),
                    progress: { [weak self] uploadProgress in
                        Task { @MainActor [weak self] in
                            guard let self else { return }
                            let displayProgress = min(0.28, uploadProgress.fractionCompleted * 0.28)
                            let previousPercent = Int(progress * 100)
                            progress = max(progress, displayProgress)
                            let currentPercent = Int(progress * 100)
                            if currentPercent != previousPercent || currentPercent >= 28 {
                                storyLaunchDebugLog("phase=upload_progress app_percent=\(currentPercent) upload_fraction=\(uploadProgress.fractionCompleted) bytes=\(uploadProgress.uploadedBytes)/\(uploadProgress.totalBytes) completed_uploads=\(uploadProgress.completedUploads)/\(uploadProgress.totalUploads) asset=\(uploadProgress.assetID) upload=\(uploadProgress.uploadID)")
                            }
                        }
                    }
                )
                try Task.checkCancellation()

                phaseTitle = analysisTitle(for: draft.type)
                phaseDetail = "Scanning media locally so the server can build the first AI draft."
                storyLaunchDebugLog("phase=analysis_start assets=\(assets.count)")
                let analyses = try await analyzeAssets(assets)
                try Task.checkCancellation()

                phaseTitle = buildingTitle(for: draft.type)
                phaseDetail = "Sending analyzed media to FrameMio before server-side planning starts."
                progress = max(progress, 0.86)
                storyLaunchDebugLog("phase=story_create_start app_percent=\(Int(progress * 100)) analyses=\(analyses.count) duration_mode=\(draft.storyOptions.durationMode.rawValue) target_duration=\(draft.storyOptions.targetDuration.map { String(Int($0.rounded())) } ?? "auto") total_video_source_seconds=\(totalVideoSourceDuration)")
                let story = try await createStoryWithUploadRefreshRetry(
                    draft: draft,
                    assets: assets,
                    selection: selection,
                    analyses: analyses,
                    uploads: uploads
                )
                storyID = story.id
                progress = 1
                phaseTitle = processingTitle(for: draft.type)
                phaseDetail = "Server-side planning and rendering will continue next."
                storyLaunchDebugLog("phase=story_create_complete story=\(story.id) status=\(story.status.rawValue)")
                refreshCreditBalance()
                FrameMioHaptics.success()
            } catch is CancellationError {
                storyLaunchDebugLog("phase=cancelled progress=\(progress)")
                phaseTitle = preparingTitle(for: draft.type)
                phaseDetail = "Preparing selected media before upload starts."
            } catch {
                let appError = AppError.from(error)
                storyLaunchDebugLog("phase=failed progress=\(progress) error=\(appError.localizedDescription) raw=\(error.localizedDescription)")
                if case let .insufficientCredits(required, available) = appError {
                    insufficientCredits = CreditIssue(action: .createStory, required: required, available: available)
                    errorMessage = nil
                    phaseTitle = "Not enough credits"
                    storyLaunchDebugLog("phase=insufficient_credits required=\(required) available=\(available)")
                } else {
                    errorMessage = appError.localizedDescription
                    phaseTitle = "Something went wrong"
                }
            }
        }
    }

    private func verifyCredits(for action: CreditAction, assets: [MediaAsset]) async throws {
        guard let creditService else { return }
        if action == .createStory {
            let quote = try await creditService.quote(action: action, stats: .createStory(assets: assets))
            storyLaunchDebugLog("credit_preflight type=create_story required=\(quote.credits) balance=\(quote.balance) eligible=\(quote.eligible) reason=\(quote.reason ?? "none")")
            guard quote.eligible, quote.balance >= quote.credits else {
                throw AppError.insufficientCredits(required: quote.credits, available: quote.balance)
            }
            return
        }
        let balance = try await creditService.balance()
        storyLaunchDebugLog("credit_preflight type=\(action.rawValue) balance=\(balance.displayCredits) reserved=\(balance.reserved)")
        if let issue = CreditPreflight.issue(for: action, balance: balance) {
            throw AppError.insufficientCredits(required: issue.required, available: issue.available)
        }
    }

    private func refreshCreditBalance() {
        guard let creditService else { return }
        Task { [creditService] in
            do {
                let balance = try await creditService.refreshBalance()
                storyLaunchDebugLog("credit_balance_after_create available=\(balance.available) reserved=\(balance.reserved) display=\(balance.displayCredits)")
            } catch {
                storyLaunchDebugLog("credit_balance_after_create_failed error=\(error.localizedDescription)")
            }
        }
    }

    func retry(context: MomentContext, assets: [MediaAsset]) {
        start(context: context, assets: assets, launchID: UUID())
    }

    func cancel() {
        launchTask?.cancel()
    }

    private func createStoryWithUploadRefreshRetry(
        draft: VideoProjectDraft,
        assets: [MediaAsset],
        selection: MediaSelection,
        analyses: [MediaAnalysis],
        uploads: [MediaUpload]
    ) async throws -> Story {
        do {
            return try await storyService.createProject(
                request: CreateVideoProjectRequest(
                    draft: draft,
                    assets: assets,
                    analyses: analyses,
                    mediaIDsByAssetID: mediaIDsByAssetID(from: uploads)
                )
            )
        } catch {
            guard shouldRetryAfterStaleUploadCache(error) else {
                throw error
            }
            storyLaunchDebugLog("phase=story_create_conflict action=refresh_upload_cache_and_retry")
            phaseTitle = "Refreshing uploaded media..."
            phaseDetail = "Previous server media references expired, so FrameMio is refreshing them before creating the story."
            try await mediaUploadService.invalidateCompletedUploads(for: assets, optimization: .original)
            let refreshedUploads = try await mediaUploadService.upload(
                selection: selection,
                assets: assets,
                options: MediaUploadOptions(),
                progress: { [weak self] uploadProgress in
                    Task { @MainActor [weak self] in
                        guard let self else { return }
                        let refreshProgress = 0.86 + min(0.015, uploadProgress.fractionCompleted * 0.015)
                        progress = max(progress, refreshProgress)
                    }
                }
            )
            try Task.checkCancellation()
            phaseTitle = buildingTitle(for: draft.type)
            phaseDetail = "Retrying project creation with refreshed media references."
            progress = max(progress, 0.86)
            return try await storyService.createProject(
                request: CreateVideoProjectRequest(
                    draft: draft,
                    assets: assets,
                    analyses: analyses,
                    mediaIDsByAssetID: mediaIDsByAssetID(from: refreshedUploads)
                )
            )
        }
    }

    private func mediaIDsByAssetID(from uploads: [MediaUpload]) -> [String: String] {
        var result: [String: String] = [:]
        for upload in uploads where upload.status == .completed {
            guard let mediaID = upload.mediaID, !mediaID.isEmpty else { continue }
            result[upload.assetID] = mediaID
        }
        return result
    }

    private func preparingTitle(for type: VideoProjectType) -> String {
        switch type {
        case .story: "Preparing your memories..."
        case .promo: "Preparing your promo..."
        case .reel: "Preparing your reel..."
        }
    }

    private func analysisTitle(for type: VideoProjectType) -> String {
        switch type {
        case .story: "Finding meaningful moments..."
        case .promo: "Finding useful product shots..."
        case .reel: "Finding short-form highlights..."
        }
    }

    private func buildingTitle(for type: VideoProjectType) -> String {
        switch type {
        case .story: "Building your story..."
        case .promo: "Building your promo..."
        case .reel: "Building your reel..."
        }
    }

    private func processingTitle(for type: VideoProjectType) -> String {
        switch type {
        case .story: "Story is processing."
        case .promo: "Promo is processing."
        case .reel: "Reel is processing."
        }
    }

    private func shouldRetryAfterStaleUploadCache(_ error: Error) -> Bool {
        if case let AppError.server(statusCode) = AppError.from(error), statusCode == 409 {
            return true
        }
        if case let APIClientError.server(statusCode, _) = error, statusCode == 409 {
            return true
        }
        return false
    }

    private func analyzeAssets(_ assets: [MediaAsset]) async throws -> [MediaAnalysis] {
        var analyses: [MediaAnalysis] = []
        analyses.reserveCapacity(assets.count)

        try await withThrowingTaskGroup(of: MediaAnalysis.self) { group in
            for asset in assets {
                storyLaunchDebugLog("phase=analysis_enqueue asset=\(asset.id) kind=\(asset.kind.rawValue)")
                group.addTask { [analysisService] in
                    try Task.checkCancellation()
                    return try await Self.analyzeWithFallback(
                        asset: asset,
                        analysisService: analysisService,
                        timeout: .seconds(8)
                    )
                }
            }

            for try await analysis in group {
                analyses.append(analysis)
                progress = min(0.82, 0.28 + Double(analyses.count) / Double(assets.count) * 0.54)
                storyLaunchDebugLog("phase=analysis_progress app_percent=\(Int(progress * 100)) completed=\(analyses.count)/\(assets.count) asset=\(analysis.assetID) labels=\(analysis.labels.joined(separator: ","))")
            }
        }
        return analyses.sorted { $0.assetID < $1.assetID }
    }

    private static func analyzeWithFallback(
        asset: MediaAsset,
        analysisService: MediaAnalysisService,
        timeout: Duration
    ) async throws -> MediaAnalysis {
        if asset.isVideo {
            storyLaunchDebugLog("phase=analysis_video_fallback asset=\(asset.id) kind=\(asset.kind.rawValue) reason=server_video_intelligence")
            return fallbackAnalysis(for: asset)
        }

        let race = StoryLaunchAnalysisRace()
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                let analysisTask = Task {
                    do {
                        let analysis = try await analysisService.analyze(asset: asset)
                        race.resume(continuation, with: .success(analysis))
                    } catch is CancellationError {
                        race.resume(continuation, with: .failure(CancellationError()))
                    } catch {
                        storyLaunchDebugLog("phase=analysis_fallback asset=\(asset.id) kind=\(asset.kind.rawValue) error=\(error.localizedDescription)")
                        race.resume(continuation, with: .success(fallbackAnalysis(for: asset)))
                    }
                }

                let timeoutTask = Task {
                    do {
                        try await Task.sleep(for: timeout)
                        storyLaunchDebugLog("phase=analysis_timeout asset=\(asset.id) kind=\(asset.kind.rawValue)")
                        race.resume(continuation, with: .success(fallbackAnalysis(for: asset)))
                    } catch is CancellationError {
                    } catch {
                        race.resume(continuation, with: .success(fallbackAnalysis(for: asset)))
                    }
                }

                race.setTasks(analysisTask: analysisTask, timeoutTask: timeoutTask)
            }
        } onCancel: {
            race.cancel()
        }
    }

    private static func fallbackAnalysis(for asset: MediaAsset) -> MediaAnalysis {
        MediaAnalysis(
            assetID: asset.id,
            labels: asset.isVideo ? ["video", "memory"] : ["photo", "memory"],
            peopleCount: 0,
            confidence: 0.35
        )
    }

    deinit {
        launchTask?.cancel()
    }
}
