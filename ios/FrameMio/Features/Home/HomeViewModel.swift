import AVFoundation
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
    @Published private(set) var allStories: [Story] = []
    @Published private(set) var covers: [String: UIImage] = [:]
    @Published private(set) var loadingCoverIDs: Set<String> = []
    @Published private(set) var isLoading = false
    @Published private(set) var creditBalance: Int?
    @Published private(set) var isLoadingCreditBalance = false
    @Published var errorMessage: String?

    private static let homeStoryLimit = 7
    private static let coverCacheDirectoryName = "StoryCovers"

    private let storyService: StoryService
    private let photoLibrary: PhotoLibraryService
    private let creditService: CreditService?
    private let mockState: HomeMockState?
    private var loadTask: Task<Void, Never>?
    private var creditTask: Task<Void, Never>?
    private var coverTasks: [String: Task<Void, Never>] = [:]

    init(
        storyService: StoryService,
        photoLibrary: PhotoLibraryService,
        creditService: CreditService? = nil,
        mockState: HomeMockState? = nil
    ) {
        self.storyService = storyService
        self.photoLibrary = photoLibrary
        self.creditService = creditService
        self.mockState = mockState
    }

    func load() {
        guard loadTask == nil else { return }
        loadTask?.cancel()
        loadTask = Task { [weak self] in
            guard let self else { return }
            isLoading = true
            defer {
                isLoading = false
                loadTask = nil
            }

            do {
                if let mockState {
                    try await loadMockData(for: mockState)
                } else {
                    let cachedStories = sortedStories(try await storyService.cachedStories())
                    if !cachedStories.isEmpty {
                        updateStories(cachedStories)
                    }

                    let loadedStories = sortedStories(try await storyService.listStories())
                    updateStories(loadedStories)
                }
                errorMessage = nil
            } catch is CancellationError {
            } catch {
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }


    func refreshCreditBalance() {
        guard let creditService else { return }
        creditTask?.cancel()
        creditTask = Task { [weak self] in
            guard let self else { return }
            isLoadingCreditBalance = true
            defer { isLoadingCreditBalance = false }
            do {
                let balance = try await creditService.balance()
                creditBalance = balance.displayCredits
            } catch is CancellationError {
            } catch {
            }
        }
    }

    var creditBalanceTitle: String {
        guard let creditBalance else {
            return isLoadingCreditBalance ? "✦ …" : "✦"
        }
        return "✦ \(creditBalance.formatted())"
    }

    var creditBalanceAccessibilityLabel: String {
        guard let creditBalance else {
            return "Credits. Opens FrameMio Pro."
        }
        return "\(creditBalance) credits. Opens FrameMio Pro."
    }

    var isLowOnCredits: Bool {
        guard let creditBalance else { return false }
        return creditBalance <= CreditPricing.lowBalanceThreshold
    }

    func coverImage(for story: Story) -> Image? {
        guard let image = covers[story.id] else { return nil }
        return Image(uiImage: image)
    }

    func isCoverLoading(for story: Story) -> Bool {
        loadingCoverIDs.contains(story.id)
    }

    func loadCovers(for stories: [Story]) {
        loadGeneratedVideoCovers(for: stories)
    }

    private func updateStories(_ loadedStories: [Story]) {
        allStories = loadedStories
        stories = latestHomeStories(loadedStories)
        loadGeneratedVideoCovers(for: stories)
    }

    private func sortedStories(_ stories: [Story]) -> [Story] {
        stories.sorted { $0.lastModifiedAt > $1.lastModifiedAt }
    }

    private func latestHomeStories(_ stories: [Story]) -> [Story] {
        Array(sortedStories(stories).prefix(Self.homeStoryLimit))
    }

    private func loadGeneratedVideoCovers(for stories: [Story]) {
        let storyIDs = Set(allStories.map(\.id))
        covers = covers.filter { storyIDs.isEmpty || storyIDs.contains($0.key) }
        loadingCoverIDs = loadingCoverIDs.filter { storyIDs.isEmpty || storyIDs.contains($0) }
        let staleCoverTaskIDs = coverTasks.keys.filter { !storyIDs.isEmpty && !storyIDs.contains($0) }
        for storyID in staleCoverTaskIDs {
            coverTasks[storyID]?.cancel()
            coverTasks[storyID] = nil
            loadingCoverIDs.remove(storyID)
        }
        if !storyIDs.isEmpty {
            Self.removeCachedCovers(excludingStoryIDs: storyIDs)
        }

        for story in stories where story.status == .ready && covers[story.id] == nil && coverTasks[story.id] == nil {
            guard let videoURL = story.videoURL else { continue }
            let storyID = story.id

            let source = story.projectType == .reel ? thumbnailSource(for: story) : nil
            let sourceCacheKey = source.map { thumbnailCacheKey(for: $0) }
            if let sourceCacheKey,
               let cachedImage = Self.cachedSourceCover(storyID: storyID, sourceKey: sourceCacheKey) {
                covers[storyID] = cachedImage
                loadingCoverIDs.remove(storyID)
                continue
            }
            if source == nil,
               let cachedImage = Self.cachedVideoCover(storyID: storyID, videoURL: videoURL) {
                covers[storyID] = cachedImage
                loadingCoverIDs.remove(storyID)
                continue
            }

            loadingCoverIDs.insert(storyID)
            coverTasks[storyID] = Task { [weak self, photoLibrary] in
                let sourceImage: UIImage?
                if let source {
                    sourceImage = await Self.generateSourceCover(
                        from: source,
                        photoLibrary: photoLibrary
                    )
                } else {
                    sourceImage = nil
                }
                let image: UIImage?
                let usedSourceCover: Bool
                if let sourceImage {
                    image = sourceImage
                    usedSourceCover = true
                } else {
                    image = await Self.generateVideoCover(from: videoURL)
                    usedSourceCover = false
                }
                guard !Task.isCancelled else { return }
                self?.finishGeneratedVideoCover(
                    storyID: storyID,
                    videoURL: videoURL,
                    sourceKey: usedSourceCover ? sourceCacheKey : nil,
                    image: image
                )
            }
        }
    }

    private func finishGeneratedVideoCover(
        storyID: String,
        videoURL: URL,
        sourceKey: String?,
        image: UIImage?
    ) {
        coverTasks[storyID] = nil
        loadingCoverIDs.remove(storyID)
        guard allStories.contains(where: { $0.id == storyID }) else { return }
        if let image {
            covers[storyID] = image
            if let sourceKey {
                Self.saveSourceCover(image, storyID: storyID, sourceKey: sourceKey)
            } else {
                Self.saveVideoCover(image, storyID: storyID, videoURL: videoURL)
            }
        }
    }

    private func thumbnailSource(for story: Story) -> StoryMoment? {
        let moments = story.moments ?? []
        return moments.first(where: { moment in
            moment.type == .photo || moment.type == .livePhoto
        }) ?? moments.first
    }

    private func thumbnailCacheKey(for moment: StoryMoment) -> String {
        let sourceID = moment.sourceAssetID?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return sourceID.isEmpty ? moment.mediaID : sourceID
    }

    private nonisolated static func generateSourceCover(
        from moment: StoryMoment,
        photoLibrary: PhotoLibraryService
    ) async -> UIImage? {
        let sourceValue = moment.sourceAssetID?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let sourceID = sourceValue.isEmpty ? moment.mediaID : sourceValue
        guard !sourceID.isEmpty else { return nil }

        do {
            let assets = try await photoLibrary.resolveAssets(localIdentifiers: [sourceID])
            guard let asset = assets.first else { return nil }
            let data = try await photoLibrary.requestThumbnail(
                for: asset,
                targetSize: CGSize(width: 900, height: 560)
            )
            return UIImage(data: data)
        } catch is CancellationError {
            return nil
        } catch {
            return nil
        }
    }

    private nonisolated static func cachedVideoCover(storyID: String, videoURL: URL) -> UIImage? {
        let fileURL = coverCacheFileURL(storyID: storyID, videoURL: videoURL)
        guard let data = try? Data(contentsOf: fileURL) else { return nil }
        return UIImage(data: data)
    }

    private nonisolated static func cachedSourceCover(storyID: String, sourceKey: String) -> UIImage? {
        let fileURL = sourceCoverCacheFileURL(storyID: storyID, sourceKey: sourceKey)
        guard let data = try? Data(contentsOf: fileURL) else { return nil }
        return UIImage(data: data)
    }

    private nonisolated static func saveVideoCover(_ image: UIImage, storyID: String, videoURL: URL) {
        guard let data = image.jpegData(compressionQuality: 0.82) else { return }
        let directory = coverCacheDirectory
        do {
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            try data.write(to: coverCacheFileURL(storyID: storyID, videoURL: videoURL), options: .atomic)
        } catch {
        }
    }

    private nonisolated static func saveSourceCover(_ image: UIImage, storyID: String, sourceKey: String) {
        guard let data = image.jpegData(compressionQuality: 0.82) else { return }
        let directory = coverCacheDirectory
        do {
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            try data.write(
                to: sourceCoverCacheFileURL(storyID: storyID, sourceKey: sourceKey),
                options: .atomic
            )
        } catch {
        }
    }

    private nonisolated static func removeCachedCovers(excludingStoryIDs validStoryIDs: Set<String>) {
        let directory = coverCacheDirectory
        guard let files = try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil) else { return }
        let validPrefixes = Set(validStoryIDs.map { safeCacheComponent($0) + "-" })
        for file in files where !validPrefixes.contains(where: { file.lastPathComponent.hasPrefix($0) }) {
            try? FileManager.default.removeItem(at: file)
        }
    }

    private nonisolated static var coverCacheDirectory: URL {
        let baseURL = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first
            ?? FileManager.default.temporaryDirectory
        return baseURL.appendingPathComponent(coverCacheDirectoryName, isDirectory: true)
    }

    private nonisolated static func coverCacheFileURL(storyID: String, videoURL: URL) -> URL {
        let artifactKey = videoURL.path.isEmpty ? videoURL.absoluteString : videoURL.path
        return coverCacheFileURL(storyID: storyID, cacheKey: "video-\(artifactKey)")
    }

    private nonisolated static func sourceCoverCacheFileURL(storyID: String, sourceKey: String) -> URL {
        coverCacheFileURL(storyID: storyID, cacheKey: "source-\(sourceKey)")
    }

    private nonisolated static func coverCacheFileURL(storyID: String, cacheKey: String) -> URL {
        let fileName = "\(safeCacheComponent(storyID))-\(fnv1a64Hex(cacheKey)).jpg"
        return coverCacheDirectory.appendingPathComponent(fileName, isDirectory: false)
    }

    private nonisolated static func safeCacheComponent(_ value: String) -> String {
        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "-_"))
        let scalars = value.unicodeScalars.map { allowed.contains($0) ? Character($0) : "_" }
        let sanitized = String(scalars)
        return sanitized.isEmpty ? "story" : sanitized
    }

    private nonisolated static func fnv1a64Hex(_ value: String) -> String {
        var hash: UInt64 = 0xcbf29ce484222325
        for byte in value.utf8 {
            hash ^= UInt64(byte)
            hash &*= 0x100000001b3
        }
        return String(hash, radix: 16)
    }

    private nonisolated static func generateVideoCover(from url: URL) async -> UIImage? {
        await Task.detached(priority: .utility) {
            let asset = AVURLAsset(url: url)

            do {
                // Remote presigned assets are not ready for synchronous frame extraction.
                // Load their metadata first so AVFoundation can issue the required range
                // requests before asking for a frame.
                let duration = try await asset.load(.duration)
                let tracks = try await asset.load(.tracks)
                guard tracks.contains(where: { $0.mediaType == .video }) else {
                    return nil
                }

                let generator = AVAssetImageGenerator(asset: asset)
                generator.appliesPreferredTrackTransform = true
                generator.maximumSize = CGSize(width: 900, height: 560)

                let durationSeconds = duration.seconds
                let candidateSeconds: [Double]
                if durationSeconds.isFinite, durationSeconds > 0 {
                    candidateSeconds = [0.2, 0, 1.0]
                        .filter { $0 < durationSeconds || $0 == 0 }
                } else {
                    candidateSeconds = [0.2, 0, 1.0]
                }

                for seconds in candidateSeconds {
                    try Task.checkCancellation()
                    do {
                        let time = CMTime(seconds: seconds, preferredTimescale: 600)
                        let result = try await generator.image(at: time)
                        return UIImage(cgImage: result.image)
                    } catch {
                        continue
                    }
                }
            } catch is CancellationError {
                return nil
            } catch {
                return nil
            }

            return nil
        }.value
    }

    deinit {
        loadTask?.cancel()
        coverTasks.values.forEach { $0.cancel() }
    }

    private func loadMockData(for state: HomeMockState) async throws {
        let delay: Duration = state == .loading ? .milliseconds(1_000) : .milliseconds(320)
        try await Task.sleep(for: delay)
        try Task.checkCancellation()

        switch state {
        case .loading, .populated:
            updateStories(Self.mockStories)
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
            type: .story,
            title: "Kyoto Trip",
            context: .trip,
            createdAt: Calendar.current.date(byAdding: .day, value: -2, to: Date()) ?? Date(),
            updatedAt: Calendar.current.date(byAdding: .hour, value: -8, to: Date()) ?? Date(),
            duration: 42,
            assetIDs: Array(0..<9).map { "coastline-\($0)" },
            status: .ready,
            videoURL: nil
        ),
        Story(
            id: "mock-together",
            type: .promo,
            title: "FrameMio Launch",
            context: .specialMoments,
            createdAt: Calendar.current.date(byAdding: .day, value: -6, to: Date()) ?? Date(),
            updatedAt: Calendar.current.date(byAdding: .day, value: -1, to: Date()) ?? Date(),
            duration: 36,
            assetIDs: Array(0..<7).map { "together-\($0)" },
            status: .rendering,
            videoURL: nil
        ),
        Story(
            id: "mock-little-things",
            type: .reel,
            title: "Weekend Walk",
            context: .weekend,
            createdAt: Calendar.current.date(byAdding: .day, value: -11, to: Date()) ?? Date(),
            updatedAt: Calendar.current.date(byAdding: .day, value: -3, to: Date()) ?? Date(),
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
}
