import Foundation

enum TaskCancellation {
    static func check() throws {
        try Task.checkCancellation()
    }
}
