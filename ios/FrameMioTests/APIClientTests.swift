import Foundation
import XCTest
@testable import FrameMio

final class APIClientTests: XCTestCase {
    override func tearDown() {
        RetryURLProtocol.reset()
        super.tearDown()
    }

    func testRetriesTransientNetworkFailure() async throws {
        RetryURLProtocol.responses = [
            .failure(URLError(.networkConnectionLost)),
            .success(statusCode: 201, data: Data("{\"ok\":true}".utf8))
        ]
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RetryURLProtocol.self]
        let session = URLSession(configuration: configuration)
        let client = URLSessionAPIClient(
            baseURL: URL(string: "https://api.framemio.test")!,
            session: session,
            tokenProvider: StaticAuthTokenProvider(token: "token"),
            maxNetworkRetries: 1
        )

        let data = try await client.data(
            for: APIRequest(
                method: .post,
                path: "/v1/media/upload-sessions",
                body: Data("{}".utf8),
                headers: ["Idempotency-Key": "test-upload-session"]
            )
        )

        XCTAssertEqual(String(data: data, encoding: .utf8), "{\"ok\":true}")
        let requestCount = RetryURLProtocol.requestCount
        XCTAssertEqual(requestCount, 2)
    }

    func testRequestPathPreservesQueryItems() async throws {
        RetryURLProtocol.responses = [
            .success(statusCode: 200, data: Data("[]".utf8))
        ]
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RetryURLProtocol.self]
        let session = URLSession(configuration: configuration)
        let client = URLSessionAPIClient(
            baseURL: URL(string: "https://api.framemio.test")!,
            session: session,
            tokenProvider: StaticAuthTokenProvider(token: "token"),
            maxNetworkRetries: 0
        )

        _ = try await client.data(
            for: APIRequest(method: .get, path: "/v1/video-templates?project_type=story")
        )

        XCTAssertEqual(RetryURLProtocol.lastURL?.path, "/v1/video-templates")
        XCTAssertEqual(RetryURLProtocol.lastURL?.query, "project_type=story")
    }

    func testRetriesTransientServerResponseForRecentProjectsRead() async throws {
        RetryURLProtocol.responses = [
            .success(statusCode: 503, data: Data("service unavailable".utf8)),
            .success(statusCode: 200, data: Data("[]".utf8))
        ]
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RetryURLProtocol.self]
        let session = URLSession(configuration: configuration)
        let client = URLSessionAPIClient(
            baseURL: URL(string: "https://api.framemio.test")!,
            session: session,
            tokenProvider: StaticAuthTokenProvider(token: "token"),
            maxNetworkRetries: 0,
            maxTransientStatusRetries: 1,
            retryBaseDelayNanoseconds: 0
        )

        let data = try await client.data(
            for: APIRequest(method: .get, path: "/v1/stories")
        )

        XCTAssertEqual(String(data: data, encoding: .utf8), "[]")
        XCTAssertEqual(RetryURLProtocol.requestCount, 2)
    }

    func testRetriesTransientServerResponseForIdempotentPost() async throws {
        RetryURLProtocol.responses = [
            .success(statusCode: 503, data: Data("service unavailable".utf8)),
            .success(statusCode: 201, data: Data("{\"id\":\"generation-1\"}".utf8))
        ]
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RetryURLProtocol.self]
        let session = URLSession(configuration: configuration)
        let client = URLSessionAPIClient(
            baseURL: URL(string: "https://api.framemio.test")!,
            session: session,
            tokenProvider: StaticAuthTokenProvider(token: "token"),
            maxNetworkRetries: 0,
            maxTransientStatusRetries: 1,
            retryBaseDelayNanoseconds: 0
        )

        let data = try await client.data(
            for: APIRequest(
                method: .post,
                path: "/v1/text-videos",
                body: Data("{}".utf8),
                headers: ["Idempotency-Key": "ios-text-video-test"]
            )
        )

        XCTAssertEqual(String(data: data, encoding: .utf8), "{\"id\":\"generation-1\"}")
        XCTAssertEqual(RetryURLProtocol.requestCount, 2)
    }

    func testRetriesColdStartFailuresUntilBackendBecomesHealthy() async throws {
        RetryURLProtocol.responses = Array(
            repeating: .success(statusCode: 503, data: Data("service unavailable".utf8)),
            count: 18
        ) + [.success(statusCode: 200, data: Data("{\"available\":20}".utf8))]
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RetryURLProtocol.self]
        let session = URLSession(configuration: configuration)
        let client = URLSessionAPIClient(
            baseURL: URL(string: "https://api.framemio.test")!,
            session: session,
            tokenProvider: StaticAuthTokenProvider(token: "token"),
            maxNetworkRetries: 0,
            retryBaseDelayNanoseconds: 0
        )

        let data = try await client.data(
            for: APIRequest(method: .get, path: "/v1/me/credits")
        )

        XCTAssertEqual(String(data: data, encoding: .utf8), "{\"available\":20}")
        XCTAssertEqual(RetryURLProtocol.requestCount, 19)
    }

    func testAcquiresTokenAfterInitialUnauthenticatedRequest() async throws {
        RetryURLProtocol.responses = [
            .success(statusCode: 401, data: Data("unauthorized".utf8)),
            .success(statusCode: 200, data: Data("[]".utf8))
        ]
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RetryURLProtocol.self]
        let session = URLSession(configuration: configuration)
        let tokenProvider = RefreshingAuthTokenProvider()
        let client = URLSessionAPIClient(
            baseURL: URL(string: "https://api.framemio.test")!,
            session: session,
            tokenProvider: tokenProvider,
            maxNetworkRetries: 0,
            maxTransientStatusRetries: 0,
            retryBaseDelayNanoseconds: 0
        )

        let data = try await client.data(
            for: APIRequest(method: .get, path: "/v1/stories")
        )

        XCTAssertEqual(String(data: data, encoding: .utf8), "[]")
        XCTAssertEqual(RetryURLProtocol.requestCount, 2)
        let accessTokenCallCount = await tokenProvider.accessTokenCallCount
        let invalidateCallCount = await tokenProvider.invalidateCallCount
        XCTAssertEqual(accessTokenCallCount, 2)
        XCTAssertEqual(invalidateCallCount, 1)
    }

    func testDoesNotRetryNonIdempotentPostAfterNetworkFailure() async throws {
        RetryURLProtocol.responses = [
            .failure(URLError(.networkConnectionLost)),
            .success(statusCode: 201, data: Data("{\"ok\":true}".utf8))
        ]
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RetryURLProtocol.self]
        let session = URLSession(configuration: configuration)
        let client = URLSessionAPIClient(
            baseURL: URL(string: "https://api.framemio.test")!,
            session: session,
            tokenProvider: StaticAuthTokenProvider(token: "token"),
            maxNetworkRetries: 1,
            retryBaseDelayNanoseconds: 0
        )

        do {
            _ = try await client.data(
                for: APIRequest(method: .post, path: "/v1/stories", body: Data("{}".utf8))
            )
            XCTFail("Expected the transient network failure to be surfaced")
        } catch let error as URLError {
            XCTAssertEqual(error.code, .networkConnectionLost)
        }

        XCTAssertEqual(RetryURLProtocol.requestCount, 1)
    }

    func testDoesNotRetryNonIdempotentPostWithoutIdempotencyKey() async throws {
        RetryURLProtocol.responses = [
            .success(statusCode: 503, data: Data("service unavailable".utf8)),
            .success(statusCode: 201, data: Data("{\"ok\":true}".utf8))
        ]
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RetryURLProtocol.self]
        let session = URLSession(configuration: configuration)
        let client = URLSessionAPIClient(
            baseURL: URL(string: "https://api.framemio.test")!,
            session: session,
            tokenProvider: StaticAuthTokenProvider(token: "token"),
            maxNetworkRetries: 0,
            maxTransientStatusRetries: 1,
            retryBaseDelayNanoseconds: 0
        )

        do {
            _ = try await client.data(
                for: APIRequest(method: .post, path: "/v1/stories", body: Data("{}".utf8))
            )
            XCTFail("Expected the transient server response to be surfaced")
        } catch let APIClientError.server(statusCode, _) {
            XCTAssertEqual(statusCode, 503)
        }

        XCTAssertEqual(RetryURLProtocol.requestCount, 1)
    }
}

