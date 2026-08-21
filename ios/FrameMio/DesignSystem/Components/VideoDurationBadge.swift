import Foundation
import SwiftUI

struct VideoDurationBadge: View {
    let duration: TimeInterval

    var body: some View {
        Text(Self.format(duration))
            .font(FrameMioTypography.caption)
            .monospacedDigit()
            .foregroundStyle(FrameMioColors.onMedia)
            .padding(.horizontal, FrameMioSpacing.xs)
            .frame(minHeight: FrameMioMetrics.durationBadgeHeight)
            .background(FrameMioColors.mediaScrim, in: Capsule())
            .accessibilityLabel("Duration \(Self.format(duration))")
    }

    private static func format(_ duration: TimeInterval) -> String {
        let totalSeconds = max(0, Int(duration.rounded()))
        let minutes = totalSeconds / 60
        let seconds = totalSeconds % 60
        return minutes > 0 ? String(format: "%d:%02d", minutes, seconds) : String(format: "0:%02d", seconds)
    }
}

#Preview {
    VideoDurationBadge(duration: 93)
        .padding()
        .background(FrameMioColors.canvas)
}
