import AVKit
import StoreKit
import SwiftUI
import UIKit

private func purchaseRoutingDebugLog(_ message: @autoclosure () -> String) {
    print("[FrameMio][Purchases] \(message())")
}

struct AppRootView: View {
    @Environment(\.scenePhase) private var scenePhase
    let dependencies: AppDependencies
    @State private var homePath: [AppRoute] = []
    @State private var templatesPath: [AppRoute] = []
    @State private var historyPath: [AppRoute] = []
    @State private var historyMode: HistoryMode = .projects
    @State private var purchaseDestination: PurchaseDestination?
    @State private var selectedTab: RootTab = .home
    @StateObject private var homeViewModel: HomeViewModel
    @StateObject private var storyLaunchCoordinator: StoryLaunchCoordinator
    @StateObject private var creditStore: CreditBalanceStore

    init(dependencies: AppDependencies = .preview(), homeMockState: HomeMockState? = nil) {
        self.dependencies = dependencies
        _homeViewModel = StateObject(
            wrappedValue: HomeViewModel(
                storyService: dependencies.storyService,
                photoLibrary: dependencies.photoLibrary,
                creditService: dependencies.creditService,
                mockState: homeMockState
            )
        )
        _storyLaunchCoordinator = StateObject(
            wrappedValue: StoryLaunchCoordinator(
                analysisService: dependencies.mediaAnalysis,
                mediaUploadService: dependencies.mediaUploadService,
                storyService: dependencies.storyService,
                creditService: dependencies.creditService,
                localReelRenderer: dependencies.localReelRenderer
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

            historyTab
                .tabItem {
                    Label("History", systemImage: "clock.arrow.circlepath")
                }
                .tag(RootTab.history)
                .accessibilityLabel("History")

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
                onCreateProject: { draft in
                    homePath.append(.mediaPicker(draft: draft))
                },
                onOpenStory: { story in
                    homePath.append(.storyPreview(story: story))
                },
                onOpenAllStories: {
                    selectedTab = .history
                    historyPath = [.allProjects]
                },
                onOpenPricing: openPricing,
                onCreateTextVideo: {
                    homePath.append(.textToVideo(regeneration: nil))
                },
                onCreateImageVideo: {
                    homePath.append(.imageToVideo(regeneration: nil))
                }
            )
            .navigationDestination(for: AppRoute.self) { route in
                destination(for: route) { homePath.append($0) }
            }
        }
    }

    private var templatesTab: some View {
        NavigationStack(path: $templatesPath) {
            TemplatesView(
                storyService: dependencies.storyService,
                hasTemplateAccess: hasTemplateAccess,
                onOpenPricing: openPricing
            ) { template, projectType in
                templatesPath.append(.mediaPicker(draft: .fromTemplate(template, projectType: projectType)))
            }
            .navigationDestination(for: AppRoute.self) { route in
                destination(for: route) { templatesPath.append($0) }
            }
        }
    }

