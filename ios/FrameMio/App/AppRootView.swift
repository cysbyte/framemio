import AVKit
import StoreKit
import SwiftUI

private func purchaseRoutingDebugLog(_ message: @autoclosure () -> String) {
    print("[FrameMio][Purchases] \(message())")
}

struct AppRootView: View {
    @Environment(\.scenePhase) private var scenePhase
    let dependencies: AppDependencies
    @State private var homePath: [AppRoute] = []
    @State private var templatesPath: [AppRoute] = []
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

            templatesTab
                .tabItem {
                    Label("Templates", systemImage: "rectangle.stack.badge.play")
                }
                .tag(RootTab.templates)
                .accessibilityLabel("Templates")

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
        .onChange(of: storyLaunchCoordinator.storyID) { _, newStoryID in
            guard newStoryID != nil else { return }
            refreshCredits()
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
                    selectedTab = .create
                    createPath.append(.allProjects)
                },
                onOpenPricing: openPricing
            )
            .navigationDestination(for: AppRoute.self) { route in
                destination(for: route) { homePath.append($0) }
            }
        }
    }

    private var templatesTab: some View {
        NavigationStack(path: $templatesPath) {
            TemplatesView(storyService: dependencies.storyService) { template, projectType in
                templatesPath.append(.mediaPicker(draft: .fromTemplate(template, projectType: projectType)))
            }
            .navigationDestination(for: AppRoute.self) { route in
                destination(for: route) { templatesPath.append($0) }
            }
        }
    }

    private var createTab: some View {
        NavigationStack(path: $createPath) {
            CreateView(
                onSelect: { draft in
                    createPath.append(.mediaPicker(draft: draft))
                },
                onOpenProjects: {
                    createPath.append(.allProjects)
                }
            )
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
                  transaction.revocationDate == nil,
                  Self.subscriptionTransactionIsActive(transaction) else {
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

    private static func subscriptionTransactionIsActive(_ transaction: StoreKit.Transaction, now: Date = Date()) -> Bool {
        guard let expirationDate = transaction.expirationDate else { return true }
        return expirationDate > now
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
                storyService: dependencies.storyService,
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
                draft: draft,
                storyService: dependencies.storyService,
                promptTranscriptionService: dependencies.promptTranscriptionService,
                onCreate: { configuredDraft in
                    appendRoute(.storyCreating(draft: configuredDraft, assets: assets, launchID: UUID()))
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
    case templates
    case create
    case profile
}

struct CreateView: View {
    let onSelect: (VideoProjectDraft) -> Void
    let onOpenProjects: () -> Void

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

                Button {
                    FrameMioHaptics.secondaryAction()
                    onOpenProjects()
                } label: {
                    HStack(spacing: FrameMioSpacing.md) {
                        Image(systemName: "rectangle.stack.badge.play")
                            .font(.title3.weight(.semibold))
                            .foregroundStyle(FrameMioColors.accent)
                            .frame(width: 48, height: 48)
                            .background(FrameMioColors.accentMuted.opacity(0.72), in: Circle())
                            .accessibilityHidden(true)

                        VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                            Text("Projects")
                                .font(FrameMioTypography.title)
                                .foregroundStyle(FrameMioColors.ink)
                            Text("Review drafts, in-progress videos, and completed projects.")
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
                    .background(FrameMioColors.surface.opacity(0.86), in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
                    .overlay {
                        RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                            .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
                    }
                }
                .buttonStyle(FrameMioPressButtonStyle())
                .accessibilityLabel("Projects. Review drafts, in-progress videos, and completed projects.")
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

struct TemplatesView: View {
    let storyService: StoryService
    let onUseTemplate: (VideoTemplate, VideoProjectType) -> Void
    @State private var selectedProjectType: VideoProjectType = .story
    @State private var templates: [VideoTemplate] = []
    @State private var selectedTemplateID: String?
    @State private var isLoading = false

    private var visibleTemplates: [VideoTemplate] {
        templates.filter { $0.supportedProjectTypes.contains(selectedProjectType) }
    }

    private var selectedTemplate: VideoTemplate? {
        visibleTemplates.first { $0.id == selectedTemplateID } ?? visibleTemplates.first
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                    Text("Templates")
                        .font(FrameMioTypography.display)
                        .foregroundStyle(FrameMioColors.ink)
                    Text("Browse reviewed edit styles, preview the motion, then start with the defaults already filled in.")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(.top, FrameMioSpacing.lg)

                Picker("Project type", selection: $selectedProjectType) {
                    ForEach(VideoProjectType.allCases, id: \.self) { type in
                        Text(type.title).tag(type)
                    }
                }
                .pickerStyle(.segmented)
                .onChange(of: selectedProjectType) { _, _ in
                    selectedTemplateID = visibleTemplates.first?.id
                }

                if isLoading && templates.isEmpty {
                    LoadingState(title: "Loading templates", message: "Fetching the latest reviewed styles.")
                } else {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 226), spacing: FrameMioSpacing.md)], spacing: FrameMioSpacing.md) {
                        ForEach(visibleTemplates) { template in
                            TemplateCard(template: template, isSelected: selectedTemplateID == template.id) {
                                selectedTemplateID = template.id
                            }
                        }
                    }
                }

                if let selectedTemplate {
                    VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                        Text(selectedTemplate.name)
                            .font(FrameMioTypography.title)
                            .foregroundStyle(FrameMioColors.ink)
                        Text(selectedTemplate.description)
                            .font(FrameMioTypography.body)
                            .foregroundStyle(FrameMioColors.secondaryInk)
                            .fixedSize(horizontal: false, vertical: true)
                        Button {
                            FrameMioHaptics.primaryAction()
                            onUseTemplate(selectedTemplate, selectedProjectType)
                        } label: {
                            Label("Use Template", systemImage: "plus.circle.fill")
                                .frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.borderedProminent)
                        .tint(FrameMioColors.accent)
                        .padding(.top, FrameMioSpacing.sm)
                    }
                    .padding(FrameMioSpacing.lg)
                    .background(FrameMioColors.surface.opacity(0.86), in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))
                    .overlay {
                        RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
                            .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
                    }
                }
            }
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background(FrameMioColors.canvas.ignoresSafeArea())
        .navigationTitle("Templates")
        .navigationBarTitleDisplayMode(.inline)
        .task { await loadTemplates() }
    }

    private func loadTemplates() async {
        guard templates.isEmpty else { return }
        isLoading = true
        defer { isLoading = false }
        do {
            let loaded = try await storyService.videoTemplates(for: nil)
            templates = loaded.isEmpty ? VideoTemplate.localFallbackCatalog : loaded
        } catch {
            purchaseRoutingDebugLog("templates_tab_catalog_fallback error=\(String(describing: error))")
            templates = VideoTemplate.localFallbackCatalog
        }
        selectedTemplateID = visibleTemplates.first?.id
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
    @State private var templates: [VideoTemplate] = []
    @State private var selectedTemplateID: String?
    @State private var templateConfiguration: VideoTemplateConfiguration?

    let draft: VideoProjectDraft
    let storyService: StoryService
    let onContinue: (VideoProjectDraft) -> Void

    init(draft: VideoProjectDraft, storyService: StoryService, onContinue: @escaping (VideoProjectDraft) -> Void) {
        self.draft = draft
        self.storyService = storyService
        self.onContinue = onContinue
        _promoSubject = State(initialValue: draft.promo?.subject ?? .product)
        _promoGoal = State(initialValue: draft.promo?.goal ?? .sell)
        _promoDuration = State(initialValue: draft.promo?.duration ?? .automatic)
        _reelPlatform = State(initialValue: draft.reel?.platform ?? .instagram)
        _reelDuration = State(initialValue: draft.reel?.duration ?? .automatic)
        _reelStyle = State(initialValue: draft.reel?.style ?? .automatic)
        _prompt = State(initialValue: draft.prompt ?? "")
        _selectedTemplateID = State(initialValue: draft.templateID)
        _templateConfiguration = State(initialValue: draft.templateConfiguration)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                header
                templateSection

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
                templateOptions

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
        .task { await loadTemplates() }
    }

    private var templateSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            Text("Choose a visual template")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)
            Text("Preview the rhythm first. FrameMio will use your media to create this kind of edit.")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)
            if templates.isEmpty {
                ProgressView()
                    .frame(maxWidth: .infinity, minHeight: 120)
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(alignment: .top, spacing: FrameMioSpacing.md) {
                        ForEach(templates) { template in
                            TemplateCard(template: template, isSelected: selectedTemplateID == template.id) {
                                select(template)
                            }
                        }
                    }
                }
            }
        }
        .padding(FrameMioSpacing.md)
        .background(FrameMioColors.surface.opacity(0.74), in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))
    }

    @ViewBuilder
    private var templateOptions: some View {
        if let template = selectedTemplate, let configuration = templateConfiguration {
            VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
                Text("Template settings")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.ink)
                if template.editableOptions.contains("duration_seconds") {
                    Picker("Length", selection: Binding(get: { configuration.durationSeconds ?? template.defaults.durationSeconds ?? 30 }, set: { templateConfiguration?.durationSeconds = $0 })) {
                        ForEach([15, 30, 60], id: \.self) { Text("\($0) sec").tag($0) }
                    }
                    .pickerStyle(.segmented)
                }
                if template.editableOptions.contains("visual_style") {
                    Picker("Visual style", selection: Binding(get: { configuration.visualStyle ?? "cinematic" }, set: { templateConfiguration?.visualStyle = $0 })) {
                        ForEach(["cinematic", "warm", "minimal", "playful"], id: \.self) { Text($0.capitalized).tag($0) }
                    }
                    .pickerStyle(.segmented)
                }
                if template.editableOptions.contains("captions_enabled") {
                    Toggle("Captions", isOn: Binding(get: { configuration.captionsEnabled ?? true }, set: { templateConfiguration?.captionsEnabled = $0 }))
                }
                if template.editableOptions.contains("overlay_placement") {
                    TemplateOptionMenu(
                        title: "Overlay position",
                        selection: configuration.overlayPlacement ?? "bottom_right",
                        options: ["top_left", "top_right", "bottom_left", "bottom_right", "center"]
                    ) { templateConfiguration?.overlayPlacement = $0 }
                }
                if template.editableOptions.contains("overlay_size") {
                    TemplateOptionMenu(
                        title: "Overlay size",
                        selection: configuration.overlaySize ?? "medium",
                        options: ["small", "medium", "large", "half"]
                    ) { templateConfiguration?.overlaySize = $0 }
                }
            }
            .padding(FrameMioSpacing.md)
            .background(FrameMioColors.surface.opacity(0.74), in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))
        }
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
            var configured = VideoProjectDraft.promo(
                configuration: PromoCreationConfiguration(
                    subject: promoSubject,
                    goal: promoGoal,
                    duration: promoDuration,
                    prompt: trimmedPrompt.isEmpty ? nil : trimmedPrompt
                )
            )
            configured.templateID = selectedTemplateID
            configured.templateVersion = selectedTemplate?.version
            configured.templateConfiguration = templateConfiguration
            return configured
        }
        var configured = VideoProjectDraft.reel(
            configuration: ReelCreationConfiguration(
                platform: reelPlatform,
                duration: reelDuration,
                style: reelStyle,
                prompt: trimmedPrompt.isEmpty ? nil : trimmedPrompt
            )
        )
        configured.templateID = selectedTemplateID
        configured.templateVersion = selectedTemplate?.version
        configured.templateConfiguration = templateConfiguration
        return configured
    }

    private var selectedTemplate: VideoTemplate? {
        templates.first(where: { $0.id == selectedTemplateID })
    }

    private func loadTemplates() async {
        do {
            let loaded = try await storyService.videoTemplates(for: draft.type)
            templates = loaded.isEmpty ? VideoTemplate.localFallbackCatalog.filter { $0.supportedProjectTypes.contains(draft.type) } : loaded
            if selectedTemplateID == nil, let first = templates.first {
                select(first)
            }
        } catch {
            templates = VideoTemplate.localFallbackCatalog.filter { $0.supportedProjectTypes.contains(draft.type) }
            if selectedTemplateID == nil, let first = templates.first {
                select(first)
            }
            purchaseRoutingDebugLog("template_catalog_fallback type=\(draft.type.rawValue) error=\(String(describing: error))")
        }
    }

    private func select(_ template: VideoTemplate) {
        selectedTemplateID = template.id
        templateConfiguration = compatibleConfiguration(templateConfiguration, with: template)
    }

    private func compatibleConfiguration(_ previous: VideoTemplateConfiguration?, with template: VideoTemplate) -> VideoTemplateConfiguration {
        var resolved = template.defaults
        guard let previous else { return resolved }
        if template.editableOptions.contains("duration_seconds") { resolved.durationSeconds = previous.durationSeconds }
        if template.editableOptions.contains("aspect_ratio") { resolved.aspectRatio = previous.aspectRatio }
        if template.editableOptions.contains("visual_style") { resolved.visualStyle = previous.visualStyle }
        if template.editableOptions.contains("captions_enabled") { resolved.captionsEnabled = previous.captionsEnabled }
        if template.editableOptions.contains("caption_style") { resolved.captionStyle = previous.captionStyle }
        if template.editableOptions.contains("scene_detail") { resolved.sceneDetail = previous.sceneDetail }
        if template.editableOptions.contains("overlay_placement") { resolved.overlayPlacement = previous.overlayPlacement }
        if template.editableOptions.contains("overlay_size") { resolved.overlaySize = previous.overlaySize }
        if template.editableOptions.contains("overlay_opacity") { resolved.overlayOpacity = previous.overlayOpacity }
        if template.editableOptions.contains("overlay_frame") { resolved.overlayFrame = previous.overlayFrame }
        if template.editableOptions.contains("overlay_animation") { resolved.overlayAnimation = previous.overlayAnimation }
        return resolved
    }
}

