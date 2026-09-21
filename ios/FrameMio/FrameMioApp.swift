import Foundation
import OSLog
import SwiftUI

@main
struct FrameMioApp: App {
    private static let startupLogger = Logger(subsystem: "com.framemio.app", category: "Startup")
    private let dependencies: AppDependencies

    init() {
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "unknown"
        let build = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "unknown"
        let message = "FrameMio startup log test version=\(version) build=\(build)"
        Self.startupLogger.notice("\(message, privacy: .public)")
        NSLog("[FrameMio][Startup] %@", message)
        if let data = "[FrameMio][Startup][stderr] \(message)\n".data(using: .utf8) {
            FileHandle.standardError.write(data)
        }
        dependencies = AppDependencies.live()
    }

    var body: some Scene {
        WindowGroup {
            AppRootView(dependencies: dependencies)
                .task {
                    await warmStoreKitProductCaches()
                }
        }
    }
}