    private var historyTab: some View {
        NavigationStack(path: $historyPath) {
            VStack(spacing: 0) {
                Picker("History type", selection: $historyMode) {
                    ForEach(HistoryMode.allCases, id: \.self) { mode in
                        Text(mode.title).tag(mode)
                    }
                }
                .pickerStyle(.segmented)
                .padding(.horizontal, FrameMioSpacing.lg)
                .padding(.top, FrameMioSpacing.md)
                .padding(.bottom, FrameMioSpacing.sm)

                switch historyMode {
                case .projects:
                    AllStoriesView(
                        viewModel: homeViewModel,
                        onOpenStory: { story in
                            historyPath.append(.storyPreview(story: story))
                        }
                    )
                case .generatedVideos:
                    GeneratedVideoHistoryView(
                        service: dependencies.textToVideoService,
                        photoLibrary: dependencies.photoLibrary,
                        mediaUploadStore: dependencies.mediaUploadStore,
                        onDeleted: {},
                        onRegenerate: { generation in
                            if generation.imageMediaID?.isEmpty == false {
                                historyPath.append(.imageToVideo(regeneration: generation))
                            } else {
                                historyPath.append(.textToVideo(regeneration: generation))
                            }
                        }
                    )
                }
            }
            .navigationDestination(for: AppRoute.self) { route in
                destination(for: route) { historyPath.append($0) }
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

    private var hasTemplateAccess: Bool {
        creditStore.balance?.hasActiveSubscriptionCycle == true
    }

    private func openBuyCredits(_ source: PricingEntryPoint) {
        purchaseRoutingDebugLog("buy_credits_tapped source=\(source.rawValue) destination=lookup cached_balance=\(creditStore.balance?.available.description ?? "nil") cached_reserved=\(creditStore.balance?.reserved.description ?? "nil") cached_has_subscription_cycle=\(creditStore.balance?.hasActiveSubscriptionCycle.description ?? "nil")")
        openPurchaseDestination(source)
    }

    private func openPurchaseDestination(_ source: PricingEntryPoint) {
        Task { @MainActor in
            let balance = (try? await dependencies.creditService.balance()) ?? creditStore.balance
            let hasBackendSubscriptionCycle = balance?.hasActiveSubscriptionCycle == true
            let hasStoreKitProEntitlement = hasBackendSubscriptionCycle ? false : await hasActiveProEntitlementForCreditPacks()
            let shouldShowCreditPacks = hasBackendSubscriptionCycle || hasStoreKitProEntitlement
            purchaseRoutingDebugLog("destination_lookup_complete source=\(source.rawValue) show_credit_packs=\(shouldShowCreditPacks) available=\(balance?.available.description ?? "nil") reserved=\(balance?.reserved.description ?? "nil") has_subscription_cycle=\(balance?.hasActiveSubscriptionCycle.description ?? "nil")")
            if shouldShowCreditPacks {
                purchaseRoutingDebugLog("destination=credit_packs source=\(source.rawValue) reason=active_subscription available=\(balance?.available.description ?? "nil") reserved=\(balance?.reserved.description ?? "nil")")
                purchaseDestination = .creditPacks(source)
            } else {
                purchaseRoutingDebugLog("destination=pro source=\(source.rawValue) reason=no_active_subscription available=\(balance?.available.description ?? "nil") reserved=\(balance?.reserved.description ?? "nil")")
                purchaseDestination = .pro(source)
            }
        }
    }

    private func hasActiveProEntitlementForCreditPacks() async -> Bool {
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
                purchaseRoutingDebugLog("subscription_route_status product=\(transaction.productID) state=\(String(describing: status.state)) will_auto_renew=\(renewalInfo.willAutoRenew) result=credit_packs")
                return true
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
        creditStore.refresh(force: true)
    }

    private func completePurchaseFlow() {
        purchaseRoutingDebugLog("purchase_completed current_tab=\(String(describing: selectedTab))")
        refreshCredits()
        purchaseDestination = nil
    }

    private func openCreationFromPurchase(_ route: AppRoute, source: String) {
        purchaseRoutingDebugLog("\(source) route=\(String(describing: route))")
        purchaseDestination = nil
        selectedTab = .home
        homePath.append(route)
    }

    private func closeCreatedProjectPreview() {
        homeViewModel.load()
        homePath.removeAll()
        templatesPath.removeAll()
        historyPath.removeAll()
        selectedTab = .home
    }

    private func handleProjectDeleted(_ storyID: String) {
        homeViewModel.removeStory(id: storyID)
        homeViewModel.load()
    }

    @ViewBuilder
    private func destination(for route: AppRoute, appendRoute: @escaping (AppRoute) -> Void) -> some View {
        switch route {
        case .allProjects:
            AllStoriesView(
                viewModel: homeViewModel,
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
                assets: assets,
                photoLibrary: dependencies.photoLibrary,
                storyService: dependencies.storyService,
                creditService: dependencies.creditService,
                promptTranscriptionService: dependencies.promptTranscriptionService,
                hasTemplateAccess: hasTemplateAccess,
                onContinue: { configuredDraft in
                    appendRoute(.storyCreating(draft: configuredDraft, assets: assets, launchID: UUID()))
                },
                onOpenPricing: openPricing
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
                hasTemplateAccess: hasTemplateAccess,
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
                onOpenPricing: openPricing,
                onClose: closeCreatedProjectPreview,
                onDeleted: handleProjectDeleted
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
                onOpenPricing: openPricing,
                onDeleted: handleProjectDeleted
            )
        case let .textToVideo(regeneration):
            TextToVideoView(
                regeneration: regeneration,
                service: dependencies.textToVideoService,
                creditService: dependencies.creditService,
                promptTranscriptionService: dependencies.promptTranscriptionService,
                photoLibrary: dependencies.photoLibrary,
                mediaUploadStore: dependencies.mediaUploadStore,
                mediaUploadService: dependencies.mediaUploadService,
                onOpenPricing: openPricing,
                onCreditsUpdated: refreshCredits,
                onRegenerate: { generation in
                    if generation.imageMediaID?.isEmpty == false {
                        appendRoute(.imageToVideo(regeneration: generation))
                    } else {
                        appendRoute(.textToVideo(regeneration: generation))
                    }
                }
            )
        case let .imageToVideo(regeneration):
            TextToVideoView(
                mode: .imageToVideo,
                regeneration: regeneration,
                service: dependencies.textToVideoService,
                creditService: dependencies.creditService,
                promptTranscriptionService: dependencies.promptTranscriptionService,
                photoLibrary: dependencies.photoLibrary,
                mediaUploadStore: dependencies.mediaUploadStore,
                mediaUploadService: dependencies.mediaUploadService,
                onOpenPricing: openPricing,
                onCreditsUpdated: refreshCredits,
                onRegenerate: { generation in
                    if generation.imageMediaID?.isEmpty == false {
                        appendRoute(.imageToVideo(regeneration: generation))
                    } else {
                        appendRoute(.textToVideo(regeneration: generation))
                    }
                }
            )
        }
    }
}

private enum RootTab: Hashable {
    case home
    case templates
    case history
    case profile
}

private enum HistoryMode: String, CaseIterable, Hashable {
    case projects
    case generatedVideos

    var title: String {
        switch self {
        case .projects:
            "Projects"
        case .generatedVideos:
            "Generated Videos"
        }
    }
}

struct TemplatesView: View {
    let storyService: StoryService
    let hasTemplateAccess: Bool
    let onOpenPricing: (PricingEntryPoint) -> Void
    let onUseTemplate: (VideoTemplate, VideoProjectType) -> Void
    @State private var selectedProjectType: VideoProjectType = .story
    @State private var templates: [VideoTemplate] = VideoTemplate.localFallbackCatalog
    @State private var selectedTemplateID: String? = VideoTemplate.localFallbackCatalog.first?.id
    @State private var isLoading = false
    @State private var hasAttemptedRemoteLoad = false

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
                    LazyVGrid(columns: templateGridColumns, alignment: .leading, spacing: FrameMioSpacing.md) {
                        ForEach(Array(visibleTemplates.enumerated()), id: \.element.id) { index, template in
                            let isLocked = templateIsLocked(at: index, template: template)
                            TemplateCard(
                                template: template,
                                isSelected: selectedTemplateID == template.id,
                                isLocked: isLocked,
                                cardWidth: nil,
                                previewHeight: 132
                            ) {
                                guard !isLocked else {
                                    onOpenPricing(.settings)
                                    return
                                }
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
        .safeAreaInset(edge: .bottom) {
            if selectedTemplate != nil {
                fixedUseTemplateButton
            }
        }
        .task { await loadTemplates() }
        .refreshable { await loadTemplates(force: true) }
    }

    private var fixedUseTemplateButton: some View {
        VStack(spacing: 0) {
            Divider()
                .overlay(FrameMioColors.divider.opacity(0.7))
            PrimaryButton(
                title: "Use Template",
                systemImage: "plus.circle.fill",
                action: useSelectedTemplate,
                isDisabled: selectedTemplateIsLocked
            )
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.md)
            .padding(.bottom, FrameMioSpacing.md)
            .background(FrameMioColors.canvas.opacity(0.96))
        }
    }

    private var templateGridColumns: [GridItem] {
        [
            GridItem(.flexible(minimum: 0), spacing: FrameMioSpacing.md),
            GridItem(.flexible(minimum: 0), spacing: FrameMioSpacing.md)
        ]
    }

    private func templateIsLocked(at index: Int, template: VideoTemplate) -> Bool {
        TemplateAccessPolicy.isLocked(index: index, template: template, hasTemplateAccess: hasTemplateAccess)
    }

    private var selectedTemplateIndex: Int? {
        visibleTemplates.firstIndex { $0.id == selectedTemplate?.id }
    }

    private var selectedTemplateIsLocked: Bool {
        guard let selectedTemplateIndex else { return true }
        guard let selectedTemplate else { return true }
        return templateIsLocked(at: selectedTemplateIndex, template: selectedTemplate)
    }

    private func useSelectedTemplate() {
        guard let selectedTemplate, !selectedTemplateIsLocked else { return }
        onUseTemplate(selectedTemplate, selectedProjectType)
    }

    private func loadTemplates(force: Bool = false) async {
        guard force || !hasAttemptedRemoteLoad else { return }
        isLoading = true
        defer { isLoading = false }

        do {
            let loaded = try await storyService.videoTemplates(for: nil)
            templates = loaded.isEmpty ? VideoTemplate.localFallbackCatalog : loaded
            hasAttemptedRemoteLoad = true
        } catch is CancellationError {
            return
        } catch {
            purchaseRoutingDebugLog("templates_tab_catalog_fallback error=\(String(describing: error))")
            hasAttemptedRemoteLoad = true
        }
        if !visibleTemplates.contains(where: { $0.id == selectedTemplateID }) {
            selectedTemplateID = visibleTemplates.first?.id
        }
    }
}

struct ProjectConfigurationView: View {
    @Environment(\.scenePhase) private var scenePhase
    @State private var promoSubject: PromoSubject
    @State private var promoGoal: PromoGoal
    @State private var promoDuration: ProjectDurationOption
    @State private var reelPlatform: ReelPlatform
    @State private var reelDuration: ProjectDurationOption
    @State private var reelStyle: ReelStyle
    @State private var prompt: String
    @State private var templates: [VideoTemplate]
    @State private var selectedTemplateID: String?
    @State private var templateConfiguration: VideoTemplateConfiguration?
    @State private var isShowingCustomization = false
    @State private var thumbnails: [String: UIImage] = [:]
    @State private var thumbnailTasks: [String: Task<Void, Never>] = [:]
    @State private var isCheckingCredits = false
    @State private var creditCost: Int
    @State private var creditBalance: Int?
    @State private var creditErrorMessage: String?

    let draft: VideoProjectDraft
    let assets: [MediaAsset]
    let photoLibrary: PhotoLibraryService
    let storyService: StoryService
    let creditService: CreditService?
    let promptTranscriptionService: PromptTranscriptionService?
    let hasTemplateAccess: Bool
    let onContinue: (VideoProjectDraft) -> Void
    let onOpenPricing: (PricingEntryPoint) -> Void

    init(
        draft: VideoProjectDraft,
        assets: [MediaAsset],
        photoLibrary: PhotoLibraryService,
        storyService: StoryService,
        creditService: CreditService? = nil,
        promptTranscriptionService: PromptTranscriptionService? = nil,
        hasTemplateAccess: Bool = false,
        onContinue: @escaping (VideoProjectDraft) -> Void,
        onOpenPricing: @escaping (PricingEntryPoint) -> Void
    ) {
        self.draft = draft
        self.assets = assets
        self.photoLibrary = photoLibrary
        self.storyService = storyService
        self.creditService = creditService
        self.promptTranscriptionService = promptTranscriptionService
        self.hasTemplateAccess = hasTemplateAccess
        self.onContinue = onContinue
        self.onOpenPricing = onOpenPricing
        _promoSubject = State(initialValue: draft.promo?.subject ?? .product)
        _promoGoal = State(initialValue: draft.promo?.goal ?? .sell)
        _promoDuration = State(initialValue: draft.promo?.duration ?? .automatic)
        _reelPlatform = State(initialValue: draft.reel?.platform ?? .instagram)
        _reelDuration = State(initialValue: draft.reel?.duration ?? .automatic)
        _reelStyle = State(initialValue: draft.reel?.style ?? .automatic)
        _prompt = State(initialValue: draft.prompt ?? "")
        _templates = State(initialValue: VideoTemplate.localFallbackCatalog.filter { $0.supportedProjectTypes.contains(draft.type) })
        _selectedTemplateID = State(initialValue: draft.templateID)
        _templateConfiguration = State(initialValue: draft.templateConfiguration)
        _creditCost = State(initialValue: CreditPricing.price(for: Self.creditAction(for: draft.type), stats: Self.creditStats(for: assets)))
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                header
                selectedMediaSection
                Divider()
                    .overlay(FrameMioColors.divider)
                templateSection

                if draft.type == .promo {
                    optionPicker(title: "What are you promoting?", selection: $promoSubject, values: PromoSubject.allCases)
                    promoGoalPicker
                } else {
                    optionPicker(title: "Platform", selection: $reelPlatform, values: ReelPlatform.allCases)
                }

                customizationEntry

                if let creditErrorMessage {
                    Text(creditErrorMessage)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.accent)
                        .accessibilityAddTraits(.isStaticText)
                }
            }
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.lg)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background(FrameMioColors.canvas.ignoresSafeArea())
        .navigationTitle(draft.type == .promo ? "Create Promo" : "Create Reel")
        .navigationBarTitleDisplayMode(.inline)
        .safeAreaInset(edge: .bottom) {
            fixedGenerateButton
        }
        .task {
            await loadTemplates()
            loadMissingThumbnails()
            refreshCreditQuote()
        }
        .onChange(of: scenePhase) { _, newPhase in
            guard newPhase == .active else { return }
            loadMissingThumbnails()
            refreshCreditQuote()
        }
        .sheet(isPresented: $isShowingCustomization) {
            if let template = selectedTemplate {
                ProjectTemplateSettingsSheet(
                    template: template,
                    configuration: Binding(
                        get: { resolvedTemplateConfiguration ?? creationDefaults(for: template).sanitized(for: template) },
                        set: { templateConfiguration = $0.sanitized(for: template) }
                    ),
                    projectDuration: draft.type == .promo ? $promoDuration : $reelDuration,
                    prompt: $prompt,
                    promptTranscriptionService: promptTranscriptionService
                )
            }
        }
    }

    private var fixedGenerateButton: some View {
        VStack(spacing: 0) {
            Divider()
                .overlay(FrameMioColors.divider.opacity(0.7))
            PrimaryButton(
                title: fixedGenerateButtonTitle,
                systemImage: shouldCreateLocally ? "play.rectangle.fill" : "sparkles",
                action: requestContinue,
                isDisabled: isCheckingCredits,
                isLoading: isCheckingCredits
            )
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.md)
            .padding(.bottom, FrameMioSpacing.md)
            .background(FrameMioColors.canvas.opacity(0.96))
        }
    }