struct TemplateCard: View {
    let template: VideoTemplate
    let isSelected: Bool
    let onSelect: () -> Void
    private let cardWidth: CGFloat = 226
    private let previewHeight: CGFloat = 176
    private let cornerRadius: CGFloat = 8

    var body: some View {
        Button(action: onSelect) {
            VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                InlineTemplateVideo(path: template.demoVideoPath)
                    .frame(width: cardWidth - (FrameMioSpacing.md * 2), height: previewHeight)
                    .clipShape(RoundedRectangle(cornerRadius: cornerRadius, style: .continuous))
                Text(template.name)
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.ink)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
                Text(template.description)
                    .font(FrameMioTypography.caption)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                    .lineLimit(3)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(FrameMioSpacing.md)
            .frame(width: cardWidth, alignment: .topLeading)
            .frame(minHeight: 314, alignment: .topLeading)
            .background(isSelected ? FrameMioColors.accentMuted : FrameMioColors.elevatedSurface, in: RoundedRectangle(cornerRadius: cornerRadius, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
                    .stroke(isSelected ? FrameMioColors.accent : FrameMioColors.divider, lineWidth: isSelected ? 2 : 1)
            }
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(template.name). \(template.description)")
        .accessibilityValue(isSelected ? "Selected" : "Not selected")
    }
}

struct TemplateOptionMenu: View {
    let title: String
    let selection: String
    let options: [String]
    let onSelect: (String) -> Void

