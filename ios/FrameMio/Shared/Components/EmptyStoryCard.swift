import SwiftUI

struct EmptyStoryCard: View {
    let title: String
    let message: String

    var body: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Image(systemName: "photo.stack")
                .font(.title2.weight(.medium))
                .foregroundStyle(FrameMioColors.accent)
                .frame(width: 56, height: 56)
                .background(FrameMioColors.accentMuted.opacity(0.7), in: Circle())

            Text(title)
                .font(FrameMioTypography.title)
                .foregroundStyle(FrameMioColors.ink)

            Text(message)
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(FrameMioSpacing.xl)
        .background(FrameMioColors.surface.opacity(0.82), in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.subtle)
    }
}

struct EmptyStoryCard_Previews: PreviewProvider {
    static var previews: some View {
        EmptyStoryCard(
            title: "Your first story is waiting",
            message: "Choose a few moments from your Camera Roll and FrameMio will bring them together."
        )
        .padding()
    }
}
