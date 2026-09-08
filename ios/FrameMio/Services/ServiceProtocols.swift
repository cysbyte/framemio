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

struct PromptAudioRecording: Sendable {
    let data: Data
    let contentType: String
    let fileName: String
}

struct PromptLiveTranscriptionConfiguration: Decodable, Sendable {
    let accessToken: String
    let expiresIn: Int
    let websocketURL: URL
    let model: String
    let language: String?
    let authorizationScheme: String?
}

protocol PromptTranscriptionService: Sendable {
    func transcribePromptAudio(_ recording: PromptAudioRecording) async throws -> String
    func livePromptConfiguration() async throws -> PromptLiveTranscriptionConfiguration
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
    let subscriptionCredits: Int
    let purchasedCredits: Int
    let subscriptionReserved: Int
    let purchasedReserved: Int
    let subscriptionCycleExpiresAt: Date?
    let initialCreditGrant: Int
    let monthlySubscriptionCycleCredits: Int
    let yearlySubscriptionCycleCredits: Int
    let totalCredits: Int

    var displayCredits: Int { available }
    var spendableCredits: Int { available }
    var displaySubscriptionCredits: Int { subscriptionCredits }
    var displayPurchasedCredits: Int { purchasedCredits }
    var displayReservedCredits: Int { reserved }

    var hasActiveSubscriptionCycle: Bool {
        if subscriptionCredits > 0 || subscriptionReserved > 0 {
            return true
        }
        guard let subscriptionCycleExpiresAt else { return false }
        return subscriptionCycleExpiresAt > Date()
    }

    enum CodingKeys: String, CodingKey {
        case available
        case reserved
        case subscriptionCredits = "subscription_credits"
        case purchasedCredits = "purchased_credits"
        case subscriptionReserved = "subscription_reserved"
        case purchasedReserved = "purchased_reserved"
        case subscriptionCycleExpiresAt = "subscription_cycle_expires_at"
        case initialCreditGrant = "initial_credit_grant"
        case monthlySubscriptionCycleCredits = "monthly_subscription_cycle_credits"
        case yearlySubscriptionCycleCredits = "yearly_subscription_cycle_credits"
        case totalCredits = "total_credits"
    }

    init(
        available: Int,
        reserved: Int,
        subscriptionCredits: Int = 0,
        purchasedCredits: Int = 0,
        subscriptionReserved: Int = 0,
        purchasedReserved: Int = 0,
        subscriptionCycleExpiresAt: Date? = nil,
        initialCreditGrant: Int = AppConfiguration.initialCreditGrant,
        monthlySubscriptionCycleCredits: Int = AppConfiguration.monthlySubscriptionCycleCredits,
        yearlySubscriptionCycleCredits: Int = AppConfiguration.yearlySubscriptionCycleCredits,
        totalCredits: Int? = nil
    ) {
        self.available = available
        self.reserved = reserved
        self.subscriptionCredits = subscriptionCredits
        self.purchasedCredits = purchasedCredits
        self.subscriptionReserved = subscriptionReserved
        self.purchasedReserved = purchasedReserved
        self.subscriptionCycleExpiresAt = subscriptionCycleExpiresAt
        self.initialCreditGrant = initialCreditGrant
        self.monthlySubscriptionCycleCredits = monthlySubscriptionCycleCredits
        self.yearlySubscriptionCycleCredits = yearlySubscriptionCycleCredits
        self.totalCredits = totalCredits ?? (available + reserved)
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        available = try container.decode(Int.self, forKey: .available)
        reserved = try container.decodeIfPresent(Int.self, forKey: .reserved) ?? 0
        subscriptionCredits = try container.decodeIfPresent(Int.self, forKey: .subscriptionCredits) ?? 0
        purchasedCredits = try container.decodeIfPresent(Int.self, forKey: .purchasedCredits) ?? max(available, 0)
        subscriptionReserved = try container.decodeIfPresent(Int.self, forKey: .subscriptionReserved) ?? 0
        purchasedReserved = try container.decodeIfPresent(Int.self, forKey: .purchasedReserved) ?? reserved
        subscriptionCycleExpiresAt = try container.decodeIfPresent(Date.self, forKey: .subscriptionCycleExpiresAt)
        initialCreditGrant = try container.decodeIfPresent(Int.self, forKey: .initialCreditGrant) ?? AppConfiguration.initialCreditGrant
        monthlySubscriptionCycleCredits = try container.decodeIfPresent(Int.self, forKey: .monthlySubscriptionCycleCredits) ?? AppConfiguration.monthlySubscriptionCycleCredits
        yearlySubscriptionCycleCredits = try container.decodeIfPresent(Int.self, forKey: .yearlySubscriptionCycleCredits) ?? AppConfiguration.yearlySubscriptionCycleCredits
        totalCredits = try container.decodeIfPresent(Int.self, forKey: .totalCredits) ?? (available + reserved)
    }
}

