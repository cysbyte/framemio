import StoreKit
import SwiftUI

private func pricingDebugLog(_ message: @autoclosure () -> String) {
    print("[FrameMio][Pricing] \(message())")
}
import UIKit

enum FrameMioProPlanKind: String, CaseIterable, Identifiable, Sendable {
    case yearly
    case monthly

    var id: String { rawValue }

    var title: String {
        switch self {
        case .yearly:
            "Yearly"
        case .monthly:
            "Monthly"
        }
    }

    var productID: String {
        switch self {
        case .yearly:
            FrameMioProCatalog.yearlyProductID
        case .monthly:
            FrameMioProCatalog.monthlyProductID
        }
    }

    var fallbackPrice: String {
        switch self {
        case .yearly:
            FrameMioProCatalog.yearlyFallbackDisplayPrice
        case .monthly:
            FrameMioProCatalog.monthlyFallbackDisplayPrice
        }
    }

    var pricePeriodText: String {
        switch self {
        case .yearly:
            "/ year"
        case .monthly:
            "/ month"
        }
    }

    var bestValueBadge: String? {
        switch self {
        case .yearly:
            "BEST VALUE"
        case .monthly:
            nil
        }
    }

    var savingsBadge: String? {
        switch self {
        case .yearly:
            "Save 33%"
        case .monthly:
            nil
        }
    }
}

@MainActor
final class FrameMioProViewModel: ObservableObject {
    @Published private(set) var monthlyProduct: Product?
    @Published private(set) var yearlyProduct: Product?
    @Published private(set) var balance: Int?
    @Published private(set) var isSubscribed = false
    @Published private(set) var renewalDate: Date?
    @Published private(set) var isLoading = false
    @Published private(set) var isPurchasing = false
    @Published private(set) var isRestoring = false
    @Published private(set) var monthlySubscriptionCycleCredits = FrameMioProCatalog.monthlyCredits
    @Published private(set) var yearlySubscriptionCycleCredits = FrameMioProCatalog.yearlyCredits
    @Published var selectedPlan: FrameMioProPlanKind = .yearly
    @Published var statusMessage: String?
    @Published var errorMessage: String?

    private let source: PricingEntryPoint
    private let creditService: CreditService?
    private let onBalanceUpdated: () -> Void
    private var loadTask: Task<Void, Never>?
    private var updatesTask: Task<Void, Never>?

    init(
        source: PricingEntryPoint,
        creditService: CreditService?,
        onBalanceUpdated: @escaping () -> Void
    ) {
        self.source = source
        self.creditService = creditService
        self.onBalanceUpdated = onBalanceUpdated
        startTransactionUpdates()
    }

    private var shouldSimulatePurchaseSuccess: Bool {
        let environment = ProcessInfo.processInfo.environment["FRAMEMIO_SIMULATE_PURCHASE_SUCCESS"]?.lowercased()
        if environment == "1" || environment == "true" || environment == "yes" {
            return true
        }
#if DEBUG
        return true
#else
        return false
#endif
    }

    deinit {
        loadTask?.cancel()
        updatesTask?.cancel()
    }

    var currentBalanceText: String {
        guard let balance else {
            return "Loading credits..."
        }
        if isSubscribed {
            return "\(balance.formatted()) credits remaining"
        }
        return "You have \(balance.formatted()) credits"
    }

    var renewalText: String? {
        guard let renewalDate else { return nil }
        return "Renews \(renewalDate.formatted(date: .long, time: .omitted))"
    }

    var primaryButtonTitle: String {
        if isSubscribed {
            return "Manage Subscription"
        }
        return "Start FrameMio Pro — \(selectedPlan.displayPrice(using: product(for: selectedPlan), fallback: selectedPlan.fallbackPrice))"
    }