    private var createButtonTitle: String {
        draft.type == .promo ? "Create Promo" : "Create Reel"
    }

    private var fixedGenerateButtonTitle: String {
        shouldCreateLocally ? "Create Reel Locally (\(creditCost) credits)" : "\(createButtonTitle) (\(creditCost) credits)"
    }

    private func refreshCreditQuote() {
        guard let creditService else { return }
        Task { @MainActor in
            do {
                let action = Self.creditAction(for: draft.type)
                async let balanceTask = creditService.balance()
                async let quoteTask = creditService.quote(action: action, stats: creditStats)
                let (balance, quote) = try await (balanceTask, quoteTask)
                creditBalance = balance.displayCredits
                creditCost = quote.credits
                creditErrorMessage = nil
            } catch {
                if creditErrorMessage == nil {
                    creditErrorMessage = AppError.from(error).localizedDescription
                }
            }
        }
    }

    private func requestContinue() {
        guard !isCheckingCredits else { return }
        guard let creditService else {
            onContinue(configuredDraft)
            return
        }

        isCheckingCredits = true
        creditErrorMessage = nil
        Task { @MainActor in
            defer { isCheckingCredits = false }
            do {
                let action = Self.creditAction(for: draft.type)
                let quote = try await creditService.quote(action: action, stats: creditStats)
                creditBalance = quote.balance
                creditCost = quote.credits
                if let issue = Self.creditIssue(from: quote, action: action) {
                    purchaseRoutingDebugLog("project_config_preflight_insufficient_credits type=\(draft.type.rawValue) required=\(issue.required) available=\(issue.available)")
                    onOpenPricing(.insufficientCredits)
                    return
                }
                purchaseRoutingDebugLog("project_config_preflight_ok type=\(draft.type.rawValue) required=\(quote.credits) balance=\(quote.balance)")
                onContinue(configuredDraft)
            } catch {
                creditErrorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    private var creditStats: CreditMediaStats {
        Self.creditStats(for: assets)
    }

    private static func creditStats(for assets: [MediaAsset]) -> CreditMediaStats {
        CreditMediaStats.createStory(assets: assets)
    }

    private static func creditAction(for projectType: VideoProjectType) -> CreditAction {
        projectType == .reel ? .createVideo : .createStory
    }

    private static func creditIssue(from quote: CreditQuote, action: CreditAction) -> CreditIssue? {
        guard quote.eligible, quote.balance >= quote.credits else {
            return CreditIssue(action: action, required: quote.credits, available: quote.balance)
        }
        return nil
    }

    private var selectedMediaSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Selected moments", subtitle: "\(assets.count) photos and videos")
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible()), GridItem(.flexible())], spacing: FrameMioSpacing.sm) {
                ForEach(assets) { asset in
                    MediaThumbnail(
                        image: thumbnailImage(for: asset),
                        isLoading: isThumbnailLoading(for: asset),
                        duration: asset.isVideo ? asset.duration : nil,
                        isLivePhoto: asset.isLivePhoto,
                        placeholderSystemImage: asset.isVideo ? "video.fill" : "photo.fill",
                        accessibilityTitle: asset.isVideo ? "Selected video" : "Selected photo"
                    )
                    .onAppear {
                        loadThumbnail(for: asset)
                    }
                    .onDisappear {
                        cancelThumbnail(for: asset)
                    }
                    .id(asset.id)
                }
            }
        }
    }

    private func thumbnailImage(for asset: MediaAsset) -> Image? {
        guard let image = thumbnails[asset.id] else { return nil }
        return Image(uiImage: image)
    }

    private func isThumbnailLoading(for asset: MediaAsset) -> Bool {
        thumbnailTasks[asset.id] != nil
    }

    private func loadMissingThumbnails() {
        for asset in assets where thumbnails[asset.id] == nil {
            loadThumbnail(for: asset)
        }
    }

    private func loadThumbnail(for asset: MediaAsset) {
        let assetID = asset.id
        guard thumbnails[assetID] == nil, thumbnailTasks[assetID] == nil else { return }
        thumbnailTasks[assetID] = Task { [photoLibrary] in
            do {
                let data = try await photoLibrary.requestThumbnail(for: asset, targetSize: FrameMioMetrics.mediaThumbnailRequestSize)
                try Task.checkCancellation()
                guard let image = UIImage(data: data) else {
                    await MainActor.run {
                        thumbnailTasks[assetID] = nil
                    }
                    return
                }
                await MainActor.run {
                    thumbnails[assetID] = image
                    thumbnailTasks[assetID] = nil
                }
            } catch {
                await MainActor.run {
                    thumbnailTasks[assetID] = nil
                }
            }
        }
    }

    private func cancelThumbnail(for asset: MediaAsset) {
        let assetID = asset.id
        thumbnailTasks[assetID]?.cancel()
        thumbnailTasks[assetID] = nil
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
                        ForEach(Array(templates.enumerated()), id: \.element.id) { index, template in
                            let isLocked = templateIsLocked(at: index, template: template)
                            TemplateCard(
                                template: template,
                                isSelected: selectedTemplateID == template.id,
                                isLocked: isLocked,
                                cardWidth: 184,
                                previewHeight: 122,
                                minHeight: 268
                            ) {
                                guard !isLocked else {
                                    onOpenPricing(.settings)
                                    return
                                }
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

    private var customizationEntry: some View {
        Button {
            guard selectedTemplate != nil else { return }
            FrameMioHaptics.secondaryAction()
            isShowingCustomization = true
        } label: {
            HStack(spacing: FrameMioSpacing.md) {
                Image(systemName: "slider.horizontal.3")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.accent)
                    .frame(width: FrameMioMetrics.minimumTouchTarget)
                    .accessibilityHidden(true)

                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text("Customize")
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                    Text(templateConfigurationSummary)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .lineLimit(2)
                        .fixedSize(horizontal: false, vertical: true)
                }

                Spacer(minLength: FrameMioSpacing.sm)

                Image(systemName: "chevron.right")
                    .font(FrameMioTypography.caption)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, FrameMioSpacing.md)
            .padding(.vertical, FrameMioSpacing.sm)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                    .stroke(FrameMioColors.divider.opacity(0.55), lineWidth: FrameMioMetrics.borderWidth)
            }
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .disabled(selectedTemplate == nil)
        .accessibilityLabel("Customize \(draft.type.title.lowercased()) template")
        .accessibilityValue(templateConfigurationSummary)
    }

    private var templateConfigurationSummary: String {
        guard let template = selectedTemplate else { return "Select a template first" }
        let configuration = resolvedTemplateConfiguration ?? creationDefaults(for: template).sanitized(for: template)
        var values = [template.name]
        if draft.type == .reel && LocalReelPolicy.shouldRenderLocally(draft: configuredDraft, assets: assets) {
            values.append("Local render")
        }
        if template.editableOptions.contains("duration_seconds") {
            switch selectedProjectDuration {
            case .fullSource:
                values.append(configuration.removeFillerAndPauses == true ? "Full length, cleaned" : "Full length")
            case .automatic:
                values.append("Auto length")
            case .fifteen, .thirty, .fortyFive, .sixty:
                values.append(selectedProjectDuration.title)
            }
        }
        if template.editableOptions.contains("visual_style"), let style = configuration.visualStyle {
            values.append(style.capitalized)
        }
        if template.editableOptions.contains("captions_enabled") {
            values.append(configuration.captionsEnabled == false ? "Captions off" : "Captions on")
        }
        if template.editableOptions.contains("overlay_placement"),
           let placement = configuration.overlayPlacement {
            values.append(placement.replacingOccurrences(of: "_", with: " ").capitalized)
        }
        if template.editableOptions.contains("overlay_size"),
           let size = configuration.overlaySize {
            values.append(size.capitalized)
        }
        if !prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            values.append("Prompt added")
        }
        return values.joined(separator: " · ")
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Tag(draft.type.title, systemImage: draft.type.icon)
            Text(draft.type == .promo ? "Configure the promotion." : "Choose the social format.")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)
            Text(draft.type == .promo ? "Keep the brief simple. Detailed editing comes after FrameMio creates the first draft." : "Pick the platform and style. FrameMio will handle the first short-form edit.")
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(FrameMioSpacing.lg)
        .frame(maxWidth: .infinity, alignment: .leading)
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

    private var promoGoalPicker: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Text("Goal")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)

            Picker("Promotion goal", selection: $promoGoal) {
                ForEach(PromoGoal.allCases) { goal in
                    Text(goal.title)
                        .tag(goal)
                }
            }
            .pickerStyle(.inline)
            .labelsHidden()
            .tint(FrameMioColors.accent)
            .environment(\.defaultMinListRowHeight, 40)
            .onChange(of: promoGoal) { _, _ in
                FrameMioHaptics.secondaryAction()
            }
        }
        .padding(.horizontal, FrameMioSpacing.md)
        .padding(.vertical, FrameMioSpacing.sm)
        .background(FrameMioColors.surface.opacity(0.74), in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))
        .accessibilityLabel("Promotion goal")
        .accessibilityValue(promoGoal.title)
    }

    private var configuredDraft: VideoProjectDraft {
        let trimmedPrompt = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        let configuration = resolvedTemplateConfiguration
        if draft.type == .promo {
            var configured = VideoProjectDraft.promo(
                configuration: PromoCreationConfiguration(
                    subject: promoSubject,
                    goal: promoGoal,
                    duration: promoDuration,
                    removeFillerAndPauses: configuration?.removeFillerAndPauses == true && configuration?.durationMode == .fullSource,
                    prompt: trimmedPrompt.isEmpty ? nil : trimmedPrompt
                )
            )
            configured.templateID = selectedTemplateID
            configured.templateVersion = selectedTemplate?.version
            configured.templateConfiguration = configuration
            return configured
        }
        var configured = VideoProjectDraft.reel(
            configuration: ReelCreationConfiguration(
                platform: reelPlatform,
                duration: reelDuration,
                removeFillerAndPauses: configuration?.removeFillerAndPauses == true && configuration?.durationMode == .fullSource,
                style: reelStyle,
                prompt: trimmedPrompt.isEmpty ? nil : trimmedPrompt
            )
        )
        configured.templateID = selectedTemplateID
        configured.templateVersion = selectedTemplate?.version
        configured.templateConfiguration = configuration
        return configured
    }

    private var shouldCreateLocally: Bool {
        LocalReelPolicy.shouldRenderLocally(draft: configuredDraft, assets: assets)
    }

    private var selectedTemplate: VideoTemplate? {
        templates.first(where: { $0.id == selectedTemplateID })
    }

    private var selectedProjectDuration: ProjectDurationOption {
        draft.type == .promo ? promoDuration : reelDuration
    }

    private var resolvedTemplateConfiguration: VideoTemplateConfiguration? {
        guard let template = selectedTemplate else { return templateConfiguration }
        return (templateConfiguration ?? creationDefaults(for: template))
            .sanitized(for: template)
    }

    private func creationDefaults(for template: VideoTemplate) -> VideoTemplateConfiguration {
        switch draft.type {
        case .story:
            return template.defaults.withAutomaticCreationDuration()
        case .promo:
            return template.defaults.withAutomaticCreationDuration()
        case .reel:
            return template.defaults.withLocalReelCreationDefaults()
        }
    }

    private func loadTemplates() async {
        do {
            let loaded = try await storyService.videoTemplates(for: draft.type)
            templates = loaded.isEmpty ? VideoTemplate.localFallbackCatalog.filter { $0.supportedProjectTypes.contains(draft.type) } : loaded
            reconcileTemplateSelection()
        } catch {
            templates = VideoTemplate.localFallbackCatalog.filter { $0.supportedProjectTypes.contains(draft.type) }
            reconcileTemplateSelection()
            purchaseRoutingDebugLog("template_catalog_fallback type=\(draft.type.rawValue) error=\(String(describing: error))")
        }
    }

    private func reconcileTemplateSelection() {
        if let selectedTemplate {
            templateConfiguration = resolvedTemplateConfiguration?.sanitized(for: selectedTemplate)
        } else if let first = templates.first {
            select(first)
        }
    }

    private func select(_ template: VideoTemplate) {
        guard let index = templates.firstIndex(where: { $0.id == template.id }), !templateIsLocked(at: index, template: template) else { return }
        selectedTemplateID = template.id
        templateConfiguration = compatibleConfiguration(templateConfiguration, with: template)
    }

    private func templateIsLocked(at index: Int, template: VideoTemplate) -> Bool {
        TemplateAccessPolicy.isLocked(index: index, template: template, hasTemplateAccess: hasTemplateAccess)
    }

    private func compatibleConfiguration(_ previous: VideoTemplateConfiguration?, with template: VideoTemplate) -> VideoTemplateConfiguration {
        var resolved = draft.type == .story
            ? template.defaults
            : creationDefaults(for: template)
        guard let previous else { return resolved }
        if template.editableOptions.contains("duration_seconds") { resolved.durationSeconds = previous.durationSeconds }
        if template.supportsFullSourceDuration && template.editableOptions.contains("duration_mode") {
            resolved.durationMode = previous.durationMode
            resolved.removeFillerAndPauses = previous.durationMode == .fullSource && previous.removeFillerAndPauses == true
        } else {
            resolved.durationMode = resolved.durationSeconds == nil ? .auto : .fixed
            resolved.removeFillerAndPauses = false
        }
        if template.editableOptions.contains("aspect_ratio") { resolved.aspectRatio = previous.aspectRatio }
        if template.editableOptions.contains("visual_style") { resolved.visualStyle = previous.visualStyle }
        if template.editableOptions.contains("captions_enabled") { resolved.captionsEnabled = previous.captionsEnabled }
        if template.editableOptions.contains("caption_style") { resolved.captionStyle = previous.captionStyle }
        if template.editableOptions.contains("scene_detail") { resolved.sceneDetail = previous.sceneDetail }
        if template.editableOptions.contains("overlay_placement") { resolved.overlayPlacement = previous.overlayPlacement }
        if template.editableOptions.contains("overlay_size") { resolved.overlaySize = previous.overlaySize }
        if template.editableOptions.contains("overlay_opacity") { resolved.overlayOpacity = previous.overlayOpacity }
        if template.editableOptions.contains("overlay_frame") { resolved.overlayFrame = previous.overlayFrame }
        if template.editableOptions.contains("overlay_animation"),
           let previousAnimation = previous.overlayAnimation,
           previousAnimation == "auto" ? template.id == "photo-over-video" : template.rendererCapabilities.contains(previousAnimation) {
            resolved.overlayAnimation = previousAnimation
        }
        return resolved.sanitized(for: template)
    }
}

