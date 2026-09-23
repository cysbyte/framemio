import Foundation

enum AppRoute: Hashable {
    case allProjects
    case mediaPicker(draft: VideoProjectDraft)
    case projectConfiguration(draft: VideoProjectDraft, assets: [MediaAsset])
    case storyCreation(draft: VideoProjectDraft, assets: [MediaAsset])
    case storyPreview(story: Story)
    case storyCreating(draft: VideoProjectDraft, assets: [MediaAsset], launchID: UUID)
    case textToVideo
}
