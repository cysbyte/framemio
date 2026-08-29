import StoreKit
import SwiftUI
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

    var creditsPerCycle: Int {
        switch self {
        case .yearly, .monthly:
            FrameMioProCatalog.monthlyCredits
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
            defer { isLoading = false }
            do {
                async let productsTask = Product.products(for: FrameMioProCatalog.productIDs)
                async let balanceTask: Void = fetchBalance()
                let products = try await productsTask
                monthlyProduct = products.first { $0.id == FrameMioProCatalog.monthlyProductID }
                yearlyProduct = products.first { $0.id == FrameMioProCatalog.yearlyProductID }
                _ = await balanceTask
                await refreshEntitlements()
                if selectedPlan == .yearly, yearlyProduct == nil, monthlyProduct != nil {
                    selectedPlan = .monthly
                } else if selectedPlan == .monthly, monthlyProduct == nil, yearlyProduct != nil {
                    selectedPlan = .yearly
                }
                statusMessage = nil
                errorMessage = nil
            } catch {
                statusMessage = nil
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    func purchaseSelectedPlan() {
        guard !isPurchasing else { return }
        guard let product = product(for: selectedPlan) else {
            errorMessage = "Loading plans..."
            return
        }
        isPurchasing = true
        errorMessage = nil
        statusMessage = "Processing your purchase..."
        Task { [weak self] in
            guard let self else { return }
            defer { isPurchasing = false }
            do {
                let result = try await product.purchase()
                switch result {
                case let .success(verificationResult):
                    let transaction = try verifiedTransaction(from: verificationResult)
                    await transaction.finish()
                    await refreshEntitlements()
                    await fetchBalance()
                    onBalanceUpdated()
                    statusMessage = isSubscribed ? "FrameMio Pro is active." : "Purchase completed."
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
        }
    }

    func restorePurchases() {
        guard !isRestoring else { return }
        isRestoring = true
        errorMessage = nil
        statusMessage = "Restoring purchases..."
        Task { [weak self] in
            guard let self else { return }
            defer { isRestoring = false }
            do {
                try await AppStore.sync()
                await refreshEntitlements()
                await fetchBalance()
                onBalanceUpdated()
                statusMessage = "Purchases restored."
            } catch {
                errorMessage = AppError.from(error).localizedDescription
                statusMessage = nil
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
    }

    private func fetchBalance() async {
        guard let creditService else { return }
        do {
            let creditBalance = try await creditService.balance()
            balance = creditBalance.available
        } catch {
        }
    }

    private func startTransactionUpdates() {
        updatesTask?.cancel()
        updatesTask = Task { [weak self] in
            guard let self else { return }
            for await result in Transaction.updates {
                guard let transaction = try? verifiedTransaction(from: result) else { continue }
                guard FrameMioProCatalog.productIDs.contains(transaction.productID), transaction.revocationDate == nil else {
                    continue
                }
                await transaction.finish()
                await refreshEntitlements()
                await fetchBalance()
                onBalanceUpdated()
            }
        }
    }
}

struct FrameMioProView: View {
    @Environment(\.dismiss) private var dismiss
    @StateObject private var viewModel: FrameMioProViewModel
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
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.xl) {
                header
                plansSection
                benefitsSection
                primaryActionSection
                restoreAction
                footer
            }
            .padding(FrameMioSpacing.lg)
            .padding(.bottom, FrameMioSpacing.xxxl)
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
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            heroImagePlaceholder

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

    private var heroImagePlaceholder: some View {
        FrameMioBannerArtwork(assetName: "FrameMioProBanner")
            .frame(maxWidth: .infinity)
            .frame(height: 176)
            .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
            .accessibilityLabel("FrameMio Pro story image")
    }

    private var plansSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Choose your plan", subtitle: "Cancel anytime in App Store settings")
            VStack(spacing: FrameMioSpacing.md) {
                planCard(for: .yearly)
                planCard(for: .monthly)
            }
        }
    }

    private func planCard(for plan: FrameMioProPlanKind) -> some View {
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
                Text("\(plan.creditsPerCycle.formatted()) credits every month")
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                if let savings = plan.savingsBadge {
                    Text(savings)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.accent)
                }
            }
            .padding(FrameMioSpacing.lg)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                isSelected ? FrameMioColors.accentMuted.opacity(0.7) : FrameMioColors.surface.opacity(0.82),
                in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                    .stroke(isSelected ? FrameMioColors.accent.opacity(0.38) : FrameMioColors.divider.opacity(0.58), lineWidth: FrameMioMetrics.borderWidth)
            }
            .frameMioElevation(.subtle)
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .accessibilityLabel("\(plan.title) plan")
    }

    private var benefitsSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "What’s included", subtitle: "Premium tools for making stories faster")
            VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                benefitRow("AI Story Creation")
                benefitRow("Automatic Video Editing")
                benefitRow("AI Captions")
                benefitRow("Background Music")
                benefitRow("HD Export")
                benefitRow("Story Editing")
            }
        }
    }

    private func benefitRow(_ title: String) -> some View {
        HStack(spacing: FrameMioSpacing.sm) {
            Image(systemName: "checkmark.circle.fill")
                .foregroundStyle(FrameMioColors.accent)
            Text(title)
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.ink)
            Spacer(minLength: 0)
        }
        .padding(.vertical, FrameMioSpacing.xxs)
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
            .disabled(viewModel.isPurchasing || viewModel.isLoading || viewModel.isRestoring)

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