    func load() {
        loadTask?.cancel()
        loadTask = Task { [weak self] in
            guard let self else { return }
            isLoading = true
            pricingDebugLog("load_start source=\(source.rawValue)")
            defer { isLoading = false }
            do {
                if shouldSimulatePurchaseSuccess {
                    pricingDebugLog("load_debug_skip_storekit_products source=\(source.rawValue) simulated=\(shouldSimulatePurchaseSuccess)")
                    monthlyProduct = nil
                    yearlyProduct = nil
                    _ = await fetchBalance()
                    await refreshEntitlements()
                    statusMessage = nil
                    errorMessage = nil
                    pricingDebugLog("load_complete_debug subscribed=\(isSubscribed) balance=\(balance.map(String.init) ?? "nil") renewal=\(renewalDate?.description ?? "nil")")
                } else {
                    async let productsTask = Product.products(for: FrameMioProCatalog.productIDs)
                    async let balanceTask: Void = fetchBalance()
                    let products = try await productsTask
                    monthlyProduct = products.first { $0.id == FrameMioProCatalog.monthlyProductID }
                    yearlyProduct = products.first { $0.id == FrameMioProCatalog.yearlyProductID }
                    pricingDebugLog("load_products_loaded monthly=\(monthlyProduct != nil) yearly=\(yearlyProduct != nil)")
                    _ = await balanceTask
                    await refreshEntitlements()
                    if selectedPlan == .yearly, yearlyProduct == nil, monthlyProduct != nil {
                        selectedPlan = .monthly
                    } else if selectedPlan == .monthly, monthlyProduct == nil, yearlyProduct != nil {
                        selectedPlan = .yearly
                    }
                    statusMessage = nil
                    errorMessage = nil
                    pricingDebugLog("load_complete subscribed=\(isSubscribed) balance=\(balance.map(String.init) ?? "nil") renewal=\(renewalDate?.description ?? "nil")")
                }
            } catch {
                statusMessage = nil
#if DEBUG
                errorMessage = nil
                pricingDebugLog("load_failed_debug suppressed_alert error=\(error.localizedDescription) mapped=\(AppError.from(error).localizedDescription)")
#else
                errorMessage = AppError.from(error).localizedDescription
                pricingDebugLog("load_failed error=\(error.localizedDescription) mapped=\(AppError.from(error).localizedDescription)")
#endif
            }
        }
    }

    func purchaseSelectedPlan() {
        guard !isPurchasing else { return }
        isPurchasing = true
        errorMessage = nil
        statusMessage = "Processing your purchase..."
        pricingDebugLog("purchase_tapped plan=\(selectedPlan.rawValue) source=\(source.rawValue) debug_success=\(shouldSimulatePurchaseSuccess)")
        Task { [weak self] in
            guard let self else { return }
            defer { isPurchasing = false }
            #if DEBUG
                pricingDebugLog("simulate_purchase_success plan=\(selectedPlan.rawValue) source=\(source.rawValue) simulated=\(shouldSimulatePurchaseSuccess)")
                await completePurchaseSuccess(
                    purchaseID: "debug-\(selectedPlan.rawValue)",
                    statusMessage: "Debug purchase completed."
                )
            #else
                guard let product = product(for: selectedPlan) else {
                    errorMessage = "Loading plans..."
                    statusMessage = nil
                    return
                }
                do {
                    let result = try await product.purchase()
                    switch result {
                    case let .success(verificationResult):
                        let transaction = try verifiedTransaction(from: verificationResult)
                        await transaction.finish()
                        await completePurchaseSuccess(
                            purchaseID: String(transaction.id),
                            statusMessage: isSubscribed ? "FrameMio Pro is active." : "Purchase completed."
                        )
                    case .pending:
                        statusMessage = "Purchase pending approval."
                    case .userCancelled:
                        statusMessage = nil
                    @unknown default:
                        statusMessage = "Purchase could not be completed."
                    }
                } catch {
                    errorMessage = AppError.from(error).localizedDescription
                    statusMessage = nil
                }
            #endif
        }
    }

    func restorePurchases() {
        guard !isRestoring else { return }
        isRestoring = true
        errorMessage = nil
        statusMessage = "Restoring purchases..."
        pricingDebugLog("restore_tapped source=\(source.rawValue)")
        Task { [weak self] in
            guard let self else { return }
            defer { isRestoring = false }
            do {
                try await AppStore.sync()
                await refreshEntitlements()
                await fetchBalance()
                onBalanceUpdated()
                statusMessage = "Purchases restored."
                pricingDebugLog("restore_complete subscribed=\(isSubscribed) balance=\(balance.map(String.init) ?? "nil")")
            } catch {
                errorMessage = AppError.from(error).localizedDescription
                statusMessage = nil
                pricingDebugLog("restore_failed error=\(error.localizedDescription) mapped=\(AppError.from(error).localizedDescription)")
            }
        }
    }