private actor StaticAuthTokenProvider: AuthTokenProvider {
    let token: String?

    init(token: String?) {
        self.token = token
    }

    func accessToken() async -> String? {
        token
    }

    func invalidateAccessToken() async {}
}

private actor RefreshingAuthTokenProvider: AuthTokenProvider {
    private(set) var accessTokenCallCount = 0
    private(set) var invalidateCallCount = 0

    func accessToken() async -> String? {
        accessTokenCallCount += 1
        return accessTokenCallCount == 1 ? nil : "refreshed-token"
    }

    func invalidateAccessToken() async {
        invalidateCallCount += 1
    }
}

private final class RetryURLProtocol: URLProtocol, @unchecked Sendable {
    enum Response: Sendable {
        case failure(URLError)
        case success(statusCode: Int, data: Data)
    }

    private static let lock = NSLock()
    private static var storedResponses: [Response] = []
    private static var storedRequestCount = 0
    private static var storedLastURL: URL?

    static var responses: [Response] {
        get {
            lock.lock()
            defer { lock.unlock() }
            return storedResponses
        }
        set {
            lock.lock()
            storedResponses = newValue
            storedRequestCount = 0
            lock.unlock()
        }
    }

    static var requestCount: Int {
        lock.lock()
        defer { lock.unlock() }
        return storedRequestCount
    }

