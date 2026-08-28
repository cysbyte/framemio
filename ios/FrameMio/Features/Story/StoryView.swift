import SwiftUI

struct StoryView: View {
    var body: some View {
        NavigationStack {
            EmptyStoryCard(
                title: "Stories will live here",
                message: "Once FrameMio understands a set of moments, your finished memory videos will appear in this quiet gallery."
            )
            .padding(FrameMioSpacing.lg)
            .frame(maxHeight: .infinity, alignment: .top)
            .background(FrameMioColors.canvas)
            .navigationTitle("Stories")
            .navigationBarTitleDisplayMode(.large)
        }
    }
}

struct StoryView_Previews: PreviewProvider {
    static var previews: some View {
        StoryView()
    }
}