private struct ProjectTemplateSettingsSheet: View {
    @Environment(\.dismiss) private var dismiss
    let template: VideoTemplate
    @Binding var configuration: VideoTemplateConfiguration
    @Binding var projectDuration: ProjectDurationOption
    @Binding var prompt: String
    let promptTranscriptionService: PromptTranscriptionService?

    private let styleOptions = ["cinematic", "warm", "minimal", "playful", "elegant"]
    private let captionStyleOptions: [(title: String, value: String)] = [
        ("Bold", "bold_social"),
        ("Cinematic", "cinematic_subtitle"),
        ("Minimal", "clean_minimal"),
        ("Playful", "playful_pop")
    ]
    private let sceneDetailOptions: [(title: String, value: String)] = [
        ("Balanced", "balanced"),
        ("More details", "more"),
        ("Main moments", "none")
    ]
    private let durationOptions = [15, 30, 45, 60]

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                    templateSummary

                    if supports("duration_seconds") {
                        durationSection
                    }

                    if supports("aspect_ratio") {
                        aspectRatioSection
                    }

                    if supports("visual_style") {
                        styleSection
                    }

                    if supports("scene_detail") {
                        sceneDetailSection
                    }

                    if supports("captions_enabled") || supports("caption_style") {
                        captionSection
                    }

