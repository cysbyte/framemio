import SwiftUI

struct StoryCard: View {
    let title: String
    let subtitle: String?
    let image: Image?
    let isImageLoading: Bool
    let duration: TimeInterval?
    let projectType: VideoProjectType
    let lastModified: Date?
    let status: StoryStatus?
    let action: (() -> Void)?

    init(
        title: String,
        subtitle: String? = nil,
        image: Image? = nil,
        isImageLoading: Bool = false,
        duration: TimeInterval? = nil,
        projectType: VideoProjectType = .story,
        lastModified: Date? = nil,
        status: StoryStatus? = nil,
        action: (() -> Void)? = nil
    ) {
        self.title = title
        self.subtitle = subtitle
        self.image = image
        self.isImageLoading = isImageLoading
        self.duration = duration
        self.projectType = projectType
        self.lastModified = lastModified
        self.status = status
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
        .accessibilityLabel([projectType.title, title, subtitle, status?.projectListLabel].compactMap { $0 }.joined(separator: ", "))
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
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: FrameMioSpacing.xs) {
                        typeTag
                        statusTag
                    }

                    VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
                        typeTag
                        statusTag
                    }
                }

                Text(title)
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.ink)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)

                if let subtitle {
                    Text(subtitle)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .lineLimit(2)
                        .fixedSize(horizontal: false, vertical: true)
                }

                if let lastModified {
                    Text("Modified \(lastModified.formatted(.relative(presentation: .named)))")
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.tertiaryInk)
                        .lineLimit(1)
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

    private var typeTag: some View {
        Tag(projectType.title, systemImage: projectType.icon)
    }

    @ViewBuilder
    private var statusTag: some View {
        if let status, status.showsInProjectList {
            Tag(status.projectListLabel, systemImage: status.projectListIcon)
        }
    }
}

extension StoryStatus {
    var showsInProjectList: Bool {
        self != .ready
    }

    var projectListLabel: String {
        switch self {
        case .queued:
            "Queued"
        case .analyzing:
            "Processing"
        case .rendering:
            "Rendering"
        case .ready:
            "Ready"
        case .failed:
            "Needs attention"
        }
    }

    var projectListIcon: String {
        switch self {
        case .queued:
            "clock"
        case .analyzing:
            "sparkles"
        case .rendering:
            "film"
        case .ready:
            "checkmark"
        case .failed:
            "exclamationmark.triangle"
        }
    }
}

struct StoryCard_Previews: PreviewProvider {
    static var previews: some View {
        StoryCard(title: "A slow Sunday", subtitle: "12 moments", duration: 58)
            .padding()
            .background(FrameMioColors.canvas)
    }
}
