import SwiftUI

struct FrameMioProgressView: View {
    let value: Double?
    let title: String?

    init(value: Double? = nil, title: String? = nil) {
        self.value = value
        self.title = title
    }

    var body: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
            if let title {
                HStack {
                    Text(title)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)

                    Spacer()

                    if let value {
                        Text(value, format: .percent.precision(.fractionLength(0)))
                            .font(FrameMioTypography.caption)
                            .foregroundStyle(FrameMioColors.secondaryInk)
                    }
                }
            }

            ProgressView(value: value)
                .tint(FrameMioColors.accent)
                .accessibilityLabel(title ?? "Progress")
                .accessibilityValue(value.map { $0.formatted(.percent.precision(.fractionLength(0))) } ?? "In progress")
        }
    }
}

struct FrameMioProgressView_Previews: PreviewProvider {
    static var previews: some View {
        FrameMioProgressView(value: 0.62, title: "Creating your story")
            .padding()
            .background(FrameMioColors.canvas)
    }
}
