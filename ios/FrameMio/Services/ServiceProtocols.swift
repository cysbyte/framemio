import Foundation

protocol PhotoLibraryService: Sendable {
    func authorizationStatus() async -> PhotoLibraryAuthorization
    func requestAuthorization() async -> PhotoLibraryAuthorization
    func fetchAssets(for context: MomentContext, filter: PhotoLibraryMediaFilter) async throws -> [MediaAsset]
    func requestThumbnail(for asset: MediaAsset, targetSize: CGSize) async throws -> Data
    func startCachingThumbnails(for assets: [MediaAsset], targetSize: CGSize)
    func stopCachingThumbnails(for assets: [MediaAsset], targetSize: CGSize)
}

struct MediaSelectionScope: Codable, Hashable, Sendable {
    enum Kind: String, Codable, Hashable, Sendable {
        case all
        case context
        case timePeriod
        case album
    }

    let kind: Kind
    let context: MomentContext?
    let startDate: Date?
    let endDate: Date?
    let albumLocalIdentifier: String?

    static let all = MediaSelectionScope(kind: .all)

    static func context(_ context: MomentContext) -> MediaSelectionScope {
        MediaSelectionScope(kind: .context, context: context)
    }

    static func timePeriod(startDate: Date, endDate: Date) -> MediaSelectionScope {
        MediaSelectionScope(kind: .timePeriod, startDate: startDate, endDate: endDate)
    }

    static func album(localIdentifier: String) -> MediaSelectionScope {
        MediaSelectionScope(kind: .album, albumLocalIdentifier: localIdentifier)
    }

    private init(
        kind: Kind,
        context: MomentContext? = nil,
        startDate: Date? = nil,
        endDate: Date? = nil,
        albumLocalIdentifier: String? = nil
    ) {
        self.kind = kind
        self.context = context
        self.startDate = startDate
        self.endDate = endDate
        self.albumLocalIdentifier = albumLocalIdentifier
    }
}

struct MediaAlbum: Identifiable, Codable, Hashable, Sendable {
    let id: String
    let title: String
    let estimatedAssetCount: Int
}

protocol MediaSelectionService: Sendable {
    func fetchAssets(
        in scope: MediaSelectionScope,
        filter: PhotoLibraryMediaFilter
    ) async throws -> [MediaAsset]
    func fetchAlbums() async throws -> [MediaAlbum]
}

struct OriginalMediaFile: Sendable {
    let url: URL
    let fileName: String
    let contentType: String
    let fileSizeBytes: Int64
}

protocol OriginalMediaSource: Sendable {
    func materializeOriginalMedia(
        for asset: MediaAsset,
        allowsNetworkAccess: Bool,
        progress: (@Sendable (Double) -> Void)?
    ) async throws -> OriginalMediaFile
    func removeMaterializedMedia(at url: URL) async
}

enum PhotoLibraryMediaFilter: String, CaseIterable, Hashable, Sendable {
    case all
    case photos
    case videos

    var title: String {
        switch self {
        case .all: "All"
        case .photos: "Photos"
        case .videos: "Videos"
        }
    }
}

enum PhotoLibraryAuthorization: String, Sendable {
    case notDetermined
    case restricted
    case denied
    case authorized
    case limited
}

protocol MediaAnalysisService: Sendable {
    func analyze(asset: MediaAsset) async throws -> MediaAnalysis
}

protocol MediaIntelligenceService: Sendable {
    func analyze(
        assets: [MediaAsset],
        options: MediaIntelligenceOptions
    ) async throws -> [MediaAssetIntelligence]
}

protocol MediaIntelligenceStore: Sendable {
    func intelligence(for assetID: String) async throws -> MediaAssetIntelligence?
    func save(_ intelligence: MediaAssetIntelligence) async throws
    func removeIntelligence(for assetID: String) async throws
    func removeAll() async throws
}

protocol MediaClustering: Sendable {
    func clusters(from intelligence: [MediaAssetIntelligence]) async throws -> [MediaCluster]
}

protocol VideoMetadataService: Sendable {
    func inspectVideo(at url: URL) async throws -> VideoMetadata
}

protocol AuthTokenProvider: Sendable {
    func accessToken() async -> String?
}

struct APIRequest: Sendable {
    enum Method: String, Sendable {
        case get = "GET"
        case post = "POST"
    }

    let method: Method
    let path: String
    let body: Data?
    let headers: [String: String]

    init(method: Method, path: String, body: Data? = nil, headers: [String: String] = [:]) {
        self.method = method
        self.path = path
        self.body = body
        self.headers = headers
    }
}

protocol APIClient: Sendable {
    func data(for request: APIRequest) async throws -> Data
}

protocol StoryService: Sendable {
    func listStories() async throws -> [Story]
    func story(id: String) async throws -> Story
    func createStory(request: CreateStoryRequest) async throws -> Story
    func retryProcessing(id: String) async throws -> Story
    func regenerateStory(id: String) async throws -> Story
    func renderAgain(id: String) async throws -> Story
    func replaceMoment(storyID: String, assetID: String) async throws -> Story
    func removeMoment(storyID: String, assetID: String) async throws -> Story
}

protocol StoryPersistence: Sendable {
    func loadStories() async throws -> [Story]
    func saveStories(_ stories: [Story]) async throws
}

protocol MediaUploadStore: Sendable {
    func upload(for id: String) async throws -> MediaUpload?
    func save(_ upload: MediaUpload) async throws
    func remove(uploadID: String) async throws
    func allUploads() async throws -> [MediaUpload]
}

protocol MediaUploadAuthorizer: Sendable {
    func createUploadSession(
        metadata: MediaUploadMetadata,
        idempotencyKey: String
    ) async throws -> UploadSession
    func completeMultipartUpload(
        session: UploadSession,
        parts: [MultipartPartCompletion]
    ) async throws
    func notifyUploadCompleted(_ notification: UploadCompletionNotification) async throws
}

struct DirectObjectUploadResult: Hashable, Sendable {
    let statusCode: Int
    let eTag: String?
}

protocol DirectObjectStorageClient: Sendable {
    func upload(
        fileURL: URL,
        to presignedURL: URL,
        headers: [String: String],
        usesBackgroundSession: Bool,
        progress: @escaping @Sendable (Int64, Int64) -> Void
    ) async throws -> DirectObjectUploadResult
}

protocol MediaUploadService: Sendable {
    func upload(
        selection: MediaSelection,
        assets: [MediaAsset],
        options: MediaUploadOptions,
        progress: @escaping @Sendable (MediaUploadProgress) -> Void
    ) async throws -> [MediaUpload]
    func resumePendingUploads(
        options: MediaUploadOptions,
        progress: @escaping @Sendable (MediaUploadProgress) -> Void
    ) async throws -> [MediaUpload]
}
