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

actor UserDefaultsTextVideoPersistence: TextVideoPersistence {
    private let defaults: UserDefaults
    private let storageKey = "framemio.text-video-generations"
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder

    init() {
        self.defaults = .standard
        self.encoder = FrameMioJSONCoding.apiEncoder()
        self.decoder = FrameMioJSONCoding.apiDecoder()
    }

    init(defaults: UserDefaults) {
        self.defaults = defaults
        self.encoder = FrameMioJSONCoding.apiEncoder()
        self.decoder = FrameMioJSONCoding.apiDecoder()
    }

    func loadGenerations() async throws -> [TextVideoGeneration] {
        guard let data = defaults.data(forKey: storageKey) else {
            return []
        }
        do {
            return try decoder.decode([TextVideoGeneration].self, from: data)
        } catch {
            throw AppError.persistenceFailure
        }
    }

    func saveGenerations(_ generations: [TextVideoGeneration]) async throws {
        do {
            defaults.set(try encoder.encode(generations), forKey: storageKey)
        } catch {
            throw AppError.persistenceFailure
        }
    }

    func removeGeneration(id: String) async throws {
        let generations = try await loadGenerations().filter { $0.id != id }
        try await saveGenerations(generations)
    }
}

actor MockTextVideoPersistence: TextVideoPersistence {
    private var generations: [TextVideoGeneration] = []

    func loadGenerations() async throws -> [TextVideoGeneration] {
        generations
    }

    func saveGenerations(_ generations: [TextVideoGeneration]) async throws {
        self.generations = generations
    }

    func removeGeneration(id: String) async throws {
        generations.removeAll { $0.id == id }
    }
}
