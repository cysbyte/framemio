import Combine
import Foundation

@MainActor
final class StoryPreviewViewModel: ObservableObject {
    @Published private(set) var story: Story?
    @Published private(set) var isLoading = false
    @Published private(set) var activeAction: StoryPreviewAction?
    @Published var errorMessage: String?

    private let storyID: String
    private let storyService: StoryService
    private var loadTask: Task<Void, Never>?
    private var actionTask: Task<Void, Never>?

    init(storyID: String, storyService: StoryService) {
        self.storyID = storyID
        self.storyService = storyService
    }

    var isPerformingAction: Bool {
        activeAction != nil
    }

    var progressTitle: String {
        switch story?.status {
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
        switch story?.status {
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
            guard let self else { return }
            isLoading = true
            defer { isLoading = false }
            do {
                story = try await storyService.story(id: storyID)
                errorMessage = nil
            } catch is CancellationError {
            } catch {
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    func retryProcessing() {
        perform(.retry) { [storyService, storyID] in
            try await storyService.retryProcessing(id: storyID)
        }
    }

    func regenerateStory() {
        perform(.regenerate) { [storyService, storyID] in
            try await storyService.regenerateStory(id: storyID)
        }
    }

    func renderAgain() {
        perform(.renderAgain) { [storyService, storyID] in
            try await storyService.renderAgain(id: storyID)
        }
    }

    func replaceMoment(assetID: String) {
        perform(.replaceMoment) { [storyService, storyID] in
            try await storyService.replaceMoment(storyID: storyID, assetID: assetID)
        }
    }

    func removeMoment(assetID: String) {
        perform(.removeMoment) { [storyService, storyID] in
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
