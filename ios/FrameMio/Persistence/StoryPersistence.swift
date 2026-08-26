import Foundation

actor UserDefaultsStoryPersistence: StoryPersistence {
    private let defaults: UserDefaults
    private let storageKey = "framemio.stories"
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()

    init() {
        self.defaults = .standard
        encoder.dateEncodingStrategy = .iso8601
        decoder.dateDecodingStrategy = .iso8601
    }

    init(defaults: UserDefaults) {
        self.defaults = defaults
        encoder.dateEncodingStrategy = .iso8601
        decoder.dateDecodingStrategy = .iso8601
    }

    func loadStories() async throws -> [Story] {
        guard let data = defaults.data(forKey: storageKey) else {
            return []
        }
        do {
            return try decoder.decode([Story].self, from: data)
        } catch {
            throw AppError.persistenceFailure
        }
    }

    func saveStories(_ stories: [Story]) async throws {
        do {
            defaults.set(try encoder.encode(stories), forKey: storageKey)
        } catch {
            throw AppError.persistenceFailure
        }
    }

    func removeStory(id: String) async throws {
        let stories = try await loadStories().filter { $0.id != id }
        try await saveStories(stories)
    }
}

actor MockStoryPersistence: StoryPersistence {
    private var stories: [Story] = []

    func loadStories() async throws -> [Story] {
        stories
    }

    func saveStories(_ stories: [Story]) async throws {
        self.stories = stories
    }

    func removeStory(id: String) async throws {
        stories.removeAll { $0.id == id }
    }
}