                    if supportsAny(["overlay_placement", "overlay_size", "overlay_opacity", "overlay_frame", "overlay_animation"]) {
                        overlaySection
                    }

                    promptSection
                }
                .padding(FrameMioSpacing.lg)
                .background {
                    Color.clear
                        .contentShape(Rectangle())
                        .onTapGesture(perform: dismissKeyboard)
                }
            }
            .background(FrameMioColors.canvas.ignoresSafeArea())
            .navigationTitle("Customize \(template.name)")
            .navigationBarTitleDisplayMode(.inline)
            .scrollDismissesKeyboard(.immediately)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") {
                        dismiss()
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    private var templateSummary: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Template", subtitle: layoutDescription)
            HStack(alignment: .top, spacing: FrameMioSpacing.sm) {
                Image(systemName: layoutIcon)
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.accent)
                    .frame(width: FrameMioMetrics.minimumTouchTarget)
                    .accessibilityHidden(true)

                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text(template.name)
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                    Text(template.description)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .padding(FrameMioSpacing.md)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
        }
    }

    private var durationSection: some View {
        settingsSection(title: "Length", subtitle: "Set the target duration for this template") {
            LazyVGrid(columns: settingsGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                settingChip(
                    title: "Auto",
                    isSelected: projectDuration == .automatic
                ) {
                    projectDuration = .automatic
                    configuration.durationMode = .auto
                    configuration.durationSeconds = nil
                    configuration.removeFillerAndPauses = false
                }
                ForEach(durationOptions, id: \.self) { seconds in
                    settingChip(
                        title: "\(seconds)s",
                        isSelected: projectDuration.seconds.map(Int.init) == seconds
                    ) {
                        projectDuration = projectDurationOption(for: seconds)
                        configuration.durationMode = .fixed
                        configuration.durationSeconds = seconds
                        configuration.removeFillerAndPauses = false
                    }
                }
                if supportsFullLength {
                    settingChip(
                        title: "Full length",
                        isSelected: projectDuration == .fullSource
                    ) {
                        projectDuration = .fullSource
                        configuration.durationMode = .fullSource
                        configuration.durationSeconds = nil
                    }
                }
            }
            if projectDuration == .fullSource {
                Toggle("Remove filler words and pauses", isOn: Binding(
                    get: { configuration.removeFillerAndPauses ?? false },
                    set: { configuration.removeFillerAndPauses = $0 }
                ))
                .tint(FrameMioColors.accent)
            }
        }
        .onAppear(perform: normalizeDurationSelection)
    }

    private var aspectRatioSection: some View {
        settingsSection(title: "Frame", subtitle: "Choose the output shape for this template") {
            TemplateOptionMenu(
                title: "Aspect ratio",
                selection: configuration.aspectRatio ?? "9:16",
                options: ["9:16", "16:9", "1:1"]
            ) {
                configuration.aspectRatio = $0
            }
        }
    }

    private var styleSection: some View {
        settingsSection(title: "Visual style", subtitle: "Adjust the overall visual treatment") {
            LazyVGrid(columns: settingsGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                ForEach(styleOptions, id: \.self) { style in
                    settingChip(
                        title: style.capitalized,
                        isSelected: configuration.visualStyle?.lowercased() == style
                    ) {
                        configuration.visualStyle = style
                    }
                }
            }
        }
    }

    private var sceneDetailSection: some View {
        settingsSection(title: "Scene details", subtitle: "Choose how much supporting footage to include") {
            LazyVGrid(columns: settingsGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                ForEach(sceneDetailOptions, id: \.value) { option in
                    settingChip(
                        title: option.title,
                        isSelected: normalizedSceneDetail(configuration.sceneDetail) == option.value
                    ) {
                        configuration.sceneDetail = option.value
                    }
                }
            }
        }
    }

    private var captionSection: some View {
        settingsSection(title: "Captions", subtitle: "Control captions for this template") {
            if supports("captions_enabled") {
                Toggle("Show captions", isOn: Binding(
                    get: { configuration.captionsEnabled ?? true },
                    set: { configuration.captionsEnabled = $0 }
                ))
                .tint(FrameMioColors.accent)
            }

            if supports("caption_style") && (configuration.captionsEnabled ?? true) {
                LazyVGrid(columns: settingsGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                    ForEach(captionStyleOptions, id: \.value) { option in
                        settingChip(
                            title: option.title,
                            isSelected: normalizedCaptionStyle(configuration.captionStyle) == option.value
                        ) {
                            configuration.captionStyle = option.value
                        }
                    }
                }
            }
        }
    }

    private var overlaySection: some View {
        settingsSection(
            title: "Overlay",
            subtitle: template.defaults.layout == "picture_in_picture"
                ? "Keep the base video playing while supporting media appears above it."
                : "Adjust the bounded supporting-media treatment."
        ) {
            if supports("overlay_placement") {
                TemplateOptionMenu(
                    title: "Position",
                    selection: configuration.overlayPlacement ?? "bottom_right",
                    options: ["top_left", "top_right", "bottom_left", "bottom_right", "center"]
                ) {
                    configuration.overlayPlacement = $0
                }
            }

            if supports("overlay_size") {
                TemplateOptionMenu(
                    title: "Size",
                    selection: configuration.overlaySize ?? "medium",
                    options: ["small", "medium", "large", "half"]
                ) {
                    configuration.overlaySize = $0
                }
            }

            if supports("overlay_opacity") {
                HStack(spacing: FrameMioSpacing.md) {
                    Text("Opacity")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.ink)
                    Spacer()
                    Text("\(Int((configuration.overlayOpacity ?? 1) * 100))%")
                        .monospacedDigit()
                        .foregroundStyle(FrameMioColors.secondaryInk)
                    Stepper(
                        "",
                        value: Binding(
                            get: { configuration.overlayOpacity ?? 1 },
                            set: { configuration.overlayOpacity = min(1, max(0.35, $0)) }
                        ),
                        in: 0.35...1,
                        step: 0.05
                    )
                }
            }

            if supports("overlay_frame") {
                TemplateOptionMenu(
                    title: "Frame",
                    selection: configuration.overlayFrame ?? "subtle",
                    options: ["none", "subtle", "bold"]
                ) {
                    configuration.overlayFrame = $0
                }
            }

            if supports("overlay_animation") {
                TemplateOptionMenu(
                    title: "Motion",
                    selection: configuration.overlayAnimation ?? "fade",
                    options: template.id == "photo-over-video"
                        ? ["auto", "none", "fade", "scale", "move"]
                        : ["none", "fade", "subtle_scale"]
                ) {
                    configuration.overlayAnimation = $0
                }
            }
        }
    }

    private var promptSection: some View {
        FrameMioPromptInputSection(
            title: "Custom prompt",
            subtitle: "Guide the feeling, message, or pacing of the generated video",
            placeholder: templatePromptPlaceholder,
            quickPrompts: templatePromptSuggestions,
            prompt: $prompt,
            promptTranscriptionService: promptTranscriptionService,
            transcriptLogContext: "ProjectTemplateSettingsSheet",
            inputMinHeight: 180
        )
    }

    private func dismissKeyboard() {
        UIApplication.shared.sendAction(
            #selector(UIResponder.resignFirstResponder),
            to: nil,
            from: nil,
            for: nil
        )
    }

    private var templatePromptPlaceholder: String {
        switch template.defaults.layout {
        case "picture_in_picture":
            return "Describe the supporting image or video you want to emphasize."
        case "split_screen":
            return "Describe the two moments or ideas that should be compared."
        case "montage":
            return "Describe the energy, order, or feeling for the montage."
        default:
            return "Describe the message, feeling, or pacing you want."
        }
    }

    private var templatePromptSuggestions: [String] {
        switch template.defaults.layout {
        case "picture_in_picture":
            return ["Highlight the product", "Emphasize the proof", "Keep it conversational"]
        case "split_screen":
            return ["Compare before and after", "Show the contrast", "Make the offer clear"]
        case "montage":
            return ["Make it fast and punchy", "Lead with the strongest moment", "Build momentum"]
        default:
            return ["More cinematic", "More premium", "Focus on the message"]
        }
    }

    private func settingsSection<Content: View>(
        title: String,
        subtitle: String,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: title, subtitle: subtitle)
            content()
        }
        .padding(FrameMioSpacing.md)
        .background(FrameMioColors.surface.opacity(0.74), in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))
    }

    private func settingChip(title: String, isSelected: Bool, action: @escaping () -> Void) -> some View {
        Button {
            FrameMioHaptics.secondaryAction()
            action()
        } label: {
            Text(title)
                .font(FrameMioTypography.footnote)
                .foregroundStyle(isSelected ? FrameMioColors.inverseInk : FrameMioColors.ink)
                .frame(maxWidth: .infinity)
                .frame(minHeight: FrameMioMetrics.compactButtonHeight)
                .padding(.horizontal, FrameMioSpacing.sm)
                .background(
                    isSelected ? FrameMioColors.accent : FrameMioColors.elevatedSurface.opacity(0.82),
                    in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                )
        }
        .buttonStyle(FrameMioPressButtonStyle())
    }

    private var settingsGridColumns: [GridItem] {
        [GridItem(.adaptive(minimum: 108), spacing: FrameMioSpacing.sm, alignment: .leading)]
    }

    private func supports(_ option: String) -> Bool {
        template.editableOptions.contains(option)
    }

    private var supportsFullLength: Bool {
        template.supportsFullSourceDuration
    }

    private func supportsAny(_ options: [String]) -> Bool {
        options.contains(where: supports)
    }

    private func normalizeDurationSelection() {
        if projectDuration == .fullSource && !supportsFullLength {
            projectDuration = .automatic
        }
        if configuration.durationMode == .fullSource && !supportsFullLength {
            configuration.durationMode = configuration.durationSeconds == nil ? .auto : .fixed
            configuration.removeFillerAndPauses = false
        }
        if projectDuration != .fullSource || configuration.durationMode != .fullSource {
            configuration.removeFillerAndPauses = false
        }
    }

    private func projectDurationOption(for seconds: Int) -> ProjectDurationOption {
        switch seconds {
        case 15: .fifteen
        case 30: .thirty
        case 45: .fortyFive
        case 60: .sixty
        default: .automatic
        }
    }

    private func normalizedCaptionStyle(_ value: String?) -> String {
        switch value?.lowercased() {
        case "bold", "bold_social":
            return "bold_social"
        case "cinematic", "cinematic_subtitle":
            return "cinematic_subtitle"
        case "minimal", "clean_minimal":
            return "clean_minimal"
        case "playful", "playful_pop":
            return "playful_pop"
        default:
            return "cinematic_subtitle"
        }
    }

    private func normalizedSceneDetail(_ value: String?) -> String {
        switch value?.lowercased() {
        case "more", "more_details", "moredetails":
            return "more"
        case "none", "main_moments", "mainmoments":
            return "none"
        default:
            return "balanced"
        }
    }

    private var layoutIcon: String {
        switch template.defaults.layout {
        case "picture_in_picture":
            return "rectangle.on.rectangle"
        case "split_screen":
            return "rectangle.split.2x1"
        case "montage":
            return "square.grid.2x2"
        default:
            return "rectangle.fill"
        }
    }

    private var layoutDescription: String {
        switch template.defaults.layout {
        case "picture_in_picture":
            return "Picture over continuing video"
        case "split_screen":
            return "Two moments share the frame"
        case "montage":
            return "Several moments cut together quickly"
        default:
            return "Full-screen visual treatment"
        }
    }
}

