import Foundation

private func directUploadDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][DirectUpload] \(message())")
#endif
}

final class URLSessionDirectObjectStorageClient: DirectObjectStorageClient, @unchecked Sendable {
    private let foregroundDelegate: DirectUploadSessionDelegate
    private let backgroundDelegate: DirectUploadSessionDelegate
    private let foregroundSession: URLSession
    private let backgroundSession: URLSession
    private let maximumUploadRetries: Int

    init(backgroundIdentifier: String = "com.framemio.direct-uploads", maximumUploadRetries: Int = 2) {
        let foregroundDelegate = DirectUploadSessionDelegate()
        let backgroundDelegate = DirectUploadSessionDelegate()
        let foregroundConfiguration = URLSessionConfiguration.default
        foregroundConfiguration.waitsForConnectivity = true
        foregroundConfiguration.timeoutIntervalForRequest = 60
        foregroundConfiguration.timeoutIntervalForResource = 60 * 60
        let backgroundConfiguration = URLSessionConfiguration.background(withIdentifier: backgroundIdentifier)
        backgroundConfiguration.sessionSendsLaunchEvents = true
        backgroundConfiguration.isDiscretionary = false
        backgroundConfiguration.waitsForConnectivity = true
        backgroundConfiguration.timeoutIntervalForRequest = 60
        backgroundConfiguration.timeoutIntervalForResource = 60 * 60

        self.foregroundDelegate = foregroundDelegate
        self.backgroundDelegate = backgroundDelegate
        self.foregroundSession = URLSession(
            configuration: foregroundConfiguration,
            delegate: foregroundDelegate,
            delegateQueue: OperationQueue()
        )
        self.backgroundSession = URLSession(
            configuration: backgroundConfiguration,
            delegate: backgroundDelegate,
            delegateQueue: OperationQueue()
        )
        self.maximumUploadRetries = max(0, maximumUploadRetries)
    }

    func upload(
        fileURL: URL,
        to presignedURL: URL,
        headers: [String: String],
        usesBackgroundSession: Bool,
        progress: @escaping @Sendable (Int64, Int64) -> Void
    ) async throws -> DirectObjectUploadResult {
        var attempt = 0
        while true {
            do {
                let result = try await uploadOnce(
                    fileURL: fileURL,
                    presignedURL: presignedURL,
                    headers: headers,
                    usesBackgroundSession: usesBackgroundSession,
                    progress: progress
                )
                return result
            } catch is CancellationError {
                throw CancellationError()
            } catch {
                guard attempt < maximumUploadRetries, isRetryableUploadError(error) else {
                    throw error
                }
                attempt += 1
                directUploadDebugLog("retrying upload host=\(presignedURL.host ?? "nil") attempt=\(attempt) error=\(error.localizedDescription)")
                try await Task.sleep(for: .milliseconds(500 * (1 << min(attempt - 1, 3))))
            }
        }
    }

    private func uploadOnce(
        fileURL: URL,
        presignedURL: URL,
        headers: [String: String],
        usesBackgroundSession: Bool,
        progress: @escaping @Sendable (Int64, Int64) -> Void
    ) async throws -> DirectObjectUploadResult {
        var request = URLRequest(url: presignedURL)
        request.httpMethod = "PUT"
        for (field, value) in headers {
            request.setValue(value, forHTTPHeaderField: field)
        }

        let session = usesBackgroundSession ? backgroundSession : foregroundSession
        let delegate = usesBackgroundSession ? backgroundDelegate : foregroundDelegate
        let task = session.uploadTask(with: request, fromFile: fileURL)
        let fileSize = (try? FileManager.default.attributesOfItem(atPath: fileURL.path)[.size] as? NSNumber)?.int64Value ?? -1
        directUploadDebugLog("task created id=\(task.taskIdentifier) background=\(usesBackgroundSession) host=\(presignedURL.host ?? "nil") file_bytes=\(fileSize) headers=\(headers.keys.sorted().joined(separator: ","))")

        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                delegate.register(
                    task: task,
                    continuation: continuation,
                    progress: progress
                )
                directUploadDebugLog("task resume id=\(task.taskIdentifier)")
                task.resume()
            }
        } onCancel: {
            task.cancel()
        }
    }

    private func isRetryableUploadError(_ error: Error) -> Bool {
        if let urlError = error as? URLError {
            switch urlError.code {
            case .networkConnectionLost, .timedOut, .cannotConnectToHost, .cannotFindHost, .dnsLookupFailed, .notConnectedToInternet:
                return true
            default:
                return false
            }
        }
        if case let AppError.server(statusCode) = error {
            return statusCode == 408 || statusCode == 425 || statusCode == 429 || statusCode == 500 || statusCode == 502 || statusCode == 503 || statusCode == 504
        }
        return false
    }
}

