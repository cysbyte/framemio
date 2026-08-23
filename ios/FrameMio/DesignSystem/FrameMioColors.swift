import SwiftUI
import UIKit

enum FrameMioColors {
    static let canvas = Color(frameMioLight: FrameMioPalette.canvasLight, dark: FrameMioPalette.canvasDark)
    static let surface = Color(frameMioLight: FrameMioPalette.surfaceLight, dark: FrameMioPalette.surfaceDark)
    static let elevatedSurface = Color(frameMioLight: FrameMioPalette.elevatedSurfaceLight, dark: FrameMioPalette.elevatedSurfaceDark)
    static let ink = Color(frameMioLight: FrameMioPalette.inkLight, dark: FrameMioPalette.inkDark)
    static let secondaryInk = Color(frameMioLight: FrameMioPalette.secondaryInkLight, dark: FrameMioPalette.secondaryInkDark)
    static let tertiaryInk = Color(frameMioLight: FrameMioPalette.tertiaryInkLight, dark: FrameMioPalette.tertiaryInkDark)
    static let accent = Color(frameMioLight: FrameMioPalette.accentLight, dark: FrameMioPalette.accentDark)
    static let accentMuted = Color(frameMioLight: FrameMioPalette.accentMutedLight, dark: FrameMioPalette.accentMutedDark)
    static let divider = Color(frameMioLight: FrameMioPalette.dividerLight, dark: FrameMioPalette.dividerDark)
    static let selection = Color(frameMioLight: FrameMioPalette.selectionLight, dark: FrameMioPalette.selectionDark)
    static let inverseInk = Color(frameMioLight: FrameMioPalette.inverseInkLight, dark: FrameMioPalette.inverseInkDark)
    static let onMedia = Color.white
    static let mediaPlaceholder = Color(frameMioLight: FrameMioPalette.mediaPlaceholderLight, dark: FrameMioPalette.mediaPlaceholderDark)
    static let memorySky = Color(frameMioLight: FrameMioPalette.memorySkyLight, dark: FrameMioPalette.memorySkyDark)
    static let memoryLight = Color(frameMioLight: FrameMioPalette.memoryLightLight, dark: FrameMioPalette.memoryLightDark)
    static let memoryEarth = Color(frameMioLight: FrameMioPalette.memoryEarthLight, dark: FrameMioPalette.memoryEarthDark)
    static let mediaScrim = Color.black.opacity(FrameMioOpacity.mediaScrim)
    static let selectionIdle = Color.black.opacity(FrameMioOpacity.selectionIdle)
    static let shadow = Color(frameMioLight: FrameMioPalette.shadowLight, dark: FrameMioPalette.shadowDark)
}

private enum FrameMioPalette {
    static let canvasLight = UIColor(frameMioHex: 0xFBF4EA)
    static let canvasDark = UIColor(frameMioHex: 0x181311)
    static let surfaceLight = UIColor(frameMioHex: 0xFFFAF3)
    static let surfaceDark = UIColor(frameMioHex: 0x241F1C)
    static let elevatedSurfaceLight = UIColor(frameMioHex: 0xFFFFFF)
    static let elevatedSurfaceDark = UIColor(frameMioHex: 0x302720)
    static let inkLight = UIColor(frameMioHex: 0x241F1C)
    static let inkDark = UIColor(frameMioHex: 0xFFF8EF)
    static let secondaryInkLight = UIColor(frameMioHex: 0x625852)
    static let secondaryInkDark = UIColor(frameMioHex: 0xD8CABD)
    static let tertiaryInkLight = UIColor(frameMioHex: 0x88786F)
    static let tertiaryInkDark = UIColor(frameMioHex: 0xBFAFA2)
    static let accentLight = UIColor(frameMioHex: 0xE86F61)
    static let accentDark = UIColor(frameMioHex: 0xF2A49A)
    static let accentMutedLight = UIColor(frameMioHex: 0xF7D7C8)
    static let accentMutedDark = UIColor(frameMioHex: 0x5C342B)
    static let dividerLight = UIColor(frameMioHex: 0xE8D9CC)
    static let dividerDark = UIColor(frameMioHex: 0x4C3A32)
    static let selectionLight = UIColor(frameMioHex: 0xB76E56)
    static let selectionDark = UIColor(frameMioHex: 0xF2A49A)
    static let inverseInkLight = UIColor(frameMioHex: 0xFFF8EF)
    static let inverseInkDark = UIColor(frameMioHex: 0x241F1C)
    static let mediaPlaceholderLight = UIColor(frameMioHex: 0xF2B8AA)
    static let mediaPlaceholderDark = UIColor(frameMioHex: 0x5E423A)
    static let memorySkyLight = UIColor(frameMioHex: 0xF6EADB)
    static let memorySkyDark = UIColor(frameMioHex: 0x3A302B)
    static let memoryLightLight = UIColor(frameMioHex: 0xF2A49A)
    static let memoryLightDark = UIColor(frameMioHex: 0xB76E56)
    static let memoryEarthLight = UIColor(frameMioHex: 0x7A4E3D)
    static let memoryEarthDark = UIColor(frameMioHex: 0x332720)
    static let shadowLight = UIColor(frameMioHex: 0x5B3726, alpha: 0.14)
    static let shadowDark = UIColor(frameMioHex: 0x000000, alpha: 0.32)
}

private extension UIColor {
    convenience init(frameMioHex: UInt32, alpha: CGFloat = 1) {
        self.init(
            red: CGFloat((frameMioHex >> 16) & 0xFF) / 255,
            green: CGFloat((frameMioHex >> 8) & 0xFF) / 255,
            blue: CGFloat(frameMioHex & 0xFF) / 255,
            alpha: alpha
        )
    }
}

private extension Color {
    init(frameMioLight light: UIColor, dark: UIColor) {
        self.init(uiColor: UIColor { traits in
            traits.userInterfaceStyle == .dark ? dark : light
        })
    }
}