struct TemplateCard: View {
    let template: VideoTemplate
    let isSelected: Bool
    let isLocked: Bool
    let onSelect: () -> Void
    @State private var fullScreenDemo: TemplateDemoPlayback?
    private let cardWidth: CGFloat?
    private let previewHeight: CGFloat
    private let minHeight: CGFloat
    private let cornerRadius: CGFloat = 8

    init(
        template: VideoTemplate,
        isSelected: Bool,
        isLocked: Bool = false,
        cardWidth: CGFloat? = 226,
        previewHeight: CGFloat = 176,
        minHeight: CGFloat = 314,
        onSelect: @escaping () -> Void
    ) {
        self.template = template
        self.isSelected = isSelected
        self.isLocked = isLocked
        self.cardWidth = cardWidth
        self.previewHeight = previewHeight
        self.minHeight = minHeight
        self.onSelect = onSelect
    }

    var body: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            ZStack(alignment: .bottomTrailing) {
                InlineTemplateVideo(path: template.demoVideoPath)
                    .frame(maxWidth: .infinity, minHeight: previewHeight, maxHeight: previewHeight)
                    .blur(radius: isLocked ? 4 : 0)
                    .saturation(isLocked ? 0.35 : 1)
                    .overlay {
                        if isLocked {
                            RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
                                .fill(.black.opacity(0.34))
                        }
                    }
                    .clipShape(RoundedRectangle(cornerRadius: cornerRadius, style: .continuous))

                Button {
                    guard !isLocked else { return }
                    FrameMioHaptics.secondaryAction()
                    if let url = templateDemoURL(template.demoVideoPath) {
                        fullScreenDemo = TemplateDemoPlayback(title: template.name, url: url)
                    }
                } label: {
                    Image(systemName: "play.fill")
                        .font(.caption.weight(.bold))
                        .foregroundStyle(.white)
                        .frame(width: 34, height: 34)
                        .background(.black.opacity(0.62), in: Circle())
                        .overlay {
                            Circle().stroke(.white.opacity(0.24), lineWidth: FrameMioMetrics.borderWidth)
                        }
                }
                .buttonStyle(FrameMioPressButtonStyle())
                .padding(FrameMioSpacing.xs)
                .accessibilityLabel("Play \(template.name) fullscreen")
                .disabled(isLocked)
                .opacity(isLocked ? 0 : 1)

                if isLocked {
                    VStack(spacing: FrameMioSpacing.xs) {
                        Image(systemName: "lock.fill")
                            .font(.title3.weight(.semibold))
                            .foregroundStyle(.white)
                            .frame(width: 42, height: 42)
                            .background(.black.opacity(0.58), in: Circle())
                        Text("Pro")
                            .font(FrameMioTypography.caption.weight(.semibold))
                            .foregroundStyle(.white)
                            .padding(.horizontal, FrameMioSpacing.sm)
                            .padding(.vertical, 4)
                            .background(.black.opacity(0.58), in: Capsule())
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .center)
                    .accessibilityHidden(true)
                }
            }
            .frame(maxWidth: .infinity, minHeight: previewHeight, maxHeight: previewHeight)
            .clipped()

