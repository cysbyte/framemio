import SwiftUI

struct HomeView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ObservedObject private var viewModel: HomeViewModel
    let onCreateStory: (MomentContext) -> Void
    let onOpenStory: (Story) -> Void
    let onOpenSettings: () -> Void

    init(
        viewModel: HomeViewModel,
        onCreateStory: @escaping (MomentContext) -> Void,
        onOpenStory: @escaping (Story) -> Void,
        onOpenSettings: @escaping () -> Void
    ) {
        _viewModel = ObservedObject(wrappedValue: viewModel)
        self.onCreateStory = onCreateStory
        self.onOpenStory = onOpenStory
        self.onOpenSettings = onOpenSettings
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.xxl) {
                hero
                if !viewModel.stories.isEmpty {
                    createAction
                        .transition(FrameMioAnimation.transition(reduceMotion: reduceMotion))
                }
                shortcuts

                if viewModel.isLoading && viewModel.stories.isEmpty {
                    LoadingState(
                        title: "Finding your moments",
                        message: "Looking for the memories that belong together."
                    )
                    .frame(maxWidth: .infinity)
                    .transition(FrameMioAnimation.transition(reduceMotion: reduceMotion))
                } else if viewModel.stories.isEmpty {
                    EmptyState(
                        systemImage: "photo.stack",
                        title: "Your first story is waiting",
                        message: "Choose a few moments and let FrameMio turn them into something worth revisiting.",
                        actionTitle: "Create Story",
                        action: { onCreateStory(.specialMoments) }
                    )
                    .frame(maxWidth: .infinity)
                    .transition(FrameMioAnimation.transition(reduceMotion: reduceMotion))
                } else {
                    recentStories
                        .transition(FrameMioAnimation.transition(reduceMotion: reduceMotion))
                }
            }
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.xs)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background(FrameMioColors.canvas)
        .navigationTitle("Your moments")
        .navigationBarTitleDisplayMode(.large)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button(action: onOpenSettings) {
                    Image(systemName: "gearshape")
                        .accessibilityLabel("Settings")
                }
            }
        }
        .task { viewModel.load() }
        .refreshable { viewModel.load() }
        .animation(FrameMioAnimation.gentle(reduceMotion: reduceMotion), value: viewModel.isLoading)
        .animation(FrameMioAnimation.gentle(reduceMotion: reduceMotion), value: viewModel.stories)
        .alert("Couldn’t load stories", isPresented: errorBinding) {
            Button("Try Again") { viewModel.load() }
            Button("Dismiss", role: .cancel) { viewModel.errorMessage = nil }
        } message: {
            Text(viewModel.errorMessage ?? "")
        }
    }

    private var hero: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Text("Turn the moments you’ve captured into a story.")
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
                .fixedSize(horizontal: false, vertical: true)
        }
        .accessibilityElement(children: .combine)
    }

    private var createAction: some View {
        PrimaryButton(title: "Create Story", systemImage: "sparkles") {
            onCreateStory(.specialMoments)
        }
        .accessibilityHint("Choose photos and videos to create a memory story")
    }

    private var shortcuts: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Start with a moment")

            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: FrameMioSpacing.sm) {
                    shortcut(title: "Today", icon: MomentContext.today.icon, context: .today)
                    shortcut(title: "Weekend", icon: MomentContext.weekend.icon, context: .weekend)
                    shortcut(title: "Trip", icon: MomentContext.trip.icon, context: .trip)
                    shortcut(title: "Select Photos", icon: "checkmark.circle", context: .specialMoments)
                }
            }
        }
    }

    private func shortcut(title: String, icon: String, context: MomentContext) -> some View {
        Button {
            FrameMioHaptics.secondaryAction()
            onCreateStory(context)
        } label: {
            ContextTile(title: title, systemImage: icon)
        }
        .frame(width: FrameMioMetrics.homeShortcutWidth)
        .buttonStyle(FrameMioPressButtonStyle())
        .accessibilityLabel(title)
    }

    private var recentStories: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Recent stories", subtitle: "Made from your moments")

            ScrollView(.horizontal, showsIndicators: false) {
                HStack(alignment: .top, spacing: FrameMioSpacing.md) {
                    ForEach(viewModel.stories) { story in
                        StoryCard(
                            title: story.title,
                            subtitle: storySubtitle(for: story),
                            image: viewModel.coverImage(for: story),
                            duration: story.duration,
                            action: { onOpenStory(story) }
                        )
                        .frame(width: FrameMioMetrics.homeStoryCardWidth)
                    }
                }
            }
            .scrollClipDisabled()
        }
    }

    private func storySubtitle(for story: Story) -> String {
        let date = story.createdAt.formatted(.dateTime.month(.abbreviated).day())
        let moments = story.assetIDs.count == 1 ? "1 moment" : "\(story.assetIDs.count) moments"
        return "\(date) · \(moments)"
    }

    private var errorBinding: Binding<Bool> {
        Binding(
            get: { viewModel.errorMessage != nil },
            set: { if !$0 { viewModel.errorMessage = nil } }
        )
    }
}