    var body: some View {
        Menu {
            ForEach(options, id: \.self) { option in
                Button {
                    onSelect(option)
                } label: {
                    if option == selection {
                        Label(displayName(option), systemImage: "checkmark")
                    } else {
                        Text(displayName(option))
                    }
                }
            }
        } label: {
            HStack(spacing: FrameMioSpacing.sm) {
                Text(title)
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.ink)
                Spacer()
                Text(displayName(selection))
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.accent)
                    .lineLimit(1)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(FrameMioColors.accent)
                    .accessibilityHidden(true)
            }
            .padding(.vertical, FrameMioSpacing.xs)
            .contentShape(Rectangle())
        }
        .accessibilityLabel(title)
        .accessibilityValue(displayName(selection))
    }

    private func displayName(_ value: String) -> String {
        value
            .replacingOccurrences(of: "_", with: " ")
            .split(separator: " ")
            .map { $0.capitalized }
            .joined(separator: " ")
    }
}

struct InlineTemplateVideo: View {
    let path: String
    @State private var player: AVPlayer?
    @State private var endObserver: NSObjectProtocol?
    @State private var itemStatusObserver: NSKeyValueObservation?
    @State private var failedToLoad = false

    var body: some View {
        ZStack {
            if let player {
                InlineTemplatePlayer(player: player)
                    .onAppear {
                        player.isMuted = true
                        player.play()
                    }
            } else {
                templateDemoPlaceholder
            }
        }
        .task(id: path) {
            preparePlayer()
        }
        .onChange(of: path) { _, _ in
            resetPlayer()
            preparePlayer()
        }
        .onDisappear {
            resetPlayer()
        }
        .accessibilityLabel("Muted template demo")
    }

