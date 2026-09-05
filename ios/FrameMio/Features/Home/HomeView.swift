import AVFoundation
import SwiftUI
import UIKit

struct HomeView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ObservedObject private var viewModel: HomeViewModel
    @ObservedObject private var creditStore: CreditBalanceStore
    let onCreateStory: (MomentContext) -> Void
    let onOpenStory: (Story) -> Void
    let onOpenAllStories: () -> Void
    let onOpenPricing: (PricingEntryPoint) -> Void

    init(
        viewModel: HomeViewModel,
        creditStore: CreditBalanceStore,
        onCreateStory: @escaping (MomentContext) -> Void,
        onOpenStory: @escaping (Story) -> Void,
        onOpenAllStories: @escaping () -> Void,
        onOpenPricing: @escaping (PricingEntryPoint) -> Void
    ) {
        _viewModel = ObservedObject(wrappedValue: viewModel)
        _creditStore = ObservedObject(wrappedValue: creditStore)
        self.onCreateStory = onCreateStory
        self.onOpenStory = onOpenStory
        self.onOpenAllStories = onOpenAllStories
        self.onOpenPricing = onOpenPricing
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
                creditButton
            }
        }
        .task {
            viewModel.load()
            creditStore.refresh()
        }
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


    private var creditButton: some View {
        Button {
            FrameMioHaptics.secondaryAction()
            onOpenPricing(.homeCreditButton)
        } label: {
            HStack(spacing: FrameMioSpacing.xs) {
                Image(systemName: "sparkles")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(FrameMioColors.inverseInk)
                    .frame(width: 22, height: 22)
                    .background(FrameMioColors.accent, in: Circle())
                    .accessibilityHidden(true)
                Text(creditStore.compactTitle)
                    .font(FrameMioTypography.footnote.weight(.bold))
                    .foregroundStyle(FrameMioColors.ink)
            }
            .padding(.leading, FrameMioSpacing.xs)
            .padding(.trailing, FrameMioSpacing.md)
            .frame(minHeight: FrameMioMetrics.minimumTouchTarget)
            .background {
                Capsule()
                    .fill(
                        LinearGradient(
                            colors: [
                                FrameMioColors.accentMuted.opacity(creditStore.isLowOnCredits ? 0.95 : 0.82),
                                FrameMioColors.surface.opacity(0.96)
                            ],
                            startPoint: .topLeading,
                            endPoint: .bottomTrailing
                        )
                    )
            }
            .overlay {
                Capsule()
                    .stroke(FrameMioColors.accent.opacity(creditStore.isLowOnCredits ? 0.52 : 0.36), lineWidth: FrameMioMetrics.borderWidth)
            }
            .shadow(color: FrameMioColors.accent.opacity(0.18), radius: 10, x: 0, y: 4)
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .accessibilityLabel(creditStore.compactAccessibilityLabel)
    }

    private var hero: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xl) {
            HStack(alignment: .center, spacing: FrameMioSpacing.sm) {
                Image("AppLogo")
                    .resizable()
                    .scaledToFit()
                    .frame(width: 48, height: 48)
                    .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous))
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
                    shortcut(title: "Select Medias", icon: "checkmark.circle", context: .specialMoments)
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
            SectionHeader(
                title: "Recent stories",
                subtitle: "Made from your moments",
                actionTitle: "See all",
                action: onOpenAllStories
            )

            ScrollView(.horizontal, showsIndicators: false) {
                HStack(alignment: .top, spacing: FrameMioSpacing.md) {
                    ForEach(viewModel.stories) { story in
                        StoryCard(
                            title: story.title,
                            subtitle: storySubtitle(for: story),
                            image: viewModel.coverImage(for: story),
                            isImageLoading: viewModel.isCoverLoading(for: story),
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


struct AllStoriesView: View {
    @ObservedObject private var viewModel: HomeViewModel
    @State private var pageIndex = 0
    let onOpenStory: (Story) -> Void

    private let pageSize = 10

    init(viewModel: HomeViewModel, onOpenStory: @escaping (Story) -> Void) {
        _viewModel = ObservedObject(wrappedValue: viewModel)
        self.onOpenStory = onOpenStory
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                SectionHeader(
                    title: "All stories",
                    subtitle: pageSubtitle
                )

                LazyVStack(spacing: FrameMioSpacing.md) {
                    ForEach(pageStories) { story in
                        StoryCard(
                            title: story.title,
                            subtitle: storySubtitle(for: story),
                            image: viewModel.coverImage(for: story),
                            isImageLoading: viewModel.isCoverLoading(for: story),
                            duration: story.duration,
                            action: { onOpenStory(story) }
                        )
                    }
                }

                if pageCount > 1 {
                    paginationControls
                }
            }
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.lg)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background(FrameMioColors.canvas.ignoresSafeArea())
        .navigationTitle("All stories")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: pageIndex) {
            viewModel.loadCovers(for: pageStories)
        }
        .refreshable { viewModel.load() }
        .onChange(of: viewModel.allStories.count) { _, _ in
            pageIndex = min(pageIndex, max(pageCount - 1, 0))
        }
    }

    private var pageStories: [Story] {
        let startIndex = pageIndex * pageSize
        guard startIndex < viewModel.allStories.count else { return [] }
        let endIndex = min(startIndex + pageSize, viewModel.allStories.count)
        return Array(viewModel.allStories[startIndex..<endIndex])
    }

    private var pageCount: Int {
        max(1, Int(ceil(Double(viewModel.allStories.count) / Double(pageSize))))
    }

    private var pageSubtitle: String {
        guard !viewModel.allStories.isEmpty else { return "No stories yet" }
        let firstStory = pageIndex * pageSize + 1
        let lastStory = min(firstStory + pageStories.count - 1, viewModel.allStories.count)
        return "Showing \(firstStory)–\(lastStory) of \(viewModel.allStories.count)"
    }

    private var paginationControls: some View {
        HStack(spacing: FrameMioSpacing.md) {
            Button {
                pageIndex = max(pageIndex - 1, 0)
            } label: {
                Label("Previous", systemImage: "chevron.left")
            }
            .disabled(pageIndex == 0)

            Spacer()

            Text("Page \(pageIndex + 1) of \(pageCount)")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)

            Spacer()

            Button {
                pageIndex = min(pageIndex + 1, pageCount - 1)
            } label: {
                Label("Next", systemImage: "chevron.right")
                    .labelStyle(.titleAndIcon)
            }
            .disabled(pageIndex >= pageCount - 1)
        }
        .font(FrameMioTypography.headline)
        .foregroundStyle(FrameMioColors.accent)
        .padding(FrameMioSpacing.md)
        .background(
            FrameMioColors.surface.opacity(0.86),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
    }

    private func storySubtitle(for story: Story) -> String {
        let date = story.createdAt.formatted(.dateTime.month(.abbreviated).day())
        let moments = story.assetIDs.count == 1 ? "1 moment" : "\(story.assetIDs.count) moments"
        return "\(date) · \(moments)"
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
