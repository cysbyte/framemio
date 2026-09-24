import Foundation

final class RemoteTextToVideoService: TextToVideoService, @unchecked Sendable {
    private let apiClient: APIClient
    private let persistence: TextVideoPersistence
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder

    init(apiClient: APIClient, persistence: TextVideoPersistence) {
        self.apiClient = apiClient
        self.persistence = persistence
        self.encoder = FrameMioJSONCoding.apiEncoder()
        self.decoder = FrameMioJSONCoding.apiDecoder()
    }

    func cachedGenerations() async throws -> [TextVideoGeneration] {
        try await persistence.loadGenerations()
    }

    func sourceImageURL(for mediaID: String) async throws -> URL {
        struct Response: Decodable {
            let url: URL
        }

        let data = try await apiClient.data(
            for: APIRequest(
                method: .get,
                path: "/v1/media/\(Self.percentEncodedPathComponent(mediaID))/content"
            )
        )
        do {
            return try decoder.decode(Response.self, from: data).url
        } catch {
            throw AppError.invalidResponse
        }
    }

    func create(
        prompt: String,
        duration: Int,
        resolution: String,
        aspectRatio: String,
        idempotencyKey: String,
        imageMediaID: String?
    ) async throws -> TextVideoGeneration {
        struct Request: Encodable {
            let prompt: String
            let seconds: Int
            let resolution: String
            let aspectRatio: String
            let imageMediaID: String?

            enum CodingKeys: String, CodingKey {
                case prompt
                case seconds
                case resolution
                case aspectRatio = "aspect_ratio"
                case imageMediaID = "image_media_id"
            }
        }

        let body = try encoder.encode(
            Request(
                prompt: normalizedPrompt(prompt, imageMediaID: imageMediaID),
                seconds: duration,
                resolution: resolution,
                aspectRatio: aspectRatio,
                imageMediaID: imageMediaID
            )
        )
        let data = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/v1/text-videos",
                body: body,
                headers: ["Idempotency-Key": idempotencyKey]
            )
        )
        do {
            let generation = try decoder.decode(TextVideoGeneration.self, from: data)
            let existing = try await persistence.loadGenerations()
            try await persistence.saveGenerations(
                [generation] + existing.filter { $0.id != generation.id }
            )
            return generation
        } catch {
            if error is AppError {
                throw error
            }
            throw AppError.invalidResponse
        }
    }

    private func normalizedPrompt(_ prompt: String, imageMediaID: String?) -> String {
        let trimmedPrompt = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmedPrompt.isEmpty, imageMediaID != nil else {
            return trimmedPrompt
        }
        return TextVideoPromptDefaults.imageToVideo
    }

    func list() async throws -> [TextVideoGeneration] {
        do {
            let data = try await apiClient.data(
                for: APIRequest(method: .get, path: "/v1/text-videos")
            )
            let generations = try decoder.decode([TextVideoGeneration].self, from: data)
            let cachedGenerations = try await persistence.loadGenerations()
            if generations.isEmpty, !cachedGenerations.isEmpty {
                return cachedGenerations
            }
            try await persistence.saveGenerations(generations)
            return generations
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            let cachedGenerations = try await persistence.loadGenerations()
            let appError = AppError.from(error)
            if !cachedGenerations.isEmpty || appError == .networkUnavailable || appError == .invalidConfiguration {
                return cachedGenerations
            }
            throw appError
        }
    }

    func downloadContent(for generationID: String) async throws -> URL {
        let data = try await apiClient.data(
            for: APIRequest(
                method: .get,
                path: "/v1/text-videos/\(Self.percentEncodedPathComponent(generationID))/content"
            )
        )
        let destination = FileManager.default.temporaryDirectory
            .appendingPathComponent("framemio-text-video-\(generationID)-\(UUID().uuidString)")
            .appendingPathExtension("mp4")
        do {
            try data.write(to: destination, options: .atomic)
            return destination
        } catch {
            throw AppError.persistenceFailure
        }
    }

    func delete(generationID: String) async throws {
        _ = try await apiClient.data(
            for: APIRequest(
                method: .delete,
                path: "/v1/text-videos/\(Self.percentEncodedPathComponent(generationID))"
            )
        )
        try await persistence.removeGeneration(id: generationID)
    }

    private static func percentEncodedPathComponent(_ value: String) -> String {
        value.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? value
    }
}

final actor MockTextToVideoService: TextToVideoService {
    private var generations: [TextVideoGeneration] = []
    private let persistence: TextVideoPersistence?

    init(persistence: TextVideoPersistence? = nil) {
        self.persistence = persistence
    }

    func cachedGenerations() async throws -> [TextVideoGeneration] {
        if let persistence {
            return try await persistence.loadGenerations()
        }
        return generations
    }

    func sourceImageURL(for mediaID: String) async throws -> URL {
        URL(string: "https://example.invalid/media/\(mediaID)")!
    }

    func create(
        prompt: String,
        duration: Int,
        resolution: String,
        aspectRatio: String,
        idempotencyKey _: String,
        imageMediaID: String?
    ) async throws -> TextVideoGeneration {
        let now = Date()
        let generation = TextVideoGeneration(
            id: "preview-video-\(UUID().uuidString)",
            providerTaskID: "preview-video",
            prompt: prompt,
            imageMediaID: imageMediaID,
            seconds: duration,
            resolution: resolution,
            aspectRatio: aspectRatio,
            status: .completed,
            progress: 1,
            videoURL: Bundle.main.url(forResource: "HomeHeroPreview", withExtension: "mp4")?.absoluteString,
            errorMessage: nil,
            createdAt: now,
            updatedAt: now,
            completedAt: now
        )
        generations.insert(generation, at: 0)
        if let persistence {
            let existing = try await persistence.loadGenerations()
            try await persistence.saveGenerations(
                [generation] + existing.filter { $0.id != generation.id }
            )
        }
        return generation
    }

    func list() async throws -> [TextVideoGeneration] {
        if let persistence {
            return try await persistence.loadGenerations()
        }
        return generations
    }

    func downloadContent(for generationID: String) async throws -> URL {
        let generation = generations.first { $0.id == generationID }
        guard let generation, let videoURL = generation.videoURL.flatMap(URL.init(string:)) else {
            throw AppError.invalidResponse
        }
        return videoURL
    }

    func delete(generationID: String) async throws {
        generations.removeAll { $0.id == generationID }
        try await persistence?.removeGeneration(id: generationID)
    }
}
