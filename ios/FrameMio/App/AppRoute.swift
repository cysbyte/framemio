import Foundation

enum AppRoute: Hashable {
    case allStories
    case mediaPicker(context: MomentContext)
    case storyCreation(context: MomentContext, assets: [MediaAsset])
    case storyPreview(story: Story)
    case storyCreating(context: MomentContext, assets: [MediaAsset], options: StoryGenerationOptions, launchID: UUID)
}
