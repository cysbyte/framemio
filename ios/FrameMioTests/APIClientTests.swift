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
            for: APIRequest(method: .post, path: "/v1/media/upload-sessions", body: Data("{}".utf8))
        )

        XCTAssertEqual(String(data: data, encoding: .utf8), "{\"ok\":true}")
        let requestCount = RetryURLProtocol.requestCount
        XCTAssertEqual(requestCount, 2)
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

    static func reset() {
        lock.lock()
        storedResponses = []
        storedRequestCount = 0
        lock.unlock()
    }

    override class func canInit(with request: URLRequest) -> Bool {
        true
    }

    override class func canonicalRequest(for request: URLRequest) -> URLRequest {
        request
    }

    override func startLoading() {
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
}

final class StoryModelTests: XCTestCase {
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
            replace: [StoryMomentReplacement(fromMomentID: "moment-lake", fromMediaID: "media-lake", toMediaID: "media-sunset")],
            style: "cinematic",
            targetDuration: 15,
            instruction: "Make it feel like summer vacation."
        )

        let data = try FrameMioJSONCoding.apiEncoder().encode(request)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])

        XCTAssertEqual(object["removeMomentIds"] as? [String], ["moment-walking"])
        XCTAssertEqual(object["addMediaIds"] as? [String], ["media-sunset"])
        XCTAssertEqual(object["style"] as? String, "cinematic")
        XCTAssertEqual(object["targetDuration"] as? Double, 15)
        XCTAssertEqual(object["instruction"] as? String, "Make it feel like summer vacation.")
        let replacements = try XCTUnwrap(object["replace"] as? [[String: Any]])
        XCTAssertEqual(replacements.first?["fromMomentId"] as? String, "moment-lake")
        XCTAssertEqual(replacements.first?["toMediaId"] as? String, "media-sunset")
    }
}
