import Foundation

final class RemoteTextToVideoService: TextToVideoService, @unchecked Sendable {
    private let apiClient: APIClient
    private let encoder: JSONEncoder
    private let pollIntervalNanoseconds: UInt64
    private let maxPollAttempts: Int

    init(
        apiClient: APIClient,
        pollIntervalNanoseconds: UInt64 = 2_000_000_000,
        maxPollAttempts: Int = 150
    ) {
        self.apiClient = apiClient
        self.encoder = FrameMioJSONCoding.apiEncoder()
        self.pollIntervalNanoseconds = pollIntervalNanoseconds
        self.maxPollAttempts = max(1, maxPollAttempts)
    }

    func generate(
        prompt: String,
        duration: Int,
        resolution: String,
        aspectRatio: String,
        progress: (@Sendable (TextVideoGenerationStatus) -> Void)?
    ) async throws -> TextVideoGenerationResult {
        let request = TextVideoGenerationRequest(
            model: resolution == "1080p" ? "grok-imagine-video-1.5-preview" : "grok-imagine-video",
            prompt: prompt.trimmingCharacters(in: .whitespacesAndNewlines),
            seconds: duration,
            resolution: resolution,
            aspectRatio: aspectRatio,
            images: []
        )
        let body = try encoder.encode(request)
        let data = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/v1/videos",
                body: body,
                headers: ["Idempotency-Key": "ios-text-video-\(UUID().uuidString)"]
            )
        )

        var latest = try decodeStatus(from: data)
        progress?(latest)
        if let videoURL = latest.videoURL {
            return TextVideoGenerationResult(taskID: latest.taskID ?? UUID().uuidString, videoURL: videoURL)
        }
        guard let taskID = latest.taskID, !taskID.isEmpty else {
            throw AppError.message("The video provider did not return a task to track.")
        }
        if latest.isFailure {
            throw AppError.message("Video generation failed. Try a different prompt.")
        }

        for _ in 0..<maxPollAttempts {
            try await Task.sleep(nanoseconds: pollIntervalNanoseconds)
            let statusData = try await apiClient.data(
                for: APIRequest(method: .get, path: "/v1/videos/\( Self.percentEncodedPathComponent(taskID))")
            )
            latest = try decodeStatus(from: statusData)
            progress?(latest)

            if let videoURL = latest.videoURL {
                return TextVideoGenerationResult(taskID: taskID, videoURL: videoURL)
            }
            if latest.isFailure {
                throw AppError.message("Video generation failed. Try a different prompt.")
            }
        }

        throw AppError.message("Video generation is taking longer than expected. Check back shortly.")
    }

    private func decodeStatus(from data: Data) throws -> TextVideoGenerationStatus {
        let object: Any
        do {
            object = try JSONSerialization.jsonObject(with: data)
        } catch {
            throw AppError.invalidResponse
        }

        let taskID = findString(in: object, keys: ["id", "video_id", "task_id", "job_id"])
        let status = findString(in: object, keys: ["status"]).lowercased()
        let progress = findValue(in: object, keys: ["progress"]).map { String(describing: $0) }
        let videoURLString = findString(
            in: object,
            keys: ["video_url", "download_url", "url", "output_url", "file_url", "content_url"]
        )
        let videoURL = videoURLString.isEmpty ? nil : URL(string: videoURLString)
        return TextVideoGenerationStatus(
            taskID: taskID.isEmpty ? nil : taskID,
            status: status.isEmpty ? "queued" : status,
            progress: progress,
            videoURL: videoURL
        )
    }

    private func findString(in value: Any, keys: Set<String>) -> String {
        if let dictionary = value as? [String: Any] {
            for (key, nestedValue) in dictionary {
                let normalizedKey = key.lowercased().replacingOccurrences(of: "-", with: "_")
                if keys.contains(normalizedKey), let string = nestedValue as? String, !string.isEmpty {
                    return string
                }
            }
            for nestedValue in dictionary.values {
                let found = findString(in: nestedValue, keys: keys)
                if !found.isEmpty {
                    return found
                }
            }
        } else if let array = value as? [Any] {
            for nestedValue in array {
                let found = findString(in: nestedValue, keys: keys)
                if !found.isEmpty {
                    return found
                }
            }
        }
        return ""
    }

    private func findValue(in value: Any, keys: Set<String>) -> String? {
        if let dictionary = value as? [String: Any] {
            for (key, nestedValue) in dictionary {
                let normalizedKey = key.lowercased().replacingOccurrences(of: "-", with: "_")
                if keys.contains(normalizedKey) {
                    return String(describing: nestedValue)
                }
            }
            for nestedValue in dictionary.values {
                if let found = findValue(in: nestedValue, keys: keys) {
                    return found
                }
            }
        } else if let array = value as? [Any] {
            for nestedValue in array {
                if let found = findValue(in: nestedValue, keys: keys) {
                    return found
                }
            }
        }
        return nil
    }

    private static func percentEncodedPathComponent(_ value: String) -> String {
        value.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? value
    }
}

private struct TextVideoGenerationRequest: Encodable {
    let model: String
    let prompt: String
    let seconds: Int
    let resolution: String
    let aspectRatio: String
    let images: [String]

    enum CodingKeys: String, CodingKey {
        case model
        case prompt
        case seconds
        case resolution
        case aspectRatio = "aspect_ratio"
        case images
    }
}

private extension TextVideoGenerationStatus {
    var isFailure: Bool {
        ["failed", "cancelled", "canceled", "error"].contains(status)
    }
}

final class MockTextToVideoService: TextToVideoService, @unchecked Sendable {
    func generate(
        prompt: String,
        duration: Int,
        resolution: String,
        aspectRatio: String,
        progress: (@Sendable (TextVideoGenerationStatus) -> Void)?
    ) async throws -> TextVideoGenerationResult {
        let taskID = "preview-video"
        progress?(TextVideoGenerationStatus(taskID: taskID, status: "queued", progress: nil, videoURL: nil))
        try await Task.sleep(nanoseconds: 300_000_000)
        guard let videoURL = URL(string: "https://example.com/preview-video.mp4") else {
            throw AppError.invalidResponse
        }
        progress?(TextVideoGenerationStatus(taskID: taskID, status: "completed", progress: "100", videoURL: videoURL))
        return TextVideoGenerationResult(taskID: taskID, videoURL: videoURL)
    }
}
