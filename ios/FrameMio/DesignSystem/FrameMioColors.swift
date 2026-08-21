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
    static let canvasLight = UIColor(frameMioHex: 0xF7F5F1)
    static let canvasDark = UIColor(frameMioHex: 0x151513)
    static let surfaceLight = UIColor(frameMioHex: 0xFFFEFB)
    static let surfaceDark = UIColor(frameMioHex: 0x201F1C)
    static let elevatedSurfaceLight = UIColor(frameMioHex: 0xFFFFFF)
    static let elevatedSurfaceDark = UIColor(frameMioHex: 0x292824)
    static let inkLight = UIColor(frameMioHex: 0x1D1D1B)
    static let inkDark = UIColor(frameMioHex: 0xF6F2EA)
    static let secondaryInkLight = UIColor(frameMioHex: 0x6E6A63)
    static let secondaryInkDark = UIColor(frameMioHex: 0xBDB7AC)
    static let tertiaryInkLight = UIColor(frameMioHex: 0x76716A)
    static let tertiaryInkDark = UIColor(frameMioHex: 0xA29B90)
    static let accentLight = UIColor(frameMioHex: 0x8F4F39)
    static let accentDark = UIColor(frameMioHex: 0xDEA081)
    static let accentMutedLight = UIColor(frameMioHex: 0xF0DDD2)
    static let accentMutedDark = UIColor(frameMioHex: 0x4A3026)
    static let dividerLight = UIColor(frameMioHex: 0xE5E0D8)
    static let dividerDark = UIColor(frameMioHex: 0x3A3732)
    static let selectionLight = UIColor(frameMioHex: 0xB66A4D)
    static let selectionDark = UIColor(frameMioHex: 0xE1A183)
    static let inverseInkLight = UIColor(frameMioHex: 0xFFFDF8)
    static let inverseInkDark = UIColor(frameMioHex: 0x1D1D1B)
    static let mediaPlaceholderLight = UIColor(frameMioHex: 0xD8C7B8)
    static let mediaPlaceholderDark = UIColor(frameMioHex: 0x453D36)
    static let memorySkyLight = UIColor(frameMioHex: 0xAEBAC0)
    static let memorySkyDark = UIColor(frameMioHex: 0x344047)
    static let memoryLightLight = UIColor(frameMioHex: 0xE7C8A8)
    static let memoryLightDark = UIColor(frameMioHex: 0x8A654B)
    static let memoryEarthLight = UIColor(frameMioHex: 0x796558)
    static let memoryEarthDark = UIColor(frameMioHex: 0x332B27)
    static let shadowLight = UIColor(frameMioHex: 0x16130F, alpha: 0.10)
    static let shadowDark = UIColor(frameMioHex: 0x000000, alpha: 0.28)
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