    func manageSubscription() {
        guard let url = URL(string: "https://apps.apple.com/account/subscriptions") else { return }
        UIApplication.shared.open(url)
    }

    private func product(for plan: FrameMioProPlanKind) -> Product? {
        switch plan {
        case .yearly:
            yearlyProduct
        case .monthly:
            monthlyProduct
        }
    }

    private func verifiedTransaction<T>(from result: VerificationResult<T>) throws -> T {
        switch result {
        case let .verified(value):
            return value
        case let .unverified(_, error):
            throw error
        }
    }

    private func refreshEntitlements() async {
        var activeSubscriptionDate: Date?
        var subscriptionActive = false
        pricingDebugLog("refresh_entitlements_start")
        for await result in Transaction.currentEntitlements {
            guard let transaction = try? verifiedTransaction(from: result) else { continue }
            guard FrameMioProCatalog.productIDs.contains(transaction.productID), transaction.revocationDate == nil else {
                continue
            }
            subscriptionActive = true
            if let expirationDate = transaction.expirationDate {
                if let currentDate = activeSubscriptionDate {
                    activeSubscriptionDate = max(currentDate, expirationDate)
                } else {
                    activeSubscriptionDate = expirationDate
                }
            }
        }
        isSubscribed = subscriptionActive
        renewalDate = activeSubscriptionDate
        pricingDebugLog("refresh_entitlements_complete subscribed=\(subscriptionActive) renewal=\(activeSubscriptionDate?.description ?? "nil")")
    }

    private func fetchBalance() async {
        guard let creditService else { return }
        do {
            let creditBalance = try await creditService.balance()
            balance = creditBalance.available
            monthlySubscriptionCycleCredits = creditBalance.monthlySubscriptionCycleCredits
            yearlySubscriptionCycleCredits = creditBalance.yearlySubscriptionCycleCredits
            pricingDebugLog("fetch_balance_complete balance=\(creditBalance.available) reserved=\(creditBalance.reserved)")
        } catch {
            pricingDebugLog("fetch_balance_failed error=\(error.localizedDescription)")
        }
    }

    private func grantPurchasedCredits(purchaseID: String) async throws {
        guard let creditService else { return }
        pricingDebugLog("grant_subscription_cycle_start plan=\(selectedPlan.rawValue) purchase_id=\(purchaseID)")
        do {
            try await creditService.grantSubscriptionCycle(plan: selectedPlan.rawValue, purchaseID: purchaseID, expiresAt: renewalDate)
            pricingDebugLog("grant_subscription_cycle_complete plan=\(selectedPlan.rawValue) purchase_id=\(purchaseID)")
        } catch {
            pricingDebugLog("grant_subscription_cycle_failed plan=\(selectedPlan.rawValue) purchase_id=\(purchaseID) error=\(error.localizedDescription)")
            throw error
        }
    }

    private func completePurchaseSuccess(purchaseID: String, statusMessage: String) async {
        pricingDebugLog("complete_purchase_success_start")
        do {
            try await grantPurchasedCredits(purchaseID: purchaseID)
        } catch {
            self.errorMessage = AppError.from(error).localizedDescription
            self.statusMessage = nil
            return
        }
        await refreshEntitlements()
        await fetchBalance()
        onBalanceUpdated()
        errorMessage = nil
        self.statusMessage = statusMessage
        pricingDebugLog("complete_purchase_success_complete subscribed=\(isSubscribed) balance=\(balance.map(String.init) ?? "nil")")
    }

    private func startTransactionUpdates() {
        updatesTask?.cancel()
        updatesTask = Task { [weak self] in
            guard let self else { return }
            pricingDebugLog("transaction_updates_listener_started")
            for await result in Transaction.updates {
                guard let transaction = try? verifiedTransaction(from: result) else { continue }
                guard FrameMioProCatalog.productIDs.contains(transaction.productID), transaction.revocationDate == nil else {
                    continue
                }
                await transaction.finish()
                await refreshEntitlements()
                await fetchBalance()
                onBalanceUpdated()
                pricingDebugLog("transaction_update_applied product=\(transaction.productID)")
            }
        }
    }
}

