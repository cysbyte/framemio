import AVFoundation
import SwiftUI
import UIKit

struct HomeView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.scenePhase) private var scenePhase
    @ObservedObject private var viewModel: HomeViewModel
    @ObservedObject private var creditStore: CreditBalanceStore
    let onCreateProject: (VideoProjectDraft) -> Void
    let onOpenStory: (Story) -> Void
    let onOpenAllStories: () -> Void
    let onOpenPricing: (PricingEntryPoint) -> Void
    let onCreateTextVideo: () -> Void

    init(
        viewModel: HomeViewModel,
        creditStore: CreditBalanceStore,
        onCreateProject: @escaping (VideoProjectDraft) -> Void,
        onOpenStory: @escaping (Story) -> Void,
        onOpenAllStories: @escaping () -> Void,
        onOpenPricing: @escaping (PricingEntryPoint) -> Void,
        onCreateTextVideo: @escaping () -> Void
    ) {
        _viewModel = ObservedObject(wrappedValue: viewModel)
        _creditStore = ObservedObject(wrappedValue: creditStore)
        self.onCreateProject = onCreateProject
        self.onOpenStory = onOpenStory
        self.onOpenAllStories = onOpenAllStories
        self.onOpenPricing = onOpenPricing
        self.onCreateTextVideo = onCreateTextVideo
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.xxl) {
                hero

                if viewModel.isLoading && viewModel.stories.isEmpty {
                    LoadingState(
                        title: "Loading your projects",
                        message: "Finding your latest videos."
                    )
                    .frame(maxWidth: .infinity)
                    .transition(FrameMioAnimation.transition(reduceMotion: reduceMotion))
                } else if viewModel.stories.isEmpty {
                    EmptyState(
                        systemImage: "rectangle.stack.badge.play",
                        title: "No projects yet",
                        message: "Choose Story, Promo, or Reel above to get started."
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
        .onAppear {
            viewModel.load()
        }
        .onChange(of: scenePhase) { _, newPhase in
            if newPhase == .active {
                viewModel.load()
            }
        }
        .refreshable {
            viewModel.load()
            creditStore.refresh(force: true)
        }
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
            VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                Text("Create videos from your photos and footage.")
                    .font(FrameMioTypography.display)
                    .foregroundStyle(FrameMioColors.ink)
                    .fixedSize(horizontal: false, vertical: true)

                Text("FrameMio helps turn raw media into finished stories, promos, and reels.")
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                    .fixedSize(horizontal: false, vertical: true)
            }

            projectTypeActions

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
        .accessibilityElement(children: .contain)
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
                    Text("AI-assisted video")
                }
                .font(FrameMioTypography.caption)
                .foregroundStyle(FrameMioColors.inverseInk)
                .padding(.horizontal, FrameMioSpacing.sm)
                .frame(minHeight: FrameMioMetrics.tagHeight)
                .background(FrameMioColors.mediaScrim, in: Capsule())

                Text("Your photos and footage, shaped into a finished video.")
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

    private var projectTypeActions: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Text("Start a new video")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)

            HStack(spacing: FrameMioSpacing.sm) {
                HomeCreationModeButton(
                    title: "Text to Video",
                    description: "Generate video from text",
                    systemImage: "text.alignleft",
                    background: FrameMioColors.accent.opacity(0.9),
                    action: onCreateTextVideo
                )

                HomeCreationModeButton(
                    title: "Image to Video",
                    description: "Generate video from image",
                    systemImage: "photo.on.rectangle.angled",
                    background: FrameMioColors.selection.opacity(0.88),
                    action: {}
                )
            }

            ForEach(VideoProjectType.allCases, id: \.self) { type in
                ProjectTypeAction(type: type) {
                    onCreateProject(draft(for: type))
                }
            }
        }
    }

    private func draft(for type: VideoProjectType) -> VideoProjectDraft {
        switch type {
        case .story:
            .story()
        case .promo:
            .promo()
        case .reel:
            .reel()
        }
    }

    private var recentStories: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(
                title: "Recent projects",
                subtitle: "Stories, promos, and reels",
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
                            projectType: story.projectType,
                            lastModified: story.lastModifiedAt,
                            status: story.status,
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
        let items = story.assetIDs.count == 1 ? itemName(for: story.projectType) : "\(story.assetIDs.count) \(itemName(for: story.projectType).lowercased())s"
        return items
    }

    private func itemName(for type: VideoProjectType) -> String {
        switch type {
        case .story:
            "Moment"
        case .promo:
            "Scene"
        case .reel:
            "Clip"
        }
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
    let onCreate: (() -> Void)?
    let onOpenStory: (Story) -> Void

    init(viewModel: HomeViewModel, onCreate: (() -> Void)? = nil, onOpenStory: @escaping (Story) -> Void) {
        _viewModel = ObservedObject(wrappedValue: viewModel)
        self.onCreate = onCreate
        self.onOpenStory = onOpenStory
    }

    var body: some View {
        ScrollView {
            ProjectsHistoryList(viewModel: viewModel, onCreate: onCreate, onOpenStory: onOpenStory)
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.lg)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background(FrameMioColors.canvas.ignoresSafeArea())
        .navigationTitle("History")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { viewModel.load() }
    }
}

