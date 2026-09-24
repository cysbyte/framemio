import Foundation

final class RemoteTextToVideoService: TextToVideoService, @unchecked Sendable {
    private let apiClient: APIClient
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder

    init(apiClient: APIClient) {
        self.apiClient = apiClient
        self.encoder = FrameMioJSONCoding.apiEncoder()
        self.decoder = FrameMioJSONCoding.apiDecoder()
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
                prompt: prompt.trimmingCharacters(in: .whitespacesAndNewlines),
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
            return try decoder.decode(TextVideoGeneration.self, from: data)
        } catch {
            throw AppError.invalidResponse
        }
    }

    func list() async throws -> [TextVideoGeneration] {
        let data = try await apiClient.data(
            for: APIRequest(method: .get, path: "/v1/text-videos")
        )
        do {
            return try decoder.decode([TextVideoGeneration].self, from: data)
        } catch {
            throw AppError.invalidResponse
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
    }

    private static func percentEncodedPathComponent(_ value: String) -> String {
        value.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? value
    }
}

final actor MockTextToVideoService: TextToVideoService {
    private var generations: [TextVideoGeneration] = []

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
        return generation
    }

    func list() async throws -> [TextVideoGeneration] {
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
    }
}
