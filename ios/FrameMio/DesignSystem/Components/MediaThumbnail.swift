import SwiftUI

struct MediaThumbnail: View {
    let image: Image?
    let duration: TimeInterval?
    let isLivePhoto: Bool
    let isSelected: Bool
    let aspectRatio: CGFloat
    let placeholderSystemImage: String
    let accessibilityTitle: String
    let action: (() -> Void)?

    init(
        image: Image? = nil,
        duration: TimeInterval? = nil,
        isSelected: Bool = false,
        aspectRatio: CGFloat = FrameMioMetrics.thumbnailAspectRatio,
        placeholderSystemImage: String = "photo.fill",
        accessibilityTitle: String = "Media",
        action: (() -> Void)? = nil
    ) {
        self.image = image
        self.duration = duration
        self.isLivePhoto = false
        self.isSelected = isSelected
        self.aspectRatio = aspectRatio
        self.placeholderSystemImage = placeholderSystemImage
        self.accessibilityTitle = accessibilityTitle
        self.action = action
    }

    init(
        image: Image? = nil,
        duration: TimeInterval? = nil,
        isLivePhoto: Bool,
        isSelected: Bool = false,
        aspectRatio: CGFloat = FrameMioMetrics.thumbnailAspectRatio,
        placeholderSystemImage: String = "photo.fill",
        accessibilityTitle: String = "Media",
        action: (() -> Void)? = nil
    ) {
        self.image = image
        self.duration = duration
        self.isLivePhoto = isLivePhoto
        self.isSelected = isSelected
        self.aspectRatio = aspectRatio
        self.placeholderSystemImage = placeholderSystemImage
        self.accessibilityTitle = accessibilityTitle
        self.action = action
    }

    var body: some View {
        Group {
            if let action {
                Button(action: action) {
                    thumbnailContent
                }
                .buttonStyle(FrameMioPressButtonStyle())
            } else {
                thumbnailContent
            }
        }
        .accessibilityLabel(accessibilityTitle)
        .accessibilityValue(accessibilityValue)
    }

    private var thumbnailContent: some View {
        ZStack(alignment: .topTrailing) {
            RoundedRectangle(cornerRadius: FrameMioRadius.media, style: .continuous)
                .fill(
                    LinearGradient(
                        colors: [FrameMioColors.accentMuted, FrameMioColors.memoryLight, FrameMioColors.memoryEarth],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    )
                )
                .aspectRatio(aspectRatio, contentMode: .fit)
                .overlay {
                    if let image {
                        image
                            .resizable()
                            .scaledToFill()
                    } else {
                        Image(systemName: placeholderSystemImage)
                            .font(.title2.weight(.medium))
                            .symbolRenderingMode(.monochrome)
                            .foregroundStyle(FrameMioColors.inverseInk.opacity(FrameMioOpacity.artworkSymbol))
                            .accessibilityHidden(true)
                    }
                }
                .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.media, style: .continuous))

            if duration != nil {
                LinearGradient(
                    colors: [.clear, FrameMioColors.mediaScrim],
                    startPoint: .center,
                    endPoint: .bottom
                )
                .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.media, style: .continuous))
                .accessibilityHidden(true)
            }

            if isSelected {
                SelectionIndicator(isSelected: true)
                    .padding(FrameMioSpacing.sm)
            }

            if isLivePhoto {
                Image(systemName: "livephoto")
                    .font(FrameMioTypography.caption)
                    .foregroundStyle(FrameMioColors.onMedia)
                    .frame(
                        width: FrameMioMetrics.durationBadgeHeight,
                        height: FrameMioMetrics.durationBadgeHeight
                    )
                    .background(FrameMioColors.mediaScrim, in: Capsule())
                    .padding(FrameMioSpacing.sm)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomLeading)
                    .accessibilityLabel("Live Photo")
            }

            if let duration {
                VideoDurationBadge(duration: duration)
                    .padding(FrameMioSpacing.sm)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomTrailing)
            }
        }
        .frame(maxWidth: .infinity)
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.media, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .contentShape(RoundedRectangle(cornerRadius: FrameMioRadius.media, style: .continuous))
    }

    private var accessibilityValue: String {
        var values = [isSelected ? "Selected" : "Not selected"]
        if let duration {
            values.append("Video")
            values.append("\(Int(duration.rounded())) seconds")
        } else if isLivePhoto {
            values.append("Live Photo")
        } else {
            values.append("Photo")
        }
        return values.joined(separator: ", ")
    }
}

#Preview {
    MediaThumbnail(duration: 42, isSelected: true)
        .padding()
        .background(FrameMioColors.canvas)
}
