import StoreKit
import SwiftUI

private func purchaseRoutingDebugLog(_ message: @autoclosure () -> String) {
    print("[FrameMio][Purchases] \(message())")
}

struct AppRootView: View {
    @Environment(\.scenePhase) private var scenePhase
    let dependencies: AppDependencies
    @State private var homePath: [AppRoute] = []
    @State private var projectsPath: [AppRoute] = []
    @State private var createPath: [AppRoute] = []
    @State private var purchaseDestination: PurchaseDestination?
    @State private var selectedTab: RootTab = .home
    @StateObject private var homeViewModel: HomeViewModel
    @StateObject private var storyLaunchCoordinator: StoryLaunchCoordinator
    @StateObject private var creditStore: CreditBalanceStore

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
    }

    var body: some View {
        TabView(selection: $selectedTab) {
            homeTab
                .tabItem {
                    Label("Home", systemImage: "house")
                }
                .tag(RootTab.home)
                .accessibilityLabel("Home")

            projectsTab
                .tabItem {
                    Label("Projects", systemImage: "rectangle.stack.badge.play")
                }
                .tag(RootTab.projects)
                .accessibilityLabel("Projects")

            createTab
                .tabItem {
                    Label("Create", systemImage: "plus.circle.fill")
                }
                .tag(RootTab.create)
                .accessibilityLabel("Create")

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
            if newTab == .profile {
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
                    homePath.append(.mediaPicker(draft: .story(context: context)))
                },
                onOpenCreate: {
                    selectedTab = .create
                },
                onOpenStory: { story in
                    homePath.append(.storyPreview(story: story))
                },
                onOpenAllStories: {
                    selectedTab = .projects
                },
                onOpenPricing: openPricing
            )
            .navigationDestination(for: AppRoute.self) { route in
                destination(for: route) { homePath.append($0) }
            }
        }
    }

    private var projectsTab: some View {
        NavigationStack(path: $projectsPath) {
            AllStoriesView(
                viewModel: homeViewModel,
                onCreate: {
                    selectedTab = .create
                },
                onOpenStory: { story in
                    projectsPath.append(.storyPreview(story: story))
                }
            )
            .navigationDestination(for: AppRoute.self) { route in
                destination(for: route) { projectsPath.append($0) }
            }
        }
    }

    private var createTab: some View {
        NavigationStack(path: $createPath) {
            CreateView { draft in
                createPath.append(.mediaPicker(draft: draft))
            }
            .navigationDestination(for: AppRoute.self) { route in
                destination(for: route) { createPath.append($0) }
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
    private func destination(for route: AppRoute, appendRoute: @escaping (AppRoute) -> Void) -> some View {
        switch route {
        case .allProjects:
            AllStoriesView(
                viewModel: homeViewModel,
                onCreate: {
                    selectedTab = .create
                },
                onOpenStory: { story in
                    appendRoute(.storyPreview(story: story))
                }
            )
        case let .mediaPicker(draft):
            MediaPickerView(
                viewModel: MediaPickerViewModel(
                    context: draft.context,
                    photoLibrary: dependencies.photoLibrary,
                    selectionService: dependencies.mediaSelection
                ),
                projectType: draft.type,
                onContinue: { assets in
                    switch draft.type {
                    case .story:
                        appendRoute(.storyCreation(draft: draft, assets: assets))
                    case .promo, .reel:
                        appendRoute(.projectConfiguration(draft: draft, assets: assets))
                    }
                }
            )
            .id("\(draft.type.rawValue)-\(draft.context.rawValue)")
        case let .projectConfiguration(draft, assets):
            ProjectConfigurationView(
                draft: draft,
                onContinue: { configuredDraft in
                    appendRoute(.storyCreating(draft: configuredDraft, assets: assets, launchID: UUID()))
                }
            )
        case let .storyCreation(draft, assets):
            StoryCreationView(
                viewModel: StoryCreationViewModel(
                    context: draft.context,
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
                    appendRoute(.storyCreating(draft: .story(context: draft.context, options: options), assets: assets, launchID: UUID()))
                },
                onCreated: { story in
                    appendRoute(.storyPreview(story: story))
                },
                onOpenPricing: openPricing
            )
        case let .storyCreating(draft, assets, launchID):
            StoryPreviewView(
                viewModel: StoryPreviewViewModel(
                    draft: draft,
                    assets: assets,
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
    case projects
    case create
    case profile
}

struct CreateView: View {
    let onSelect: (VideoProjectDraft) -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                    Text("Create")
                        .font(FrameMioTypography.display)
                        .foregroundStyle(FrameMioColors.ink)

                    Text("Choose what FrameMio should make from your photos and footage.")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(.top, FrameMioSpacing.lg)

                VStack(spacing: FrameMioSpacing.md) {
                    creationOption(.story) {
                        onSelect(.story())
                    }
                    creationOption(.promo) {
                        onSelect(.promo())
                    }
                    creationOption(.reel) {
                        onSelect(.reel())
                    }
                }
            }
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background(FrameMioColors.canvas.ignoresSafeArea())
        .navigationTitle("Create")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func creationOption(_ type: VideoProjectType, action: @escaping () -> Void) -> some View {
        Button {
            FrameMioHaptics.primaryAction()
            action()
        } label: {
            HStack(spacing: FrameMioSpacing.md) {
                Image(systemName: type.icon)
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(FrameMioColors.accent)
                    .frame(width: 48, height: 48)
                    .background(FrameMioColors.accentMuted.opacity(0.72), in: Circle())
                    .accessibilityHidden(true)

                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text(type.title)
                        .font(FrameMioTypography.title)
                        .foregroundStyle(FrameMioColors.ink)

                    Text(type.description)
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .fixedSize(horizontal: false, vertical: true)
                }

                Spacer(minLength: FrameMioSpacing.sm)

                Image(systemName: "chevron.right")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.tertiaryInk)
                    .accessibilityHidden(true)
            }
            .padding(FrameMioSpacing.lg)
            .frame(maxWidth: .infinity, minHeight: 88, alignment: .leading)
            .background(
                FrameMioColors.surface.opacity(0.86),
                in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                    .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
            }
            .frameMioElevation(.subtle)
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .accessibilityHint("Opens media selection for a \(type.title.lowercased()) project")
        .accessibilityLabel("\(type.title). \(type.description)")
    }
}

struct ProjectConfigurationView: View {
    @State private var promoSubject: PromoSubject
    @State private var promoGoal: PromoGoal
    @State private var promoDuration: ProjectDurationOption
    @State private var reelPlatform: ReelPlatform
    @State private var reelDuration: ProjectDurationOption
    @State private var reelStyle: ReelStyle
    @State private var prompt: String

    let draft: VideoProjectDraft
    let onContinue: (VideoProjectDraft) -> Void

    init(draft: VideoProjectDraft, onContinue: @escaping (VideoProjectDraft) -> Void) {
        self.draft = draft
        self.onContinue = onContinue
        _promoSubject = State(initialValue: draft.promo?.subject ?? .product)
        _promoGoal = State(initialValue: draft.promo?.goal ?? .sell)
        _promoDuration = State(initialValue: draft.promo?.duration ?? .automatic)
        _reelPlatform = State(initialValue: draft.reel?.platform ?? .instagram)
        _reelDuration = State(initialValue: draft.reel?.duration ?? .automatic)
        _reelStyle = State(initialValue: draft.reel?.style ?? .automatic)
        _prompt = State(initialValue: draft.prompt ?? "")
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                header

                if draft.type == .promo {
                    optionPicker(title: "What are you promoting?", selection: $promoSubject, values: PromoSubject.allCases)
                    optionPicker(title: "Goal", selection: $promoGoal, values: PromoGoal.allCases)
                    optionPicker(title: "Duration", selection: $promoDuration, values: ProjectDurationOption.allCases)
                } else {
                    optionPicker(title: "Platform", selection: $reelPlatform, values: ReelPlatform.allCases)
                    optionPicker(title: "Duration", selection: $reelDuration, values: ProjectDurationOption.allCases)
                    optionPicker(title: "Style", selection: $reelStyle, values: ReelStyle.allCases)
                }

                promptField

                PrimaryButton(title: "Generate First Draft", systemImage: "sparkles") {
                    onContinue(configuredDraft)
                }
                .padding(.top, FrameMioSpacing.sm)
            }
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.lg)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background(FrameMioColors.canvas.ignoresSafeArea())
        .navigationTitle(draft.type == .promo ? "Create Promo" : "Create Reel")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Tag(draft.type.title, systemImage: draft.type.icon)
            Text(draft.type == .promo ? "Configure the promotion." : "Choose the social format.")
                .font(FrameMioTypography.screenTitle)
                .foregroundStyle(FrameMioColors.ink)
            Text(draft.type == .promo ? "Keep the brief simple. Detailed editing comes after FrameMio creates the first draft." : "Pick the platform and style. FrameMio will handle the first short-form edit.")
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(FrameMioSpacing.lg)
        .background(FrameMioColors.surface.opacity(0.86), in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
    }

    private func optionPicker<Value: CaseIterable & Hashable & Identifiable & TitledOption>(
        title: String,
        selection: Binding<Value>,
        values: Value.AllCases
    ) -> some View where Value.AllCases: RandomAccessCollection {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Text(title)
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)

            Picker(title, selection: selection) {
                ForEach(Array(values)) { value in
                    Text(value.title).tag(value)
                }
            }
            .pickerStyle(.segmented)
            .tint(FrameMioColors.accent)
        }
        .padding(FrameMioSpacing.md)
        .background(FrameMioColors.surface.opacity(0.74), in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))
    }

    private var promptField: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Text("Custom prompt")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)

            TextEditor(text: $prompt)
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.ink)
                .frame(minHeight: 112)
                .scrollContentBackground(.hidden)
                .padding(FrameMioSpacing.sm)
                .background(FrameMioColors.elevatedSurface.opacity(0.86), in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
                .overlay(alignment: .topLeading) {
                    if prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        Text(draft.type == .promo ? "Create a premium 20-second advertisement for my app." : "Make this feel fast, bright, and ready for Instagram.")
                            .font(FrameMioTypography.body)
                            .foregroundStyle(FrameMioColors.tertiaryInk)
                            .padding(FrameMioSpacing.md)
                            .allowsHitTesting(false)
                    }
                }
        }
        .padding(FrameMioSpacing.md)
        .background(FrameMioColors.surface.opacity(0.74), in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))
    }

    private var configuredDraft: VideoProjectDraft {
        let trimmedPrompt = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        if draft.type == .promo {
            return .promo(
                configuration: PromoCreationConfiguration(
                    subject: promoSubject,
                    goal: promoGoal,
                    duration: promoDuration,
                    prompt: trimmedPrompt.isEmpty ? nil : trimmedPrompt
                )
            )
        }
        return .reel(
            configuration: ReelCreationConfiguration(
                platform: reelPlatform,
                duration: reelDuration,
                style: reelStyle,
                prompt: trimmedPrompt.isEmpty ? nil : trimmedPrompt
            )
        )
    }
}

protocol TitledOption {
    var title: String { get }
}

extension PromoSubject: Identifiable, TitledOption { var id: String { rawValue } }
extension PromoGoal: Identifiable, TitledOption { var id: String { rawValue } }
extension ProjectDurationOption: Identifiable, TitledOption { var id: String { rawValue } }
extension ReelPlatform: Identifiable, TitledOption { var id: String { rawValue } }
extension ReelStyle: Identifiable, TitledOption { var id: String { rawValue } }

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