struct CreditPack: Codable, Hashable, Identifiable, Sendable {
    let id: String
    let title: String
    let subtitle: String
    let productID: String?
    let credits: Int

    enum CodingKeys: String, CodingKey {
        case id
        case title
        case subtitle
        case productID = "product_id"
        case credits
    }
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

struct CreditMediaStats: Codable, Hashable, Sendable {
    let photoCount: Int
    let videoCount: Int
    let totalMediaCount: Int
    let totalVideoSeconds: Int
    let totalBytes: Int64
    let storyMediaCount: Int
    let storyVideoSeconds: Int
    let addedPhotoCount: Int
    let addedVideoSeconds: Int
    let addedMediaCount: Int
    let hasAIInstruction: Bool
    let hasCaptionOnlyEdit: Bool

    enum CodingKeys: String, CodingKey {
        case photoCount = "photo_count"
        case videoCount = "video_count"
        case totalMediaCount = "total_media_count"
        case totalVideoSeconds = "total_video_seconds"
        case totalBytes = "total_bytes"
        case storyMediaCount = "story_media_count"
        case storyVideoSeconds = "story_video_seconds"
        case addedPhotoCount = "added_photo_count"
        case addedVideoSeconds = "added_video_seconds"
        case addedMediaCount = "added_media_count"
        case hasAIInstruction = "has_ai_instruction"
        case hasCaptionOnlyEdit = "has_caption_only_edit"
    }

    init(
        photoCount: Int = 0,
        videoCount: Int = 0,
        totalMediaCount: Int = 0,
        totalVideoSeconds: Int = 0,
        totalBytes: Int64 = 0,
        storyMediaCount: Int = 0,
        storyVideoSeconds: Int = 0,
        addedPhotoCount: Int = 0,
        addedVideoSeconds: Int = 0,
        addedMediaCount: Int = 0,
        hasAIInstruction: Bool = false,
        hasCaptionOnlyEdit: Bool = false
    ) {
        self.photoCount = photoCount
        self.videoCount = videoCount
        self.totalMediaCount = totalMediaCount
        self.totalVideoSeconds = totalVideoSeconds
        self.totalBytes = totalBytes
        self.storyMediaCount = storyMediaCount
        self.storyVideoSeconds = storyVideoSeconds
        self.addedPhotoCount = addedPhotoCount
        self.addedVideoSeconds = addedVideoSeconds
        self.addedMediaCount = addedMediaCount
        self.hasAIInstruction = hasAIInstruction
        self.hasCaptionOnlyEdit = hasCaptionOnlyEdit
    }

    static func createStory(assets: [MediaAsset]) -> CreditMediaStats {
        let videos = assets.filter(\.isVideo)
        return CreditMediaStats(
            photoCount: assets.count - videos.count,
            videoCount: videos.count,
            totalMediaCount: assets.count,
            totalVideoSeconds: videos.reduce(0) { total, asset in
                total + max(0, Int(asset.duration + 0.5))
            }
        )
    }
}

struct CreditQuote: Codable, Hashable, Sendable {
    let action: CreditAction
    let credits: Int
    let balance: Int
    let balanceAfter: Int
    let eligible: Bool
    let reason: String?
}

struct FrameMioProCatalog {
    static let monthlyProductID = "com.framemio.pro.monthly"
    static let yearlyProductID = "com.framemio.pro.yearly"
    static let monthlyFallbackDisplayPrice = "$9.99"
    static let yearlyFallbackDisplayPrice = "$89.99"
    static let monthlyCredits = AppConfiguration.monthlySubscriptionCycleCredits
    static let yearlyCredits = AppConfiguration.yearlySubscriptionCycleCredits

    static let productIDs = [monthlyProductID, yearlyProductID]
}

struct FrameMioCreditPackCatalog {
    static let smallProductID = "com.framemio.credits.small"
    static let mediumProductID = "com.framemio.credits.medium"
    static let largeProductID = "com.framemio.credits.large"

