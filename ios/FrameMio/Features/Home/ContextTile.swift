import SwiftUI

struct ContextTile: View {
    let context: MomentContext
    private let title: String?
    private let systemImage: String?

    init(context: MomentContext) {
        self.context = context
        self.title = nil
        self.systemImage = nil
    }

    init(title: String, systemImage: String) {
        self.context = .specialMoments
        self.title = title
        self.systemImage = systemImage
    }

    var body: some View {
        VStack(alignment: .center, spacing: FrameMioSpacing.md) {
            Image(systemName: systemImage ?? context.icon)
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.accent)
                .frame(
                    width: FrameMioMetrics.contextIconContainer,
                    height: FrameMioMetrics.contextIconContainer
                )
                .background(FrameMioColors.accentMuted.opacity(0.78), in: Circle())

            Text(title ?? context.rawValue)
                .font(FrameMioTypography.callout.weight(.medium))
                .foregroundStyle(FrameMioColors.ink)
                .multilineTextAlignment(.center)
                .lineLimit(2)
        }
        .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.contextTileHeight, alignment: .center)
        .padding(FrameMioSpacing.md)
        .background(
            FrameMioColors.surface.opacity(0.84),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.subtle)
    }
}
