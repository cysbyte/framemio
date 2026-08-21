import SwiftUI

struct EmptyStoryCard: View {
    let title: String
    let message: String

    var body: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Image(systemName: "photo.stack")
                .font(.title2.weight(.medium))
                .foregroundStyle(FrameMioColors.accent)

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
        .background(FrameMioColors.surface, in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))
    }
}

#Preview {
    EmptyStoryCard(
        title: "Your first story is waiting",
        message: "Choose a few moments from your Camera Roll and FrameMio will bring them together."
    )
    .padding()
}
