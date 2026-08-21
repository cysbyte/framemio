import SwiftUI

enum FrameMioTypography {
    static let display = Font.system(.largeTitle, design: .serif).weight(.semibold)
    static let editorialTitle = Font.system(.title2, design: .serif).weight(.semibold)
    static let title = Font.system(.title2, design: .default).weight(.semibold)
    static let headline = Font.system(.headline, design: .default).weight(.semibold)
    static let body = Font.system(.body, design: .default)
    static let callout = Font.system(.callout, design: .default)
    static let caption = Font.system(.caption, design: .default).weight(.semibold)
    static let footnote = Font.system(.footnote, design: .default)
    static let eyebrow = Font.system(.caption2, design: .default).weight(.semibold)
    static let memorySymbol = Font.system(.largeTitle, design: .default).weight(.regular)
}
