import SwiftUI

/// A filmstrip with a light pass travelling along it, for the waiting states
/// where a story is still being assembled. The motion is decorative only: the
/// surrounding progress view remains the accessible source of truth, and this
/// holds still when Reduce Motion is on.
struct StoryWaitingAnimation: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// Forces the still presentation. Only for previews — at runtime the
    /// environment decides.
    private let forcesStillPresentation: Bool

    init(forcesStillPresentation: Bool = false) {
        self.forcesStillPresentation = forcesStillPresentation
    }

    private enum Metrics {
        static let frameCount = 5
        static let frameWidth: CGFloat = 34
        static let frameHeight: CGFloat = 44
        static let frameSpacing: CGFloat = 6
        static let stripPadding: CGFloat = 12
        static let sprocketSize: CGFloat = 3
        static let sprocketWidth: CGFloat = 7
        static let sprocketCount = 9
        static let sprocketInset: CGFloat = 3
        static let cycleDuration: CGFloat = 2.6
        /// How far past the last frame the light travels before looping, so the
        /// strip goes fully dim between passes instead of snapping back.
        static let travelOvershoot: CGFloat = 0.9
        static let highlightSpread: CGFloat = 1.1
        static let restingOpacity: CGFloat = 0.42
        static let liftHeight: CGFloat = 5
        static let maximumScale: CGFloat = 0.14
    }

    var body: some View {
        Group {
            if reduceMotion || forcesStillPresentation {
                // Light the middle frame, so the still version reads as
                // deliberate composition rather than a paused animation.
                strip(highlightPosition: CGFloat(Metrics.frameCount - 1) / 2)
            } else {
                TimelineView(.animation) { context in
                    strip(highlightPosition: highlightPosition(at: context.date))
                }
            }
        }
        .frame(maxWidth: .infinity)
        .accessibilityHidden(true)
    }

    private func strip(highlightPosition: CGFloat) -> some View {
        HStack(spacing: Metrics.frameSpacing) {
            ForEach(0 ..< Metrics.frameCount, id: \.self) { index in
                frame(warmth: warmth(forFrameAt: index, highlightPosition: highlightPosition))
            }
        }
        // Fixed width, so the strip hugs its frames instead of stretching to the
        // container and leaving the cells floating in dead space.
        .padding(.horizontal, Metrics.stripPadding)
        .padding(.vertical, Metrics.stripPadding)
        .background(stripBackground)
        .overlay(alignment: .top) { sprockets }
        .overlay(alignment: .bottom) { sprockets }
        .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous))
        .fixedSize()
    }

    /// One cell of the strip. `warmth` runs 0 (waiting) to 1 (lit by the pass).
    private func frame(warmth: CGFloat) -> some View {
        RoundedRectangle(cornerRadius: FrameMioRadius.small, style: .continuous)
            .fill(
                LinearGradient(
                    colors: [
                        FrameMioColors.memoryLight.opacity(0.55 + 0.45 * warmth),
                        FrameMioColors.accent.opacity(0.5 + 0.5 * warmth)
                    ],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                )
            )
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.small, style: .continuous)
                    .stroke(FrameMioColors.onMedia.opacity(0.5 * warmth), lineWidth: FrameMioMetrics.borderWidth)
            }
            .frame(width: Metrics.frameWidth, height: Metrics.frameHeight)
            .opacity(Metrics.restingOpacity + (1 - Metrics.restingOpacity) * warmth)
            .scaleEffect(1 + Metrics.maximumScale * warmth)
            .offset(y: -Metrics.liftHeight * warmth)
            .shadow(
                color: FrameMioColors.accent.opacity(0.42 * warmth),
                radius: 9 * warmth,
                y: 3 * warmth
            )
    }

    private var stripBackground: some View {
        RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous)
            .fill(FrameMioColors.memorySky.opacity(0.85))
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous)
                    .stroke(FrameMioColors.divider.opacity(0.55), lineWidth: FrameMioMetrics.borderWidth)
            }
    }

    /// Sprocket holes along the strip edges. Static by design — the travelling
    /// light is the only thing that should draw the eye.
    private var sprockets: some View {
        HStack(spacing: 0) {
            ForEach(0 ..< Metrics.sprocketCount, id: \.self) { _ in
                Capsule(style: .continuous)
                    .fill(FrameMioColors.memoryEarth.opacity(0.22))
                    .frame(width: Metrics.sprocketWidth, height: Metrics.sprocketSize)
                    .frame(maxWidth: .infinity)
            }
        }
        .padding(.horizontal, Metrics.stripPadding * 0.4)
        .padding(.vertical, Metrics.sprocketInset)
    }

    /// Where the light sits, in frame indices. Runs from just before the first
    /// frame to `travelOvershoot` past the last one, then loops.
    private func highlightPosition(at date: Date) -> CGFloat {
        let span = CGFloat(Metrics.frameCount - 1) + Metrics.travelOvershoot * 2
        let elapsed = CGFloat(date.timeIntervalSinceReferenceDate)
        let phase = (elapsed / Metrics.cycleDuration).truncatingRemainder(dividingBy: 1)
        return phase * span - Metrics.travelOvershoot
    }

    /// Distance falloff, so the light bleeds into neighbouring frames instead of
    /// jumping cell to cell.
    private func warmth(forFrameAt index: Int, highlightPosition: CGFloat) -> CGFloat {
        let distance = abs(CGFloat(index) - highlightPosition)
        let normalized = max(0, 1 - distance / Metrics.highlightSpread)
        // Smoothstep: eases both ends of the falloff rather than ramping linearly.
        return normalized * normalized * (3 - 2 * normalized)
    }
}

struct StoryWaitingAnimation_Previews: PreviewProvider {
    static var previews: some View {
        VStack(spacing: FrameMioSpacing.lg) {
            StoryWaitingAnimation()
            FrameMioProgressView(value: 0.58, title: "Finding meaningful moments")
        }
        .padding(FrameMioSpacing.xl)
        .background(FrameMioColors.canvas)
        .previewDisplayName("Waiting")

        VStack(spacing: FrameMioSpacing.lg) {
            StoryWaitingAnimation(forcesStillPresentation: true)
            FrameMioProgressView(value: 0.58, title: "Finding meaningful moments")
        }
        .padding(FrameMioSpacing.xl)
        .background(FrameMioColors.canvas)
        .previewDisplayName("Reduce Motion")
    }
}
