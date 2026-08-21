import SwiftUI

struct SecondaryButton: View {
    let title: String
    let systemImage: String?
    let action: () -> Void
    var isDisabled = false

    @Environment(\.colorSchemeContrast) private var colorSchemeContrast

    var body: some View {
        Button {
            action()
        } label: {
            HStack(spacing: FrameMioSpacing.xs) {
                if let systemImage {
                    Image(systemName: systemImage)
                        .font(FrameMioTypography.headline)
                        .symbolRenderingMode(.monochrome)
                        .accessibilityHidden(true)
                }

                Text(title)
                    .font(FrameMioTypography.headline)
            }
            .foregroundStyle(FrameMioColors.ink)
            .padding(.horizontal, FrameMioSpacing.lg)
            .frame(minWidth: FrameMioMetrics.minimumTouchTarget, minHeight: FrameMioMetrics.compactButtonHeight)
            .background(
                FrameMioColors.surface,
                in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                    .stroke(
                        FrameMioColors.divider,
                        lineWidth: colorSchemeContrast == .increased
                            ? FrameMioMetrics.increasedBorderWidth
                            : FrameMioMetrics.borderWidth
                    )
            }
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .opacity(isDisabled ? FrameMioOpacity.disabled : 1)
        .disabled(isDisabled)
        .accessibilityLabel(title)
    }
}

#Preview {
    SecondaryButton(title: "Preview", systemImage: "play.fill") {}
        .padding()
}
