import Foundation

enum AppError: LocalizedError, Hashable, Sendable {
    case notAuthorized
    case invalidConfiguration
    case networkUnavailable
    case server(statusCode: Int)
    case insufficientCredits(required: Int, available: Int)
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
        case let .insufficientCredits(required, available):
            "You need \(required) credits. You have \(available) credits."
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
            case let .server(statusCode, payload):
                if let payload, let mappedError = backendError(from: payload, statusCode: statusCode) {
                    return mappedError
                }
                return .server(statusCode: statusCode)
            }
        }
        if (error as NSError).domain == NSURLErrorDomain {
            return .networkUnavailable
        }
        return .message(error.localizedDescription)
    }

    private static func backendError(from payload: Data, statusCode: Int) -> AppError? {
        guard let response = try? JSONDecoder().decode(BackendErrorPayload.self, from: payload) else {
            return nil
        }
        switch response.error {
        case "insufficient_credits":
            return .insufficientCredits(required: response.required ?? 0, available: response.available ?? 0)
        case "story_too_large":
            return .message(response.reason ?? response.message ?? "This story is too large for this action.")
        default:
            if let message = response.message, !message.isEmpty, statusCode < 500 || response.error.hasPrefix("transcription_") {
                return .message(message)
            }
            return nil
        }
    }
}

private struct BackendErrorPayload: Decodable {
    let error: String
    let message: String?
    let reason: String?
    let required: Int?
    let available: Int?
}