            Text(template.name)
                .font(FrameMioTypography.headline)
                .foregroundStyle(isLocked ? FrameMioColors.secondaryInk : FrameMioColors.ink)
                .lineLimit(2)
                .frame(maxWidth: .infinity, minHeight: 46, maxHeight: 46, alignment: .topLeading)
            Text(template.description)
                .font(FrameMioTypography.caption)
                .foregroundStyle(FrameMioColors.secondaryInk)
                .lineLimit(3)
                .frame(maxWidth: .infinity, minHeight: 58, maxHeight: 58, alignment: .topLeading)
        }
        .padding(FrameMioSpacing.md)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .frame(width: cardWidth, alignment: .topLeading)
        .frame(height: minHeight, alignment: .topLeading)
        .background(isSelected ? FrameMioColors.accentMuted : FrameMioColors.elevatedSurface, in: RoundedRectangle(cornerRadius: cornerRadius, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
                .stroke(isSelected ? FrameMioColors.accent : (isLocked ? FrameMioColors.divider.opacity(0.54) : FrameMioColors.divider), lineWidth: isSelected ? 2 : 1)
        }
        .contentShape(RoundedRectangle(cornerRadius: cornerRadius, style: .continuous))
        .onTapGesture(perform: onSelect)
        .fullScreenCover(item: $fullScreenDemo) { demo in
            FullScreenTemplateDemoPlayer(url: demo.url, title: demo.title)
        }
        .accessibilityLabel("\(template.name). \(template.description)")
        .accessibilityValue(isLocked ? "Locked" : (isSelected ? "Selected" : "Not selected"))
        .accessibilityAddTraits(.isButton)
        .accessibilityAction {
            onSelect()
        }
    }
}

