import Foundation

struct AppDependencies {
    let photoLibrary: PhotoLibraryService
    let mediaSelection: MediaSelectionService
    let mediaAnalysis: MediaAnalysisService
    let mediaIntelligence: MediaIntelligenceService
    let mediaClustering: MediaClustering
    let mediaIntelligenceStore: MediaIntelligenceStore
    let videoMetadata: VideoMetadataService
    let apiClient: APIClient
    let storyService: StoryService
    let storyPersistence: StoryPersistence
    let originalMediaSource: OriginalMediaSource
    let mediaUploadStore: MediaUploadStore
    let mediaUploadAuthorizer: MediaUploadAuthorizer
    let directObjectStorage: DirectObjectStorageClient
    let mediaUploadService: MediaUploadService
    let creditService: CreditService
    let promptTranscriptionService: PromptTranscriptionService

    static func live() -> AppDependencies {
        let persistence = UserDefaultsStoryPersistence()
        let apiSession = URLSession(configuration: AppConfiguration.apiSessionConfiguration)
        let tokenProvider = KeychainAuthTokenProvider(baseURL: AppConfiguration.apiBaseURL, session: apiSession)
        let photoLibrary = PhotoKitPhotoLibraryService()
        let mediaIntelligenceStore = FileMediaIntelligenceStore()
        let apiClient = URLSessionAPIClient(
            baseURL: AppConfiguration.apiBaseURL,
            session: apiSession,
            tokenProvider: tokenProvider
        )
        let uploadStore = FileMediaUploadStore()
        let uploadAuthorizer = RemoteMediaUploadAuthorizer(apiClient: apiClient)
        let objectStorage = URLSessionDirectObjectStorageClient()
        let uploadService = MediaUploadCoordinator(
            source: photoLibrary,
            authorizer: uploadAuthorizer,
            objectStorage: objectStorage,
            store: uploadStore
        )

        return AppDependencies(
            photoLibrary: photoLibrary,
            mediaSelection: photoLibrary,
            mediaAnalysis: VisionMediaAnalysisService(photoLibrary: photoLibrary),
            mediaIntelligence: PhotoKitMediaIntelligenceService(store: mediaIntelligenceStore),
            mediaClustering: MediaClusterer(),
            mediaIntelligenceStore: mediaIntelligenceStore,
            videoMetadata: AVFoundationVideoMetadataService(),
            apiClient: apiClient,
            storyService: RemoteStoryService(apiClient: apiClient, persistence: persistence),
            storyPersistence: persistence,
            originalMediaSource: photoLibrary,
            mediaUploadStore: uploadStore,
            mediaUploadAuthorizer: uploadAuthorizer,
            directObjectStorage: objectStorage,
            mediaUploadService: uploadService,
            creditService: RemoteCreditService(apiClient: apiClient),
            promptTranscriptionService: RemotePromptTranscriptionService(apiClient: apiClient)
        )
    }

    static func preview() -> AppDependencies {
        let persistence = MockStoryPersistence()
        let mediaIntelligenceStore = MockMediaIntelligenceStore()
        let storyService = MockStoryService(persistence: persistence)
        let mediaSelection = MockMediaSelectionService()
        let originalMediaSource = MockOriginalMediaSource()
        let uploadStore = MockMediaUploadStore()
        let uploadAuthorizer = MockMediaUploadAuthorizer()
        let objectStorage = MockDirectObjectStorageClient()
        let uploadService = MediaUploadCoordinator(
            source: originalMediaSource,
            authorizer: uploadAuthorizer,
            objectStorage: objectStorage,
            store: uploadStore
        )
        return AppDependencies(
            photoLibrary: MockPhotoLibraryService(),
            mediaSelection: mediaSelection,
            mediaAnalysis: MockMediaAnalysisService(),
            mediaIntelligence: MockMediaIntelligenceService(store: mediaIntelligenceStore),
            mediaClustering: MediaClusterer(),
            mediaIntelligenceStore: mediaIntelligenceStore,
            videoMetadata: MockVideoMetadataService(),
            apiClient: MockAPIClient(),
            storyService: storyService,
            storyPersistence: persistence,
            originalMediaSource: originalMediaSource,
            mediaUploadStore: uploadStore,
            mediaUploadAuthorizer: uploadAuthorizer,
            directObjectStorage: objectStorage,
            mediaUploadService: uploadService,
            creditService: MockCreditService(),
            promptTranscriptionService: MockPromptTranscriptionService()
        )
    }
}

enum AppConfiguration {
    static var apiBaseURL: URL {
        let configuredValue = Bundle.main.object(forInfoDictionaryKey: "APIBaseURL") as? String
        return URL(string: configuredValue ?? "https://api.framemio.com")!
    }

    static var initialCreditGrant: Int {
        integerConfigurationValue(
            infoKey: "FrameMioInitialCreditGrant",
            environmentKey: "CREDIT_INITIAL_GRANT",
            fallback: 100
        )
    }

    static var monthlySubscriptionCycleCredits: Int {
        integerConfigurationValue(
            infoKey: "FrameMioMonthlySubscriptionCycleCredits",
            environmentKey: "CREDIT_SUBSCRIPTION_MONTHLY_CYCLE_CREDITS",
            fallback: 300
        )
    }

    static var yearlySubscriptionCycleCredits: Int {
        integerConfigurationValue(
            infoKey: "FrameMioYearlySubscriptionCycleCredits",
            environmentKey: "CREDIT_SUBSCRIPTION_YEARLY_CYCLE_CREDITS",
            fallback: 360
        )
    }

    static var apiSessionConfiguration: URLSessionConfiguration {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.requestCachePolicy = .reloadIgnoringLocalCacheData
        configuration.urlCache = nil
        configuration.waitsForConnectivity = true
        configuration.timeoutIntervalForRequest = 60
        configuration.timeoutIntervalForResource = 120
        return configuration
    }

    private static func integerConfigurationValue(infoKey: String, environmentKey: String, fallback: Int) -> Int {
        if let environmentValue = ProcessInfo.processInfo.environment[environmentKey],
           let value = Int(environmentValue.trimmingCharacters(in: .whitespacesAndNewlines)),
           value >= 0 {
            return value
        }

        if let number = Bundle.main.object(forInfoDictionaryKey: infoKey) as? NSNumber {
            return max(0, number.intValue)
        }

        if let text = Bundle.main.object(forInfoDictionaryKey: infoKey) as? String,
           let value = Int(text.trimmingCharacters(in: .whitespacesAndNewlines)),
           value >= 0 {
            return value
        }

        return fallback
    }
}