    static var lastURL: URL? {
        lock.lock()
        defer { lock.unlock() }
        return storedLastURL
    }

    static func reset() {
        lock.lock()
        storedResponses = []
        storedRequestCount = 0
        storedLastURL = nil
        lock.unlock()
    }

    override class func canInit(with request: URLRequest) -> Bool {
        true
    }

    override class func canonicalRequest(for request: URLRequest) -> URLRequest {
        request
    }

    override func startLoading() {
        Self.record(request)
        let response: Response = Self.nextResponse()
        switch response {
        case let .failure(error):
            client?.urlProtocol(self, didFailWithError: error)
        case let .success(statusCode, data):
            let httpResponse = HTTPURLResponse(
                url: request.url!,
                statusCode: statusCode,
                httpVersion: "HTTP/1.1",
                headerFields: ["Content-Type": "application/json"]
            )!
            client?.urlProtocol(self, didReceive: httpResponse, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        }
    }

    override func stopLoading() {}

    private static func nextResponse() -> Response {
        lock.lock()
        defer { lock.unlock() }
        storedRequestCount += 1
        guard !storedResponses.isEmpty else {
            return .success(statusCode: 200, data: Data())
        }
        return storedResponses.removeFirst()
    }

    private static func record(_ request: URLRequest) {
        lock.lock()
        storedLastURL = request.url
        lock.unlock()
    }
}

final class CreditBalanceModelTests: XCTestCase {
    func testCreditBalanceDecodesSubscriptionCycleExpiry() throws {
        let data = Data("""
        {
          "available": 20,
          "reserved": 0,
          "subscription_credits": 0,
          "purchased_credits": 20,
          "subscription_cycle_expires_at": "2099-09-29T00:00:00Z",
          "total_credits": 20
        }
        """.utf8)

        let decoder = FrameMioJSONCoding.apiDecoder()
        let balance = try decoder.decode(CreditBalance.self, from: data)

        XCTAssertEqual(balance.available, 20)
        XCTAssertNotNil(balance.subscriptionCycleExpiresAt)
        XCTAssertTrue(balance.hasActiveSubscriptionCycle)
    }

    func testPurchasedOnlyCreditsAreNotActiveSubscriptionCycle() throws {
        let data = Data("""
        {
          "available": 100,
          "reserved": 0,
          "subscription_credits": 0,
          "purchased_credits": 100,
          "total_credits": 100
        }
        """.utf8)

        let decoder = FrameMioJSONCoding.apiDecoder()
        let balance = try decoder.decode(CreditBalance.self, from: data)

        XCTAssertEqual(balance.available, 100)
        XCTAssertFalse(balance.hasActiveSubscriptionCycle)
    }

