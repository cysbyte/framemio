import SwiftUI

struct SettingsView: View {
    @ObservedObject private var viewModel: SettingsViewModel

    init(viewModel: SettingsViewModel) {
        _viewModel = ObservedObject(wrappedValue: viewModel)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
                    Tag("Privacy", systemImage: "lock.fill")
                    Text("Your memories stay personal.")
                        .font(FrameMioTypography.screenTitle)
                        .foregroundStyle(FrameMioColors.ink)
                        .fixedSize(horizontal: false, vertical: true)
                    Text("FrameMio is designed around clear choices, careful media handling, and user control over what gets selected and shared.")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(FrameMioSpacing.xl)
                .background(
                    LinearGradient(
                        colors: [FrameMioColors.surface.opacity(0.94), FrameMioColors.accentMuted.opacity(0.58)],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    ),
                    in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                )
                .overlay {
                    RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                        .stroke(FrameMioColors.divider.opacity(0.58), lineWidth: FrameMioMetrics.borderWidth)
                }
                .frameMioElevation(.subtle)

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
        .task { viewModel.refresh() }
        .alert("Photo access", isPresented: errorBinding) {
            Button("Dismiss", role: .cancel) { viewModel.errorMessage = nil }
        } message: {
            Text(viewModel.errorMessage ?? "")
        }
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
