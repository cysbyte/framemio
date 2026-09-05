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
        let deepgramLanguageLogValue = AppConfiguration.deepgramLanguage ?? "nil"
        print("[FrameMio][Config] bundle_path=\(Bundle.main.bundlePath)")
        print("[FrameMio][Config] deepgram_api_key_configured=\(AppConfiguration.deepgramAPIKey != nil) deepgram_language=\(deepgramLanguageLogValue) source=\(AppConfiguration.deepgramAPIKeySource)")
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
    private static let localEnvironmentValues: [String: String] = loadLocalEnvironmentValues()

    static var apiBaseURL: URL {
        let configuredValue = Bundle.main.object(forInfoDictionaryKey: "APIBaseURL") as? String
        return URL(string: configuredValue ?? "https://api.framemio.com")!
    }

    static var deepgramAPIKey: String? {
        let value = stringConfigurationValue(infoKey: "FrameMioDeepgramAPIKey", environmentKey: "DEEPGRAM_API_KEY")
        if value == "dg-mock-replace-with-real-key" {
            return nil
        }
        return value
    }

    static var deepgramAPIKeySource: String {
        resolvedStringConfigurationSource(infoKey: "FrameMioDeepgramAPIKey", environmentKey: "DEEPGRAM_API_KEY")
    }

    static var deepgramLanguage: String? {
        stringConfigurationValue(infoKey: "FrameMioDeepgramLanguage", environmentKey: "DEEPGRAM_LANGUAGE")
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

    private static func stringConfigurationValue(infoKey: String, environmentKey: String) -> String? {
        if let environmentValue = debugEnvironmentValue(forKey: environmentKey) {
            print("[FrameMio][Config] resolved_from_environment key=\(environmentKey) value=\(environmentValue)")
            return environmentValue
        }

        if let localEnvironmentValue = localEnvironmentValue(forKey: environmentKey) {
            print("[FrameMio][Config] resolved_from_local_env key=\(environmentKey) value=\(localEnvironmentValue)")
            return localEnvironmentValue
        }

        if let infoValue = debugInfoPlistValue(forKey: infoKey) {
            print("[FrameMio][Config] resolved_from_info_plist key=\(infoKey) value=\(infoValue)")
            return infoValue
        }

        print("[FrameMio][Config] missing_value key=\(infoKey) environment_key=\(environmentKey)")
        return nil
    }

    static func debugEnvironmentValue(forKey key: String) -> String? {
        guard let value = ProcessInfo.processInfo.environment[key]?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else {
            return nil
        }
        return value
    }

    static func localEnvironmentValue(forKey key: String) -> String? {
        guard let value = localEnvironmentValues[key]?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else {
            return nil
        }
        return value
    }

    static func loadLocalEnvironmentValues() -> [String: String] {
        guard let url = Bundle.main.url(forResource: "FrameMio", withExtension: "env"),
              let data = try? Data(contentsOf: url),
              let contents = String(data: data, encoding: .utf8) else {
            return [:]
        }

        var values: [String: String] = [:]
        for rawLine in contents.components(separatedBy: .newlines) {
            let line = rawLine.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !line.isEmpty, !line.hasPrefix("#"), let equalsIndex = line.firstIndex(of: "=") else {
                continue
            }

            let key = line[..<equalsIndex].trimmingCharacters(in: .whitespacesAndNewlines)
            let valueStart = line.index(after: equalsIndex)
            let value = line[valueStart...].trimmingCharacters(in: .whitespacesAndNewlines)
            guard !key.isEmpty else {
                continue
            }

            values[String(key)] = unquoteEnvironmentValue(value)
        }

        return values
    }

    static func debugInfoPlistValue(forKey key: String) -> String? {
        guard let value = Bundle.main.object(forInfoDictionaryKey: key) as? String else {
            return nil
        }

        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            return nil
        }
        guard !(trimmed.hasPrefix("$(") && trimmed.hasSuffix(")")) else {
            return nil
        }
        return trimmed
    }

    private static func resolvedStringConfigurationSource(infoKey: String, environmentKey: String) -> String {
        if debugEnvironmentValue(forKey: environmentKey) != nil {
            return "environment"
        }
        if localEnvironmentValue(forKey: environmentKey) != nil {
            return "local_env"
        }
        if debugInfoPlistValue(forKey: infoKey) != nil {
            return "info_plist"
        }
        return "missing"
    }

    private static func unquoteEnvironmentValue(_ value: String) -> String {
        guard value.count >= 2 else {
            return value
        }

        if (value.hasPrefix("\"") && value.hasSuffix("\"")) || (value.hasPrefix("'") && value.hasSuffix("'")) {
            return String(value.dropFirst().dropLast())
        }

        return value
    }
}
