import StoreKit
import SwiftUI

private func pricingDebugLog(_ message: @autoclosure () -> String) {
    print("[FrameMio][Pricing] \(message())")
}

private enum StoreKitProductLoadError: LocalizedError {
    case timedOut(seconds: Int)

    var errorDescription: String? {
        switch self {
        case let .timedOut(seconds):
            return "StoreKit product loading timed out after \(seconds) seconds."
        }
    }
}

private func loadStoreKitProducts(productIDs: [String], timeoutSeconds: Int, logPrefix: String) async throws -> [Product] {
    pricingDebugLog("\(logPrefix)_storekit_products_request_started ids=\(productIDs.joined(separator: ",")) timeout=\(timeoutSeconds)")
    return try await withThrowingTaskGroup(of: [Product].self) { group in
        group.addTask {
            try await Product.products(for: productIDs)
        }
        group.addTask {
            try await Task.sleep(nanoseconds: UInt64(timeoutSeconds) * 1_000_000_000)
            throw StoreKitProductLoadError.timedOut(seconds: timeoutSeconds)
        }
        guard let products = try await group.next() else {
            group.cancelAll()
            return []
        }
        group.cancelAll()
        pricingDebugLog("\(logPrefix)_storekit_products_request_finished returned=\(products.map(\.id).sorted().joined(separator: ",")) count=\(products.count)")
        return products
    }
}
import UIKit

enum FrameMioProPlanKind: String, CaseIterable, Identifiable, Sendable {
    case yearly
    case monthly

    var id: String { rawValue }

    init?(productID: String) {
        switch productID {
        case FrameMioProCatalog.yearlyProductID:
            self = .yearly
        case FrameMioProCatalog.monthlyProductID:
            self = .monthly
        default:
            return nil
        }
    }

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
            "Save 25%"
        case .monthly:
            nil
        }
    }
}

private struct FrameMioCreditPackPurchase: Sendable {
    let packID: String
    let productID: String

    init?(pack: CreditPack) {
        guard let productID = pack.productID ?? FrameMioCreditPackCatalog.productID(for: pack.id) else {
            return nil
        }
        self.packID = pack.id
        self.productID = productID
    }

    init?(productID: String, packs: [CreditPack]) {
        guard let pack = packs.first(where: { ($0.productID ?? FrameMioCreditPackCatalog.productID(for: $0.id)) == productID }) else {
            return nil
        }
        self.init(pack: pack)
    }
}

@MainActor
final class FrameMioProViewModel: ObservableObject {
    @Published private(set) var monthlyProduct: Product?
    @Published private(set) var yearlyProduct: Product?
    @Published private(set) var balance: Int?
    @Published private(set) var reservedBalance = 0
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
    private let onPurchaseCompleted: () -> Void
    private let onBalanceUpdated: () -> Void
    private var loadTask: Task<Void, Never>?
    private var updatesTask: Task<Void, Never>?

    init(
        source: PricingEntryPoint,
        creditService: CreditService?,
        onPurchaseCompleted: @escaping () -> Void,
        onBalanceUpdated: @escaping () -> Void
    ) {
        self.source = source
        self.creditService = creditService
        self.onPurchaseCompleted = onPurchaseCompleted
        self.onBalanceUpdated = onBalanceUpdated
        startTransactionUpdates()
    }

