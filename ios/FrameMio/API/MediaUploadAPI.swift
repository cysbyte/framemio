import Foundation

private struct CreateUploadSessionRequest: Codable, Sendable {
    let idempotencyKey: String
    let metadata: MediaUploadMetadata
}

private struct CompleteMultipartUploadRequest: Codable, Sendable {
    let uploadId: String
    let s3Key: String
    let parts: [MultipartPartCompletion]
}

final class RemoteMediaUploadAuthorizer: MediaUploadAuthorizer, @unchecked Sendable {
    private let apiClient: APIClient
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder

    init(apiClient: APIClient) {
        self.apiClient = apiClient
        encoder = FrameMioJSONCoding.apiEncoder()
        decoder = FrameMioJSONCoding.apiDecoder()
    }

    func createUploadSession(
        metadata: MediaUploadMetadata,
        idempotencyKey: String
    ) async throws -> UploadSession {
        let body = try encoder.encode(
            CreateUploadSessionRequest(idempotencyKey: idempotencyKey, metadata: metadata)
        )
        let data = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/v1/media/upload-sessions",
                body: body,
                headers: ["Idempotency-Key": idempotencyKey]
            )
        )
        do {
            return try decoder.decode(UploadSession.self, from: data)
        } catch {
            throw AppError.invalidResponse
        }
    }

    func completeMultipartUpload(
        session: UploadSession,
        parts: [MultipartPartCompletion]
    ) async throws {
        let body = try encoder.encode(
            CompleteMultipartUploadRequest(
                uploadId: session.uploadID,
                s3Key: session.s3Key,
                parts: parts
            )
        )
        _ = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/v1/media/upload-sessions/\(session.uploadID)/complete",
                body: body,
                headers: ["Idempotency-Key": "ios-upload-complete-\(session.uploadID)"]
            )
        )
    }

    func notifyUploadCompleted(_ notification: UploadCompletionNotification) async throws {
        let body = try encoder.encode(notification)
        _ = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/v1/media/uploads/complete",
                body: body,
                headers: ["Idempotency-Key": "ios-upload-notify-\(notification.uploadId)"]
            )
        )
    }
}

actor MockMediaUploadAuthorizer: MediaUploadAuthorizer {
    private let completedAssetIDs: Set<String>
    private var sessionsByIdempotencyKey: [String: UploadSession] = [:]
    private(set) var requestedIdempotencyKeys: [String] = []
    private(set) var createSessionCallCount = 0
    private(set) var completedMultipartUploads: [[MultipartPartCompletion]] = []
    private(set) var completedNotifications: [UploadCompletionNotification] = []

    init(completedAssetIDs: Set<String> = []) {
        self.completedAssetIDs = completedAssetIDs
    }

    func createUploadSession(
        metadata: MediaUploadMetadata,
        idempotencyKey: String
    ) async throws -> UploadSession {
        createSessionCallCount += 1
        requestedIdempotencyKeys.append(idempotencyKey)
        if let session = sessionsByIdempotencyKey[idempotencyKey] {
            return session
        }

        let uploadID = "upload-\(metadata.assetID)"
        let strategy: UploadStrategy = metadata.fileSizeBytes > metadata.preferredPartSizeBytes
            ? .multipart
            : .single
        let parts = Self.makeParts(
            uploadID: uploadID,
            fileSizeBytes: metadata.fileSizeBytes,
            partSizeBytes: metadata.preferredPartSizeBytes
        )
        let isCompleted = completedAssetIDs.contains(metadata.assetID)
        let session = UploadSession(
            uploadID: uploadID,
            mediaID: "media-\(metadata.assetID)",
            s3Key: "media/\(metadata.assetID)",
            strategy: strategy,
            partSizeBytes: metadata.preferredPartSizeBytes,
            s3MultipartUploadID: strategy == .multipart ? "s3-\(uploadID)" : nil,
            expiresAt: Date().addingTimeInterval(3_600),
            completed: isCompleted,
            parts: parts.map { part in
                guard isCompleted else { return part }
                var completedPart = part
                completedPart.status = .completed
                completedPart.eTag = "etag-\(part.partNumber)"
                return completedPart
            }
        )
        sessionsByIdempotencyKey[idempotencyKey] = session
        return session
    }

    func completeMultipartUpload(
        session: UploadSession,
        parts: [MultipartPartCompletion]
    ) async throws {
        completedMultipartUploads.append(parts)
        _ = session
    }

    func notifyUploadCompleted(_ notification: UploadCompletionNotification) async throws {
        completedNotifications.append(notification)
        for (idempotencyKey, storedSession) in sessionsByIdempotencyKey
            where storedSession.uploadID == notification.uploadId {
            var completedSession = storedSession
            completedSession.completed = true
            completedSession.parts = completedSession.parts.map { part in
                var completedPart = part
                completedPart.status = .completed
                completedPart.eTag = completedPart.eTag ?? "etag-\(part.partNumber)"
                return completedPart
            }
            sessionsByIdempotencyKey[idempotencyKey] = completedSession
        }
    }

    private static func makeParts(
        uploadID: String,
        fileSizeBytes: Int64,
        partSizeBytes: Int64
    ) -> [UploadPart] {
        var parts: [UploadPart] = []
        var offset: Int64 = 0
        var partNumber = 1
        while offset < fileSizeBytes {
            let length = min(partSizeBytes, fileSizeBytes - offset)
            parts.append(
                UploadPart(
                    partNumber: partNumber,
                    offsetBytes: offset,
                    lengthBytes: length,
                    presignedURL: URL(string: "https://s3.example.test/\(uploadID)/part/\(partNumber)?X-Amz-SignedHeaders=host")!,
                    status: .pending,
                    attempts: 0,
                    eTag: nil,
                    lastError: nil
                )
            )
            offset += length
            partNumber += 1
        }
        return parts
    }
}