private final class DirectUploadSessionDelegate: NSObject, URLSessionDataDelegate, URLSessionTaskDelegate, @unchecked Sendable {
    private final class State: @unchecked Sendable {
        let continuation: CheckedContinuation<DirectObjectUploadResult, Error>
        let progress: @Sendable (Int64, Int64) -> Void
        var statusCode: Int?
        var eTag: String?

        init(
            continuation: CheckedContinuation<DirectObjectUploadResult, Error>,
            progress: @escaping @Sendable (Int64, Int64) -> Void
        ) {
            self.continuation = continuation
            self.progress = progress
        }
    }

    private let lock = NSLock()
    private var states: [Int: State] = [:]

    func register(
        task: URLSessionUploadTask,
        continuation: CheckedContinuation<DirectObjectUploadResult, Error>,
        progress: @escaping @Sendable (Int64, Int64) -> Void
    ) {
        lock.lock()
        states[task.taskIdentifier] = State(continuation: continuation, progress: progress)
        lock.unlock()
    }

    func urlSession(
        _ session: URLSession,
        dataTask: URLSessionDataTask,
        didReceive response: URLResponse,
        completionHandler: @escaping (URLSession.ResponseDisposition) -> Void
    ) {
        guard let httpResponse = response as? HTTPURLResponse else {
            completionHandler(.cancel)
            return
        }
        lock.lock()
        states[dataTask.taskIdentifier]?.statusCode = httpResponse.statusCode
        states[dataTask.taskIdentifier]?.eTag = httpResponse.value(forHTTPHeaderField: "ETag")
        directUploadDebugLog("response received id=\(dataTask.taskIdentifier) status=\(httpResponse.statusCode) etag_present=\(httpResponse.value(forHTTPHeaderField: "ETag") != nil)")
        lock.unlock()
        completionHandler(.allow)
    }

    func urlSession(
        _ session: URLSession,
        task: URLSessionTask,
        didSendBodyData bytesSent: Int64,
        totalBytesSent: Int64,
        totalBytesExpectedToSend: Int64
    ) {
        lock.lock()
        let progress = states[task.taskIdentifier]?.progress
        lock.unlock()
        progress?(totalBytesSent, totalBytesExpectedToSend)
    }

    func urlSession(
        _ session: URLSession,
        task: URLSessionTask,
        didCompleteWithError error: Error?
    ) {
        lock.lock()
        let state = states.removeValue(forKey: task.taskIdentifier)
        lock.unlock()
        guard let state else {
            return
        }

        if let error {
            directUploadDebugLog("task failed id=\(task.taskIdentifier) error=\(error.localizedDescription)")
            state.continuation.resume(throwing: error)
            return
        }

        let httpResponse = task.response as? HTTPURLResponse
        let statusCode = state.statusCode ?? httpResponse?.statusCode ?? 0
        guard (200..<300).contains(statusCode) else {
            directUploadDebugLog("task completed non2xx id=\(task.taskIdentifier) status=\(statusCode)")
            state.continuation.resume(throwing: AppError.server(statusCode: statusCode))
            return
        }
        directUploadDebugLog("task completed id=\(task.taskIdentifier) status=\(statusCode) etag_present=\((state.eTag ?? httpResponse?.value(forHTTPHeaderField: "ETag")) != nil)")
        state.continuation.resume(
            returning: DirectObjectUploadResult(
                statusCode: statusCode,
                eTag: state.eTag ?? httpResponse?.value(forHTTPHeaderField: "ETag")
            )
        )
    }
}