    private var templateDemoPlaceholder: some View {
        ZStack {
            LinearGradient(
                colors: [
                    FrameMioColors.accentMuted.opacity(0.78),
                    FrameMioColors.elevatedSurface.opacity(0.94)
                ],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )
            VStack(spacing: FrameMioSpacing.xs) {
                Image(systemName: failedToLoad ? "video.slash" : "play.rectangle")
                    .font(.title2)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                    .accessibilityHidden(true)
                Text(failedToLoad ? "Demo pending" : "Loading demo")
                    .font(FrameMioTypography.caption)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }
        }
    }

    private func preparePlayer() {
        guard player == nil else { return }
        guard let url = templateDemoURL(path) else {
            failedToLoad = true
            return
        }
        failedToLoad = false
        let item = AVPlayerItem(url: url)
        let loaded = AVPlayer(playerItem: item)
        loaded.isMuted = true
        loaded.actionAtItemEnd = .none
        itemStatusObserver = item.observe(\.status, options: [.new]) { observedItem, _ in
            guard observedItem.status == .failed else { return }
            Task { @MainActor in
                resetPlayer()
                failedToLoad = true
            }
        }
        endObserver = NotificationCenter.default.addObserver(
            forName: .AVPlayerItemDidPlayToEndTime,
            object: item,
            queue: .main
        ) { _ in
            loaded.seek(to: .zero)
            loaded.play()
        }
        player = loaded
        loaded.play()
    }

