import Foundation
import Security

enum APIClientError: Error, Sendable {
    case invalidURL
    case invalidResponse
    case server(statusCode: Int)
}

final class URLSessionAPIClient: APIClient, @unchecked Sendable {
    private let baseURL: URL
    private let session: URLSession
    private let tokenProvider: AuthTokenProvider

    init(
        baseURL: URL,
        session: URLSession = .shared,
        tokenProvider: AuthTokenProvider
    ) {
        self.baseURL = baseURL
        self.session = session
        self.tokenProvider = tokenProvider
    }

    func data(for request: APIRequest) async throws -> Data {
        var url = baseURL
        for component in request.path.split(separator: "/") {
            url.appendPathComponent(String(component))
        }

        var urlRequest = URLRequest(url: url)
        urlRequest.httpMethod = request.method.rawValue
        urlRequest.httpBody = request.body
        urlRequest.setValue("application/json", forHTTPHeaderField: "Accept")

        if request.body != nil {
            urlRequest.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }

        for (field, value) in request.headers {
            urlRequest.setValue(value, forHTTPHeaderField: field)
        }

        if let token = await tokenProvider.accessToken() {
            urlRequest.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }

        let (data, response) = try await session.data(for: urlRequest)
        try Task.checkCancellation()

        guard let httpResponse = response as? HTTPURLResponse else {
            throw APIClientError.invalidResponse
        }

        guard (200..<300).contains(httpResponse.statusCode) else {
            throw APIClientError.server(statusCode: httpResponse.statusCode)
        }

        return data
    }
}

final class KeychainAuthTokenProvider: AuthTokenProvider, @unchecked Sendable {
    private let service: String
    private let account: String

    init(service: String = "com.framemio.app", account: String = "access-token") {
        self.service = service
        self.account = account
    }

    func accessToken() async -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne
        ]

        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        guard status == errSecSuccess, let data = result as? Data else {
            return nil
        }
        return String(data: data, encoding: .utf8)
    }
}

final class MockAPIClient: APIClient, @unchecked Sendable {
    func data(for request: APIRequest) async throws -> Data {
        try Task.checkCancellation()
        return Data()
    }
}

