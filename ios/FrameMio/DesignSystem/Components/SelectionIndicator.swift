import SwiftUI

struct SelectionIndicator: View {
    let isSelected: Bool
    var label = "Select"

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.colorSchemeContrast) private var colorSchemeContrast

    var body: some View {
        ZStack {
            Circle()
                .fill(isSelected ? FrameMioColors.selection : FrameMioColors.selectionIdle)

            Circle()
                .stroke(
                    FrameMioColors.onMedia,
                    lineWidth: colorSchemeContrast == .increased
                        ? FrameMioMetrics.increasedSelectionBorderWidth
                        : FrameMioMetrics.selectionBorderWidth
                )

            if isSelected {
                Image(systemName: "checkmark")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(FrameMioColors.onMedia)
                    .transition(FrameMioAnimation.transition(reduceMotion: reduceMotion))
            }
        }
        .frame(width: FrameMioMetrics.selectionIndicator, height: FrameMioMetrics.selectionIndicator)
        .frame(minWidth: FrameMioMetrics.minimumTouchTarget, minHeight: FrameMioMetrics.minimumTouchTarget)
        .animation(FrameMioAnimation.selection(reduceMotion: reduceMotion), value: isSelected)
        .accessibilityLabel(isSelected ? "Selected" : label)
    }
}

struct SelectionIndicator_Previews: PreviewProvider {
    static var previews: some View {
        HStack {
            SelectionIndicator(isSelected: false)
            SelectionIndicator(isSelected: true)
        }
        .padding()
        .background(FrameMioColors.canvas)
    }
}
