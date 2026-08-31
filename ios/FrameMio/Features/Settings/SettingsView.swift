import StoreKit
import SwiftUI

struct SettingsView: View {
    @Environment(\.requestReview) private var requestReview
    @ObservedObject private var viewModel: SettingsViewModel
    @ObservedObject private var creditStore: CreditBalanceStore
    let onBuyCredits: () -> Void

    init(
        viewModel: SettingsViewModel,
        creditStore: CreditBalanceStore,
        onBuyCredits: @escaping () -> Void
    ) {
        _viewModel = ObservedObject(wrappedValue: viewModel)
        _creditStore = ObservedObject(wrappedValue: creditStore)
        self.onBuyCredits = onBuyCredits
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                header
                purchasesSection
                photoLibrarySection
                appSection
                aboutSection
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
        .navigationTitle("Settings")
        .navigationBarTitleDisplayMode(.inline)
        .task { viewModel.refresh() }
        .alert("Settings", isPresented: errorBinding) {
            Button("Dismiss", role: .cancel) { viewModel.errorMessage = nil }
        } message: {
            Text(viewModel.errorMessage ?? "")
        }
    }

    private var header: some View {
        FrameMioBannerArtwork(assetName: "FrameMioSettingsBanner")
            .frame(maxWidth: .infinity)
            .frame(height: 130)
            .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
            .accessibilityLabel("Settings banner")
    }

    private var purchasesSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Purchases", subtitle: "Manage FrameMio Pro and credits")
            SettingsActionRow(title: "Buy Credits", subtitle: creditSummary, systemImage: "sparkles") {
                FrameMioHaptics.secondaryAction()
                onBuyCredits()
            }
            .accessibilityLabel("Buy Credits. Opens subscription or extra credit packs.")
            SettingsActionRow(
                title: viewModel.isRestoringPurchases ? "Restoring Purchases" : "Restore Purchases",
                subtitle: viewModel.statusMessage ?? "Sync purchases from the App Store",
                systemImage: "arrow.clockwise"
            ) {
                FrameMioHaptics.secondaryAction()
                viewModel.restorePurchases {
                    creditStore.refresh()
                }
            }
            .disabled(viewModel.isRestoringPurchases)
            .accessibilityLabel("Restore Purchases")
        }
        .settingsCardStyle()
    }

    private var photoLibrarySection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Photo Library", subtitle: "Control what FrameMio can see")

            HStack(spacing: FrameMioSpacing.md) {
                Image(systemName: "photo.on.rectangle")
                    .foregroundStyle(FrameMioColors.accent)
                    .frame(width: 44, height: 44)
                    .background(FrameMioColors.accentMuted.opacity(0.7), in: Circle())
                    .accessibilityHidden(true)

                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text("Access")
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                    Text(statusTitle)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }

                Spacer(minLength: FrameMioSpacing.sm)
            }
            .accessibilityElement(children: .combine)

            if viewModel.authorization != .authorized && viewModel.authorization != .limited {
                SecondaryButton(title: "Allow Photo Access", systemImage: "photo.badge.plus") {
                    viewModel.requestPhotoAccess()
                }
            }
        }
        .settingsCardStyle()
    }

    private var appSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "App", subtitle: "Support FrameMio")
            SettingsActionRow(title: "Rate FrameMio", subtitle: "Open the native App Store rating prompt", systemImage: "star") {
                FrameMioHaptics.secondaryAction()
                requestReview()
            }
            .accessibilityLabel("Rate FrameMio")
        }
        .settingsCardStyle()
    }

    private var aboutSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "About", subtitle: "FrameMio for iPhone memories")
            HStack {
                Text("Version")
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.ink)
                Spacer()
                Text("1.0")
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }
            Text("Terms of Use · Privacy Policy")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)
        }
        .settingsCardStyle()
    }

    private var creditSummary: String {
        guard let available = creditStore.availableCredits else {
            return creditStore.isLoading ? "Loading current balance" : "Open FrameMio Pro"
        }
        return "Current balance: \(available.formatted()) credits"
    }

    private var statusTitle: String {
        switch viewModel.authorization {
        case .authorized: "Allowed"
        case .limited: "Limited"
        case .notDetermined: "Not set"
        case .restricted: "Restricted"
        case .denied: "Not allowed"
        }
    }

    private var errorBinding: Binding<Bool> {
        Binding(
            get: { viewModel.errorMessage != nil },
            set: { if !$0 { viewModel.errorMessage = nil } }
        )
    }
}

private struct SettingsActionRow: View {
    let title: String
    let subtitle: String
    let systemImage: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: FrameMioSpacing.md) {
                Image(systemName: systemImage)
                    .foregroundStyle(FrameMioColors.accent)
                    .frame(width: 42, height: 42)
                    .background(FrameMioColors.accentMuted.opacity(0.68), in: Circle())
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text(title)
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                    Text(subtitle)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }
                Spacer(minLength: FrameMioSpacing.sm)
                Image(systemName: "chevron.right")
                    .font(FrameMioTypography.footnote.weight(.semibold))
                    .foregroundStyle(FrameMioColors.secondaryInk.opacity(0.6))
                    .accessibilityHidden(true)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(FrameMioPressButtonStyle())
    }
}

private extension View {
    func settingsCardStyle() -> some View {
        padding(FrameMioSpacing.lg)
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
