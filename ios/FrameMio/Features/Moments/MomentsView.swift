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
            .background {
                LinearGradient(
                    colors: [FrameMioColors.canvas, FrameMioColors.accentMuted.opacity(0.26)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                )
                .ignoresSafeArea()
            }
            .navigationTitle("Moments")
            .navigationBarTitleDisplayMode(.large)
        }
    }
}

struct MomentsView_Previews: PreviewProvider {
    static var previews: some View {
        MomentsView()
    }
}
