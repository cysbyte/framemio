import SwiftUI

struct Tag: View {
    let title: String
    let systemImage: String?

    init(_ title: String, systemImage: String? = nil) {
        self.title = title
        self.systemImage = systemImage
    }

    var body: some View {
        HStack(spacing: FrameMioSpacing.xxs) {
            if let systemImage {
                Image(systemName: systemImage)
                    .symbolRenderingMode(.monochrome)
                    .accessibilityHidden(true)
            }

            Text(title)
                .font(FrameMioTypography.caption)
        }
        .foregroundStyle(FrameMioColors.accent)
        .padding(.horizontal, FrameMioSpacing.sm)
        .frame(minHeight: FrameMioMetrics.tagHeight)
        .background(FrameMioColors.accentMuted, in: Capsule())
        .accessibilityLabel(title)
    }
}

#Preview {
    Tag("Weekend", systemImage: "calendar")
        .padding()
        .background(FrameMioColors.canvas)
}
