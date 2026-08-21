import SwiftUI

struct EmptyState: View {
    let systemImage: String
    let title: String
    let message: String
    let actionTitle: String?
    let action: (() -> Void)?

    init(
        systemImage: String,
        title: String,
        message: String,
        actionTitle: String? = nil,
        action: (() -> Void)? = nil
    ) {
        self.systemImage = systemImage
        self.title = title
        self.message = message
        self.actionTitle = actionTitle
        self.action = action
    }

    var body: some View {
        VStack(spacing: FrameMioSpacing.md) {
            Image(systemName: systemImage)
                .font(.title2.weight(.medium))
                .symbolRenderingMode(.monochrome)
                .foregroundStyle(FrameMioColors.accent)
                .accessibilityHidden(true)

            Text(title)
                .font(FrameMioTypography.title)
                .foregroundStyle(FrameMioColors.ink)
                .multilineTextAlignment(.center)

            Text(message)
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)

            if let action, let actionTitle {
                PrimaryButton(title: actionTitle, systemImage: nil, action: action)
                    .frame(maxWidth: FrameMioMetrics.compactActionMaxWidth)
                    .padding(.top, FrameMioSpacing.xs)
            }
        }
        .frame(maxWidth: FrameMioMetrics.emptyStateMaxWidth)
        .padding(FrameMioSpacing.xl)
        .accessibilityElement(children: .contain)
    }
}

#Preview {
    EmptyState(
        systemImage: "photo.stack",
        title: "Your first story is waiting",
        message: "Choose a few moments and let FrameMio bring them together.",
        actionTitle: "Create Story"
    ) {}
    .padding()
    .background(FrameMioColors.canvas)
}
