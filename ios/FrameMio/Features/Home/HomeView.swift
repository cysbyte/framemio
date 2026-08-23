import AVFoundation
import SwiftUI
import UIKit

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
        .background {
            LinearGradient(
                colors: [FrameMioColors.canvas, FrameMioColors.accentMuted.opacity(0.34)],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )
            .ignoresSafeArea()
        }
        .navigationTitle("FrameMio")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button(action: onOpenSettings) {
                    Image(systemName: "gearshape")
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                        .frame(width: FrameMioMetrics.minimumTouchTarget, height: FrameMioMetrics.minimumTouchTarget)
                        .background(FrameMioColors.surface.opacity(0.78), in: Circle())
                        .accessibilityLabel("Settings")
                }
                .buttonStyle(FrameMioPressButtonStyle())
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
        VStack(alignment: .leading, spacing: FrameMioSpacing.xl) {
            HStack(alignment: .center, spacing: FrameMioSpacing.sm) {
                ZStack {
                    RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous)
                        .fill(FrameMioColors.accentMuted)
                    RoundedRectangle(cornerRadius: FrameMioRadius.small, style: .continuous)
                        .fill(FrameMioColors.ink)
                        .frame(width: 30, height: 30)
                    Image(systemName: "play.fill")
                        .font(.caption.weight(.bold))
                        .foregroundStyle(FrameMioColors.inverseInk)
                }
                .frame(width: 48, height: 48)
                .accessibilityHidden(true)

                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text("FrameMio")
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                    Text("Memory stories")
                        .font(FrameMioTypography.eyebrow)
                        .foregroundStyle(FrameMioColors.accent)
                        .textCase(.uppercase)
                }
            }

            VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                Text("Turn your moments into cinematic memory stories.")
                    .font(FrameMioTypography.display)
                    .foregroundStyle(FrameMioColors.ink)
                    .fixedSize(horizontal: false, vertical: true)

                Text("Select meaningful photos and videos from real life, then let FrameMio shape them into a warm story-style film.")
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                    .fixedSize(horizontal: false, vertical: true)
            }

            heroPreview
        }
        .padding(FrameMioSpacing.xl)
        .background(
            LinearGradient(
                colors: [FrameMioColors.surface.opacity(0.94), FrameMioColors.accentMuted.opacity(0.58)],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            ),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.58), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.floating)
        .accessibilityElement(children: .combine)
    }

    private var heroPreview: some View {
        ZStack(alignment: .bottomLeading) {
            LoopingVideoPreview(resourceName: "HomeHeroPreview", isPlaying: !reduceMotion)
                .frame(height: 188)
                .background(
                    LinearGradient(
                        colors: [FrameMioColors.memoryLight, FrameMioColors.accent, FrameMioColors.memoryEarth],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    )
                )
                .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))

            LinearGradient(
                colors: [FrameMioColors.ink.opacity(0.06), FrameMioColors.ink.opacity(0.48)],
                startPoint: .top,
                endPoint: .bottom
            )
            .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))

            VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                HStack(spacing: FrameMioSpacing.xs) {
                    Image(systemName: "sparkles")
                        .accessibilityHidden(true)
                    Text("Made from your day")
                }
                .font(FrameMioTypography.caption)
                .foregroundStyle(FrameMioColors.inverseInk)
                .padding(.horizontal, FrameMioSpacing.sm)
                .frame(minHeight: FrameMioMetrics.tagHeight)
                .background(FrameMioColors.mediaScrim, in: Capsule())

                Text("Golden hour, small laughs, one perfect wave.")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.inverseInk)
                    .fixedSize(horizontal: false, vertical: true)
                    .shadow(color: FrameMioColors.ink.opacity(0.28), radius: 14, x: 0, y: 8)
            }
            .padding(FrameMioSpacing.md)
        }
        .frame(height: 188)
        .accessibilityHidden(true)
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
                    shortcut(title: "Select Photos and Videos", icon: "checkmark.circle", context: .specialMoments)
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

private struct LoopingVideoPreview: UIViewRepresentable {
    let resourceName: String
    let isPlaying: Bool

    func makeUIView(context: Context) -> LoopingVideoPlayerView {
        let view = LoopingVideoPlayerView()
        view.configure(resourceName: resourceName, fileExtension: "mp4")
        view.setPlayback(isPlaying: isPlaying)
        return view
    }

    func updateUIView(_ uiView: LoopingVideoPlayerView, context: Context) {
        uiView.configure(resourceName: resourceName, fileExtension: "mp4")
        uiView.setPlayback(isPlaying: isPlaying)
    }

    static func dismantleUIView(_ uiView: LoopingVideoPlayerView, coordinator: ()) {
        uiView.stop()
    }
}

private final class LoopingVideoPlayerView: UIView {
    private var currentResourceName: String?
    private var player: AVQueuePlayer?
    private var playerLooper: AVPlayerLooper?

    override class var layerClass: AnyClass {
        AVPlayerLayer.self
    }

    private var playerLayer: AVPlayerLayer {
        layer as! AVPlayerLayer
    }

    func configure(resourceName: String, fileExtension: String) {
        guard currentResourceName != resourceName else { return }
        currentResourceName = resourceName

        guard let url = Bundle.main.url(forResource: resourceName, withExtension: fileExtension) else {
            return
        }

        let item = AVPlayerItem(url: url)
        let queuePlayer = AVQueuePlayer(playerItem: item)
        queuePlayer.isMuted = true
        queuePlayer.actionAtItemEnd = .none

        playerLooper = AVPlayerLooper(player: queuePlayer, templateItem: item)
        player = queuePlayer
        playerLayer.player = queuePlayer
        playerLayer.videoGravity = .resizeAspectFill
    }

    func setPlayback(isPlaying: Bool) {
        if isPlaying {
            player?.play()
        } else {
            player?.pause()
            player?.seek(to: .zero)
        }
    }

    func stop() {
        player?.pause()
        player = nil
        playerLooper = nil
        playerLayer.player = nil
    }
}
