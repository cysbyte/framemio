import SwiftUI

struct LoadingState: View {
    let title: String
    let message: String?

    init(title: String = "Preparing your story", message: String? = nil) {
        self.title = title
        self.message = message
    }

    var body: some View {
        VStack(spacing: FrameMioSpacing.md) {
            ProgressView()
                .tint(FrameMioColors.accent)
                .accessibilityLabel("Loading")

            Text(title)
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)

            if let message {
                Text(message)
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                    .multilineTextAlignment(.center)
            }
        }
        .frame(maxWidth: FrameMioMetrics.loadingStateMaxWidth)
        .padding(FrameMioSpacing.xl)
        .background(
            FrameMioColors.surface.opacity(0.78),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.subtle)
        .accessibilityElement(children: .combine)
    }
}

#Preview {
    LoadingState(message: "Finding the moments that belong together.")
        .padding()
        .background(FrameMioColors.canvas)
}
