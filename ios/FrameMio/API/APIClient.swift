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

        if httpResponse.statusCode == 401,
           urlRequest.value(forHTTPHeaderField: "Authorization") != nil,
           let retryData = try await retryWithFreshToken(urlRequest) {
            return retryData
        }

        guard (200..<300).contains(httpResponse.statusCode) else {
            throw APIClientError.server(statusCode: httpResponse.statusCode)
        }

        return data
    }

    private func retryWithFreshToken(_ originalRequest: URLRequest) async throws -> Data? {
        await tokenProvider.invalidateAccessToken()
        guard let token = await tokenProvider.accessToken() else {
            return nil
        }
        var request = originalRequest
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let (data, response) = try await session.data(for: request)
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
    private let installationAccount: String
    private let baseURL: URL?
    private let session: URLSession
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()

    init(
        service: String = "com.framemio.app",
        account: String = "access-token",
        installationAccount: String = "anonymous-installation-id",
        baseURL: URL? = nil,
        session: URLSession = .shared
    ) {
        self.service = service
        self.account = account
        self.installationAccount = installationAccount
        self.baseURL = baseURL
        self.session = session
    }

    func accessToken() async -> String? {
        if let token = storedValue(account: account) {
            return token
        }
        guard let baseURL else {
            return nil
        }
        return await issueAnonymousToken(baseURL: baseURL)
    }

    func invalidateAccessToken() async {
        deleteValue(account: account)
    }

    private func issueAnonymousToken(baseURL: URL) async -> String? {
        let installationID = storedValue(account: installationAccount) ?? saveNewInstallationID()
        var url = baseURL
        for component in "v1/auth/anonymous".split(separator: "/") {
            url.appendPathComponent(String(component))
        }

        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try? encoder.encode(AnonymousAuthRequest(installationID: installationID))

        do {
            let (data, response) = try await session.data(for: request)
            guard let httpResponse = response as? HTTPURLResponse,
                  (200..<300).contains(httpResponse.statusCode) else {
                return nil
            }
            let token = try decoder.decode(AnonymousAuthResponse.self, from: data).accessToken
            saveValue(token, account: account)
            return token
        } catch {
            return nil
        }
    }

    private func saveNewInstallationID() -> String {
        let installationID = UUID().uuidString.lowercased()
        saveValue(installationID, account: installationAccount)
        return installationID
    }

    private func storedValue(account: String) -> String? {
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

    private func saveValue(_ value: String, account: String) {
        guard let data = value.data(using: .utf8) else { return }
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account
        ]
        let attributes: [String: Any] = [kSecValueData as String: data]
        let status = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            var item = query
            item[kSecValueData as String] = data
            SecItemAdd(item as CFDictionary, nil)
        }
    }

    private func deleteValue(account: String) {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account
        ]
        SecItemDelete(query as CFDictionary)
    }
}

private struct AnonymousAuthRequest: Encodable {
    let installationID: String

    enum CodingKeys: String, CodingKey {
        case installationID = "installation_id"
    }
}

private struct AnonymousAuthResponse: Decodable {
    let accessToken: String

    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token"
    }
}

final class MockAPIClient: APIClient, @unchecked Sendable {
    func data(for request: APIRequest) async throws -> Data {
        try Task.checkCancellation()
        return Data()
    }
}

enum FrameMioJSONCoding {
    static func apiEncoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }

    static func apiDecoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let value = try container.decode(String.self)
            if let date = fractionalISO8601.date(from: value) ?? standardISO8601.date(from: value) {
                return date
            }
            throw DecodingError.dataCorruptedError(
                in: container,
                debugDescription: "Invalid ISO 8601 date: \(value)"
            )
        }
        return decoder
    }

    private static let standardISO8601: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter
    }()

    private static let fractionalISO8601: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()
}
