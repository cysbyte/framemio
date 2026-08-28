import SwiftUI

struct ProfileView: View {
    var body: some View {
        NavigationStack {
            EmptyStoryCard(
                title: "Made for your life",
                message: "A simple home for preferences, privacy, and the memories you choose to keep."
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
            .navigationTitle("You")
            .navigationBarTitleDisplayMode(.large)
        }
    }
}

struct ProfileView_Previews: PreviewProvider {
    static var previews: some View {
        ProfileView()
    }
}
