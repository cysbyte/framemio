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
