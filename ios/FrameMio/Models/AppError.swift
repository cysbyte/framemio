import Foundation

enum AppError: LocalizedError, Hashable, Sendable {
    case notAuthorized
    case invalidConfiguration
    case networkUnavailable
    case server(statusCode: Int)
    case invalidResponse
    case persistenceFailure
    case unsupportedMedia
    case cancelled
    case message(String)

    var errorDescription: String? {
        switch self {
        case .notAuthorized:
            "Photo access is needed to find your memories."
        case .invalidConfiguration:
            "FrameMio is not configured for this environment yet."
        case .networkUnavailable:
            "We couldn't reach FrameMio. Check your connection and try again."
        case let .server(statusCode):
            "FrameMio returned an unexpected response (\(statusCode))."
        case .invalidResponse:
            "The response could not be understood."
        case .persistenceFailure:
            "Your local stories could not be saved."
        case .unsupportedMedia:
            "This media format is not supported yet."
        case .cancelled:
            "The operation was cancelled."
        case let .message(message):
            message
        }
    }

    static func from(_ error: Error) -> AppError {
        if error is CancellationError {
            return .cancelled
        }
        if let appError = error as? AppError {
            return appError
        }
        if let apiError = error as? APIClientError {
            switch apiError {
            case .invalidURL:
                return .invalidConfiguration
            case .invalidResponse:
                return .invalidResponse
            case let .server(statusCode):
                return .server(statusCode: statusCode)
            }
        }
        if (error as NSError).domain == NSURLErrorDomain {
            return .networkUnavailable
        }
        return .message(error.localizedDescription)
    }
}
