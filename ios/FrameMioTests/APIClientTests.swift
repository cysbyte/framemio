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