    func testExpiredSubscriptionCycleIsNotActive() throws {
        let data = Data("""
        {
          "available": 100,
          "reserved": 0,
          "subscription_credits": 0,
          "purchased_credits": 100,
          "subscription_cycle_expires_at": "2001-01-01T00:00:00Z",
          "total_credits": 100
        }
        """.utf8)

        let decoder = FrameMioJSONCoding.apiDecoder()
        let balance = try decoder.decode(CreditBalance.self, from: data)

        XCTAssertEqual(balance.available, 100)
        XCTAssertFalse(balance.hasActiveSubscriptionCycle)
    }
}

@MainActor
final class CreditBalanceStoreTests: XCTestCase {
    func testProfileCreditRefreshRetriesAfterTransientFailure() async throws {
        let defaults = makeIsolatedDefaults()
        let service = RetryingCreditService(
            failuresBeforeSuccess: 1,
            balance: CreditBalance(available: 42, reserved: 0)
        )
        let store = CreditBalanceStore(
            creditService: service,
            defaults: defaults,
            maxRefreshRetries: 1,
            refreshRetryBaseDelayNanoseconds: 0
        )

        store.refresh()

        for _ in 0..<100 {
            if store.balance != nil {
                break
            }
            try await Task.sleep(nanoseconds: 1_000_000)
        }

        XCTAssertEqual(store.balance?.available, 42)
        XCTAssertFalse(store.isLoading)
        XCTAssertNil(store.errorMessage)
        let requestCount = await service.requestCount
        XCTAssertEqual(requestCount, 2)
    }

    func testCachedBalanceIsHydratedBeforeRefresh() async throws {
        let defaults = makeIsolatedDefaults()
        let expectedBalance = CreditBalance(available: 42, reserved: 3)
        let initialService = RetryingCreditService(
            failuresBeforeSuccess: 0,
            balance: expectedBalance
        )
        let initialStore = CreditBalanceStore(
            creditService: initialService,
            defaults: defaults,
            maxRefreshRetries: 0,
            refreshRetryBaseDelayNanoseconds: 0
        )

        initialStore.refresh()
        for _ in 0..<100 {
            if initialStore.balance != nil {
                break
            }
            try await Task.sleep(nanoseconds: 1_000_000)
        }

        XCTAssertEqual(initialStore.balance, expectedBalance)

        let refreshedService = RetryingCreditService(
            failuresBeforeSuccess: 1,
            balance: CreditBalance(available: 100, reserved: 0)
        )
        let restoredStore = CreditBalanceStore(
            creditService: refreshedService,
            defaults: defaults,
            maxRefreshRetries: 0,
            refreshRetryBaseDelayNanoseconds: 0
        )

        XCTAssertEqual(restoredStore.balance, expectedBalance)
        XCTAssertEqual(restoredStore.compactTitle, "✦ 42")
        XCTAssertEqual(restoredStore.largeTitle, "✦ 42 Credits")
        XCTAssertFalse(restoredStore.isLoading)

        restoredStore.refresh()
        try await Task.sleep(nanoseconds: 1_000_000)
        XCTAssertEqual(restoredStore.balance, expectedBalance)
        XCTAssertFalse(restoredStore.isLoading)
    }

    private func makeIsolatedDefaults() -> UserDefaults {
        let suiteName = "CreditBalanceStoreTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suiteName)!
        defaults.removePersistentDomain(forName: suiteName)
        addTeardownBlock {
            defaults.removePersistentDomain(forName: suiteName)
        }
        return defaults
    }
}

final class TextVideoPersistenceTests: XCTestCase {
    func testGenerationsSurvivePersistenceReloadAndDeletion() async throws {
        let suiteName = "TextVideoPersistenceTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suiteName)!
        defaults.removePersistentDomain(forName: suiteName)
        addTeardownBlock {
            defaults.removePersistentDomain(forName: suiteName)
        }

        let now = Date()
        let generation = TextVideoGeneration(
            id: "generation-1",
            providerTaskID: "provider-1",
            prompt: "A slow cinematic camera move",
            imageMediaID: "media-1",
            seconds: 5,
            resolution: "720p",
            aspectRatio: "9:16",
            status: .completed,
            progress: 1,
            videoURL: "https://cdn.example.com/video.mp4",
            errorMessage: nil,
            createdAt: now,
            updatedAt: now,
            completedAt: now
        )

        let persistence = UserDefaultsTextVideoPersistence(defaults: defaults)
        try await persistence.saveGenerations([generation])

        let restored = UserDefaultsTextVideoPersistence(defaults: defaults)
        let restoredGenerations = try await restored.loadGenerations()
        XCTAssertEqual(restoredGenerations, [generation])

        try await restored.removeGeneration(id: generation.id)
        let remainingGenerations = try await restored.loadGenerations()
        XCTAssertTrue(remainingGenerations.isEmpty)
    }

