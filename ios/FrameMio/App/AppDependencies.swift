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

    static func live() -> AppDependencies {
        let persistence = UserDefaultsStoryPersistence()
        let tokenProvider = KeychainAuthTokenProvider(baseURL: AppConfiguration.apiBaseURL)
        let photoLibrary = PhotoKitPhotoLibraryService()
        let mediaIntelligenceStore = FileMediaIntelligenceStore()
        let apiClient = URLSessionAPIClient(
            baseURL: AppConfiguration.apiBaseURL,
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
            mediaUploadService: uploadService
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
            mediaUploadService: uploadService
        )
    }
}

enum AppConfiguration {
    static var apiBaseURL: URL {
        let configuredValue = Bundle.main.object(forInfoDictionaryKey: "APIBaseURL") as? String
        return URL(string: configuredValue ?? "https://api.framemio.com")!
    }
}
