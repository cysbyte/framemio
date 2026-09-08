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
    private var launchTask: Task<Void, Never>?
    private var activeLaunchID: UUID?

    init(
        analysisService: MediaAnalysisService,
        mediaUploadService: MediaUploadService,
        storyService: StoryService,
        creditService: CreditService? = nil
    ) {
        self.analysisService = analysisService
        self.mediaUploadService = mediaUploadService
        self.storyService = storyService
        self.creditService = creditService
    }

    func start(context: MomentContext, assets: [MediaAsset], launchID: UUID) {
        guard activeLaunchID != launchID || (!isPreparing && storyID == nil) else {
            storyLaunchDebugLog("ignored duplicate launch id=\(launchID)")
            return
        }
        guard !assets.isEmpty else {
            errorMessage = AppError.unsupportedMedia.localizedDescription
            return
        }

        storyLaunchDebugLog("launch requested id=\(launchID) assets=\(assets.count) videos=\(assets.filter(\.isVideo).count)")
        launchTask?.cancel()
        activeLaunchID = launchID
        isPreparing = true
        progress = 0
        storyID = nil
        insufficientCredits = nil
        errorMessage = nil
        phaseTitle = "Preparing your memories..."
        phaseDetail = "Preparing selected media before upload starts."
        launchTask = Task { [weak self] in
            guard let self else { return }
            defer { isPreparing = false }

            do {
                try await verifyCredits(for: .createStory, assets: assets)
                let selection = MediaSelection(explicitlySelectedAssetIDs: assets.map(\.id))
                phaseTitle = "Uploading your photos and videos..."
                phaseDetail = "Uploading originals to FrameMio. Large videos and iCloud downloads can take longer."
                storyLaunchDebugLog("phase=upload_start progress=\(progress)")
                _ = try await mediaUploadService.upload(
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

                phaseTitle = "Finding meaningful moments..."
                phaseDetail = "Scanning media locally so the server can build the story."
                storyLaunchDebugLog("phase=analysis_start assets=\(assets.count)")
                let analyses = try await analyzeAssets(assets)
                try Task.checkCancellation()

                phaseTitle = "Building your story..."
                phaseDetail = "Sending analyzed moments to FrameMio before server-side planning starts."
                progress = max(progress, 0.86)
                storyLaunchDebugLog("phase=story_create_start app_percent=\(Int(progress * 100)) analyses=\(analyses.count)")
                let story = try await createStoryWithUploadRefreshRetry(
                    context: context,
                    assets: assets,
                    selection: selection,
                    analyses: analyses
                )
                storyID = story.id
                progress = 1
                phaseTitle = "Story is processing."
                phaseDetail = "Server-side planning and rendering will continue next."
                storyLaunchDebugLog("phase=story_create_complete story=\(story.id) status=\(story.status.rawValue)")
                FrameMioHaptics.success()
            } catch is CancellationError {
                storyLaunchDebugLog("phase=cancelled progress=\(progress)")
                phaseTitle = "Preparing your memories..."
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
            guard quote.eligible, quote.balance >= quote.credits else {
                throw AppError.insufficientCredits(required: quote.credits, available: quote.balance)
            }
            return
        }
        let balance = try await creditService.balance()
        if let issue = CreditPreflight.issue(for: action, balance: balance) {
            throw AppError.insufficientCredits(required: issue.required, available: issue.available)
        }
    }

    func retry(context: MomentContext, assets: [MediaAsset]) {
        start(context: context, assets: assets, launchID: UUID())
    }

    func cancel() {
        launchTask?.cancel()
    }

    private func createStoryWithUploadRefreshRetry(
        context: MomentContext,
        assets: [MediaAsset],
        selection: MediaSelection,
        analyses: [MediaAnalysis]
    ) async throws -> Story {
        do {
            return try await storyService.createStory(
                request: CreateStoryRequest(context: context, assets: assets, analyses: analyses)
            )
        } catch {
            guard shouldRetryAfterStaleUploadCache(error) else {
                throw error
            }
            storyLaunchDebugLog("phase=story_create_conflict action=refresh_upload_cache_and_retry")
            phaseTitle = "Refreshing uploaded media..."
            phaseDetail = "Previous server media references expired, so FrameMio is refreshing them before creating the story."
            try await mediaUploadService.invalidateCompletedUploads(for: assets, optimization: .original)
            _ = try await mediaUploadService.upload(
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
            phaseTitle = "Building your story..."
            phaseDetail = "Retrying story creation with refreshed media references."
            progress = max(progress, 0.86)
            return try await storyService.createStory(
                request: CreateStoryRequest(context: context, assets: assets, analyses: analyses)
            )
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
