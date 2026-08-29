import SwiftUI

struct AppRootView: View {
    @Environment(\.scenePhase) private var scenePhase
    let dependencies: AppDependencies
    @State private var path: [AppRoute] = []
    @State private var pricingSource: PricingEntryPoint?
    @StateObject private var homeViewModel: HomeViewModel
    @StateObject private var storyLaunchCoordinator: StoryLaunchCoordinator

    init(dependencies: AppDependencies = .preview(), homeMockState: HomeMockState? = nil) {
        self.dependencies = dependencies
        _homeViewModel = StateObject(wrappedValue: HomeViewModel(storyService: dependencies.storyService, creditService: dependencies.creditService, mockState: homeMockState))
        _storyLaunchCoordinator = StateObject(
            wrappedValue: StoryLaunchCoordinator(
                analysisService: dependencies.mediaAnalysis,
                mediaUploadService: dependencies.mediaUploadService,
                storyService: dependencies.storyService
            )
        )
    }

    var body: some View {
        NavigationStack(path: $path) {
            HomeView(
                viewModel: homeViewModel,
                onCreateStory: { context in
                    path.append(.mediaPicker(context: context))
                },
                onOpenStory: { story in
                    path.append(.storyPreview(story: story))
                },
                onOpenAllStories: {
                    path.append(.allStories)
                },
                onOpenSettings: {
                    path.append(.settings)
                },
                onOpenPricing: openPricing
            )
            .navigationDestination(for: AppRoute.self) { route in
                destination(for: route)
            }
        }
        .tint(FrameMioColors.accent)
        .sheet(item: $pricingSource) { source in
            NavigationStack {
                FrameMioProView(
                    source: source,
                    creditService: dependencies.creditService,
                    onClose: { pricingSource = nil },
                    onBalanceUpdated: { homeViewModel.refreshCreditBalance() }
                )
            }
        }
        .onChange(of: scenePhase) { _, newPhase in
            if newPhase == .active {
                homeViewModel.refreshCreditBalance()
            }
        }
    }


    private func openPricing(_ source: PricingEntryPoint) {
        pricingSource = source
    }

    @ViewBuilder
    private func destination(for route: AppRoute) -> some View {
        switch route {
        case .allStories:
            AllStoriesView(
                viewModel: homeViewModel,
                onOpenStory: { story in
                    path.append(.storyPreview(story: story))
                }
            )
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
            .id(context)
        case let .storyCreation(context, assets):
            StoryCreationView(
                viewModel: StoryCreationViewModel(
                    context: context,
                    assets: assets,
                    photoLibrary: dependencies.photoLibrary,
                    analysisService: dependencies.mediaAnalysis,
                    mediaUploadService: dependencies.mediaUploadService,
                    storyService: dependencies.storyService,
                    creditService: dependencies.creditService,
                    onCreditsUpdated: { homeViewModel.refreshCreditBalance() }
                ),
                onCreate: {
                    path.append(.storyCreating(context: context, assets: assets, launchID: UUID()))
                },
                onCreated: { story in
                    path.append(.storyPreview(story: story))
                },
                onOpenPricing: openPricing
            )
        case let .storyCreating(context, assets, launchID):
            StoryPreviewView(
                viewModel: StoryPreviewViewModel(
                    context: context,
                    assets: assets,
                    launchID: launchID,
                    storyLaunchCoordinator: storyLaunchCoordinator,
                    storyService: dependencies.storyService,
                    photoLibrary: dependencies.photoLibrary,
                    mediaUploadStore: dependencies.mediaUploadStore,
                    mediaUploadService: dependencies.mediaUploadService,
                    analysisService: dependencies.mediaAnalysis,
                    creditService: dependencies.creditService,
                    onCreditsUpdated: { homeViewModel.refreshCreditBalance() }
                ),
                onOpenPricing: openPricing
            )
        case let .storyPreview(story):
            StoryPreviewView(
                viewModel: StoryPreviewViewModel(
                    story: story,
                    storyService: dependencies.storyService,
                    photoLibrary: dependencies.photoLibrary,
                    mediaUploadStore: dependencies.mediaUploadStore,
                    mediaUploadService: dependencies.mediaUploadService,
                    analysisService: dependencies.mediaAnalysis,
                    creditService: dependencies.creditService,
                    onCreditsUpdated: { homeViewModel.refreshCreditBalance() }
                ),
                onOpenPricing: openPricing
            )
        case .settings:
            SettingsView(viewModel: SettingsViewModel(photoLibrary: dependencies.photoLibrary))
        }
    }
}

struct AppRootView_Previews: PreviewProvider {
    static var previews: some View {
        AppRootView(dependencies: .preview(), homeMockState: .populated)
    }
}
