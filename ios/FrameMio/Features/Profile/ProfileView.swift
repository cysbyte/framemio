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
            .background(FrameMioColors.canvas)
            .navigationTitle("You")
            .navigationBarTitleDisplayMode(.large)
        }
    }
}

#Preview {
    ProfileView()
}
