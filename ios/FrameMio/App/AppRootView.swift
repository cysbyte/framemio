import SwiftUI

struct AppRootView: View {
    let dependencies: AppDependencies
    @State private var path: [AppRoute] = []
    @StateObject private var homeViewModel: HomeViewModel

    init(dependencies: AppDependencies = .preview(), homeMockState: HomeMockState? = nil) {
        self.dependencies = dependencies
        _homeViewModel = StateObject(wrappedValue: HomeViewModel(storyService: dependencies.storyService, mockState: homeMockState))
    }

    var body: some View {
        NavigationStack(path: $path) {
            HomeView(
                viewModel: homeViewModel,
                onCreateStory: { context in
                    path.append(.mediaPicker(context: context))
                },
                onOpenStory: { story in
                    path.append(.storyPreview(storyID: story.id))
                },
                onOpenSettings: {
                    path.append(.settings)
                }
            )
            .navigationDestination(for: AppRoute.self) { route in
                destination(for: route)
            }
        }
        .tint(FrameMioColors.accent)
    }

    @ViewBuilder
    private func destination(for route: AppRoute) -> some View {
        switch route {
        case let .mediaPicker(context):
            MediaPickerView(
                viewModel: MediaPickerViewModel(
                    context: context,
                    photoLibrary: dependencies.photoLibrary,
                    selectionService: dependencies.mediaSelection
                ),
                onContinue: { assets in
                    path.append(.storyCreation(context: context, assets: assets))
                }
            )
        case let .storyCreation(context, assets):
            StoryCreationView(
                viewModel: StoryCreationViewModel(
                    context: context,
                    assets: assets,
                    photoLibrary: dependencies.photoLibrary,
                    analysisService: dependencies.mediaAnalysis,
                    mediaUploadService: dependencies.mediaUploadService,
                    storyService: dependencies.storyService
                ),
                onCreated: { story in
                    path.append(.storyPreview(storyID: story.id))
                }
            )
        case let .storyPreview(storyID):
            StoryPreviewView(
                viewModel: StoryPreviewViewModel(storyID: storyID, storyService: dependencies.storyService)
            )
        case .settings:
            SettingsView(viewModel: SettingsViewModel(photoLibrary: dependencies.photoLibrary))
        }
    }
}

#Preview {
    AppRootView(dependencies: .preview(), homeMockState: .populated)
}
