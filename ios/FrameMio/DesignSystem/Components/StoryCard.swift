import SwiftUI

struct StoryCard: View {
    let title: String
    let subtitle: String?
    let image: Image?
    let isImageLoading: Bool
    let duration: TimeInterval?
    let action: (() -> Void)?

    init(
        title: String,
        subtitle: String? = nil,
        image: Image? = nil,
        isImageLoading: Bool = false,
        duration: TimeInterval? = nil,
        action: (() -> Void)? = nil
    ) {
        self.title = title
        self.subtitle = subtitle
        self.image = image
        self.isImageLoading = isImageLoading
        self.duration = duration
        self.action = action
    }

    var body: some View {
        Group {
            if let action {
                Button {
                    FrameMioHaptics.secondaryAction()
                    action()
                } label: {
                    cardContent
                }
                .buttonStyle(FrameMioPressButtonStyle())
            } else {
                cardContent
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel([title, subtitle].compactMap { $0 }.joined(separator: ", "))
    }

    private var cardContent: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            MediaThumbnail(
                image: image,
                isLoading: isImageLoading,
                duration: duration,
                aspectRatio: FrameMioMetrics.storyAspectRatio,
                placeholderSystemImage: "mountain.2.fill",
                accessibilityTitle: title
            )

            VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                Text(title)
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.ink)

                if let subtitle {
                    Text(subtitle)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }
            }
            .padding(.horizontal, FrameMioSpacing.xs)
            .padding(.bottom, FrameMioSpacing.xs)
        }
        .padding(FrameMioSpacing.xs)
        .background(
            FrameMioColors.surface.opacity(0.86),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.subtle)
    }
}

#Preview {
    StoryCard(title: "A slow Sunday", subtitle: "12 moments", duration: 58)
        .padding()
        .background(FrameMioColors.canvas)
}
