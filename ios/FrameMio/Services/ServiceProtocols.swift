import Combine
import Foundation

protocol PhotoLibraryService: Sendable {
    func authorizationStatus() async -> PhotoLibraryAuthorization
    func requestAuthorization() async -> PhotoLibraryAuthorization
    func fetchAssets(for context: MomentContext, filter: PhotoLibraryMediaFilter) async throws -> [MediaAsset]
    func resolveAssets(localIdentifiers: [String]) async throws -> [MediaAsset]
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
    func invalidateAccessToken() async
}

struct APIRequest: Sendable {
    enum Method: String, Sendable {
        case get = "GET"
        case post = "POST"
        case delete = "DELETE"
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

enum CreditAction: String, Codable, Hashable, Sendable {
    case createStory = "CREATE_STORY"
    case regenerateStory = "REGENERATE_STORY"
    case addMoment = "ADD_MOMENT"
    case aiEditStory = "AI_EDIT_STORY"
    case changeMusic = "CHANGE_MUSIC"
    case changeCaption = "CHANGE_CAPTION"
    case reorderMoment = "REORDER_MOMENT"
    case removeMoment = "REMOVE_MOMENT"
    case preview = "PREVIEW"
}

struct CreditBalance: Codable, Hashable, Sendable {
    let available: Int
    let reserved: Int
}

enum PricingEntryPoint: String, Codable, Hashable, Identifiable, Sendable {
    case homeCreditButton
    case insufficientCredits
    case profile
    case settings

    var id: String { rawValue }
}

struct CreditIssue: Identifiable, Hashable, Sendable {
    let id = UUID().uuidString
    let action: CreditAction
    let required: Int
    let available: Int
}

struct FrameMioProCatalog {
    static let monthlyProductID = "com.framemio.pro.monthly"
    static let yearlyProductID = "com.framemio.pro.yearly"
    static let monthlyFallbackDisplayPrice = "$9.99"
    static let yearlyFallbackDisplayPrice = "$79.99"
    static let monthlyCredits = AppConfiguration.monthlySubscriptionCycleCredits
    static let yearlyCredits = AppConfiguration.yearlySubscriptionCycleCredits

    static let productIDs = [monthlyProductID, yearlyProductID]
}

struct CreditPricing {
    static let createStory = 40
    static let regenerateStory = 40
    static let addMoment = 20
    static let aiEditStory = 20
    static let changeMusic = 0
    static let changeCaption = 0
    static let reorderMoment = 0
    static let removeMoment = 0
    static let preview = 0
    static let lowBalanceThreshold = 25

    static func price(for action: CreditAction) -> Int {
        switch action {
        case .createStory:
            return createStory
        case .regenerateStory:
            return regenerateStory
        case .addMoment:
            return addMoment
        case .aiEditStory:
            return aiEditStory
        case .changeMusic:
            return changeMusic
        case .changeCaption:
            return changeCaption
        case .reorderMoment:
            return reorderMoment
        case .removeMoment:
            return removeMoment
        case .preview:
            return preview
        }
    }
}

protocol CreditService: Sendable {
    func balance() async throws -> CreditBalance
    func grantSubscriptionCycle(plan: String, purchaseID: String, expiresAt: Date?) async throws
}

@MainActor
final class CreditBalanceStore: ObservableObject {
    @Published private(set) var balance: CreditBalance?
    @Published private(set) var isLoading = false
    @Published private(set) var errorMessage: String?

    private let creditService: CreditService?
    private var refreshTask: Task<Void, Never>?

    init(creditService: CreditService?) {
        self.creditService = creditService
    }

    var availableCredits: Int? {
        balance?.available
    }

    var compactTitle: String {
        guard let availableCredits else {
            return isLoading ? "✦ …" : "✦"
        }
        return "✦ \(availableCredits.formatted())"
    }

    var largeTitle: String {
        guard let availableCredits else {
            return isLoading ? "✦ Loading Credits" : "✦ Credits"
        }
        return "✦ \(availableCredits.formatted()) Credits"
    }

    var isLowOnCredits: Bool {
        guard let availableCredits else { return false }
        return availableCredits <= CreditPricing.lowBalanceThreshold
    }

    var compactAccessibilityLabel: String {
        guard let availableCredits else {
            return "Credits. Opens FrameMio Pro."
        }
        return "\(availableCredits) credits. Opens FrameMio Pro."
    }

    var largeAccessibilityLabel: String {
        guard let availableCredits else {
            return "Credit balance loading."
        }
        return "\(availableCredits) credits available."
    }

    func refresh() {
        guard let creditService else { return }
        refreshTask?.cancel()
        refreshTask = Task { [weak self] in
            guard let self else { return }
            isLoading = true
            defer { isLoading = false }
            do {
                balance = try await creditService.balance()
                errorMessage = nil
            } catch is CancellationError {
            } catch {
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    deinit {
        refreshTask?.cancel()
    }
}

protocol StoryService: Sendable {
    func cachedStories() async throws -> [Story]
    func listStories() async throws -> [Story]
    func story(id: String) async throws -> Story
    func createStory(request: CreateStoryRequest) async throws -> Story
    func retryProcessing(id: String) async throws -> Story
    func regenerateStory(id: String) async throws -> Story
    func renderAgain(id: String) async throws -> Story
    func saveStoryEdits(storyID: String, request: StoryEditRequest) async throws -> Story
    func deleteStory(id: String) async throws
    func replaceMoment(storyID: String, assetID: String) async throws -> Story
    func removeMoment(storyID: String, assetID: String) async throws -> Story
}

protocol StoryPersistence: Sendable {
    func loadStories() async throws -> [Story]
    func saveStories(_ stories: [Story]) async throws
    func removeStory(id: String) async throws
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
    func invalidateCompletedUploads(
        for assets: [MediaAsset],
        optimization: MediaUploadOptimization
    ) async throws
}
