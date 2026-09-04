import Foundation

final class RemoteStoryService: StoryService, @unchecked Sendable {
    private let apiClient: APIClient
    private let persistence: StoryPersistence
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder

    init(apiClient: APIClient, persistence: StoryPersistence) {
        self.apiClient = apiClient
        self.persistence = persistence
        encoder = FrameMioJSONCoding.apiEncoder()
        decoder = FrameMioJSONCoding.apiDecoder()
    }

    func cachedStories() async throws -> [Story] {
        try await persistence.loadStories()
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
            let appError = AppError.from(error)
            let cachedStories = try await persistence.loadStories()
            if (appError == .networkUnavailable || appError == .invalidConfiguration),
               let cachedStory = cachedStories.first(where: { $0.id == id }) {
                return cachedStory
            }
            throw appError
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
            for: APIRequest(
                method: .post,
                path: "/v1/stories",
                body: body,
                headers: ["Idempotency-Key": "ios-story-\(UUID().uuidString)"]
            )
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
        try await requestRender(id: id)
    }

    func regenerateStory(id: String) async throws -> Story {
        let data = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/v1/stories/\(id)/regenerate",
                headers: ["Idempotency-Key": "ios-story-regenerate-\(id)-\(UUID().uuidString)"]
            )
        )
        do {
            let updated = try decoder.decode(Story.self, from: data)
            let existing = try await persistence.loadStories()
            try await persistence.saveStories([updated] + existing.filter { $0.id != updated.id })
            return updated
        } catch {
            throw AppError.invalidResponse
        }
    }

    func renderAgain(id: String) async throws -> Story {
        try await requestRender(id: id)
    }

    func saveStoryEdits(storyID: String, request: StoryEditRequest) async throws -> Story {
        guard request.hasChanges else {
            return try await story(id: storyID)
        }
        let body: Data
        do {
            body = try encoder.encode(request)
        } catch {
            throw AppError.invalidResponse
        }
        let data = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/v1/stories/\(storyID)/versions",
                body: body,
                headers: ["Idempotency-Key": "ios-story-edit-\(storyID)-\(UUID().uuidString)"]
            )
        )
        do {
            let updated = try decoder.decode(Story.self, from: data)
            let existing = try await persistence.loadStories()
            try await persistence.saveStories([updated] + existing.filter { $0.id != updated.id })
            return updated
        } catch {
            throw AppError.invalidResponse
        }
    }

    func deleteStory(id: String) async throws {
        _ = try await apiClient.data(for: APIRequest(method: .delete, path: "/v1/stories/\(id)"))
        try await persistence.removeStory(id: id)
    }

    private func requestRender(id: String) async throws -> Story {
        let body: Data
        do {
            body = try encoder.encode(["output_preset": "social"])
        } catch {
            throw AppError.invalidResponse
        }
        _ = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/projects/\(id)/render",
                body: body,
                headers: ["Idempotency-Key": "ios-render-\(id)-\(UUID().uuidString)"]
            )
        )
        return try await story(id: id)
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


final class RemotePromptTranscriptionService: PromptTranscriptionService, @unchecked Sendable {
    private let apiClient: APIClient
    private let decoder: JSONDecoder

    init(apiClient: APIClient) {
        self.apiClient = apiClient
        decoder = FrameMioJSONCoding.apiDecoder()
    }

    func transcribePromptAudio(_ recording: PromptAudioRecording) async throws -> String {
        let data = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/v1/transcriptions",
                body: recording.data,
                headers: [
                    "Content-Type": recording.contentType,
                    "X-File-Name": recording.fileName
                ]
            )
        )
        do {
            let response = try decoder.decode(PromptTranscriptionResponse.self, from: data)
            return response.transcript
        } catch {
            throw AppError.invalidResponse
        }
    }
}

private struct PromptTranscriptionResponse: Decodable {
    let transcript: String
}

final class MockStoryService: StoryService, @unchecked Sendable {
    private let persistence: StoryPersistence

    init(persistence: StoryPersistence) {
        self.persistence = persistence
    }

    func cachedStories() async throws -> [Story] {
        try await persistence.loadStories()
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

    func saveStoryEdits(storyID: String, request: StoryEditRequest) async throws -> Story {
        try await mutateStory(id: storyID) { story in
            if !request.removeMediaIDs.isEmpty || !request.removeMomentIDs.isEmpty {
                let removed = Set(request.removeMediaIDs + request.removeMomentIDs)
                story.assetIDs.removeAll { removed.contains($0) }
                story.moments = story.moments?.filter { !removed.contains($0.id) && !removed.contains($0.mediaID) }
            }
            for mediaID in request.addMediaIDs where !story.assetIDs.contains(mediaID) {
                story.assetIDs.append(mediaID)
            }
            if let style = request.style, !style.isEmpty {
                story.description = "FrameMio is making this version feel \(style)."
            }
            if let targetDuration = request.targetDuration {
                story.duration = targetDuration
            }
            story.status = .analyzing
        }
    }

    func deleteStory(id: String) async throws {
        try await Task.sleep(for: .milliseconds(140))
        try await persistence.removeStory(id: id)
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

final class MockPromptTranscriptionService: PromptTranscriptionService, @unchecked Sendable {
    func transcribePromptAudio(_ recording: PromptAudioRecording) async throws -> String {
        try await Task.sleep(nanoseconds: 300_000_000)
        return "Make it warm and emotional, with a slower pace and more focus on smiling faces."
    }
}
