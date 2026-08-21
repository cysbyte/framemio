import Foundation

final class URLSessionDirectObjectStorageClient: DirectObjectStorageClient, @unchecked Sendable {
    private let foregroundDelegate: DirectUploadSessionDelegate
    private let backgroundDelegate: DirectUploadSessionDelegate
    private let foregroundSession: URLSession
    private let backgroundSession: URLSession

    init(backgroundIdentifier: String = "com.framemio.direct-uploads") {
        let foregroundDelegate = DirectUploadSessionDelegate()
        let backgroundDelegate = DirectUploadSessionDelegate()
        let foregroundConfiguration = URLSessionConfiguration.default
        foregroundConfiguration.waitsForConnectivity = true
        let backgroundConfiguration = URLSessionConfiguration.background(withIdentifier: backgroundIdentifier)
        backgroundConfiguration.sessionSendsLaunchEvents = true
        backgroundConfiguration.isDiscretionary = false
        backgroundConfiguration.waitsForConnectivity = true

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
    }

    func upload(
        fileURL: URL,
        to presignedURL: URL,
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

        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                delegate.register(
                    task: task,
                    continuation: continuation,
                    progress: progress
                )
                task.resume()
            }
        } onCancel: {
            task.cancel()
        }
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
            state.continuation.resume(throwing: error)
            return
        }

        let statusCode = state.statusCode ?? (task.response as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(statusCode) else {
            state.continuation.resume(throwing: AppError.server(statusCode: statusCode))
            return
        }
        state.continuation.resume(
            returning: DirectObjectUploadResult(statusCode: statusCode, eTag: state.eTag)
        )
    }
}