    private func resetPlayer() {
        if let endObserver {
            NotificationCenter.default.removeObserver(endObserver)
            self.endObserver = nil
        }
        itemStatusObserver?.invalidate()
        itemStatusObserver = nil
        player?.pause()
        player = nil
    }

    private func templateDemoURL(_ value: String) -> URL? {
        if let url = URL(string: value), url.scheme != nil {
            return url
        }
        let fileName = URL(fileURLWithPath: value).deletingPathExtension().lastPathComponent
        if let bundled = Bundle.main.url(forResource: fileName, withExtension: "mp4") {
            return bundled
        }
        guard value.hasPrefix("/") else { return nil }
        return AppConfiguration.templateDemoBaseURL.appendingPathComponent(String(value.dropFirst()))
    }
}

private struct InlineTemplatePlayer: UIViewControllerRepresentable {
    let player: AVPlayer

    func makeUIViewController(context: Context) -> AVPlayerViewController {
        let controller = AVPlayerViewController()
        controller.player = player
        controller.showsPlaybackControls = false
        controller.entersFullScreenWhenPlaybackBegins = false
        controller.exitsFullScreenWhenPlaybackEnds = false
        controller.videoGravity = .resizeAspectFill
        controller.view.backgroundColor = .clear
        return controller
    }

    func updateUIViewController(_ controller: AVPlayerViewController, context: Context) {
        controller.player = player
        controller.showsPlaybackControls = false
        controller.entersFullScreenWhenPlaybackBegins = false
        controller.exitsFullScreenWhenPlaybackEnds = false
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
