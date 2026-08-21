import SwiftUI

enum FrameMioAnimation {
    static let fastDuration = 0.18
    static let standardDuration = 0.28
    static let reducedMotionDuration = 0.01
    static let pressedScale = 0.985
    static let transitionScale = 0.99

    static let gentle = Animation.easeInOut(duration: standardDuration)
    static let responsive = Animation.easeOut(duration: fastDuration)

    static func gentle(reduceMotion: Bool) -> Animation {
        reduceMotion ? .linear(duration: reducedMotionDuration) : gentle
    }

    static func selection(reduceMotion: Bool) -> Animation {
        reduceMotion ? .linear(duration: reducedMotionDuration) : .spring(response: standardDuration, dampingFraction: 0.82)
    }

    static func transition(reduceMotion: Bool) -> AnyTransition {
        reduceMotion ? .opacity : .opacity.combined(with: .scale(scale: transitionScale))
    }
}
