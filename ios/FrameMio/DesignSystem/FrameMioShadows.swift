import SwiftUI

enum FrameMioElevation {
    case none
    case subtle
    case floating

    var color: Color {
        switch self {
        case .none:
            .clear
        case .subtle, .floating:
            FrameMioColors.shadow
        }
    }

    var radius: CGFloat {
        switch self {
        case .none:
            0
        case .subtle:
            6
        case .floating:
            14
        }
    }

    var y: CGFloat {
        switch self {
        case .none:
            0
        case .subtle:
            2
        case .floating:
            6
        }
    }
}

extension View {
    func frameMioElevation(_ elevation: FrameMioElevation) -> some View {
        shadow(color: elevation.color, radius: elevation.radius, y: elevation.y)
    }
}