struct ProjectsHistoryList: View {
    @ObservedObject private var viewModel: HomeViewModel
    @State private var pageIndex = 0
    @State private var filter: ProjectFilter = .all
    let onCreate: (() -> Void)?
    let onOpenStory: (Story) -> Void

    private let pageSize = 10

    init(viewModel: HomeViewModel, onCreate: (() -> Void)? = nil, onOpenStory: @escaping (Story) -> Void) {
        _viewModel = ObservedObject(wrappedValue: viewModel)
        self.onCreate = onCreate
        self.onOpenStory = onOpenStory
    }

    var body: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
            SectionHeader(
                title: "Project History",
                subtitle: pageSubtitle
            )

            if shouldShowFilters {
                projectFilterPicker
            }

            if filteredStories.isEmpty {
                EmptyState(
                    systemImage: "rectangle.stack.badge.play",
                    title: "No \(filter.title.lowercased()) projects yet",
                    message: "Create a Story, Promo, or Reel from your photos and footage.",
                    actionTitle: onCreate == nil ? nil : "Create",
                    action: onCreate
                )
                .frame(maxWidth: .infinity)
            } else {
                LazyVStack(spacing: FrameMioSpacing.md) {
                    ForEach(pageStories) { story in
                        StoryCard(
                            title: story.title,
                            subtitle: storySubtitle(for: story),
                            image: viewModel.coverImage(for: story),
                            isImageLoading: viewModel.isCoverLoading(for: story),
                            duration: story.duration,
                            projectType: story.projectType,
                            lastModified: story.lastModifiedAt,
                            status: story.status,
                            action: { onOpenStory(story) }
                        )
                    }
                }
            }

            if pageCount > 1 {
                paginationControls
            }
        }
        .task(id: pageIndex) {
            viewModel.loadCovers(for: pageStories)
        }
        .onChange(of: viewModel.allStories.count) { _, _ in
            pageIndex = min(pageIndex, max(pageCount - 1, 0))
        }
        .onChange(of: filter) { _, _ in
            pageIndex = 0
        }
    }

    private var filteredStories: [Story] {
        guard let projectType = filter.projectType else {
            return viewModel.allStories
        }
        return viewModel.allStories.filter { $0.projectType == projectType }
    }

    private var pageStories: [Story] {
        let startIndex = pageIndex * pageSize
        guard startIndex < filteredStories.count else { return [] }
        let endIndex = min(startIndex + pageSize, filteredStories.count)
        return Array(filteredStories[startIndex..<endIndex])
    }

    private var pageCount: Int {
        max(1, Int(ceil(Double(filteredStories.count) / Double(pageSize))))
    }

    private var pageSubtitle: String {
        guard !viewModel.allStories.isEmpty else { return "No projects yet" }
        guard !filteredStories.isEmpty else { return "No \(filter.title.lowercased()) projects" }
        let firstStory = pageIndex * pageSize + 1
        let lastStory = min(firstStory + pageStories.count - 1, filteredStories.count)
        return "Showing \(firstStory)-\(lastStory) of \(filteredStories.count)"
    }

    private var shouldShowFilters: Bool {
        Set(viewModel.allStories.map(\.projectType)).count > 1
    }

    private var projectFilterPicker: some View {
        HStack(spacing: 0) {
            ForEach(ProjectFilter.allCases, id: \.self) { option in
                Button {
                    FrameMioHaptics.secondaryAction()
                    filter = option
                } label: {
                    Text(option.title)
                        .font(FrameMioTypography.footnote.weight(.bold))
                        .foregroundStyle(filter == option ? FrameMioColors.inverseInk : FrameMioColors.secondaryInk)
                        .lineLimit(1)
                        .frame(maxWidth: .infinity)
                        .frame(height: 36)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .background {
                    if filter == option {
                        RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous)
                            .fill(FrameMioColors.accent)
                    }
                }
                .accessibilityAddTraits(filter == option ? .isSelected : [])
            }
        }
        .padding(4)
        .background(
            FrameMioColors.surface.opacity(0.92),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.64), lineWidth: FrameMioMetrics.borderWidth)
        }
        .accessibilityLabel("Filter projects by type")
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
        let count = story.assetIDs.count
        let noun = itemName(for: story.projectType).lowercased()
        return count == 1 ? "1 \(noun)" : "\(count) \(noun)s"
    }

    private func itemName(for type: VideoProjectType) -> String {
        switch type {
        case .story:
            "Moment"
        case .promo:
            "Scene"
        case .reel:
            "Clip"
        }
    }
}

