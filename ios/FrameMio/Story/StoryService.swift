import Foundation

final class RemoteStoryService: StoryService, @unchecked Sendable {
    private let apiClient: APIClient
    private let persistence: StoryPersistence
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()

    init(apiClient: APIClient, persistence: StoryPersistence) {
        self.apiClient = apiClient
        self.persistence = persistence
        encoder.dateEncodingStrategy = .iso8601
        decoder.dateDecodingStrategy = .iso8601
    }

    func listStories() async throws -> [Story] {
        do {
            let data = try await apiClient.data(for: APIRequest(method: .get, path: "/v1/stories"))
            let stories = try decoder.decode([Story].self, from: data)
            try await persistence.saveStories(stories)
            return stories
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            let cachedStories = try await persistence.loadStories()
            let appError = AppError.from(error)
            if !cachedStories.isEmpty || appError == .networkUnavailable || appError == .invalidConfiguration {
                return cachedStories
            }
            throw appError
        }
    }

    func story(id: String) async throws -> Story {
        do {
            let data = try await apiClient.data(for: APIRequest(method: .get, path: "/v1/stories/\(id)"))
            return try decoder.decode(Story.self, from: data)
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            let cachedStories = try await persistence.loadStories()
            guard let cachedStory = cachedStories.first(where: { $0.id == id }) else {
                throw AppError.from(error)
            }
            return cachedStory
        }
    }

    func createStory(request: CreateStoryRequest) async throws -> Story {
        let body: Data
        do {
            body = try encoder.encode(request)
        } catch {
            throw AppError.invalidResponse
        }

        let data = try await apiClient.data(
            for: APIRequest(method: .post, path: "/v1/stories", body: body)
        )
        do {
            let story = try decoder.decode(Story.self, from: data)
            let existing = try await persistence.loadStories()
            try await persistence.saveStories([story] + existing.filter { $0.id != story.id })
            return story
        } catch {
            throw AppError.invalidResponse
        }
    }

    func retryProcessing(id: String) async throws -> Story {
        try await updateCachedStory(id: id) { story in
            story.status = .queued
        }
    }

    func regenerateStory(id: String) async throws -> Story {
        try await updateCachedStory(id: id) { story in
            story.status = .analyzing
        }
    }

    func renderAgain(id: String) async throws -> Story {
        try await updateCachedStory(id: id) { story in
            story.status = .rendering
        }
    }

    func replaceMoment(storyID: String, assetID: String) async throws -> Story {
        try await updateCachedStory(id: storyID) { story in
            if let firstAssetID = story.assetIDs.first {
                story.assetIDs = story.assetIDs.map { $0 == firstAssetID ? assetID : $0 }
            } else {
                story.assetIDs = [assetID]
            }
            story.assetIDs = deduplicated(story.assetIDs)
            story.status = .queued
        }
    }

    func removeMoment(storyID: String, assetID: String) async throws -> Story {
        try await updateCachedStory(id: storyID) { story in
            story.assetIDs.removeAll { $0 == assetID }
            story.duration = max(8, Double(max(story.assetIDs.count, 1)) * 4)
            story.status = story.assetIDs.isEmpty ? .failed : .queued
        }
    }

    private func updateCachedStory(id: String, transform: (inout Story) -> Void) async throws -> Story {
        let existing = try await persistence.loadStories()
        guard let story = existing.first(where: { $0.id == id }) else {
            throw AppError.invalidResponse
        }
        var updated = story
        transform(&updated)
        try await persistence.saveStories([updated] + existing.filter { $0.id != id })
        return updated
    }

    private func deduplicated(_ assetIDs: [String]) -> [String] {
        var seen = Set<String>()
        return assetIDs.filter { !$0.isEmpty && seen.insert($0).inserted }
    }
}

final class MockStoryService: StoryService, @unchecked Sendable {
    private let persistence: StoryPersistence

    init(persistence: StoryPersistence) {
        self.persistence = persistence
    }

    func listStories() async throws -> [Story] {
        try await Task.sleep(for: .milliseconds(180))
        let existing = try await persistence.loadStories()
        if !existing.isEmpty {
            return existing
        }

        let sample = Story(
            id: "preview-story",
            title: "Sunday at home",
            context: .family,
            createdAt: Date(),
            duration: 48,
            assetIDs: [],
            status: .ready,
            videoURL: nil
        )
        try await persistence.saveStories([sample])
        return [sample]
    }

    func story(id: String) async throws -> Story {
        try await Task.sleep(for: .milliseconds(120))
        guard let story = try await listStories().first(where: { $0.id == id }) else {
            throw AppError.invalidResponse
        }
        return story
    }

    func createStory(request: CreateStoryRequest) async throws -> Story {
        try await Task.sleep(for: .milliseconds(240))
        let story = Story(
            id: UUID().uuidString,
            title: request.context.rawValue,
            context: request.context,
            createdAt: Date(),
            duration: max(12, Double(request.assets.count) * 4),
            assetIDs: request.assets.map(\.id),
            status: .ready,
            videoURL: nil
        )
        let existing = try await persistence.loadStories()
        try await persistence.saveStories([story] + existing)
        return story
    }

    func retryProcessing(id: String) async throws -> Story {
        try await mutateStory(id: id) { story in
            story.status = .queued
        }
    }

    func regenerateStory(id: String) async throws -> Story {
        try await mutateStory(id: id) { story in
            story.status = .analyzing
            story.title = "Your moments, revisited"
        }
    }

    func renderAgain(id: String) async throws -> Story {
        try await mutateStory(id: id) { story in
            story.status = .rendering
        }
    }

    func replaceMoment(storyID: String, assetID: String) async throws -> Story {
        try await mutateStory(id: storyID) { story in
            if story.assetIDs.isEmpty {
                story.assetIDs = [assetID]
            } else {
                story.assetIDs[0] = assetID
            }
            story.assetIDs = deduplicated(story.assetIDs)
            story.status = .queued
        }
    }

    func removeMoment(storyID: String, assetID: String) async throws -> Story {
        try await mutateStory(id: storyID) { story in
            story.assetIDs.removeAll { $0 == assetID }
            story.duration = max(8, Double(max(story.assetIDs.count, 1)) * 4)
            story.status = story.assetIDs.isEmpty ? .failed : .queued
        }
    }

    private func mutateStory(id: String, transform: (inout Story) -> Void) async throws -> Story {
        let existing = try await persistence.loadStories()
        guard let story = existing.first(where: { $0.id == id }) else {
            throw AppError.invalidResponse
        }
        try await Task.sleep(for: .milliseconds(140))
        var updated = story
        transform(&updated)
        try await persistence.saveStories([updated] + existing.filter { $0.id != id })
        return updated
    }

    private func deduplicated(_ assetIDs: [String]) -> [String] {
        var seen = Set<String>()
        return assetIDs.filter { !$0.isEmpty && seen.insert($0).inserted }
    }
}
