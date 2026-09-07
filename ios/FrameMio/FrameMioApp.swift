import SwiftUI

@main
struct FrameMioApp: App {
    private let dependencies = AppDependencies.live()

    var body: some Scene {
        WindowGroup {
            AppRootView(dependencies: dependencies)
                .task {
                    await warmStoreKitProductCaches()
                }
        }
    }
}
