import Combine
import Foundation

@MainActor
final class StoryCreationViewModel: ObservableObject {
    @Published private(set) var progress: Double = 0
    @Published private(set) var phaseTitle = "Preparing your memories..."
    @Published private(set) var isCreating = false
    @Published private(set) var story: Story?
    @Published var errorMessage: String?

    let context: MomentContext
    let assets: [MediaAsset]
    private let analysisService: MediaAnalysisService
    private let mediaUploadService: MediaUploadService
    private let storyService: StoryService
    private var creationTask: Task<Void, Never>?

    init(
        context: MomentContext,
        assets: [MediaAsset],
        analysisService: MediaAnalysisService,
        mediaUploadService: MediaUploadService,
        storyService: StoryService
    ) {
        self.context = context
        self.assets = assets
        self.analysisService = analysisService
        self.mediaUploadService = mediaUploadService
        self.storyService = storyService
    }

    func createStory() {
        guard !isCreating else { return }
        creationTask?.cancel()
        creationTask = Task { [weak self] in
            guard let self else { return }
            isCreating = true
            progress = 0
            story = nil
            errorMessage = nil
            defer { isCreating = false }

            do {
                phaseTitle = "Looking through your videos..."
                let selection = MediaSelection(explicitlySelectedAssetIDs: assets.map(\.id))
                _ = try await mediaUploadService.upload(
                    selection: selection,
                    assets: assets,
                    options: MediaUploadOptions(),
                    progress: { [weak self] uploadProgress in
                        Task { @MainActor [weak self] in
                            guard let self else { return }
                            progress = min(
                                0.28,
                                uploadProgress.fractionCompleted * 0.28
                            )
                        }
                    }
                )
                try Task.checkCancellation()
                phaseTitle = "Finding meaningful moments..."
                let analyses = try await analyzeAssets()
                try Task.checkCancellation()
                phaseTitle = "Building your story..."
                progress = max(progress, 0.86)
                story = try await storyService.createStory(
                    request: CreateStoryRequest(context: context, assets: assets, analyses: analyses)
                )
                progress = 1
                phaseTitle = "Ready to watch."
                FrameMioHaptics.success()
            } catch is CancellationError {
                phaseTitle = "Preparing your memories..."
            } catch {
                errorMessage = AppError.from(error).localizedDescription
                phaseTitle = "Something went wrong"
            }
        }
    }

    func cancel() {
        creationTask?.cancel()
    }

    private func analyzeAssets() async throws -> [MediaAnalysis] {
        guard !assets.isEmpty else { throw AppError.unsupportedMedia }
        var analyses: [MediaAnalysis] = []
        analyses.reserveCapacity(assets.count)

        try await withThrowingTaskGroup(of: MediaAnalysis.self) { group in
            for asset in assets {
                group.addTask { [analysisService] in
                    try Task.checkCancellation()
                    return try await analysisService.analyze(asset: asset)
                }
            }

            for try await analysis in group {
                analyses.append(analysis)
                progress = min(0.82, Double(analyses.count) / Double(assets.count) * 0.82)
            }
        }
        return analyses.sorted { $0.assetID < $1.assetID }
    }

    deinit {
        creationTask?.cancel()
    }
}