    func testEmptyRemoteListDoesNotEraseCachedGenerations() async throws {
        let suiteName = "TextVideoPersistenceTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suiteName)!
        defaults.removePersistentDomain(forName: suiteName)
        addTeardownBlock {
            defaults.removePersistentDomain(forName: suiteName)
            RetryURLProtocol.reset()
        }

        let now = Date()
        let generation = TextVideoGeneration(
            id: "generation-1",
            providerTaskID: "provider-1",
            prompt: "A cached cinematic camera move",
            imageMediaID: nil,
            seconds: 5,
            resolution: "720p",
            aspectRatio: "9:16",
            status: .completed,
            progress: 1,
            videoURL: "https://cdn.example.com/video.mp4",
            errorMessage: nil,
            createdAt: now,
            updatedAt: now,
            completedAt: now
        )

        let persistence = UserDefaultsTextVideoPersistence(defaults: defaults)
        try await persistence.saveGenerations([generation])

        RetryURLProtocol.responses = [
            .success(statusCode: 200, data: Data("[]".utf8))
        ]
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RetryURLProtocol.self]
        let session = URLSession(configuration: configuration)
        let apiClient = URLSessionAPIClient(
            baseURL: URL(string: "https://api.framemio.test")!,
            session: session,
            tokenProvider: StaticAuthTokenProvider(token: "token"),
            maxNetworkRetries: 0,
            maxTransientStatusRetries: 0
        )
        let service = RemoteTextToVideoService(
            apiClient: apiClient,
            persistence: persistence
        )

        let listed = try await service.list()
        let persisted = try await persistence.loadGenerations()

        XCTAssertEqual(listed, [generation])
        XCTAssertEqual(persisted, [generation])
    }

    func testImageToVideoUsesDefaultPromptWhenPromptIsEmpty() async throws {
        let now = Date()
        let generation = TextVideoGeneration(
            id: "generation-1",
            providerTaskID: "provider-1",
            prompt: TextVideoPromptDefaults.imageToVideo,
            imageMediaID: "media-1",
            seconds: 5,
            resolution: "720p",
            aspectRatio: "9:16",
            status: .queued,
            progress: 0,
            videoURL: nil,
            errorMessage: nil,
            createdAt: now,
            updatedAt: now,
            completedAt: nil
        )
        let response = try FrameMioJSONCoding.apiEncoder().encode(generation)
        let apiClient = CapturingTextVideoAPIClient(response: response)
        let service = RemoteTextToVideoService(
            apiClient: apiClient,
            persistence: MockTextVideoPersistence()
        )

        _ = try await service.create(
            prompt: "   ",
            duration: 5,
            resolution: "720p",
            aspectRatio: "9:16",
            idempotencyKey: "test-image-to-video",
            imageMediaID: "media-1"
        )

        let request = await apiClient.lastRequest
        let body = try XCTUnwrap(request?.body)
        let payload = try XCTUnwrap(
            JSONSerialization.jsonObject(with: body) as? [String: Any]
        )
        XCTAssertEqual(payload["prompt"] as? String, TextVideoPromptDefaults.imageToVideo)
        XCTAssertEqual(payload["image_media_id"] as? String, "media-1")
    }
}

private actor CapturingTextVideoAPIClient: APIClient {
    let response: Data
    private(set) var lastRequest: APIRequest?

    init(response: Data) {
        self.response = response
    }

    func data(for request: APIRequest) async throws -> Data {
        lastRequest = request
        return response
    }
}

