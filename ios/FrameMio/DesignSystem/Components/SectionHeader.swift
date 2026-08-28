import SwiftUI

struct SectionHeader: View {
    let title: String
    let subtitle: String?
    let actionTitle: String?
    let action: (() -> Void)?

    init(
        title: String,
        subtitle: String? = nil,
        actionTitle: String? = nil,
        action: (() -> Void)? = nil
    ) {
        self.title = title
        self.subtitle = subtitle
        self.actionTitle = actionTitle
        self.action = action
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: FrameMioSpacing.md) {
            VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                Text(title)
                    .font(FrameMioTypography.title)
                    .foregroundStyle(FrameMioColors.ink)

                if let subtitle {
                    Text(subtitle)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }
            }

            Spacer(minLength: FrameMioSpacing.sm)

            if let action, let actionTitle {
                Button(action: action) {
                    Text(actionTitle)
                        .font(FrameMioTypography.caption)
                        .foregroundStyle(FrameMioColors.accent)
                        .frame(minWidth: FrameMioMetrics.minimumTouchTarget, minHeight: FrameMioMetrics.minimumTouchTarget)
                }
                .buttonStyle(FrameMioPressButtonStyle())
                .accessibilityLabel(actionTitle)
            }
        }
    }
}

struct SectionHeader_Previews: PreviewProvider {
    static var previews: some View {
        SectionHeader(title: "Your stories", subtitle: "Made from your moments", actionTitle: "See all") {}
            .padding()
            .background(FrameMioColors.canvas)
    }
}
