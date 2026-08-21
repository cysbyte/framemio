import Combine
import Foundation
import SwiftUI
import UIKit

enum HomeMockState: Hashable, Sendable {
    case loading
    case empty
    case populated
}

@MainActor
final class HomeViewModel: ObservableObject {
    @Published private(set) var stories: [Story] = []
    @Published private(set) var covers: [String: UIImage] = [:]
    @Published private(set) var isLoading = false
    @Published var errorMessage: String?

    private let storyService: StoryService
    private let mockState: HomeMockState?
    private var loadTask: Task<Void, Never>?

    init(storyService: StoryService, mockState: HomeMockState? = nil) {
        self.storyService = storyService
        self.mockState = mockState
    }

    func load() {
        loadTask?.cancel()
        loadTask = Task { [weak self] in
            guard let self else { return }
            isLoading = true
            defer { isLoading = false }

            do {
                if let mockState {
                    try await loadMockData(for: mockState)
                } else {
                    stories = try await storyService.listStories()
                    covers = [:]
                }
                errorMessage = nil
            } catch is CancellationError {
            } catch {
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    func coverImage(for story: Story) -> Image? {
        guard let image = covers[story.id] else { return nil }
        return Image(uiImage: image)
    }

    private func loadMockData(for state: HomeMockState) async throws {
        let delay: Duration = state == .loading ? .milliseconds(1_000) : .milliseconds(320)
        try await Task.sleep(for: delay)
        try Task.checkCancellation()

        switch state {
        case .loading, .populated:
            stories = Self.mockStories
            covers = Dictionary(uniqueKeysWithValues: stories.enumerated().map { index, story in
                (story.id, Self.makeCover(index: index))
            })
        case .empty:
            stories = []
            covers = [:]
        }
    }

    private static let mockStories: [Story] = [
        Story(
            id: "mock-coastline",
            title: "An afternoon by the sea",
            context: .trip,
            createdAt: Calendar.current.date(byAdding: .day, value: -2, to: Date()) ?? Date(),
            duration: 42,
            assetIDs: Array(0..<9).map { "coastline-\($0)" },
            status: .ready,
            videoURL: nil
        ),
        Story(
            id: "mock-together",
            title: "Summer, together",
            context: .friends,
            createdAt: Calendar.current.date(byAdding: .day, value: -6, to: Date()) ?? Date(),
            duration: 36,
            assetIDs: Array(0..<7).map { "together-\($0)" },
            status: .ready,
            videoURL: nil
        ),
        Story(
            id: "mock-little-things",
            title: "The little things",
            context: .family,
            createdAt: Calendar.current.date(byAdding: .day, value: -11, to: Date()) ?? Date(),
            duration: 29,
            assetIDs: Array(0..<6).map { "little-things-\($0)" },
            status: .ready,
            videoURL: nil
        )
    ]

    private static func makeCover(index: Int) -> UIImage {
        let size = CGSize(width: 900, height: 560)
        let renderer = UIGraphicsImageRenderer(size: size)
        return renderer.image { context in
            let colors: [(UIColor, UIColor)] = [
                (UIColor(red: 0.19, green: 0.32, blue: 0.38, alpha: 1), UIColor(red: 0.82, green: 0.58, blue: 0.38, alpha: 1)),
                (UIColor(red: 0.52, green: 0.28, blue: 0.24, alpha: 1), UIColor(red: 0.94, green: 0.72, blue: 0.47, alpha: 1)),
                (UIColor(red: 0.26, green: 0.34, blue: 0.24, alpha: 1), UIColor(red: 0.77, green: 0.64, blue: 0.43, alpha: 1))
            ]
            let palette = colors[index % colors.count]
            guard let gradient = CGGradient(
                colorsSpace: CGColorSpaceCreateDeviceRGB(),
                colors: [palette.0.cgColor, palette.1.cgColor] as CFArray,
                locations: [0, 1]
            ) else {
                UIColor.secondarySystemBackground.setFill()
                context.fill(CGRect(origin: .zero, size: size))
                return
            }
            context.cgContext.drawLinearGradient(
                gradient,
                start: CGPoint(x: 0, y: 0),
                end: CGPoint(x: size.width, y: size.height),
                options: []
            )

            let horizon = CGRect(x: 0, y: size.height * 0.62, width: size.width, height: size.height * 0.38)
            UIColor.black.withAlphaComponent(0.16).setFill()
            context.fill(horizon)

            let sunSize = size.width * 0.18
            let sun = CGRect(
                x: size.width * (0.18 + CGFloat(index) * 0.17),
                y: size.height * 0.20,
                width: sunSize,
                height: sunSize
            )
            UIColor.white.withAlphaComponent(0.42).setFill()
            context.cgContext.fillEllipse(in: sun)
        }
    }

    deinit {
        loadTask?.cancel()
    }
}
