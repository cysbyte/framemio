import Foundation

enum AppRoute: Hashable {
    case mediaPicker(context: MomentContext)
    case storyCreation(context: MomentContext, assets: [MediaAsset])
    case storyPreview(storyID: String)
    case settings
}

