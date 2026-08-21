import SwiftUI

struct MomentsView: View {
    var body: some View {
        NavigationStack {
            EmptyStoryCard(
                title: "Select moments",
                message: "Your Camera Roll will become the starting point for every story."
            )
            .padding(FrameMioSpacing.lg)
            .frame(maxHeight: .infinity, alignment: .top)
            .background(FrameMioColors.canvas)
            .navigationTitle("Moments")
            .navigationBarTitleDisplayMode(.large)
        }
    }
}

#Preview {
    MomentsView()
}
