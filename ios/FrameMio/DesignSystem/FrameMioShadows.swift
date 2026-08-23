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
            12
        case .floating:
            24
        }
    }

    var y: CGFloat {
        switch self {
        case .none:
            0
        case .subtle:
            5
        case .floating:
            12
        }
    }
}

extension View {
    func frameMioElevation(_ elevation: FrameMioElevation) -> some View {
        shadow(color: elevation.color, radius: elevation.radius, y: elevation.y)
    }
}
