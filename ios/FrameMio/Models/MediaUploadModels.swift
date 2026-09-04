import Foundation

enum UploadMediaType: String, Codable, Hashable, Sendable {
    case photo
    case video
    case livePhoto = "live_photo"
}

enum UploadStrategy: String, Codable, Hashable, Sendable {
    case single
    case multipart
}

enum UploadStatus: String, Codable, Hashable, Sendable {
    case queued
    case authorizing
    case uploading
    case retrying
    case paused
    case completing
    case completed
    case failed
    case cancelled
}

enum UploadPartStatus: String, Codable, Hashable, Sendable {
    case pending
    case uploading
    case retrying
    case completed
    case failed
}

enum MediaUploadOptimization: String, Codable, Hashable, Sendable {
    case original
    case optimized
}

struct UploadPart: Identifiable, Codable, Hashable, Sendable {
    var id: Int { partNumber }

    let partNumber: Int
    let offsetBytes: Int64
    let lengthBytes: Int64
    let presignedURL: URL
    var status: UploadPartStatus
    var attempts: Int
    var eTag: String?
    var lastError: String?

    var isCompleted: Bool {
        status == .completed && eTag != nil
    }
}

struct UploadSession: Codable, Hashable, Sendable {
    let uploadID: String
    let mediaID: String
    let s3Key: String
    let strategy: UploadStrategy
    let partSizeBytes: Int64
    let s3MultipartUploadID: String?
    let expiresAt: Date?
    /// The server reports the bytes are already durable in S3. Older responses
    /// omit the field, so absence means "not known to be complete" and the
    /// upload proceeds as usual.
    var completed: Bool?
    var parts: [UploadPart]

    var isAlreadyUploaded: Bool {
        completed == true
    }

    var requiresHeaderSafeRefresh: Bool {
        if isAlreadyUploaded {
            return false
        }
        return parts.contains { part in
            guard let components = URLComponents(url: part.presignedURL, resolvingAgainstBaseURL: false) else {
                return true
            }
            let signedHeaders = components.queryItems?.first { $0.name == "X-Amz-SignedHeaders" }?.value
            return signedHeaders != "host"
        }
    }

    /// A completed session is usable regardless of expiry: its presigned URLs
    /// are never refreshed by the server, and nothing remains to upload.
    func isUsable(at date: Date = Date()) -> Bool {
        if isAlreadyUploaded {
            return true
        }
        guard let expiresAt else {
            return true
        }
        return expiresAt > date.addingTimeInterval(60)
    }
}

struct MediaUpload: Identifiable, Codable, Hashable, Sendable {
    let id: String
    let assetID: String
    let localIdentifier: String?
    var mediaID: String?
    let mediaType: UploadMediaType
    var fileName: String
    var contentType: String
    var fileSizeBytes: Int64
    let duration: TimeInterval?
    let creationDate: Date?
    let pixelWidth: Int
    let pixelHeight: Int
    let orientation: MediaOrientation
    let optimization: MediaUploadOptimization
    var status: UploadStatus
    var uploadedBytes: Int64
    var authorizationIdempotencyKey: String?
    var session: UploadSession?
    var stagedFileURL: URL?
    var lastError: String?
    var updatedAt: Date

    var progress: Double {
        guard fileSizeBytes > 0 else {
            return status == .completed ? 1 : 0
        }
        return min(1, max(0, Double(uploadedBytes) / Double(fileSizeBytes)))
    }

    var effectiveAuthorizationIdempotencyKey: String {
        authorizationIdempotencyKey ?? id
    }

    static func makeID(for assetID: String, optimization: MediaUploadOptimization) -> String {
        "media-upload-\(optimization.rawValue)-\(assetID)"
    }

    static func makeAuthorizationID(for uploadID: String, fileSizeBytes: Int64) -> String {
        "\(uploadID)-bytes-\(fileSizeBytes)"
    }
}

struct MediaUploadProgress: Hashable, Sendable {
    let uploadID: String
    let assetID: String
    let uploadedBytes: Int64
    let totalBytes: Int64
    let completedUploads: Int
    let totalUploads: Int

    var fractionCompleted: Double {
        guard totalBytes > 0 else { return 0 }
        return min(1, max(0, Double(uploadedBytes) / Double(totalBytes)))
    }
}

struct MediaUploadOptions: Hashable, Sendable {
    let optimization: MediaUploadOptimization
    let allowsNetworkAccessToPhotos: Bool
    let usesBackgroundURLSession: Bool
    let maximumConcurrentUploads: Int
    let maximumPartRetries: Int
    let partSizeBytes: Int64
    let maximumTotalUploadBytes: Int64?

    init(
        optimization: MediaUploadOptimization = .original,
        allowsNetworkAccessToPhotos: Bool = true,
        usesBackgroundURLSession: Bool = false,
        maximumConcurrentUploads: Int = 2,
        maximumPartRetries: Int = 3,
        partSizeBytes: Int64 = 8 * 1_024 * 1_024,
        maximumTotalUploadBytes: Int64? = 1_000_000_000
    ) {
        self.optimization = optimization
        self.allowsNetworkAccessToPhotos = allowsNetworkAccessToPhotos
        self.usesBackgroundURLSession = usesBackgroundURLSession
        self.maximumConcurrentUploads = max(1, maximumConcurrentUploads)
        self.maximumPartRetries = max(1, maximumPartRetries)
        self.partSizeBytes = max(5 * 1_024 * 1_024, partSizeBytes)
        self.maximumTotalUploadBytes = maximumTotalUploadBytes.map { max(1, $0) }
    }
}

struct MediaUploadMetadata: Codable, Hashable, Sendable {
    let assetID: String
    let sourceAssetID: String?
    let mediaType: UploadMediaType
    let contentType: String
    let fileName: String
    let fileSizeBytes: Int64
    let duration: TimeInterval?
    let creationDate: Date?
    let pixelWidth: Int
    let pixelHeight: Int
    let orientation: MediaOrientation
    let optimization: MediaUploadOptimization
    let preferredPartSizeBytes: Int64

    enum CodingKeys: String, CodingKey {
        case assetID = "assetID"
        case sourceAssetID = "sourceAssetId"
        case mediaType
        case contentType
        case fileName
        case fileSizeBytes
        case duration
        case creationDate
        case pixelWidth
        case pixelHeight
        case orientation
        case optimization
        case preferredPartSizeBytes
    }
}

struct UploadCompletionNotification: Codable, Hashable, Sendable {
    let mediaId: String
    let uploadId: String
    let s3Key: String
    let mediaType: UploadMediaType
    let duration: TimeInterval?
}

struct MultipartPartCompletion: Codable, Hashable, Sendable {
    let partNumber: Int
    let eTag: String
}
