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
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            Image(systemName: systemImage ?? context.icon)
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.accent)
                .frame(
                    width: FrameMioMetrics.contextIconContainer,
                    height: FrameMioMetrics.contextIconContainer
                )
                .background(FrameMioColors.accentMuted, in: Circle())

            Text(title ?? context.rawValue)
                .font(FrameMioTypography.callout.weight(.medium))
                .foregroundStyle(FrameMioColors.ink)
                .lineLimit(2)
        }
        .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.contextTileHeight, alignment: .leading)
        .padding(FrameMioSpacing.md)
        .background(FrameMioColors.surface, in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
                .stroke(FrameMioColors.divider, lineWidth: FrameMioMetrics.borderWidth)
        }
    }
}