private enum ProjectFilter: Hashable, CaseIterable {
    case all
    case story
    case promo
    case reel

    var title: String {
        switch self {
        case .all:
            "All"
        case .story:
            VideoProjectType.story.title
        case .promo:
            VideoProjectType.promo.title
        case .reel:
            VideoProjectType.reel.title
        }
    }

    var projectType: VideoProjectType? {
        switch self {
        case .all:
            nil
        case .story:
            .story
        case .promo:
            .promo
        case .reel:
            .reel
        }
    }
}

private struct ProjectTypeAction: View {
    let type: VideoProjectType
    let action: () -> Void

    var body: some View {
        Button {
            FrameMioHaptics.primaryAction()
            action()
        } label: {
            HStack(spacing: FrameMioSpacing.md) {
                Image(systemName: type.icon)
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(FrameMioColors.accent)
                    .frame(width: 42, height: 42)
                    .background(FrameMioColors.accentMuted.opacity(0.72), in: Circle())
                    .accessibilityHidden(true)

                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text(type.title)
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)

                    Text(type.description)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .fixedSize(horizontal: false, vertical: true)
                }

                Spacer(minLength: FrameMioSpacing.sm)

                Image(systemName: "chevron.right")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.tertiaryInk)
                    .accessibilityHidden(true)
            }
            .padding(FrameMioSpacing.md)
            .frame(
                maxWidth: .infinity,
                minHeight: 88,
                maxHeight: 88,
                alignment: .leading
            )
            .background(
                FrameMioColors.surface.opacity(0.86),
                in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                    .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
            }
            .frameMioElevation(.subtle)
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .accessibilityHint("Opens media selection for a \(type.title.lowercased()) project")
        .accessibilityLabel("\(type.title). \(type.description)")
    }
}

private struct HomeCreationModeButton: View {
    let title: String
    let description: String
    let systemImage: String
    let background: Color
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(alignment: .top, spacing: FrameMioSpacing.sm) {
                Image(systemName: systemImage)
                    .font(.headline.weight(.semibold))
                    .foregroundStyle(FrameMioColors.inverseInk)
                    .frame(width: 24, alignment: .leading)
                    .accessibilityHidden(true)

                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text(title)
                        .font(FrameMioTypography.footnote.weight(.semibold))
                        .foregroundStyle(FrameMioColors.inverseInk)
                        .lineLimit(2)
                        .multilineTextAlignment(.leading)

                    Text(description)
                        .font(FrameMioTypography.caption)
                        .foregroundStyle(FrameMioColors.inverseInk.opacity(0.82))
                        .lineLimit(2)
                        .multilineTextAlignment(.leading)
                }
            }
            .frame(maxWidth: .infinity, minHeight: 72, alignment: .leading)
            .padding(.vertical, FrameMioSpacing.xs)
            .padding(.horizontal, FrameMioSpacing.sm)
            .background(
                background,
                in: RoundedRectangle(cornerRadius: FrameMioRadius.small, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.small, style: .continuous)
                    .stroke(FrameMioColors.accent.opacity(0.32), lineWidth: FrameMioMetrics.borderWidth)
            }
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .accessibilityLabel("\(title). \(description)")
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
