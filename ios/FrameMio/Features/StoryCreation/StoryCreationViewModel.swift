import Combine
import Foundation
import SwiftUI
import UIKit

private func storyCreationDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][StoryCreation] \(message())")
#endif
}

private enum StoryCreationAnalysisTimeoutError: Error {
    case timedOut
}

@MainActor
final class StoryCreationViewModel: ObservableObject {
    @Published private(set) var progress: Double = 0
    @Published private(set) var phaseTitle = "Preparing your memories..."
    @Published private(set) var isCreating = false
    @Published private(set) var isCheckingCredits = false
    @Published private(set) var story: Story?
    @Published private(set) var thumbnails: [String: UIImage] = [:]
    @Published private(set) var creditBalance: Int?
    @Published private(set) var createStoryCreditCost: Int
    @Published var insufficientCredits: CreditIssue?
    @Published var errorMessage: String?

    let context: MomentContext
    let assets: [MediaAsset]
    private let photoLibrary: PhotoLibraryService
    private let analysisService: MediaAnalysisService
    private let mediaUploadService: MediaUploadService
    private let storyService: StoryService
    private let creditService: CreditService?
    private let onCreditsUpdated: (() -> Void)?
    private var creationTask: Task<Void, Never>?
    private var thumbnailTasks: [String: Task<Void, Never>] = [:]
    private var thumbnailRequestGeneration: [String: Int] = [:]

    init(
        context: MomentContext,
        assets: [MediaAsset],
        photoLibrary: PhotoLibraryService,
        analysisService: MediaAnalysisService,
        mediaUploadService: MediaUploadService,
        storyService: StoryService,
        creditService: CreditService? = nil,
        onCreditsUpdated: (() -> Void)? = nil
    ) {
        self.context = context
        self.assets = assets
        self.photoLibrary = photoLibrary
        self.analysisService = analysisService
        self.mediaUploadService = mediaUploadService
        self.storyService = storyService
        self.creditService = creditService
        self.onCreditsUpdated = onCreditsUpdated
        self.createStoryCreditCost = CreditPricing.price(for: .createStory, stats: Self.createStoryCreditStats(for: assets))
        storyCreationDebugLog("view model initialized instance=\(ObjectIdentifier(self)) assets=\(assets.count)")
    }

    func image(for asset: MediaAsset) -> Image? {
        guard let image = thumbnails[asset.id] else { return nil }
        return Image(uiImage: image)
    }

    func isThumbnailLoading(for asset: MediaAsset) -> Bool {
        thumbnailTasks[asset.id] != nil
    }

    func loadMissingThumbnails(targetSize: CGSize) {
        let missingAssets = assets.filter { thumbnails[$0.id] == nil }
        storyCreationDebugLog("thumbnail refresh requested assets=\(assets.count) missing=\(missingAssets.count) active_tasks=\(thumbnailTasks.count)")
        for asset in missingAssets {
            loadThumbnail(for: asset, targetSize: targetSize)
        }
    }

    func loadThumbnail(for asset: MediaAsset, targetSize: CGSize) {
        let assetID = asset.id
        if thumbnails[assetID] != nil {
            storyCreationDebugLog("thumbnail request skipped asset=\(assetID) reason=cache_hit")
            return
        }
        if thumbnailTasks[assetID] != nil {
            storyCreationDebugLog("thumbnail request skipped asset=\(assetID) reason=already_loading")
            return
        }

        let generation = (thumbnailRequestGeneration[assetID] ?? 0) + 1
        thumbnailRequestGeneration[assetID] = generation
        storyCreationDebugLog("thumbnail request started asset=\(assetID) local_identifier=\(asset.localIdentifier ?? "nil") kind=\(asset.kind.rawValue) target=\(Int(targetSize.width))x\(Int(targetSize.height)) generation=\(generation)")
        thumbnailTasks[assetID] = Task { [weak self, photoLibrary] in
            defer {
                if let self, self.thumbnailRequestGeneration[assetID] == generation {
                    self.thumbnailTasks[assetID] = nil
                    storyCreationDebugLog("thumbnail task cleared asset=\(assetID) generation=\(generation)")
                }
            }

            do {
                let data = try await photoLibrary.requestThumbnail(for: asset, targetSize: targetSize)
                try Task.checkCancellation()
                if let image = UIImage(data: data) {
                    self?.thumbnails[assetID] = image
                    storyCreationDebugLog("thumbnail request succeeded asset=\(assetID) bytes=\(data.count) generation=\(generation)")
                } else {
                    storyCreationDebugLog("thumbnail request failed asset=\(assetID) reason=image_decode_failed bytes=\(data.count) generation=\(generation)")
                }
            } catch is CancellationError {
                storyCreationDebugLog("thumbnail request cancelled asset=\(assetID) generation=\(generation)")
            } catch {
                storyCreationDebugLog("thumbnail request failed asset=\(assetID) generation=\(generation) error=\(error.localizedDescription)")
            }
        }
    }

