import Combine
import Foundation

@MainActor
final class StoryPreviewViewModel: ObservableObject {
    @Published private(set) var story: Story?
    @Published private(set) var isLoading = false
    @Published private(set) var activeAction: StoryPreviewAction?
    @Published var errorMessage: String?
    @Published var isPlayingVideo = false

    private var storyID: String?
    private let launchContext: MomentContext?
    private let launchAssets: [MediaAsset]
    private let launchID: UUID?
    private let storyLaunchCoordinator: StoryLaunchCoordinator?
    private let storyService: StoryService
    private var loadTask: Task<Void, Never>?
    private var actionTask: Task<Void, Never>?
    private var launchCancellable: AnyCancellable?

    init(storyID: String, storyService: StoryService) {
        self.storyID = storyID
        launchContext = nil
        launchAssets = []
        launchID = nil
        storyLaunchCoordinator = nil
        self.storyService = storyService
    }

    init(
        context: MomentContext,
        assets: [MediaAsset],
        launchID: UUID,
        storyLaunchCoordinator: StoryLaunchCoordinator,
        storyService: StoryService
    ) {
        storyID = nil
        launchContext = context
        launchAssets = assets
        self.launchID = launchID
        self.storyLaunchCoordinator = storyLaunchCoordinator
        self.storyService = storyService
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

    var canPlayVideo: Bool {
        playableVideoURL != nil
    }

    func playVideo() {
        guard canPlayVideo else { return }
        isPlayingVideo = true
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
                errorMessage = nil
                isLoading = false

                guard latest.status.shouldPoll else { return }
                try await Task.sleep(nanoseconds: 5_000_000_000)
            } catch is CancellationError {
                return
            } catch {
                errorMessage = AppError.from(error).localizedDescription
                return
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

    func replaceMoment(assetID: String) {
        guard let storyID else { return }
        perform(.replaceMoment) { [storyService] in
            try await storyService.replaceMoment(storyID: storyID, assetID: assetID)
        }
    }

    func removeMoment(assetID: String) {
        guard let storyID else { return }
        perform(.removeMoment) { [storyService] in
            try await storyService.removeMoment(storyID: storyID, assetID: assetID)
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
                story = try await operation()
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
    }
}

enum StoryPreviewAction: Hashable, Sendable {
    case retry
    case regenerate
    case renderAgain
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
        case .replaceMoment:
            "Replacing moment"
        case .removeMoment:
            "Removing moment"
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