private actor RetryingCreditService: CreditService {
    private var remainingFailures: Int
    private let storedBalance: CreditBalance
    private(set) var requestCount = 0

    init(failuresBeforeSuccess: Int, balance: CreditBalance) {
        remainingFailures = failuresBeforeSuccess
        storedBalance = balance
    }

    func balance() async throws -> CreditBalance {
        try await load()
    }

    func refreshBalance() async throws -> CreditBalance {
        try await load()
    }

    func quote(action: CreditAction, stats: CreditMediaStats) async throws -> CreditQuote {
        fatalError("not used in CreditBalanceStoreTests")
    }

    func creditPacks() async throws -> [CreditPack] {
        fatalError("not used in CreditBalanceStoreTests")
    }

    func grantPurchasedCredits(packID: String, purchaseID: String) async throws -> CreditBalance {
        fatalError("not used in CreditBalanceStoreTests")
    }

    func grantSubscriptionCycleCredits(plan: String, purchaseID: String, expiresAt: Date?) async throws -> CreditBalance {
        fatalError("not used in CreditBalanceStoreTests")
    }

    private func load() throws -> CreditBalance {
        requestCount += 1
        guard remainingFailures == 0 else {
            remainingFailures -= 1
            throw APIClientError.server(statusCode: 503)
        }
        return storedBalance
    }
}