    func cancelThumbnail(for asset: MediaAsset) {
        let assetID = asset.id
        guard let task = thumbnailTasks[assetID] else {
            storyCreationDebugLog("thumbnail cancel skipped asset=\(assetID) reason=no_active_task")
            return
        }
        task.cancel()
        thumbnailTasks[assetID] = nil
        storyCreationDebugLog("thumbnail request cancelled asset=\(assetID) reason=view_disappeared")
    }

    func refreshCreditBalance() {
        guard let creditService else { return }
        Task { [weak self] in
            guard let self else { return }
            do {
                async let balanceTask = creditService.balance()
                async let quoteTask = creditService.quote(action: .createStory, stats: createStoryCreditStats)
                let (balance, quote) = try await (balanceTask, quoteTask)
                creditBalance = balance.displayCredits
                createStoryCreditCost = quote.credits
            } catch {
            }
        }
    }

    private var createStoryCreditStats: CreditMediaStats {
        Self.createStoryCreditStats(for: assets)
    }

    private static func createStoryCreditStats(for assets: [MediaAsset]) -> CreditMediaStats {
        CreditMediaStats.createStory(assets: assets)
    }

    func requestCreateStory(onReady: @escaping @MainActor () -> Void) {
        guard !isCreating, !isCheckingCredits else { return }
        guard let creditService else {
            onReady()
            return
        }

        isCheckingCredits = true
        insufficientCredits = nil
        errorMessage = nil
        Task { [weak self] in
            guard let self else { return }
            defer { isCheckingCredits = false }
            do {
                let quote = try await creditService.quote(action: .createStory, stats: createStoryCreditStats)
                creditBalance = quote.balance
                createStoryCreditCost = quote.credits
                if let issue = Self.creditIssue(from: quote, action: .createStory) {
                    storyCreationDebugLog("createStory preflight_insufficient_credits required=\(issue.required) available=\(issue.available)")
                    insufficientCredits = issue
                    phaseTitle = "Not enough credits"
                    return
                }
                storyCreationDebugLog("createStory preflight_ok required=\(quote.credits) balance=\(quote.balance)")
                onReady()
            } catch {
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    func createStory() {
        guard !isCreating else {
            storyCreationDebugLog("ignored duplicate create request while already creating")
            return
        }
        storyCreationDebugLog("createStory requested assets=\(assets.count) videos=\(assets.filter(\.isVideo).count) credit_cost=\(createStoryCreditCost) balance=\(creditBalance.map(String.init) ?? "nil")")
        creationTask?.cancel()
        creationTask = Task { [weak self] in
            guard let self else { return }
            isCreating = true
            progress = 0
            story = nil
            insufficientCredits = nil
            errorMessage = nil
            defer { isCreating = false }

            do {
                try await verifyCredits(for: .createStory)
                storyCreationDebugLog("phase=upload_start progress=\(progress)")
                phaseTitle = "Uploading your photos and videos..."
                let selection = MediaSelection(explicitlySelectedAssetIDs: assets.map(\.id))
                _ = try await mediaUploadService.upload(
                    selection: selection,
                    assets: assets,
                    options: MediaUploadOptions(),
                    progress: { [weak self] uploadProgress in
                        Task { @MainActor [weak self] in
                            guard let self else { return }
                            let displayProgress = min(
                                0.28,
                                uploadProgress.fractionCompleted * 0.28
                            )
                            let previousPercent = Int(progress * 100)
                            progress = max(progress, displayProgress)
                            let currentPercent = Int(progress * 100)
                            if currentPercent != previousPercent || currentPercent >= 28 {
                                storyCreationDebugLog("phase=upload_progress app_percent=\(currentPercent) upload_fraction=\(uploadProgress.fractionCompleted) bytes=\(uploadProgress.uploadedBytes)/\(uploadProgress.totalBytes) completed_uploads=\(uploadProgress.completedUploads)/\(uploadProgress.totalUploads) asset=\(uploadProgress.assetID) upload=\(uploadProgress.uploadID)")
                            }
                        }
                    }
                )
                storyCreationDebugLog("phase=upload_complete progress=\(progress)")
                try Task.checkCancellation()
                phaseTitle = "Finding meaningful moments..."
                storyCreationDebugLog("phase=analysis_start assets=\(assets.count)")
                let analyses = try await analyzeAssets()
                storyCreationDebugLog("phase=analysis_complete count=\(analyses.count)")
                try Task.checkCancellation()
                phaseTitle = "Building your story..."
                progress = max(progress, 0.86)
                storyCreationDebugLog("phase=story_create_start app_percent=\(Int(progress * 100)) analyses=\(analyses.count)")
                story = try await createStoryWithUploadRefreshRetry(
                    selection: selection,
                    analyses: analyses
                )
                progress = 1
                storyCreationDebugLog("phase=story_create_complete story=\(story?.id ?? "nil") status=\(story?.status.rawValue ?? "nil")")
                phaseTitle = "Story is processing."
                onCreditsUpdated?()
                refreshCreditBalance()
                FrameMioHaptics.success()
            } catch is CancellationError {
                storyCreationDebugLog("phase=cancelled progress=\(progress)")
                phaseTitle = "Preparing your memories..."
            } catch {
                let appError = AppError.from(error)
                storyCreationDebugLog("phase=failed progress=\(progress) error=\(appError.localizedDescription) raw=\(error.localizedDescription)")
                switch appError {
                case let .insufficientCredits(required, available):
                    storyCreationDebugLog("createStory insufficient_credits required=\(required) available=\(available)")
                    insufficientCredits = CreditIssue(action: .createStory, required: required, available: available)
                    errorMessage = nil
                    phaseTitle = "Not enough credits"
                default:
                    errorMessage = appError.localizedDescription
                    phaseTitle = "Something went wrong"
                }
            }
        }
    }

    func cancel() {
        creationTask?.cancel()
    }

    private func createStoryWithUploadRefreshRetry(
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
            storyCreationDebugLog("phase=story_create_conflict action=refresh_upload_cache_and_retry")
            phaseTitle = "Refreshing uploaded media..."
            try await mediaUploadService.invalidateCompletedUploads(for: assets, optimization: .original)
            _ = try await mediaUploadService.upload(
                selection: selection,
                assets: assets,
                options: MediaUploadOptions(),
                progress: { [weak self] uploadProgress in
                    Task { @MainActor [weak self] in
                        guard let self else { return }
                        progress = max(progress, min(0.28, uploadProgress.fractionCompleted * 0.28))
                    }
                }
            )
            try Task.checkCancellation()
            phaseTitle = "Building your story..."
            progress = max(progress, 0.86)
            return try await storyService.createStory(
                request: CreateStoryRequest(context: context, assets: assets, analyses: analyses)
            )
        }
    }

    private func verifyCredits(for action: CreditAction) async throws {
        guard let creditService else { return }
        if action == .createStory {
            let quote = try await creditService.quote(action: action, stats: createStoryCreditStats)
            creditBalance = quote.balance
            createStoryCreditCost = quote.credits
            if let issue = Self.creditIssue(from: quote, action: action) {
                throw AppError.insufficientCredits(required: issue.required, available: issue.available)
            }
            return
        }
        let balance = try await creditService.balance()
        creditBalance = balance.displayCredits
        if let issue = CreditPreflight.issue(for: action, balance: balance) {
            throw AppError.insufficientCredits(required: issue.required, available: issue.available)
        }
    }

    private static func creditIssue(from quote: CreditQuote, action: CreditAction) -> CreditIssue? {
        guard quote.eligible, quote.balance >= quote.credits else {
            return CreditIssue(action: action, required: quote.credits, available: quote.balance)
        }
        return nil
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

    private func analyzeAssets() async throws -> [MediaAnalysis] {
        guard !assets.isEmpty else { throw AppError.unsupportedMedia }
        var analyses: [MediaAnalysis] = []
        analyses.reserveCapacity(assets.count)

        try await withThrowingTaskGroup(of: MediaAnalysis.self) { group in
            for asset in assets {
                storyCreationDebugLog("phase=analysis_enqueue asset=\(asset.id) kind=\(asset.kind.rawValue)")
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
                progress = min(0.82, Double(analyses.count) / Double(assets.count) * 0.82)
                storyCreationDebugLog("phase=analysis_progress app_percent=\(Int(progress * 100)) completed=\(analyses.count)/\(assets.count) asset=\(analysis.assetID) labels=\(analysis.labels.joined(separator: ","))")
            }
        }
        return analyses.sorted { $0.assetID < $1.assetID }
    }

    private static func analyzeWithFallback(
        asset: MediaAsset,
        analysisService: MediaAnalysisService,
        timeout: Duration
    ) async throws -> MediaAnalysis {
        do {
            return try await withThrowingTaskGroup(of: MediaAnalysis.self) { group in
                group.addTask {
                    try await analysisService.analyze(asset: asset)
                }
                group.addTask {
                    try await Task.sleep(for: timeout)
                    throw StoryCreationAnalysisTimeoutError.timedOut
                }

                do {
                    guard let analysis = try await group.next() else {
                        group.cancelAll()
                        return fallbackAnalysis(for: asset)
                    }
                    group.cancelAll()
                    return analysis
                } catch is CancellationError {
                    group.cancelAll()
                    throw CancellationError()
                } catch {
                    group.cancelAll()
                    storyCreationDebugLog("phase=analysis_fallback asset=\(asset.id) kind=\(asset.kind.rawValue) error=\(error.localizedDescription)")
                    return fallbackAnalysis(for: asset)
                }
            }
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            storyCreationDebugLog("phase=analysis_fallback asset=\(asset.id) kind=\(asset.kind.rawValue) error=\(error.localizedDescription)")
            return fallbackAnalysis(for: asset)
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
        storyCreationDebugLog("view model deinitialized instance=\(ObjectIdentifier(self))")
        creationTask?.cancel()
        thumbnailTasks.values.forEach { $0.cancel() }
    }
}