struct FrameMioProView: View {
    @Environment(\.dismiss) private var dismiss
    @StateObject private var viewModel: FrameMioProViewModel
    @State private var didLogLayout = false
    private let onClose: () -> Void

    init(
        source: PricingEntryPoint,
        creditService: CreditService?,
        onClose: @escaping () -> Void,
        onBalanceUpdated: @escaping () -> Void
    ) {
        _viewModel = StateObject(
            wrappedValue: FrameMioProViewModel(
                source: source,
                creditService: creditService,
                onBalanceUpdated: onBalanceUpdated
            )
        )
        self.onClose = onClose
    }

    var body: some View {
        GeometryReader { proxy in
            let contentWidth = max(proxy.size.width - FrameMioSpacing.lg * 2, 0)
            let cardWidth = max(contentWidth - FrameMioSpacing.md, 0)

            ScrollView {
                VStack(alignment: .leading, spacing: FrameMioSpacing.xl) {
                    header(cardWidth: cardWidth)
                    plansSection(cardWidth: cardWidth)
                    primaryActionSection
                    restoreAction
                    footer
                }
                .padding(.vertical, FrameMioSpacing.lg)
                .padding(.bottom, FrameMioSpacing.xxxl)
                .frame(width: contentWidth, alignment: .leading)
                .frame(maxWidth: .infinity, alignment: .center)
            }
                .background {
                    Color.clear
                        .onAppear {
                            guard !didLogLayout else { return }
                            didLogLayout = true
                            pricingDebugLog("screen_appear width=\(Int(proxy.size.width)) height=\(Int(proxy.size.height)) safe_area_leading=\(Int(proxy.safeAreaInsets.leading)) safe_area_trailing=\(Int(proxy.safeAreaInsets.trailing)) content_width=\(Int(contentWidth)) card_width=\(Int(cardWidth))")
                        }
            }
        }
        .background(
            LinearGradient(
                colors: [FrameMioColors.canvas, FrameMioColors.accentMuted.opacity(0.26)],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )
            .ignoresSafeArea()
        )
        .navigationTitle("FrameMio Pro")
        .navigationBarTitleDisplayMode(.inline)
        .ignoresSafeArea(edges: .bottom)
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Button("Close") {
                    onClose()
                    dismiss()
                }
            }
            ToolbarItem(placement: .topBarTrailing) {
                if viewModel.isSubscribed {
                    Button("Manage") {
                        viewModel.manageSubscription()
                    }
                }
            }
        }
        .task {
            viewModel.load()
        }
        .alert("Couldn’t load plans", isPresented: errorBinding) {
            Button("Dismiss", role: .cancel) { viewModel.errorMessage = nil }
        } message: {
            Text(viewModel.errorMessage ?? "")
        }
        .onChange(of: viewModel.errorMessage) { _, newValue in
            pricingDebugLog("error_message_changed value=\(newValue ?? "nil")")
        }
    }

    private func header(cardWidth: CGFloat) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            heroImagePlaceholder(width: cardWidth)

            VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
                Text(viewModel.currentBalanceText)
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                    .fixedSize(horizontal: false, vertical: true)
                if let renewalText = viewModel.renewalText {
                    Text(renewalText)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }
                if let statusMessage = viewModel.statusMessage {
                    Text(statusMessage)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.accent)
                }
            }
            .padding(.horizontal, FrameMioSpacing.xs)
        }
    }

    private func heroImagePlaceholder(width: CGFloat) -> some View {
        FrameMioBannerArtwork(assetName: "FrameMioProBanner")
            .frame(width: width)
            .frame(height: 176)
            .frame(maxWidth: .infinity, alignment: .center)
            .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
            .background {
                GeometryReader { proxy in
                    Color.clear
                        .onAppear {
                            pricingDebugLog("hero_banner width=\(Int(proxy.size.width)) height=\(Int(proxy.size.height))")
                        }
                }
            }
            .accessibilityLabel("FrameMio Pro story image")
    }

    private func plansSection(cardWidth: CGFloat) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Choose your plan", subtitle: "Cancel anytime in App Store settings")
            VStack(spacing: FrameMioSpacing.md) {
                planCard(for: .yearly, cardWidth: cardWidth)
                planCard(for: .monthly, cardWidth: cardWidth)
            }
        }
    }

    private func planCard(for plan: FrameMioProPlanKind, cardWidth: CGFloat) -> some View {
        let isSelected = viewModel.selectedPlan == plan
        return Button {
            viewModel.selectedPlan = plan
        } label: {
            VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                HStack(alignment: .top) {
                    VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                        if let badge = plan.bestValueBadge {
                            Text(badge)
                                .font(FrameMioTypography.eyebrow)
                                .foregroundStyle(FrameMioColors.accent)
                                .textCase(.uppercase)
                        }
                        Text(plan.title)
                            .font(FrameMioTypography.title)
                            .foregroundStyle(FrameMioColors.ink)
                    }
                    Spacer(minLength: FrameMioSpacing.sm)
                    Image(systemName: isSelected ? "checkmark.circle.fill" : "circle")
                        .font(.title3)
                        .foregroundStyle(isSelected ? FrameMioColors.accent : FrameMioColors.secondaryInk.opacity(0.4))
                }

                Text(plan.displayPrice(using: product(for: plan), fallback: plan.fallbackPrice) + plan.pricePeriodText)
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.ink)
                Text("\(plan == .monthly ? viewModel.monthlySubscriptionCycleCredits : viewModel.yearlySubscriptionCycleCredits) credits every month")
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                if let savings = plan.savingsBadge {
                    Text(savings)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.accent)
                }
            }
            .padding(FrameMioSpacing.lg)
            .frame(width: cardWidth, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .center)
            .background(
                isSelected ? FrameMioColors.accentMuted.opacity(0.7) : FrameMioColors.surface.opacity(0.82),
                in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                    .stroke(isSelected ? FrameMioColors.accent.opacity(0.38) : FrameMioColors.divider.opacity(0.58), lineWidth: FrameMioMetrics.borderWidth)
            }
            .frameMioElevation(.subtle)
            .background {
                GeometryReader { proxy in
                    Color.clear
                        .onAppear {
                            pricingDebugLog("plan_card=\(plan.rawValue) width=\(Int(proxy.size.width)) height=\(Int(proxy.size.height)) selected=\(isSelected)")
                        }
                }
            }
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .accessibilityLabel("\(plan.title) plan")
    }

    private var primaryActionSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            PrimaryButton(title: viewModel.primaryButtonTitle, systemImage: viewModel.isSubscribed ? "gearshape" : "sparkles") {
                if viewModel.isSubscribed {
                    viewModel.manageSubscription()
                } else {
                    viewModel.purchaseSelectedPlan()
                }
            }
            .disabled(viewModel.isPurchasing || viewModel.isRestoring)

            if viewModel.isPurchasing {
                Text("Processing your purchase…")
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }
        }
    }

    private var restoreAction: some View {
        SecondaryButton(title: viewModel.isRestoring ? "Restoring…" : "Restore Purchases", systemImage: "arrow.clockwise") {
            viewModel.restorePurchases()
        }
        .disabled(viewModel.isRestoring || viewModel.isPurchasing)
    }

    private var footer: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
            Text("Terms of Use · Privacy Policy")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)
            if let errorMessage = viewModel.errorMessage {
                Text(errorMessage)
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.accent)
            }
        }
    }

    private var errorBinding: Binding<Bool> {
        Binding(
            get: { viewModel.errorMessage != nil },
            set: { if !$0 { viewModel.errorMessage = nil } }
        )
    }

    private func product(for plan: FrameMioProPlanKind) -> Product? {
        switch plan {
        case .yearly:
            viewModel.yearlyProduct
        case .monthly:
            viewModel.monthlyProduct
        }
    }
}

