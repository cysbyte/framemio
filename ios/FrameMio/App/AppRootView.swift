import StoreKit
import SwiftUI

private func purchaseRoutingDebugLog(_ message: @autoclosure () -> String) {
    print("[FrameMio][Purchases] \(message())")
}

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
                storyService: dependencies.storyService,
                creditService: dependencies.creditService
            )
        )
        _creditStore = StateObject(wrappedValue: CreditBalanceStore(creditService: dependencies.creditService))
        _settingsViewModel = StateObject(wrappedValue: SettingsViewModel(photoLibrary: dependencies.photoLibrary, creditService: dependencies.creditService))
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
                        onPurchaseCompleted: completePurchaseFlow,
                        onBalanceUpdated: refreshCredits
                    )
                case .creditPacks:
                    FrameMioCreditPacksView(
                        creditService: dependencies.creditService,
                        onClose: { purchaseDestination = nil },
                        onPurchaseCompleted: completePurchaseFlow,
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
        guard source == .insufficientCredits || source == .homeCreditButton else {
            purchaseDestination = .pro(source)
            return
        }

        openPurchaseDestination(source)
    }

    private func openBuyCredits(_ source: PricingEntryPoint) {
        purchaseRoutingDebugLog("buy_credits_tapped source=\(source.rawValue) destination=lookup cached_balance=\(creditStore.balance?.available.description ?? "nil") cached_reserved=\(creditStore.balance?.reserved.description ?? "nil") cached_has_subscription_cycle=\(creditStore.balance?.hasActiveSubscriptionCycle.description ?? "nil")")
        openPurchaseDestination(source)
    }

    private func openPurchaseDestination(_ source: PricingEntryPoint) {
        Task { @MainActor in
            let shouldShowCreditPacks = await shouldShowCreditPacksForPurchase()
            let balance = (try? await dependencies.creditService.balance()) ?? creditStore.balance
            purchaseRoutingDebugLog("destination_lookup_complete source=\(source.rawValue) show_credit_packs=\(shouldShowCreditPacks) available=\(balance?.available.description ?? "nil") reserved=\(balance?.reserved.description ?? "nil")")
            if shouldShowCreditPacks {
                purchaseRoutingDebugLog("destination=credit_packs source=\(source.rawValue) reason=active_and_renewing_subscription available=\(balance?.available.description ?? "nil") reserved=\(balance?.reserved.description ?? "nil")")
                purchaseDestination = .creditPacks(source)
            } else {
                purchaseRoutingDebugLog("destination=pro source=\(source.rawValue) reason=cancelled_or_not_renewing_subscription available=\(balance?.available.description ?? "nil") reserved=\(balance?.reserved.description ?? "nil")")
                purchaseDestination = .pro(source)
            }
        }
    }

    private func shouldShowCreditPacksForPurchase() async -> Bool {
        for await result in Transaction.currentEntitlements {
            guard case let .verified(transaction) = result,
                  FrameMioProCatalog.productIDs.contains(transaction.productID),
                  transaction.revocationDate == nil else {
                continue
            }
            guard let status = await transaction.subscriptionStatus else {
                purchaseRoutingDebugLog("subscription_route_status_unavailable product=\(transaction.productID)")
                return true
            }
            do {
                let renewalInfo = try verifiedRenewalInfo(from: status.renewalInfo)
                let shouldShowCreditPacks = renewalInfo.willAutoRenew
                purchaseRoutingDebugLog("subscription_route_status product=\(transaction.productID) state=\(String(describing: status.state)) will_auto_renew=\(renewalInfo.willAutoRenew) result=\(shouldShowCreditPacks ? "credit_packs" : "pro")")
                return shouldShowCreditPacks
            } catch {
                purchaseRoutingDebugLog("subscription_route_status_unverified product=\(transaction.productID) error=\(error.localizedDescription)")
                return true
            }
        }
        return false
    }

    private func verifiedRenewalInfo(from result: VerificationResult<Product.SubscriptionInfo.RenewalInfo>) throws -> Product.SubscriptionInfo.RenewalInfo {
        switch result {
        case let .verified(value):
            return value
        case let .unverified(_, error):
            throw error
        }
    }

    private func refreshCredits() {
        creditStore.refresh()
    }

    private func completePurchaseFlow() {
        refreshCredits()
        selectedTab = .profile
        purchaseDestination = nil
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
                promptTranscriptionService: dependencies.promptTranscriptionService,
                onCreate: { options in
                    homePath.append(.storyCreating(context: context, assets: assets, options: options, launchID: UUID()))
                },
                onCreated: { story in
                    homePath.append(.storyPreview(story: story))
                },
                onOpenPricing: openPricing
            )
        case let .storyCreating(context, assets, options, launchID):
            StoryPreviewView(
                viewModel: StoryPreviewViewModel(
                    context: context,
                    assets: assets,
                    generationOptions: options,
                    launchID: launchID,
                    storyLaunchCoordinator: storyLaunchCoordinator,
                    storyService: dependencies.storyService,
                    photoLibrary: dependencies.photoLibrary,
                    mediaUploadStore: dependencies.mediaUploadStore,
                    mediaUploadService: dependencies.mediaUploadService,
                    analysisService: dependencies.mediaAnalysis,
                    creditService: dependencies.creditService,
                    promptTranscriptionService: dependencies.promptTranscriptionService,
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
                    promptTranscriptionService: dependencies.promptTranscriptionService,
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