private struct TemplateDemoPlayback: Identifiable {
    let title: String
    let url: URL

    var id: String {
        "\(title)-\(url.absoluteString)"
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

private struct FullScreenTemplateDemoPlayer: View {
    @Environment(\.dismiss) private var dismiss
    let url: URL
    let title: String
    @State private var player = AVPlayer()
    @State private var endObserver: NSObjectProtocol?

    var body: some View {
        ZStack(alignment: .topTrailing) {
            Color.black
                .ignoresSafeArea()

            TemplateDemoPlayer(player: player, showsPlaybackControls: true, videoGravity: .resizeAspect)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.black)
                .ignoresSafeArea()

            Button {
                FrameMioHaptics.secondaryAction()
                dismiss()
            } label: {
                Image(systemName: "xmark")
                    .font(.headline.weight(.semibold))
                    .foregroundStyle(.white)
                    .frame(width: FrameMioMetrics.minimumTouchTarget, height: FrameMioMetrics.minimumTouchTarget)
                    .background(.black.opacity(0.56), in: Circle())
                    .overlay {
                        Circle().stroke(.white.opacity(0.18), lineWidth: FrameMioMetrics.borderWidth)
                    }
            }
            .buttonStyle(FrameMioPressButtonStyle())
            .padding(FrameMioSpacing.lg)
            .accessibilityLabel("Close \(title) preview")
        }
        .background(Color.black)
        .onAppear {
            let item = AVPlayerItem(url: url)
            player.replaceCurrentItem(with: item)
            player.isMuted = true
            player.play()
            endObserver = NotificationCenter.default.addObserver(
                forName: .AVPlayerItemDidPlayToEndTime,
                object: item,
                queue: .main
            ) { _ in
                player.seek(to: .zero)
                player.play()
            }
        }
        .onDisappear {
            if let endObserver {
                NotificationCenter.default.removeObserver(endObserver)
                self.endObserver = nil
            }
            player.pause()
            player.replaceCurrentItem(with: nil)
        }
        .accessibilityLabel("\(title) fullscreen template demo")
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

private struct TemplateDemoPlayer: UIViewControllerRepresentable {
    let player: AVPlayer
    let showsPlaybackControls: Bool
    let videoGravity: AVLayerVideoGravity

    func makeUIViewController(context: Context) -> AVPlayerViewController {
        let controller = AVPlayerViewController()
        controller.player = player
        controller.showsPlaybackControls = showsPlaybackControls
        controller.videoGravity = videoGravity
        controller.view.backgroundColor = .black
        return controller
    }

    func updateUIViewController(_ controller: AVPlayerViewController, context: Context) {
        controller.player = player
        controller.showsPlaybackControls = showsPlaybackControls
        controller.videoGravity = videoGravity
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
