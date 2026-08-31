import SwiftUI

struct AppRootView: View {
    @Environment(\.scenePhase) private var scenePhase
    let dependencies: AppDependencies
    @State private var homePath: [AppRoute] = []
    @State private var purchaseDestination: PurchaseDestination?
    @State private var selectedTab: RootTab = .home
    @StateObject private var homeViewModel: HomeViewModel
    @StateObject private var storyLaunchCoordinator: StoryLaunchCoordinator
    @StateObject private var creditStore: CreditBalanceStore
    @StateObject private var settingsViewModel: SettingsViewModel

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
        _creditStore = StateObject(wrappedValue: CreditBalanceStore(creditService: dependencies.creditService))
        _settingsViewModel = StateObject(wrappedValue: SettingsViewModel(photoLibrary: dependencies.photoLibrary))
    }

    var body: some View {
        TabView(selection: $selectedTab) {
            homeTab
                .tabItem {
                    Label("Home", systemImage: "house")
                }
                .tag(RootTab.home)
                .accessibilityLabel("Home")

            NavigationStack {
                ProfileView(creditStore: creditStore) {
                    openBuyCredits(.profile)
                }
            }
            .tabItem {
                Label("Profile", systemImage: "person")
            }
            .tag(RootTab.profile)
            .accessibilityLabel("Profile")

            NavigationStack {
                SettingsView(
                    viewModel: settingsViewModel,
                    creditStore: creditStore,
                    onBuyCredits: { openBuyCredits(.settings) }
                )
            }
            .tabItem {
                Label("Settings", systemImage: "gear")
            }
            .tag(RootTab.settings)
            .accessibilityLabel("Settings")
        }
        .tint(FrameMioColors.accent)
        .fullScreenCover(item: $purchaseDestination) { destination in
            NavigationStack {
                switch destination {
                case let .pro(source):
                    FrameMioProView(
                        source: source,
                        creditService: dependencies.creditService,
                        onClose: { purchaseDestination = nil },
                        onBalanceUpdated: refreshCredits
                    )
                case .creditPacks:
                    FrameMioCreditPacksView(
                        creditService: dependencies.creditService,
                        onClose: { purchaseDestination = nil },
                        onBalanceUpdated: refreshCredits
                    )
                }
            }
            .onDisappear(perform: refreshCredits)
        }
        .onChange(of: selectedTab) { _, newTab in
            if newTab == .profile || newTab == .settings {
                creditStore.refresh()
            }
        }
        .onChange(of: scenePhase) { _, newPhase in
            if newPhase == .active {
                refreshCredits()
            }
        }
    }

    private var homeTab: some View {
        NavigationStack(path: $homePath) {
            HomeView(
                viewModel: homeViewModel,
                creditStore: creditStore,
                onCreateStory: { context in
                    homePath.append(.mediaPicker(context: context))
                },
                onOpenStory: { story in
                    homePath.append(.storyPreview(story: story))
                },
                onOpenAllStories: {
                    homePath.append(.allStories)
                },
                onOpenPricing: openPricing
            )
            .navigationDestination(for: AppRoute.self) { route in
                destination(for: route)
            }
        }
    }

    private func openPricing(_ source: PricingEntryPoint) {
        guard source == .insufficientCredits else {
            purchaseDestination = .pro(source)
            return
        }

        if let balance = creditStore.balance, balance.hasActiveSubscriptionCycle {
            purchaseDestination = .creditPacks(source)
            return
        }

        Task { @MainActor in
            let balance = (try? await dependencies.creditService.balance()) ?? creditStore.balance
            if let balance, balance.hasActiveSubscriptionCycle {
                purchaseDestination = .creditPacks(source)
            } else {
                purchaseDestination = .pro(source)
            }
        }
    }

    private func openBuyCredits(_ source: PricingEntryPoint) {
        if let balance = creditStore.balance {
            purchaseDestination = balance.hasActiveSubscriptionCycle ? .creditPacks(source) : .pro(source)
            return
        }

        Task { @MainActor in
            let balance = (try? await dependencies.creditService.balance()) ?? creditStore.balance
            purchaseDestination = (balance?.hasActiveSubscriptionCycle == true) ? .creditPacks(source) : .pro(source)
        }
    }

    private func refreshCredits() {
        homeViewModel.refreshCreditBalance()
        creditStore.refresh()
    }

    @ViewBuilder
    private func destination(for route: AppRoute) -> some View {
        switch route {
        case .allStories:
            AllStoriesView(
                viewModel: homeViewModel,
                onOpenStory: { story in
                    homePath.append(.storyPreview(story: story))
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
                    homePath.append(.storyCreation(context: context, assets: assets))
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
                    onCreditsUpdated: refreshCredits
                ),
                onCreate: {
                    homePath.append(.storyCreating(context: context, assets: assets, launchID: UUID()))
                },
                onCreated: { story in
                    homePath.append(.storyPreview(story: story))
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
                    onCreditsUpdated: refreshCredits
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
                    onCreditsUpdated: refreshCredits
                ),
                onOpenPricing: openPricing
            )
        }
    }
}

private enum RootTab: Hashable {
    case home
    case profile
    case settings
}

private enum PurchaseDestination: Identifiable, Hashable {
    case pro(PricingEntryPoint)
    case creditPacks(PricingEntryPoint)

    var id: String {
        switch self {
        case let .pro(source):
            return "pro-\(source.rawValue)"
        case let .creditPacks(source):
            return "credit-packs-\(source.rawValue)"
        }
    }
}

struct AppRootView_Previews: PreviewProvider {
    static var previews: some View {
        AppRootView(dependencies: .preview(), homeMockState: .populated)
    }
}
