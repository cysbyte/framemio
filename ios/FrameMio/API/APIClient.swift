import Foundation
import Security

private func apiClientDebugLog(_ message: @autoclosure () -> String) {
    print("[FrameMio][APIClient] \(message())")
}

enum APIClientError: Error, Sendable {
    case invalidURL
    case invalidResponse
    case server(statusCode: Int, payload: Data? = nil)
}

final class URLSessionAPIClient: APIClient, @unchecked Sendable {
    private let baseURL: URL
    private let session: URLSession
    private let tokenProvider: AuthTokenProvider
    private let maxNetworkRetries: Int

    init(
        baseURL: URL,
        session: URLSession = .shared,
        tokenProvider: AuthTokenProvider,
        maxNetworkRetries: Int = 1
    ) {
        self.baseURL = baseURL
        self.session = session
        self.tokenProvider = tokenProvider
        self.maxNetworkRetries = max(0, maxNetworkRetries)
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

        apiClientDebugLog("request start method=\(request.method.rawValue) path=\(request.path) host=\(url.host ?? "nil")")
        let (data, httpResponse) = try await dataWithTransientRetry(for: urlRequest)
        apiClientDebugLog("request response method=\(request.method.rawValue) path=\(request.path) status=\(httpResponse.statusCode)")

        if httpResponse.statusCode == 401,
           urlRequest.value(forHTTPHeaderField: "Authorization") != nil,
           let retryData = try await retryWithFreshToken(urlRequest) {
            return retryData
        }

        guard (200..<300).contains(httpResponse.statusCode) else {
            throw APIClientError.server(statusCode: httpResponse.statusCode, payload: data)
        }

        return data
    }

    private func dataWithTransientRetry(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        var attempts = 0
        while true {
            do {
                let (data, response) = try await session.data(for: request)
                try Task.checkCancellation()
                guard let httpResponse = response as? HTTPURLResponse else {
                    throw APIClientError.invalidResponse
                }
                return (data, httpResponse)
            } catch {
                try Task.checkCancellation()
                guard attempts < maxNetworkRetries, isTransientNetworkFailure(error) else {
                    apiClientDebugLog("request failed url=\(request.url?.absoluteString ?? "nil") attempts=\(attempts + 1) error=\(error.localizedDescription)")
                    throw error
                }
                attempts += 1
                apiClientDebugLog("request retry url=\(request.url?.absoluteString ?? "nil") attempt=\(attempts) error=\(error.localizedDescription)")
            }
        }
    }

    private func isTransientNetworkFailure(_ error: Error) -> Bool {
        guard let urlError = error as? URLError else {
            return false
        }
        switch urlError.code {
        case .networkConnectionLost, .timedOut, .cannotConnectToHost, .cannotFindHost, .dnsLookupFailed, .notConnectedToInternet:
            return true
        default:
            return false
        }
    }

    private func retryWithFreshToken(_ originalRequest: URLRequest) async throws -> Data? {
        await tokenProvider.invalidateAccessToken()
        guard let token = await tokenProvider.accessToken() else {
            return nil
        }
        var request = originalRequest
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let (data, httpResponse) = try await dataWithTransientRetry(for: request)
        guard (200..<300).contains(httpResponse.statusCode) else {
            throw APIClientError.server(statusCode: httpResponse.statusCode, payload: data)
        }
        return data
    }
}

final class KeychainAuthTokenProvider: AuthTokenProvider, @unchecked Sendable {
    private let service: String
    private let account: String
    private let refreshAccount: String
    private let installationAccount: String
    private let baseURL: URL?
    private let session: URLSession
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()

    init(
        service: String = "com.framemio.app",
        account: String = "access-token",
        refreshAccount: String = "refresh-token",
        installationAccount: String = "anonymous-installation-id",
        baseURL: URL? = nil,
        session: URLSession = .shared
    ) {
        self.service = service
        self.account = account
        self.refreshAccount = refreshAccount
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
        if let refreshToken = storedValue(account: refreshAccount),
           let token = await refreshAccessToken(baseURL: baseURL, refreshToken: refreshToken) {
            return token
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
            let token = try saveAuthResponse(data)
            return token
        } catch {
            return nil
        }
    }

    private func refreshAccessToken(baseURL: URL, refreshToken: String) async -> String? {
        var url = baseURL
        for component in "v1/auth/refresh".split(separator: "/") {
            url.appendPathComponent(String(component))
        }

        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try? encoder.encode(RefreshAuthRequest(refreshToken: refreshToken))

        do {
            let (data, response) = try await session.data(for: request)
            guard let httpResponse = response as? HTTPURLResponse,
                  (200..<300).contains(httpResponse.statusCode) else {
                deleteValue(account: refreshAccount)
                return nil
            }
            return try saveAuthResponse(data)
        } catch {
            return nil
        }
    }