final class StoryModelTests: XCTestCase {
    func testCreateStoryRequestEncodesCustomizeOptionsAndPrompt() throws {
        let request = CreateStoryRequest(
            context: .family,
            assets: [],
            analyses: [],
            options: StoryGenerationOptions(
                style: "warm",
                subtitlesEnabled: true,
                subtitleStyle: "bold",
                sceneDetailStyle: "moreDetails",
                durationMode: .fixed,
                targetDuration: 30,
                removeFillerAndPauses: false,
                instruction: "Focus on the people and keep the pacing gentle."
            )
        )

        let data = try FrameMioJSONCoding.apiEncoder().encode(request)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])

        XCTAssertEqual(object["style"] as? String, "warm")
        XCTAssertEqual(object["subtitlesEnabled"] as? Bool, true)
        XCTAssertEqual(object["subtitleStyle"] as? String, "bold")
        XCTAssertEqual(object["sceneDetailStyle"] as? String, "moreDetails")
        XCTAssertEqual(object["duration_mode"] as? String, "fixed")
        XCTAssertEqual(object["targetDuration"] as? Double, 30)
        XCTAssertEqual(object["remove_filler_and_pauses"] as? Bool, false)
        XCTAssertEqual(object["instruction"] as? String, "Focus on the people and keep the pacing gentle.")
    }

    func testCreateStoryRequestEncodesFullSourceDurationMode() throws {
        let request = CreateStoryRequest(
            context: .family,
            assets: [],
            analyses: [],
            options: StoryGenerationOptions(
                durationMode: .fullSource,
                targetDuration: nil,
                removeFillerAndPauses: true
            )
        )

        let data = try FrameMioJSONCoding.apiEncoder().encode(request)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])

        XCTAssertEqual(object["duration_mode"] as? String, "full_source")
        XCTAssertNil(object["targetDuration"])
        XCTAssertEqual(object["remove_filler_and_pauses"] as? Bool, true)
    }

    func testStoryDecodesWithoutOptionalDescriptionOrMoments() throws {
        let data = Data("""
        {
          "id": "story-1",
          "title": "A Day Together",
          "context": "Family",
          "createdAt": "2026-08-26T07:00:00Z",
          "duration": 21,
          "assetIDs": ["photo-1", "video-1"],
          "status": "ready",
          "videoURL": "https://example.test/story.mp4"
        }
        """.utf8)

        let decoder = FrameMioJSONCoding.apiDecoder()
        let story = try decoder.decode(Story.self, from: data)

        XCTAssertEqual(story.id, "story-1")
        XCTAssertNil(story.description)
        XCTAssertNil(story.moments)
        XCTAssertEqual(story.assetIDs, ["photo-1", "video-1"])
    }

    func testStoryDecodesBackendProgressMetadata() throws {
        let data = Data("""
        {
          "id": "story-progress",
          "title": "Weekend Clips",
          "context": "Special moments",
          "createdAt": "2026-08-26T07:00:00Z",
          "duration": 24,
          "assetIDs": ["video-1"],
          "status": "analyzing",
          "progressTitle": "Finding the best parts...",
          "progressDetail": "Scanning scenes, motion, faces, and meaningful highlights.",
          "progressValue": 0.615
        }
        """.utf8)

        let decoder = FrameMioJSONCoding.apiDecoder()
        let story = try decoder.decode(Story.self, from: data)

        XCTAssertEqual(story.progressTitle, "Finding the best parts...")
        XCTAssertEqual(story.progressDetail, "Scanning scenes, motion, faces, and meaningful highlights.")
        XCTAssertEqual(story.progressValue, 0.615)
    }

    func testStoryDecodesMixedPhotoAndVideoMoments() throws {
        let data = Data("""
        {
          "id": "story-2",
          "title": "Lake Morning",
          "context": "Family",
          "createdAt": "2026-08-26T07:00:00Z",
          "duration": 18,
          "assetIDs": ["photo-1", "video-1"],
          "status": "ready",
          "description": "Breakfast, a walk, and everyone playing by the water.",
          "moments": [
            {
              "id": "moment-photo",
              "mediaId": "photo-1",
              "type": "photo",
              "title": "Breakfast",
              "description": "Family breakfast at home.",
              "selected": true
            },
            {
              "momentId": "moment-video",
              "mediaId": "video-1",
              "type": "video",
              "title": "Everyone laughing",
              "description": "Everyone laughing together by the lake.",
              "selected": true
            }
          ],
          "availableMoments": [
            { "id": "photo-1", "mediaId": "photo-1", "type": "photo", "title": "Photo moment", "selected": true },
            { "id": "photo-2", "mediaId": "photo-2", "type": "photo", "title": "Sunset", "selected": false }
          ]
        }
        """.utf8)

        let decoder = FrameMioJSONCoding.apiDecoder()
        let story = try decoder.decode(Story.self, from: data)
        let moments = try XCTUnwrap(story.moments)
        let availableMoments = try XCTUnwrap(story.availableMoments)

        XCTAssertEqual(story.description, "Breakfast, a walk, and everyone playing by the water.")
        XCTAssertEqual(moments.map(\.mediaID), ["photo-1", "video-1"])
        XCTAssertEqual(moments.map(\.type), [.photo, .video])
        XCTAssertEqual(moments.last?.id, "moment-video")
        XCTAssertEqual(availableMoments.map(\.mediaID), ["photo-1", "photo-2"])
        XCTAssertEqual(availableMoments.last?.isSelected, false)
    }

    func testStoryEditRequestEncodesHighLevelIntent() throws {
        let request = StoryEditRequest(
            removeMomentIDs: ["moment-walking"],
            removeMediaIDs: [],
            addMediaIDs: ["media-sunset"],
            orderedMediaIDs: ["media-sunset", "media-lake"],
            replace: [StoryMomentReplacement(fromMomentID: "moment-lake", fromMediaID: "media-lake", toMediaID: "media-sunset")],
            style: "cinematic",
            targetDuration: 15,
            instruction: "Make it feel like summer vacation."
        )

        let data = try FrameMioJSONCoding.apiEncoder().encode(request)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])

        XCTAssertEqual(object["removeMomentIds"] as? [String], ["moment-walking"])
        XCTAssertEqual(object["addMediaIds"] as? [String], ["media-sunset"])
        XCTAssertEqual(object["orderedMediaIds"] as? [String], ["media-sunset", "media-lake"])
        XCTAssertEqual(object["style"] as? String, "cinematic")
        XCTAssertEqual(object["targetDuration"] as? Double, 15)
        XCTAssertEqual(object["instruction"] as? String, "Make it feel like summer vacation.")
        let replacements = try XCTUnwrap(object["replace"] as? [[String: Any]])
        XCTAssertEqual(replacements.first?["fromMomentId"] as? String, "moment-lake")
        XCTAssertEqual(replacements.first?["toMediaId"] as? String, "media-sunset")
    }

    func testStoryEditRequestEncodesFullSourceDurationMode() throws {
        let request = StoryEditRequest(
            removeMomentIDs: [],
            removeMediaIDs: [],
            addMediaIDs: [],
            replace: [],
            durationMode: .fullSource,
            targetDuration: nil,
            removeFillerAndPauses: true,
            instruction: "Keep the full video but clean pauses."
        )

        let data = try FrameMioJSONCoding.apiEncoder().encode(request)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])

        XCTAssertEqual(object["duration_mode"] as? String, "full_source")
        XCTAssertNil(object["targetDuration"])
        XCTAssertEqual(object["remove_filler_and_pauses"] as? Bool, true)
    }
}