    private var shouldSimulatePurchaseSuccess: Bool {
        let environment = ProcessInfo.processInfo.environment["FRAMEMIO_SIMULATE_PURCHASE_SUCCESS"]?.lowercased()
        return environment == "1" || environment == "true" || environment == "yes"
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
            return "\(balance.formatted()) credits available"
        }
        return "You have \(balance.formatted()) credits available"
    }

    var reservedBalanceText: String? {
        guard reservedBalance > 0 else { return nil }
        return "\(reservedBalance.formatted()) credits are reserved for stories in progress."
    }

    var renewalText: String? {
        guard let renewalDate else { return nil }
        return "Renews \(renewalDate.formatted(date: .long, time: .omitted))"
    }

    var primaryButtonTitle: String {
        if isSubscribed {
            return "Manage Subscription"
        }
        guard canPurchaseSelectedPlan else {
            return isLoading ? "Loading FrameMio Pro…" : "Retry Loading Plans"
        }
        return "Start FrameMio Pro — \(selectedPlan.displayPrice(using: product(for: selectedPlan), fallback: selectedPlan.fallbackPrice))"
    }

    var canPurchaseSelectedPlan: Bool {
        isSubscribed || product(for: selectedPlan) != nil || shouldSimulatePurchaseSuccess
    }

    var primaryButtonIsLoading: Bool {
        !isSubscribed && (isPurchasing || (isLoading && !canPurchaseSelectedPlan))
    }

    var primaryButtonIsDisabled: Bool {
        isPurchasing || isRestoring || (!isSubscribed && isLoading && !canPurchaseSelectedPlan)
    }

    var primaryButtonDisabledReason: String {
        if isPurchasing { return "purchasing" }
        if isRestoring { return "restoring" }
        if isSubscribed { return "subscribed_manage_enabled" }
        if canPurchaseSelectedPlan { return "enabled" }
        if isLoading { return "loading_storekit_products" }
        if monthlyProduct == nil && yearlyProduct == nil { return "storekit_products_unavailable_retry_enabled" }
        return "selected_\(selectedPlan.rawValue)_product_unavailable"
    }

    var primaryButtonDebugState: String {
        "selected=\(selectedPlan.rawValue) disabled=\(primaryButtonIsDisabled) reason=\(primaryButtonDisabledReason) loading=\(isLoading) purchasing=\(isPurchasing) restoring=\(isRestoring) subscribed=\(isSubscribed) monthly_loaded=\(monthlyProduct != nil) yearly_loaded=\(yearlyProduct != nil) simulate=\(shouldSimulatePurchaseSuccess)"
    }

    func load() {
        if isLoading {
            pricingDebugLog("load_ignored reason=already_loading \(primaryButtonDebugState)")
            return
        }
        loadTask?.cancel()
        loadTask = Task { [weak self] in
            guard let self else { return }
            isLoading = true
            pricingDebugLog("load_start source=\(source.rawValue) \(primaryButtonDebugState)")
            defer {
                isLoading = false
                pricingDebugLog("load_end source=\(source.rawValue) \(primaryButtonDebugState)")
            }
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
                    async let balanceTask: Void = fetchBalance()
                    let products = try await loadStoreKitProducts(productIDs: FrameMioProCatalog.productIDs, timeoutSeconds: 8, logPrefix: "pro")
                    let loadedProductIDs = products.map(\.id).sorted()
                    pricingDebugLog("load_products_requested ids=\(FrameMioProCatalog.productIDs.joined(separator: ",")) returned=\(loadedProductIDs.joined(separator: ",")) count=\(products.count)")
                    monthlyProduct = products.first { $0.id == FrameMioProCatalog.monthlyProductID }
                    yearlyProduct = products.first { $0.id == FrameMioProCatalog.yearlyProductID }
                    pricingDebugLog("load_products_loaded monthly=\(monthlyProduct != nil) yearly=\(yearlyProduct != nil)")
                    _ = await balanceTask
                    await refreshEntitlements()
                    if isSubscribed {
                        do {
                            let synced = try await syncActiveSubscriptionCycleCredits(source: "load")
                            if synced {
                                onBalanceUpdated()
                            }
                            pricingDebugLog("load_subscription_credit_sync_complete synced=\(synced) balance=\(balance.map(String.init) ?? "nil") reserved=\(reservedBalance)")
                        } catch {
                            pricingDebugLog("load_subscription_credit_sync_failed error=\(error.localizedDescription) mapped=\(AppError.from(error).localizedDescription)")
                            statusMessage = "FrameMio Pro is active. Tap Restore Purchases if credits do not update."
                        }
                    }
                    if selectedPlan == .yearly, yearlyProduct == nil, monthlyProduct != nil {
                        pricingDebugLog("selected_plan_adjusted from=yearly to=monthly reason=yearly_product_missing")
                        selectedPlan = .monthly
                    } else if selectedPlan == .monthly, monthlyProduct == nil, yearlyProduct != nil {
                        pricingDebugLog("selected_plan_adjusted from=monthly to=yearly reason=monthly_product_missing")
                        selectedPlan = .yearly
                    }
                    statusMessage = nil
                    errorMessage = nil
                    pricingDebugLog("load_complete subscribed=\(isSubscribed) balance=\(balance.map(String.init) ?? "nil") renewal=\(renewalDate?.description ?? "nil") \(primaryButtonDebugState)")
                }
            } catch {
                statusMessage = nil
                if isCancellationLikeError(error) {
                    errorMessage = nil
                    pricingDebugLog("load_cancelled error=\(error.localizedDescription)")
                    return
                }
#if DEBUG
                errorMessage = nil
                statusMessage = "Could not load App Store plans. Tap Retry Loading Plans."
                pricingDebugLog("load_failed_debug suppressed_alert error=\(error.localizedDescription) mapped=\(AppError.from(error).localizedDescription) \(primaryButtonDebugState)")
#else
                errorMessage = nil
                statusMessage = "Could not load App Store plans. Tap Retry Loading Plans."
                pricingDebugLog("load_failed error=\(error.localizedDescription) mapped=\(AppError.from(error).localizedDescription) \(primaryButtonDebugState)")
#endif
            }
        }
    }

    func purchaseSelectedPlan() {
        guard !isPurchasing else {
            pricingDebugLog("purchase_ignored reason=already_purchasing \(primaryButtonDebugState)")
            return
        }
        guard canPurchaseSelectedPlan else {
            if isLoading {
                statusMessage = "Loading plans…"
                errorMessage = nil
                pricingDebugLog("purchase_blocked \(primaryButtonDebugState)")
            } else {
                statusMessage = "Retrying App Store product loading…"
                errorMessage = nil
                pricingDebugLog("purchase_retry_load \(primaryButtonDebugState)")
                load()
            }
            return
        }
        isPurchasing = true
        errorMessage = nil
        statusMessage = "Processing your purchase..."
        pricingDebugLog("purchase_tapped plan=\(selectedPlan.rawValue) source=\(source.rawValue) debug_success=\(shouldSimulatePurchaseSuccess) \(primaryButtonDebugState)")
        Task { [weak self] in
            guard let self else { return }
            defer { isPurchasing = false }
            if shouldSimulatePurchaseSuccess {
                pricingDebugLog("simulate_purchase_success plan=\(selectedPlan.rawValue) source=\(source.rawValue) simulated=\(shouldSimulatePurchaseSuccess)")
                try? await completePurchaseSuccess(statusMessage: "Debug purchase completed.")
            } else {
                guard let product = product(for: selectedPlan) else {
                    pricingDebugLog("purchase_missing_product selected=\(selectedPlan.rawValue) product_id=\(selectedPlan.productID) monthly_loaded=\(monthlyProduct != nil) yearly_loaded=\(yearlyProduct != nil)")
#if DEBUG
                    errorMessage = "Missing StoreKit product: \(selectedPlan.productID)"
#else
                    errorMessage = "Loading plans..."
#endif
                    statusMessage = nil
                    return
                }
                do {
                    let result = try await product.purchase()
                    switch result {
                    case let .success(verificationResult):
                        let transaction = try verifiedTransaction(from: verificationResult)
                        try await completePurchaseSuccess(for: transaction, statusMessage: "FrameMio Pro is active.")
                        await transaction.finish()
                    case .pending:
                        statusMessage = "Purchase pending approval."
                    case .userCancelled:
                        statusMessage = nil
                    @unknown default:
                        statusMessage = "Purchase could not be completed."
                    }
                } catch {
                    statusMessage = nil
                    if isCancellationLikeError(error) {
                        errorMessage = nil
                        pricingDebugLog("purchase_cancelled error=\(error.localizedDescription)")
                        return
                    }
                    errorMessage = AppError.from(error).localizedDescription
                    pricingDebugLog("purchase_failed error=\(error.localizedDescription) mapped=\(AppError.from(error).localizedDescription)")
                }
            }
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
                let synced = try await syncActiveSubscriptionCycleCredits(source: "restore")
                if !synced {
                    await fetchBalance()
                }
                onBalanceUpdated()
                statusMessage = "Purchases restored."
                pricingDebugLog("restore_complete subscribed=\(isSubscribed) synced=\(synced) balance=\(balance.map(String.init) ?? "nil") reserved=\(reservedBalance)")
            } catch {
                statusMessage = nil
                if isCancellationLikeError(error) {
                    errorMessage = nil
                    pricingDebugLog("restore_cancelled error=\(error.localizedDescription)")
                    return
                }
                errorMessage = AppError.from(error).localizedDescription
                pricingDebugLog("restore_failed error=\(error.localizedDescription) mapped=\(AppError.from(error).localizedDescription)")
            }
        }
    }

    func manageSubscription() {
        guard let url = URL(string: "https://apps.apple.com/account/subscriptions") else { return }
        UIApplication.shared.open(url)
    }

    private func isCancellationLikeError(_ error: Error) -> Bool {
        if error is CancellationError {
            return true
        }
        let nsError = error as NSError
        if nsError.domain == NSURLErrorDomain, nsError.code == NSURLErrorCancelled {
            return true
        }
        if nsError.domain == SKError.errorDomain, nsError.code == SKError.paymentCancelled.rawValue {
            return true
        }
        let description = error.localizedDescription.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        return description == "request canceled" || description == "request cancelled"
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
        for await result in StoreKit.Transaction.currentEntitlements {
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
            applyBalance(creditBalance)
            pricingDebugLog("fetch_balance_complete balance=\(creditBalance.available) reserved=\(creditBalance.reserved) subscription=\(creditBalance.subscriptionCredits) subscription_reserved=\(creditBalance.subscriptionReserved) subscription_expires=\(creditBalance.subscriptionCycleExpiresAt?.description ?? "nil")")
        } catch {
            pricingDebugLog("fetch_balance_failed error=\(error.localizedDescription)")
        }
    }

    private func applyBalance(_ creditBalance: CreditBalance) {
        balance = creditBalance.available
        reservedBalance = creditBalance.reserved
        monthlySubscriptionCycleCredits = creditBalance.monthlySubscriptionCycleCredits
        yearlySubscriptionCycleCredits = creditBalance.yearlySubscriptionCycleCredits
    }

    @discardableResult
    private func syncSubscriptionCycleCredits(for transaction: StoreKit.Transaction, source: String) async throws -> Bool {
        guard let creditService else {
            pricingDebugLog("subscription_credit_sync_skipped source=\(source) reason=no_credit_service product=\(transaction.productID) transaction=\(transaction.id)")
            return false
        }
        guard let plan = FrameMioProPlanKind(productID: transaction.productID) else {
            pricingDebugLog("subscription_credit_sync_ignored source=\(source) reason=non_pro_product product=\(transaction.productID) transaction=\(transaction.id)")
            return false
        }
        let purchaseID = String(transaction.id)
        pricingDebugLog("subscription_credit_sync_start source=\(source) plan=\(plan.rawValue) product=\(transaction.productID) transaction=\(purchaseID) expires=\(transaction.expirationDate?.description ?? "nil")")
        let creditBalance = try await creditService.grantSubscriptionCycleCredits(
            plan: transaction.productID,
            purchaseID: purchaseID,
            expiresAt: transaction.expirationDate
        )
        applyBalance(creditBalance)
        pricingDebugLog("subscription_credit_sync_success source=\(source) plan=\(plan.rawValue) product=\(transaction.productID) transaction=\(purchaseID) balance=\(creditBalance.available) reserved=\(creditBalance.reserved) subscription=\(creditBalance.subscriptionCredits) subscription_expires=\(creditBalance.subscriptionCycleExpiresAt?.description ?? "nil")")
        return true
    }

    private func syncActiveSubscriptionCycleCredits(source: String) async throws -> Bool {
        var didSync = false
        for await result in StoreKit.Transaction.currentEntitlements {
            guard let transaction = try? verifiedTransaction(from: result) else {
                pricingDebugLog("subscription_credit_sync_current_unverified source=\(source)")
                continue
            }
            guard FrameMioProPlanKind(productID: transaction.productID) != nil, transaction.revocationDate == nil else {
                pricingDebugLog("subscription_credit_sync_current_ignored source=\(source) product=\(transaction.productID) revoked=\(transaction.revocationDate != nil)")
                continue
            }
            didSync = try await syncSubscriptionCycleCredits(for: transaction, source: source) || didSync
        }
        if !didSync {
            pricingDebugLog("subscription_credit_sync_skipped source=\(source) reason=no_active_subscription")
        }
        return didSync
    }

    private func completePurchaseSuccess(for transaction: StoreKit.Transaction? = nil, statusMessage: String) async throws {
        pricingDebugLog("complete_purchase_success_start transaction=\(transaction.map { String($0.id) } ?? "nil")")
        await refreshEntitlements()
        let synced: Bool
        if let transaction {
            synced = try await syncSubscriptionCycleCredits(for: transaction, source: "purchase")
        } else {
            synced = false
            await fetchBalance()
        }
        if !synced {
            await fetchBalance()
        }
        onBalanceUpdated()
        errorMessage = nil
        self.statusMessage = statusMessage
        onPurchaseCompleted()
        pricingDebugLog("complete_purchase_success_complete subscribed=\(isSubscribed) synced=\(synced) balance=\(balance.map(String.init) ?? "nil") reserved=\(reservedBalance)")
    }

    private func startTransactionUpdates() {
        updatesTask?.cancel()
        updatesTask = Task { [weak self] in
            guard let self else { return }
            pricingDebugLog("transaction_updates_listener_started")
            for await result in StoreKit.Transaction.updates {
                guard let transaction = try? verifiedTransaction(from: result) else {
                    pricingDebugLog("transaction_update_unverified")
                    continue
                }
                guard FrameMioProPlanKind(productID: transaction.productID) != nil, transaction.revocationDate == nil else {
                    pricingDebugLog("transaction_update_ignored product=\(transaction.productID) revoked=\(transaction.revocationDate != nil)")
                    continue
                }
                do {
                    await refreshEntitlements()
                    try await syncSubscriptionCycleCredits(for: transaction, source: "transaction_update")
                    await transaction.finish()
                    onBalanceUpdated()
                    onPurchaseCompleted()
                    pricingDebugLog("transaction_update_applied product=\(transaction.productID) transaction=\(transaction.id) balance=\(balance.map(String.init) ?? "nil") reserved=\(reservedBalance)")
                } catch {
                    errorMessage = AppError.from(error).localizedDescription
                    statusMessage = nil
                    pricingDebugLog("transaction_update_sync_failed product=\(transaction.productID) transaction=\(transaction.id) error=\(error.localizedDescription) mapped=\(AppError.from(error).localizedDescription)")
                }
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
        onPurchaseCompleted: @escaping () -> Void,
        onBalanceUpdated: @escaping () -> Void
    ) {
        _viewModel = StateObject(
            wrappedValue: FrameMioProViewModel(
                source: source,
                creditService: creditService,
                onPurchaseCompleted: onPurchaseCompleted,
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
        .alert("FrameMio Pro", isPresented: errorBinding) {
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
                if let reservedBalanceText = viewModel.reservedBalanceText {
                    Text(reservedBalanceText)
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
                planCard(for: .monthly, cardWidth: cardWidth)
                planCard(for: .yearly, cardWidth: cardWidth)
            }
        }
    }

    private func planCard(for plan: FrameMioProPlanKind, cardWidth: CGFloat) -> some View {
        let isSelected = viewModel.selectedPlan == plan
        return Button {
            pricingDebugLog("plan_selected from=\(viewModel.selectedPlan.rawValue) to=\(plan.rawValue) before=\(viewModel.primaryButtonDebugState)")
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
            PrimaryButton(
                title: viewModel.primaryButtonTitle,
                systemImage: viewModel.isSubscribed ? "gearshape" : "sparkles",
                action: {
                    if viewModel.isSubscribed {
                        viewModel.manageSubscription()
                    } else {
                        viewModel.purchaseSelectedPlan()
                    }
                },
                isDisabled: viewModel.primaryButtonIsDisabled,
                isLoading: viewModel.primaryButtonIsLoading
            )
            .onAppear {
                pricingDebugLog("primary_button_state \(viewModel.primaryButtonDebugState)")
            }
            .onChange(of: viewModel.primaryButtonDebugState) { _, state in
                pricingDebugLog("primary_button_state \(state)")
            }

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
    private let onPurchaseCompleted: () -> Void
    private let onBalanceUpdated: () -> Void
    private var loadTask: Task<Void, Never>?
    private var updatesTask: Task<Void, Never>?

    private var shouldSimulatePurchaseSuccess: Bool {
        let environment = ProcessInfo.processInfo.environment["FRAMEMIO_SIMULATE_PURCHASE_SUCCESS"]?.lowercased()
        return environment == "1" || environment == "true" || environment == "yes"
    }

    init(
        creditService: CreditService?,
        onPurchaseCompleted: @escaping () -> Void,
        onBalanceUpdated: @escaping () -> Void
    ) {
        self.creditService = creditService
        self.onPurchaseCompleted = onPurchaseCompleted
        self.onBalanceUpdated = onBalanceUpdated
        startTransactionUpdates()
    }

    deinit {
        loadTask?.cancel()
        updatesTask?.cancel()
    }

    var selectedPack: CreditPack? {
        packs.first { $0.id == selectedPackID } ?? packs.first
    }

    var primaryButtonTitle: String {
        guard let selectedPack else { return isLoading ? "Loading Credit Packs…" : "Retry Loading Credit Packs" }
        if isLoading && !canPurchaseSelectedPack {
            return "Loading Credit Packs…"
        }
        if !canPurchaseSelectedPack {
            return "Retry Loading Credit Packs"
        }
        return "Buy \(selectedPack.credits.formatted()) Credits"
    }

    var canPurchaseSelectedPack: Bool {
        guard let selectedPack else { return false }
        return shouldSimulatePurchaseSuccess || product(for: selectedPack) != nil
    }

    var primaryButtonIsLoading: Bool {
        isPurchasing || (isLoading && !canPurchaseSelectedPack)
    }

    var primaryButtonIsDisabled: Bool {
        isPurchasing || (isLoading && !canPurchaseSelectedPack)
    }

    var primaryButtonDisabledReason: String {
        if isPurchasing { return "purchasing" }
        guard let selectedPack else {
            return isLoading ? "loading_credit_packs" : "no_credit_pack_selected"
        }
        if canPurchaseSelectedPack { return "enabled" }
        if isLoading { return "loading_storekit_products" }
        return "selected_\(selectedPack.id)_product_unavailable_retry_enabled"
    }

    var primaryButtonDebugState: String {
        let selectedID = selectedPackID ?? "nil"
        let selectedProductID = selectedPack.flatMap { productID(for: $0) } ?? "nil"
        let productKeys = products.keys.sorted().joined(separator: ",")
        return "selected_pack=\(selectedID) selected_product=\(selectedProductID) disabled=\(primaryButtonIsDisabled) reason=\(primaryButtonDisabledReason) loading=\(isLoading) purchasing=\(isPurchasing) packs=\(packs.count) products=\(products.count) product_ids=\(productKeys.isEmpty ? "none" : productKeys) simulate=\(shouldSimulatePurchaseSuccess)"
    }

    func displayPrice(for pack: CreditPack) -> String {
        guard let productID = productID(for: pack), let product = products[productID] else {
            if shouldSimulatePurchaseSuccess {
                return "Debug"
            }
            return "Unavailable"
        }
        return product.displayPrice
    }

    private func product(for pack: CreditPack) -> Product? {
        guard let productID = productID(for: pack) else { return nil }
        return products[productID]
    }

    private func productID(for pack: CreditPack) -> String? {
        pack.productID ?? FrameMioCreditPackCatalog.productID(for: pack.id)
    }

    func load() {
        if isLoading {
            pricingDebugLog("credit_packs_load_ignored reason=already_loading \(primaryButtonDebugState)")
            return
        }
        loadTask?.cancel()
        loadTask = Task { [weak self] in
            guard let self else { return }
            isLoading = true
            pricingDebugLog("credit_packs_load_start \(primaryButtonDebugState)")
            defer {
                isLoading = false
                pricingDebugLog("credit_packs_load_end \(primaryButtonDebugState)")
            }
            do {
                guard let creditService else {
                    pricingDebugLog("credit_packs_load_skipped reason=missing_credit_service")
                    return
                }
                async let packsTask = creditService.creditPacks()
                async let balanceTask = creditService.balance()
                let loadedPacks = try await packsTask
                packs = loadedPacks
                pricingDebugLog("credit_packs_loaded count=\(loadedPacks.count) ids=\(loadedPacks.map(\.id).joined(separator: ","))")
                if selectedPackID == nil {
                    selectedPackID = loadedPacks.first?.id
                    pricingDebugLog("credit_pack_selected_initial id=\(selectedPackID ?? "nil")")
                }
                balance = try await balanceTask
                pricingDebugLog("credit_packs_balance_loaded available=\(balance?.available.description ?? "nil") reserved=\(balance?.reserved.description ?? "nil") purchased_display=\(balance?.displayPurchasedCredits.description ?? "nil")")
                if shouldSimulatePurchaseSuccess {
                    products = [:]
                    pricingDebugLog("credit_packs_storekit_skipped reason=debug_simulation")
                } else {
                    let productIDs = loadedPacks.compactMap { self.productID(for: $0) }
                    pricingDebugLog("credit_packs_products_requested ids=\(productIDs.joined(separator: ","))")
                    let loadedProducts = try await loadStoreKitProducts(productIDs: productIDs, timeoutSeconds: 8, logPrefix: "credit_packs")
                    products = Dictionary(uniqueKeysWithValues: loadedProducts.map { ($0.id, $0) })
                    pricingDebugLog("credit_packs_products_loaded returned=\(loadedProducts.map(\.id).sorted().joined(separator: ",")) count=\(loadedProducts.count) \(primaryButtonDebugState)")
                }
                errorMessage = nil
            } catch {
                errorMessage = nil
                statusMessage = "Could not load App Store credit packs. Tap Retry Loading Credit Packs."
                pricingDebugLog("credit_packs_load_failed error=\(error.localizedDescription) mapped=\(AppError.from(error).localizedDescription) \(primaryButtonDebugState)")
            }
        }
    }

    func purchaseSelectedPack() {
        guard !isPurchasing else {
            pricingDebugLog("credit_pack_purchase_ignored reason=already_purchasing \(primaryButtonDebugState)")
            return
        }
        guard let pack = selectedPack else {
            if isLoading {
                statusMessage = "Loading credit packs…"
                errorMessage = nil
                pricingDebugLog("credit_pack_purchase_blocked reason=no_selected_pack \(primaryButtonDebugState)")
            } else {
                statusMessage = "Retrying App Store credit pack loading…"
                errorMessage = nil
                pricingDebugLog("credit_pack_purchase_retry_load reason=no_selected_pack \(primaryButtonDebugState)")
                load()
            }
            return
        }
        guard canPurchaseSelectedPack else {
            if isLoading {
                statusMessage = "Loading credit pack products…"
                errorMessage = nil
                pricingDebugLog("credit_pack_purchase_blocked \(primaryButtonDebugState)")
            } else {
                statusMessage = "Retrying App Store credit pack loading…"
                errorMessage = nil
                pricingDebugLog("credit_pack_purchase_retry_load \(primaryButtonDebugState)")
                load()
            }
            return
        }
        isPurchasing = true
        errorMessage = nil
        statusMessage = "Processing your credit purchase..."
        pricingDebugLog("credit_pack_purchase_tapped pack=\(pack.id) credits=\(pack.credits) \(primaryButtonDebugState)")
        Task { [weak self] in
            guard let self else { return }
            defer {
                isPurchasing = false
                pricingDebugLog("credit_pack_purchase_end pack=\(pack.id) \(primaryButtonDebugState)")
            }
            do {
                if shouldSimulatePurchaseSuccess {
                    let purchaseID = "debug-credit-pack-\(pack.id)"
                    balance = try await creditService?.grantPurchasedCredits(packID: pack.id, purchaseID: purchaseID)
                    statusMessage = "Debug credit purchase completed."
                    pricingDebugLog("credit_pack_purchase_debug_complete pack=\(pack.id) purchase_id=\(purchaseID) balance=\(balance?.available.description ?? "nil")")
                } else {
                    guard let productID = productID(for: pack), let product = products[productID] else {
                        statusMessage = nil
                        errorMessage = "Credit pack products are not configured yet."
                        pricingDebugLog("credit_pack_purchase_missing_product pack=\(pack.id) product_id=\(productID(for: pack) ?? "nil") \(primaryButtonDebugState)")
                        return
                    }
                    let result = try await product.purchase()
                    switch result {
                    case let .success(verificationResult):
                        let transaction = try verifiedTransaction(from: verificationResult)
                        balance = try await creditService?.grantPurchasedCredits(packID: pack.id, purchaseID: String(transaction.id))
                        await transaction.finish()
                        statusMessage = "Credits added."
                        pricingDebugLog("credit_pack_purchase_success pack=\(pack.id) product_id=\(productID) transaction=\(transaction.id) balance=\(balance?.available.description ?? "nil")")
                    case .pending:
                        statusMessage = "Purchase pending approval."
                        pricingDebugLog("credit_pack_purchase_pending pack=\(pack.id)")
                        return
                    case .userCancelled:
                        statusMessage = nil
                        pricingDebugLog("credit_pack_purchase_cancelled pack=\(pack.id)")
                        return
                    @unknown default:
                        statusMessage = "Purchase could not be completed."
                        pricingDebugLog("credit_pack_purchase_unknown_result pack=\(pack.id)")
                        return
                    }
                }
                onBalanceUpdated()
                onPurchaseCompleted()
            } catch {
                statusMessage = nil
                errorMessage = AppError.from(error).localizedDescription
                pricingDebugLog("credit_pack_purchase_failed pack=\(pack.id) error=\(error.localizedDescription) mapped=\(AppError.from(error).localizedDescription)")
            }
        }
    }

    private func startTransactionUpdates() {
        updatesTask?.cancel()
        updatesTask = Task { [weak self] in
            guard let self else { return }
            pricingDebugLog("credit_pack_transaction_updates_listener_started")
            for await result in StoreKit.Transaction.updates {
                guard let transaction = try? verifiedTransaction(from: result) else {
                    pricingDebugLog("credit_pack_transaction_update_unverified")
                    continue
                }
                guard transaction.revocationDate == nil else {
                    pricingDebugLog("credit_pack_transaction_update_ignored product=\(transaction.productID) reason=revoked")
                    continue
                }
                if let purchase = FrameMioCreditPackPurchase(productID: transaction.productID, packs: packs) {
                    do {
                        balance = try await creditService?.grantPurchasedCredits(packID: purchase.packID, purchaseID: String(transaction.id))
                        await transaction.finish()
                        onBalanceUpdated()
                        onPurchaseCompleted()
                        pricingDebugLog("credit_pack_transaction_update_applied pack=\(purchase.packID) product=\(transaction.productID) transaction=\(transaction.id) balance=\(balance?.available.description ?? "nil")")
                    } catch {
                        errorMessage = AppError.from(error).localizedDescription
                        statusMessage = nil
                        pricingDebugLog("credit_pack_transaction_update_failed product=\(transaction.productID) error=\(error.localizedDescription)")
                    }
                } else if let plan = FrameMioProPlanKind(productID: transaction.productID) {
                    do {
                        pricingDebugLog("credit_pack_subscription_transaction_sync_start plan=\(plan.rawValue) product=\(transaction.productID) transaction=\(transaction.id) expires=\(transaction.expirationDate?.description ?? "nil")")
                        balance = try await creditService?.grantSubscriptionCycleCredits(
                            plan: transaction.productID,
                            purchaseID: String(transaction.id),
                            expiresAt: transaction.expirationDate
                        )
                        await transaction.finish()
                        onBalanceUpdated()
                        onPurchaseCompleted()
                        pricingDebugLog("credit_pack_subscription_transaction_sync_success plan=\(plan.rawValue) product=\(transaction.productID) transaction=\(transaction.id) balance=\(balance?.available.description ?? "nil") subscription=\(balance?.subscriptionCredits.description ?? "nil")")
                    } catch {
                        errorMessage = AppError.from(error).localizedDescription
                        statusMessage = nil
                        pricingDebugLog("credit_pack_subscription_transaction_sync_failed product=\(transaction.productID) transaction=\(transaction.id) error=\(error.localizedDescription)")
                    }
                } else {
                    pricingDebugLog("credit_pack_transaction_update_ignored product=\(transaction.productID) reason=unknown_product packs=\(packs.count)")
                }
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
        onPurchaseCompleted: @escaping () -> Void,
        onBalanceUpdated: @escaping () -> Void
    ) {
        _viewModel = StateObject(
            wrappedValue: FrameMioCreditPacksViewModel(
                creditService: creditService,
                onPurchaseCompleted: onPurchaseCompleted,
                onBalanceUpdated: onBalanceUpdated
            )
        )
        self.onClose = onClose
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                balanceCard
                packsSection
                PrimaryButton(
                    title: viewModel.primaryButtonTitle,
                    systemImage: "sparkles",
                    action: { viewModel.purchaseSelectedPack() },
                    isDisabled: viewModel.primaryButtonIsDisabled,
                    isLoading: viewModel.primaryButtonIsLoading
                )
                .accessibilityLabel("Buy Credits. Adds purchased credits.")
                .onAppear {
                    pricingDebugLog("credit_pack_primary_button_state \(viewModel.primaryButtonDebugState)")
                }
                .onChange(of: viewModel.primaryButtonDebugState) { _, state in
                    pricingDebugLog("credit_pack_primary_button_state \(state)")
                }

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
            if let reserved = viewModel.balance?.displayReservedCredits, reserved > 0 {
                Text("\(reserved.formatted()) credits reserved for stories in progress.")
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }
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
            pricingDebugLog("credit_pack_selected from=\(viewModel.selectedPackID ?? "nil") to=\(pack.id) before=\(viewModel.primaryButtonDebugState)")
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