    private func saveAuthResponse(_ data: Data) throws -> String {
        let response = try decoder.decode(AnonymousAuthResponse.self, from: data)
        saveValue(response.accessToken, account: account)
        if let refreshToken = response.refreshToken {
            saveValue(refreshToken, account: refreshAccount)
        }
        return response.accessToken
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

private struct RefreshAuthRequest: Encodable {
    let refreshToken: String

    enum CodingKeys: String, CodingKey {
        case refreshToken = "refresh_token"
    }
}

private struct AnonymousAuthResponse: Decodable {
    let accessToken: String
    let refreshToken: String?

    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token"
        case refreshToken = "refresh_token"
    }
}

final class RemoteCreditService: CreditService, @unchecked Sendable {
    private let apiClient: APIClient
    private let decoder: JSONDecoder
    private let encoder: JSONEncoder
    private let balanceCache = RemoteCreditBalanceCache(ttlSeconds: 2)

    init(apiClient: APIClient) {
        self.apiClient = apiClient
        decoder = FrameMioJSONCoding.apiDecoder()
        encoder = FrameMioJSONCoding.apiEncoder()
    }

    func balance() async throws -> CreditBalance {
        try await balanceCache.balance {
            let data = try await self.apiClient.data(for: APIRequest(method: .get, path: "/v1/me/credits"))
            return try FrameMioJSONCoding.apiDecoder().decode(CreditBalance.self, from: data)
        }
    }

    func creditPacks() async throws -> [CreditPack] {
        struct Response: Decodable {
            let packs: [CreditPack]
        }

        let data = try await apiClient.data(for: APIRequest(method: .get, path: "/v1/credit-packs"))
        return try decoder.decode(Response.self, from: data).packs
    }

    func grantSubscriptionCycle(plan: String, purchaseID: String, expiresAt: Date?) async throws {
        struct Request: Encodable {
            let plan: String
            let purchaseID: String
            let expiresAt: Date?

            enum CodingKeys: String, CodingKey {
                case plan
                case purchaseID = "purchase_id"
                case expiresAt = "expires_at"
            }
        }

        let body = try encoder.encode(Request(plan: plan, purchaseID: purchaseID, expiresAt: expiresAt))
        _ = try await apiClient.data(for: APIRequest(method: .post, path: "/v1/me/credits/subscription-cycle", body: body))
        await balanceCache.invalidate()
    }

    func grantPurchasedCredits(packID: String, purchaseID: String) async throws -> CreditBalance {
        struct Request: Encodable {
            let packID: String
            let purchaseID: String

            enum CodingKeys: String, CodingKey {
                case packID = "pack_id"
                case purchaseID = "purchase_id"
            }
        }
        struct Response: Decodable {
            let balance: CreditBalance
        }

        let body = try encoder.encode(Request(packID: packID, purchaseID: purchaseID))
        let data = try await apiClient.data(for: APIRequest(method: .post, path: "/v1/me/credits/purchased", body: body))
        let balance = try decoder.decode(Response.self, from: data).balance
        await balanceCache.store(balance)
        return balance
    }
}

private actor RemoteCreditBalanceCache {
    private let ttlSeconds: TimeInterval
    private var cached: CreditBalance?
    private var fetchedAt: Date?
    private var inFlight: Task<CreditBalance, Error>?

    init(ttlSeconds: TimeInterval) {
        self.ttlSeconds = ttlSeconds
    }

    func balance(load: @escaping @Sendable () async throws -> CreditBalance) async throws -> CreditBalance {
        if let inFlight {
            return try await inFlight.value
        }
        if let cached, let fetchedAt, Date().timeIntervalSince(fetchedAt) < ttlSeconds {
            return cached
        }

        let task = Task { try await load() }
        inFlight = task
        do {
            let balance = try await task.value
            store(balance)
            return balance
        } catch {
            inFlight = nil
            throw error
        }
    }

    func store(_ balance: CreditBalance) {
        cached = balance
        fetchedAt = Date()
        inFlight = nil
    }

    func invalidate() {
        cached = nil
        fetchedAt = nil
        inFlight = nil
    }
}

final class MockCreditService: CreditService, @unchecked Sendable {
    private let balanceValue: CreditBalance

    init(balance: CreditBalance = CreditBalance(available: AppConfiguration.initialCreditGrant, reserved: 0)) {
        balanceValue = balance
    }

    func balance() async throws -> CreditBalance {
        balanceValue
    }

    func creditPacks() async throws -> [CreditPack] {
        [
            CreditPack(id: "small", title: "Small Pack", subtitle: "A few more story moments", productID: FrameMioCreditPackCatalog.smallProductID, credits: 100),
            CreditPack(id: "medium", title: "Medium Pack", subtitle: "More room for memories", productID: FrameMioCreditPackCatalog.mediumProductID, credits: 300),
            CreditPack(id: "large", title: "Large Pack", subtitle: "Keep creating all week", productID: FrameMioCreditPackCatalog.largeProductID, credits: 700)
        ]
    }

    func grantSubscriptionCycle(plan: String, purchaseID: String, expiresAt: Date?) async throws {
    }

    func grantPurchasedCredits(packID: String, purchaseID: String) async throws -> CreditBalance {
        balanceValue
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