private extension FrameMioProPlanKind {
    func displayPrice(using product: Product?, fallback: String) -> String {
        product?.displayPrice ?? fallback
    }
}

struct FrameMioBannerArtwork: View {
    let assetName: String

    var body: some View {
        Image(assetName)
            .resizable()
            .scaledToFill()
            .accessibilityHidden(true)
    }
}

@MainActor
final class FrameMioCreditPacksViewModel: ObservableObject {
    @Published private(set) var packs: [CreditPack] = []
    @Published private(set) var products: [String: Product] = [:]
    @Published private(set) var balance: CreditBalance?
    @Published private(set) var isLoading = false
    @Published private(set) var isPurchasing = false
    @Published var selectedPackID: String?
    @Published var statusMessage: String?
    @Published var errorMessage: String?

    private let creditService: CreditService?
    private let onBalanceUpdated: () -> Void
    private var loadTask: Task<Void, Never>?

    init(creditService: CreditService?, onBalanceUpdated: @escaping () -> Void) {
        self.creditService = creditService
        self.onBalanceUpdated = onBalanceUpdated
    }

    deinit {
        loadTask?.cancel()
    }

    var selectedPack: CreditPack? {
        packs.first { $0.id == selectedPackID } ?? packs.first
    }

    var primaryButtonTitle: String {
        guard let selectedPack else { return "Loading Credit Packs" }
        return "Buy \(selectedPack.credits.formatted()) Credits"
    }

