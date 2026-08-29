import StoreKit
import SwiftUI

struct ProfileView: View {
    @ObservedObject private var creditStore: CreditBalanceStore
    @StateObject private var viewModel = ProfileViewModel()
    let onBuyCredits: () -> Void

    init(creditStore: CreditBalanceStore, onBuyCredits: @escaping () -> Void) {
        _creditStore = ObservedObject(wrappedValue: creditStore)
        self.onBuyCredits = onBuyCredits
    }

    var body: some View {
        ScrollView {
            VStack(spacing: FrameMioSpacing.lg) {
                header
                creditCard
                buyCreditsButton
                subscriptionCard
            }
            .padding(FrameMioSpacing.lg)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background {
            LinearGradient(
                colors: [FrameMioColors.canvas, FrameMioColors.accentMuted.opacity(0.26)],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )
            .ignoresSafeArea()
        }
        .navigationTitle("Profile")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            creditStore.refresh()
            viewModel.refreshEntitlements()
        }
        .refreshable {
            creditStore.refresh()
            viewModel.refreshEntitlements()
        }
    }

    private var header: some View {
        VStack(spacing: FrameMioSpacing.md) {
            FrameMioBannerArtwork(assetName: "FrameMioProfileBanner")
                .frame(maxWidth: .infinity)
                .frame(height: 130)
                .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
            VStack(spacing: FrameMioSpacing.xxs) {
                Text("Profile")
                    .font(FrameMioTypography.screenTitle)
                    .foregroundStyle(FrameMioColors.ink)
                Text(viewModel.planTitle)
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }
        }
        .frame(maxWidth: .infinity)
        .accessibilityElement(children: .combine)
    }

    private var creditCard: some View {
        VStack(spacing: FrameMioSpacing.md) {
            Text(creditStore.largeTitle)
                .font(FrameMioTypography.display)
                .foregroundStyle(FrameMioColors.ink)
                .minimumScaleFactor(0.82)
                .lineLimit(1)
            Text(creditStore.isLowOnCredits ? "Your current available balance is running low." : "Your current available balance")
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
                .multilineTextAlignment(.center)
            if creditStore.isLoading {
                ProgressView()
                    .tint(FrameMioColors.accent)
                    .accessibilityLabel("Refreshing credits")
            }
        }
        .padding(FrameMioSpacing.xl)
        .frame(maxWidth: .infinity)
        .background(
            LinearGradient(
                colors: [FrameMioColors.surface.opacity(0.96), FrameMioColors.accentMuted.opacity(0.58)],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            ),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.accent.opacity(creditStore.isLowOnCredits ? 0.42 : 0.24), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.floating)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(creditStore.largeAccessibilityLabel)
    }

    private var buyCreditsButton: some View {
        PrimaryButton(title: "Buy Credits", systemImage: "sparkles") {
            FrameMioHaptics.secondaryAction()
            onBuyCredits()
        }
        .accessibilityLabel("Buy Credits. Opens subscription plans.")
    }

    private var subscriptionCard: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Subscription", subtitle: viewModel.subscriptionSubtitle)
            HStack(spacing: FrameMioSpacing.md) {
                Image(systemName: viewModel.isSubscribed ? "sparkles" : "person.crop.circle")
                    .foregroundStyle(FrameMioColors.accent)
                    .frame(width: 44, height: 44)
                    .background(FrameMioColors.accentMuted.opacity(0.7), in: Circle())
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text(viewModel.planTitle)
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                    if let renewalText = viewModel.renewalText {
                        Text(renewalText)
                            .font(FrameMioTypography.footnote)
                            .foregroundStyle(FrameMioColors.secondaryInk)
                    } else {
                        Text("FrameMio Pro includes \(FrameMioProCatalog.monthlyCredits.formatted())–\(FrameMioProCatalog.yearlyCredits.formatted()) credits every month.")
                            .font(FrameMioTypography.footnote)
                            .foregroundStyle(FrameMioColors.secondaryInk)
                    }
                }
                Spacer(minLength: 0)
            }
            .accessibilityElement(children: .combine)
        }
        .padding(FrameMioSpacing.lg)
        .background(
            FrameMioColors.surface.opacity(0.82),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.subtle)
    }
}

@MainActor
final class ProfileViewModel: ObservableObject {
    @Published private(set) var isSubscribed = false
    @Published private(set) var renewalDate: Date?

    var planTitle: String {
        isSubscribed ? "FrameMio Pro" : "Free Plan"
    }

    var subscriptionSubtitle: String {
        isSubscribed ? "Active" : "Upgrade when you need more credits"
    }

    var renewalText: String? {
        guard let renewalDate else { return nil }
        return "Renews \(renewalDate.formatted(date: .long, time: .omitted))"
    }

    func refreshEntitlements() {
        Task { [weak self] in
            var hasSubscription = false
            var latestRenewalDate: Date?
            for await result in Transaction.currentEntitlements {
                guard case let .verified(transaction) = result,
                      FrameMioProCatalog.productIDs.contains(transaction.productID),
                      transaction.revocationDate == nil else {
                    continue
                }
                hasSubscription = true
                if let expirationDate = transaction.expirationDate,
                   latestRenewalDate.map({ expirationDate > $0 }) ?? true {
                    latestRenewalDate = expirationDate
                }
            }
            self?.isSubscribed = hasSubscription
            self?.renewalDate = latestRenewalDate
        }
    }
}

struct ProfileView_Previews: PreviewProvider {
    static var previews: some View {
        NavigationStack {
            ProfileView(creditStore: CreditBalanceStore(creditService: MockCreditService())) {}
        }
    }
}