    static func productID(for packID: String) -> String? {
        switch packID {
        case "small":
            smallProductID
        case "medium":
            mediumProductID
        case "large":
            largeProductID
        default:
            nil
        }
    }
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
    private static let maxDynamicStoryCredits = 80

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

    static func price(for action: CreditAction, stats: CreditMediaStats) -> Int {
        let fallback = price(for: action)
        guard fallback > 0 else { return 0 }
        let durationSeconds: Int
        switch action {
        case .createStory:
            durationSeconds = stats.totalVideoSeconds
        case .regenerateStory, .aiEditStory:
            durationSeconds = stats.storyVideoSeconds
        default:
            return fallback
        }
        guard durationSeconds > 0 else { return fallback }
        let curve = 1 - Foundation.exp(-Double(durationSeconds) / 45.0)
        let price = Double(fallback) + Double(maxDynamicStoryCredits - fallback) * curve
        return min(maxDynamicStoryCredits, max(fallback, Int(price.rounded())))
    }
}

struct CreditPreflight {
    static func issue(for action: CreditAction, balance: CreditBalance) -> CreditIssue? {
        issue(for: [action], issueAction: action, balance: balance)
    }

    static func issue(for actions: [CreditAction], issueAction: CreditAction, balance: CreditBalance) -> CreditIssue? {
        let required = CreditPricing.price(for: actions)
        guard required > 0 else { return nil }
        let available = balance.spendableCredits
        guard available < required else { return nil }
        return CreditIssue(action: issueAction, required: required, available: available)
    }
}

extension CreditPricing {
    static var addMomentAndRegenerateStory: Int {
        addMoment + regenerateStory
    }

    static func price(for actions: [CreditAction]) -> Int {
        actions.reduce(0) { total, action in
            total + price(for: action)
        }
    }
}

extension StoryEditRequest {
    var regenerationCreditActions: [CreditAction] {
        var actions: [CreditAction] = [.regenerateStory]
        if !addMediaIDs.isEmpty || !replace.isEmpty {
            actions.append(.addMoment)
        }
        return actions
    }

    var regenerationCreditCost: Int {
        CreditPricing.price(for: regenerationCreditActions)
    }
}

protocol CreditService: Sendable {
    func balance() async throws -> CreditBalance
    func quote(action: CreditAction, stats: CreditMediaStats) async throws -> CreditQuote
    func creditPacks() async throws -> [CreditPack]
    func grantPurchasedCredits(packID: String, purchaseID: String) async throws -> CreditBalance
    func grantSubscriptionCycleCredits(plan: String, purchaseID: String, expiresAt: Date?) async throws -> CreditBalance
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
        balance?.spendableCredits
    }

    var displayCredits: Int? {
        balance?.displayCredits
    }

    var compactTitle: String {
        guard let displayCredits else {
            return isLoading ? "✦ …" : "✦"
        }
        return "✦ \(displayCredits.formatted())"
    }

    var largeTitle: String {
        guard let displayCredits else {
            return isLoading ? "✦ Loading Credits" : "✦ Credits"
        }
        return "✦ \(displayCredits.formatted()) Credits"
    }

    var isLowOnCredits: Bool {
        guard let availableCredits else { return false }
        return availableCredits <= CreditPricing.lowBalanceThreshold
    }

    var compactAccessibilityLabel: String {
        guard let displayCredits else {
            return "Credits. Opens FrameMio Pro."
        }
        return "\(displayCredits) credits. Opens FrameMio Pro."
    }

    var largeAccessibilityLabel: String {
        guard let displayCredits else {
            return "Credit balance loading."
        }
        return "\(displayCredits) credits available."
    }

    func refresh() {
        guard let creditService else { return }
        guard refreshTask == nil else { return }
        let shouldShowLoading = balance == nil
        if shouldShowLoading {
            isLoading = true
        }
        refreshTask = Task { [weak self] in
            guard let self else { return }
            defer {
                if shouldShowLoading {
                    isLoading = false
                }
                refreshTask = nil
            }
            do {
                let latestBalance = try await creditService.balance()
                if balance != latestBalance {
                    balance = latestBalance
                }
                if errorMessage != nil {
                    errorMessage = nil
                }
            } catch is CancellationError {
            } catch {
                let latestErrorMessage = AppError.from(error).localizedDescription
                if errorMessage != latestErrorMessage {
                    errorMessage = latestErrorMessage
                }
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