    func displayPrice(for pack: CreditPack) -> String {
        guard let productID = pack.productID, let product = products[productID] else {
#if DEBUG
            return "Debug"
#else
            return "Unavailable"
#endif
        }
        return product.displayPrice
    }

    func load() {
        loadTask?.cancel()
        loadTask = Task { [weak self] in
            guard let self else { return }
            isLoading = true
            defer { isLoading = false }
            do {
                guard let creditService else { return }
                async let packsTask = creditService.creditPacks()
                async let balanceTask = creditService.balance()
                let loadedPacks = try await packsTask
                packs = loadedPacks
                if selectedPackID == nil {
                    selectedPackID = loadedPacks.first?.id
                }
                balance = try await balanceTask
#if !DEBUG
                let productIDs = loadedPacks.compactMap(\.productID)
                let loadedProducts = try await Product.products(for: productIDs)
                products = Dictionary(uniqueKeysWithValues: loadedProducts.map { ($0.id, $0) })
#endif
                errorMessage = nil
            } catch {
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    func purchaseSelectedPack() {
        guard !isPurchasing, let pack = selectedPack else { return }
        isPurchasing = true
        errorMessage = nil
        statusMessage = "Processing your credit purchase..."
        Task { [weak self] in
            guard let self else { return }
            defer { isPurchasing = false }
            do {
#if DEBUG
                let purchaseID = "debug-credit-pack-\(pack.id)"
                balance = try await creditService?.grantPurchasedCredits(packID: pack.id, purchaseID: purchaseID)
                statusMessage = "Debug credit purchase completed."
#else
                guard let productID = pack.productID, let product = products[productID] else {
                    statusMessage = nil
                    errorMessage = "Credit pack products are not configured yet."
                    return
                }
                let result = try await product.purchase()
                switch result {
                case let .success(verificationResult):
                    let transaction = try verifiedTransaction(from: verificationResult)
                    await transaction.finish()
                    balance = try await creditService?.grantPurchasedCredits(packID: pack.id, purchaseID: String(transaction.id))
                    statusMessage = "Credits added."
                case .pending:
                    statusMessage = "Purchase pending approval."
                    return
                case .userCancelled:
                    statusMessage = nil
                    return
                @unknown default:
                    statusMessage = "Purchase could not be completed."
                    return
                }
#endif
                onBalanceUpdated()
            } catch {
                statusMessage = nil
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    private func verifiedTransaction<T>(from result: VerificationResult<T>) throws -> T {
        switch result {
        case let .verified(value):
            return value
        case let .unverified(_, error):
            throw error
        }
    }
}

struct FrameMioCreditPacksView: View {
    @Environment(\.dismiss) private var dismiss
    @StateObject private var viewModel: FrameMioCreditPacksViewModel
    private let onClose: () -> Void

    init(
        creditService: CreditService?,
        onClose: @escaping () -> Void,
        onBalanceUpdated: @escaping () -> Void
    ) {
        _viewModel = StateObject(wrappedValue: FrameMioCreditPacksViewModel(creditService: creditService, onBalanceUpdated: onBalanceUpdated))
        self.onClose = onClose
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                header
                balanceCard
                packsSection
                PrimaryButton(
                    title: viewModel.primaryButtonTitle,
                    systemImage: "sparkles",
                    action: { viewModel.purchaseSelectedPack() },
                    isDisabled: viewModel.selectedPack == nil,
                    isLoading: viewModel.isPurchasing
                )
                .accessibilityLabel("Buy Credits. Adds purchased credits.")

                if let statusMessage = viewModel.statusMessage {
                    Text(statusMessage)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .frame(maxWidth: .infinity, alignment: .center)
                }
                if let errorMessage = viewModel.errorMessage {
                    Text(errorMessage)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(.red)
                        .frame(maxWidth: .infinity, alignment: .center)
                }
            }
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, 0)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background {
            LinearGradient(
                colors: [FrameMioColors.canvas, FrameMioColors.accentMuted.opacity(0.22)],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )
            .ignoresSafeArea()
        }
        .navigationTitle("Extra Credits")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button("Done") {
                    onClose()
                    dismiss()
                }
                .accessibilityLabel("Close Extra Credits")
            }
        }
        .task { viewModel.load() }
        .refreshable { viewModel.load() }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            Text("Extra Credits")
                .font(FrameMioTypography.screenTitle)
                .foregroundStyle(FrameMioColors.ink)
            Text("Purchased credits carry over and are used after your monthly credits.")
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
        }
        .accessibilityElement(children: .combine)
    }

    private var balanceCard: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
            Text("Available Credits")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)
            Text("✦ \((viewModel.balance?.displayCredits ?? 0).formatted())")
                .font(FrameMioTypography.display)
                .foregroundStyle(FrameMioColors.ink)
            HStack {
                Text("Monthly \((viewModel.balance?.displaySubscriptionCredits ?? 0).formatted())")
                Spacer()
                Text("Purchased \((viewModel.balance?.displayPurchasedCredits ?? 0).formatted())")
            }
            .font(FrameMioTypography.footnote)
            .foregroundStyle(FrameMioColors.secondaryInk)
        }
        .padding(FrameMioSpacing.lg)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(FrameMioColors.surface.opacity(0.86), in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.55), lineWidth: FrameMioMetrics.borderWidth)
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\((viewModel.balance?.displayCredits ?? 0)) credits available")
    }

    private var packsSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Credit Packs", subtitle: "One-time credits for more stories")
            if viewModel.isLoading && viewModel.packs.isEmpty {
                ProgressView()
                    .frame(maxWidth: .infinity)
                    .tint(FrameMioColors.accent)
                    .accessibilityLabel("Loading credit packs")
            } else {
                ForEach(viewModel.packs) { pack in
                    creditPackRow(pack)
                }
            }
        }
    }

    private func creditPackRow(_ pack: CreditPack) -> some View {
        Button {
            FrameMioHaptics.secondaryAction()
            viewModel.selectedPackID = pack.id
        } label: {
            HStack(spacing: FrameMioSpacing.md) {
                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text(pack.title)
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                    Text(pack.subtitle)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                    Text("\(pack.credits.formatted()) purchased credits")
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.accent)
                }
                Spacer(minLength: FrameMioSpacing.md)
                VStack(alignment: .trailing, spacing: FrameMioSpacing.xxs) {
                    Text(viewModel.displayPrice(for: pack))
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                    Image(systemName: viewModel.selectedPackID == pack.id ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(viewModel.selectedPackID == pack.id ? FrameMioColors.accent : FrameMioColors.secondaryInk)
                        .accessibilityHidden(true)
                }
            }
            .padding(FrameMioSpacing.lg)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(FrameMioColors.surface.opacity(0.9), in: RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous)
                    .stroke(viewModel.selectedPackID == pack.id ? FrameMioColors.accent.opacity(0.7) : FrameMioColors.divider.opacity(0.5), lineWidth: FrameMioMetrics.borderWidth)
            }
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .accessibilityLabel("\(pack.title), \(pack.credits) purchased credits")
    }
}
