import SwiftUI

struct PrimaryButton: View {
    let title: String
    let systemImage: String?
    let action: () -> Void
    var isDisabled = false
    @Environment(\.isEnabled) private var isEnabled

    var body: some View {
        Button {
            FrameMioHaptics.primaryAction()
            action()
        } label: {
            ButtonLabel(title: title, systemImage: systemImage)
                .foregroundStyle(FrameMioColors.inverseInk)
                .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.buttonHeight)
                .background(
                    FrameMioColors.accent,
                    in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                )
                .frameMioElevation(.subtle)
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .opacity(isDisabled || !isEnabled ? FrameMioOpacity.disabled : 1)
        .disabled(isDisabled)
        .accessibilityLabel(title)
    }
}

private struct ButtonLabel: View {
    let title: String
    let systemImage: String?

    var body: some View {
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
        .padding(.horizontal, FrameMioSpacing.lg)
        .frame(minWidth: FrameMioMetrics.minimumTouchTarget, minHeight: FrameMioMetrics.minimumTouchTarget)
    }
}

struct FrameMioPressButtonStyle: ButtonStyle {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed && !reduceMotion ? FrameMioAnimation.pressedScale : 1)
            .opacity(configuration.isPressed ? FrameMioOpacity.pressed : 1)
            .animation(reduceMotion ? nil : FrameMioAnimation.responsive, value: configuration.isPressed)
    }
}

#Preview {
    PrimaryButton(title: "Create Memory", systemImage: "plus") {}
        .padding()
}
