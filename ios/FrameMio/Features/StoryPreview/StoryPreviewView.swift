import PhotosUI
import SwiftUI

struct StoryPreviewView: View {
    @Environment(\.dismiss) private var dismiss
    @StateObject private var viewModel: StoryPreviewViewModel
    @State private var fullScreenVideo: FullScreenStoryVideo?
    @State private var selectedMoment: StoryMomentPresentation?
    @State private var isShowingEditStory = false
    @State private var isShowingDeleteConfirmation = false

    private let generatedVideoAspectRatio: CGFloat = 9.0 / 16.0

    init(viewModel: StoryPreviewViewModel) {
        _viewModel = StateObject(wrappedValue: viewModel)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.xxl) {
                if viewModel.shouldShowInitialProgress {
                    initialLoadingProgress
                        .frame(maxWidth: .infinity)
                } else if let story = viewModel.story {
                    storyContent(story)
                } else {
                    EmptyState(
                        systemImage: "film",
                        title: "Story unavailable",
                        message: viewModel.errorMessage ?? "This story could not be opened.",
                        actionTitle: "Try Again",
                        action: { viewModel.load() }
                    )
                    .frame(maxWidth: .infinity)
                }
            }
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.md)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background(FrameMioColors.canvas.ignoresSafeArea())
        .navigationTitle("My Story")
        .navigationBarTitleDisplayMode(.inline)
        .task { viewModel.load() }
        .onChange(of: viewModel.didDeleteStory) { _, didDelete in
            if didDelete {
                dismiss()
            }
        }
        .fullScreenCover(item: $fullScreenVideo) { video in
            FullScreenStoryVideoPlayer(url: video.url)
        }
        .sheet(item: $selectedMoment) { moment in
            MomentDetailSheet(
                moment: moment,
                image: viewModel.image(forStoryAssetID: moment.assetID),
                isPerformingAction: viewModel.isPerformingAction,
                onRemove: {
                    viewModel.removeMoment(assetID: moment.assetID)
                    selectedMoment = nil
                },
                onKeep: { selectedMoment = nil }
            )
            .presentationDetents([.medium, .large])
            .presentationDragIndicator(.visible)
        }
        .sheet(isPresented: $isShowingEditStory) {
            if let story = viewModel.story {
                EditStorySheet(
                    story: story,
                    moments: momentPresentations(for: story),
                    availableMoments: availableMomentPresentations(for: story),
                    activeAction: viewModel.activeAction,
                    isPerformingAction: viewModel.isPerformingAction,
                    imageProvider: { viewModel.image(forStoryAssetID: $0) },
                    onAppearMoment: { assetID in
                        viewModel.loadThumbnail(
                            forStoryAssetID: assetID,
                            targetSize: CGSize(width: 220, height: 220)
                        )
                    },
                    onImportPhotos: { items in
                        try await viewModel.importMediaForEdit(items)
                    },
                    onSave: { request in
                        viewModel.saveStoryEdits(request)
                        isShowingEditStory = false
                    },
                    onRetry: {
                        viewModel.retryProcessing()
                        isShowingEditStory = false
                    }
                )
                .presentationDetents([.large])
                .presentationDragIndicator(.visible)
            }
        }
        .alert("Couldn’t update story", isPresented: errorBinding) {
            Button("Dismiss", role: .cancel) { viewModel.errorMessage = nil }
        } message: {
            Text(viewModel.errorMessage ?? "")
        }
        .alert("Saved", isPresented: confirmationBinding) {
            Button("OK", role: .cancel) { viewModel.confirmationMessage = nil }
        } message: {
            Text(viewModel.confirmationMessage ?? "")
        }
        .alert("Remove Story?", isPresented: $isShowingDeleteConfirmation) {
            Button("Cancel", role: .cancel) {}
            Button("Remove", role: .destructive) {
                viewModel.deleteStory()
            }
        } message: {
            Text("This removes the story from FrameMio. Your original photos and videos stay in Photos.")
        }
    }

    private var initialLoadingProgress: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
            StoryWaitingAnimation()

            VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                Text("Creating your story...")
                    .font(FrameMioTypography.title)
                    .foregroundStyle(FrameMioColors.ink)

                FrameMioProgressView(value: viewModel.progressValue, title: viewModel.progressTitle)

                processingSteps
            }
        }
        .frame(maxWidth: FrameMioMetrics.loadingStateMaxWidth)
        .padding(FrameMioSpacing.xl)
        .background(
            FrameMioColors.surface.opacity(0.82),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .frameMioElevation(.subtle)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private func storyContent(_ story: Story) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xxl) {
            storyHeader(story)
            generatedVideoHero(story)
            primaryActions(story)
            storyDescription(story)
            momentsSection(story)
            editStoryButton(story)
            removeStoryButton(story)
        }
    }

    private func storyHeader(_ story: Story) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Text("YOUR STORY")
                .font(FrameMioTypography.eyebrow)
                .foregroundStyle(FrameMioColors.accent)
                .textCase(.uppercase)
                .accessibilityHidden(true)

            Text(storyDisplayTitle(story))
                .font(FrameMioTypography.display)
                .foregroundStyle(FrameMioColors.ink)
                .fixedSize(horizontal: false, vertical: true)

            Text(storyDateDuration(story))
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func generatedVideoHero(_ story: Story) -> some View {
        VStack(spacing: FrameMioSpacing.md) {
            ZStack(alignment: .topTrailing) {
                if let videoURL = story.videoURL, story.status == .ready {
                    StoryVideoPlayer(
                        url: videoURL,
                        autoplays: true,
                        isPlaybackEnabled: fullScreenVideo == nil,
                        accessibilityTitle: "Play \(storyDisplayTitle(story))"
                    )
                    .aspectRatio(generatedVideoAspectRatio, contentMode: .fit)
                    .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))

                    fullscreenButton(for: videoURL, title: storyDisplayTitle(story))
                        .padding(FrameMioSpacing.md)
                } else {
                    videoPlaceholder(story)
                }
            }
            .frame(maxWidth: 360)
            .frame(maxWidth: .infinity)
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                    .stroke(FrameMioColors.divider.opacity(0.34), lineWidth: FrameMioMetrics.borderWidth)
            }
            .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
            .frameMioElevation(.floating)
            .accessibilityElement(children: .combine)
        }
    }

    private func fullscreenButton(for videoURL: URL, title: String) -> some View {
        Button {
            FrameMioHaptics.secondaryAction()
            fullScreenVideo = FullScreenStoryVideo(url: videoURL)
        } label: {
            Image(systemName: "arrow.up.left.and.arrow.down.right")
                .font(.headline.weight(.semibold))
                .foregroundStyle(FrameMioColors.inverseInk)
                .frame(width: FrameMioMetrics.minimumTouchTarget, height: FrameMioMetrics.minimumTouchTarget)
                .background(FrameMioColors.ink.opacity(0.58), in: Circle())
                .overlay {
                    Circle().stroke(FrameMioColors.inverseInk.opacity(0.18), lineWidth: FrameMioMetrics.borderWidth)
                }
                .shadow(color: .black.opacity(0.18), radius: 10, x: 0, y: 5)
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .accessibilityLabel("Play \(title) fullscreen")
    }

    private func videoPlaceholder(_ story: Story) -> some View {
        ZStack {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .fill(
                    LinearGradient(
                        colors: [FrameMioColors.memoryLight, FrameMioColors.accentMuted, FrameMioColors.memoryEarth],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    )
                )
                .aspectRatio(generatedVideoAspectRatio, contentMode: .fit)

            VStack(spacing: FrameMioSpacing.md) {
                Image(systemName: placeholderIcon(for: story.status))
                    .font(FrameMioTypography.memorySymbol)
                    .foregroundStyle(FrameMioColors.inverseInk)
                    .frame(width: 82, height: 82)
                    .background(FrameMioColors.mediaScrim, in: Circle())
                    .accessibilityHidden(true)

                VStack(spacing: FrameMioSpacing.xs) {
                    Text(placeholderTitle(for: story))
                        .font(FrameMioTypography.title)
                        .foregroundStyle(FrameMioColors.inverseInk)
                        .multilineTextAlignment(.center)

                    Text(placeholderMessage(for: story))
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.inverseInk.opacity(0.84))
                        .multilineTextAlignment(.center)
                }

                if story.status.shouldPoll {
                    ProgressView()
                        .tint(FrameMioColors.inverseInk)
                        .accessibilityLabel(viewModel.progressTitle)
                }
            }
            .padding(FrameMioSpacing.xl)
        }
    }

    @ViewBuilder
    private func primaryActions(_ story: Story) -> some View {
        if story.status == .failed {
            PrimaryButton(
                title: actionTitle(.retry, fallback: "Try Again"),
                systemImage: "arrow.clockwise",
                action: { viewModel.retryProcessing() },
                isLoading: viewModel.activeAction == .retry
            )
            .disabled(viewModel.isPerformingAction)
            .accessibilityLabel("Try creating story again")
        } else if let videoURL = story.videoURL, story.status == .ready {
            HStack(spacing: FrameMioSpacing.md) {
                ShareLink(item: videoURL) {
                    StoryPillActionLabel(title: "Share", systemImage: "square.and.arrow.up")
                }
                .disabled(viewModel.isPerformingAction)
                .accessibilityLabel("Share story")

                PrimaryButton(
                    title: actionTitle(.saveVideo, fallback: "Save"),
                    systemImage: "arrow.down.circle",
                    action: { viewModel.saveVideoToPhotos() },
                    isLoading: viewModel.activeAction == .saveVideo
                )
                .disabled(viewModel.isPerformingAction)
                .accessibilityLabel("Save story to Photos")
            }
        } else if story.status.shouldPoll {
            VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                FrameMioProgressView(value: viewModel.progressValue, title: viewModel.progressTitle)
                processingSteps
            }
        }
    }

    @ViewBuilder
    private func storyDescription(_ story: Story) -> some View {
        if let description = nonEmpty(story.description) {
            VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                Text("About this story")
                    .font(FrameMioTypography.title)
                    .foregroundStyle(FrameMioColors.ink)

                Text(description)
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func momentsSection(_ story: Story) -> some View {
        let moments = momentPresentations(for: story)

        return VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Moments", subtitle: momentSummary(for: story, moments: moments))

            if moments.isEmpty {
                Text("FrameMio will show the moments that shaped this story when they are available.")
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(alignment: .top, spacing: FrameMioSpacing.md) {
                        ForEach(moments) { moment in
                            StoryMomentCard(
                                moment: moment,
                                image: viewModel.image(forStoryAssetID: moment.assetID),
                                action: {
                                    selectedMoment = moment
                                }
                            )
                            .frame(width: 132)
                            .onAppear {
                                viewModel.loadThumbnail(
                                    forStoryAssetID: moment.assetID,
                                    targetSize: FrameMioMetrics.mediaThumbnailRequestSize
                                )
                            }
                            .onDisappear {
                                viewModel.cancelThumbnail(forStoryAssetID: moment.assetID)
                            }
                        }
                    }
                    .padding(.vertical, FrameMioSpacing.xs)
                }
                .scrollClipDisabled()
            }
        }
    }

    private func editStoryButton(_ story: Story) -> some View {
        PrimaryButton(title: "Edit Story", systemImage: "square.and.pencil") {
            guard !story.status.shouldPoll else { return }
            isShowingEditStory = true
        }
        .disabled(viewModel.isPerformingAction || story.status.shouldPoll)
        .accessibilityLabel("Edit story")
        .accessibilityHint(story.status.shouldPoll ? "Editing is unavailable while FrameMio is creating this story." : "Opens story editing options.")
    }

    private func removeStoryButton(_ story: Story) -> some View {
        Button(role: .destructive) {
            guard !story.status.shouldPoll else { return }
            FrameMioHaptics.secondaryAction()
            isShowingDeleteConfirmation = true
        } label: {
            HStack(spacing: FrameMioSpacing.xs) {
                if viewModel.activeAction == .deleteStory {
                    ProgressView()
                        .controlSize(.small)
                        .tint(.red)
                        .accessibilityHidden(true)
                } else {
                    Image(systemName: "trash")
                        .font(FrameMioTypography.headline)
                        .accessibilityHidden(true)
                }
                Text(actionTitle(.deleteStory, fallback: "Remove Story"))
                    .font(FrameMioTypography.headline)
            }
            .foregroundStyle(Color.red)
            .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.compactButtonHeight)
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .disabled(viewModel.isPerformingAction || story.status.shouldPoll)
        .accessibilityLabel("Remove story")
        .accessibilityHint(story.status.shouldPoll ? "Removing is unavailable while FrameMio is creating this story." : "Removes this story from FrameMio.")
    }

    private var processingSteps: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
            processingStep("Understanding your moments", isActive: viewModel.progressValue >= 0.18)
            processingStep("Finding the best parts", isActive: viewModel.progressValue >= 0.58)
            processingStep("Putting it together", isActive: viewModel.progressValue >= 0.86)
        }
    }

    private func processingStep(_ title: String, isActive: Bool) -> some View {
        Label(title, systemImage: isActive ? "checkmark.circle.fill" : "circle")
            .font(FrameMioTypography.footnote)
            .foregroundStyle(isActive ? FrameMioColors.ink : FrameMioColors.secondaryInk)
            .accessibilityLabel(title)
    }

    private func momentPresentations(for story: Story) -> [StoryMomentPresentation] {
        if let storyMoments = story.moments, !storyMoments.isEmpty {
            return storyMoments.enumerated().map { index, metadata in
                let asset = viewModel.momentAsset(for: metadata.mediaID)
                let kind = momentKind(metadata: metadata, asset: asset)
                return StoryMomentPresentation(
                    id: metadata.id,
                    assetID: metadata.mediaID,
                    title: nonEmpty(metadata.title) ?? fallbackMomentTitle(kind: kind, index: index),
                    subtitle: momentSubtitle(kind: kind, asset: asset),
                    description: nonEmpty(metadata.description),
                    kind: kind,
                    duration: kind == .video ? asset?.duration : nil
                )
            }
        }

        return story.assetIDs.enumerated().map { index, assetID in
            let metadata = story.moments?.first { $0.mediaID == assetID || $0.id == assetID }
            let asset = viewModel.momentAsset(for: assetID)
            let kind = momentKind(metadata: metadata, asset: asset)
            let title = nonEmpty(metadata?.title) ?? fallbackMomentTitle(kind: kind, index: index)
            let subtitle = momentSubtitle(kind: kind, asset: asset)
            return StoryMomentPresentation(
                id: "\(assetID)-\(index)",
                assetID: assetID,
                title: title,
                subtitle: subtitle,
                description: nonEmpty(metadata?.description),
                kind: kind,
                duration: kind == .video ? asset?.duration : nil
            )
        }
    }

    private func availableMomentPresentations(for story: Story) -> [StoryMomentPresentation] {
        if let availableMoments = story.availableMoments, !availableMoments.isEmpty {
            return availableMoments.enumerated().map { index, metadata in
                let asset = viewModel.momentAsset(for: metadata.mediaID)
                let kind = momentKind(metadata: metadata, asset: asset)
                return StoryMomentPresentation(
                    id: metadata.id,
                    assetID: metadata.mediaID,
                    title: nonEmpty(metadata.title) ?? fallbackMomentTitle(kind: kind, index: index),
                    subtitle: momentSubtitle(kind: kind, asset: asset),
                    description: nonEmpty(metadata.description),
                    kind: kind,
                    duration: kind == .video ? asset?.duration : nil
                )
            }
        }

        return story.assetIDs.enumerated().map { index, assetID in
            let metadata = story.moments?.first { $0.mediaID == assetID || $0.id == assetID }
            let asset = viewModel.momentAsset(for: assetID)
            let kind = momentKind(metadata: metadata, asset: asset)
            return StoryMomentPresentation(
                id: metadata?.id ?? "available-\(assetID)-\(index)",
                assetID: assetID,
                title: nonEmpty(metadata?.title) ?? fallbackMomentTitle(kind: kind, index: index),
                subtitle: momentSubtitle(kind: kind, asset: asset),
                description: nonEmpty(metadata?.description),
                kind: kind,
                duration: kind == .video ? asset?.duration : nil
            )
        }
    }

    private func momentKind(metadata: StoryMoment?, asset: MediaAsset?) -> StoryMomentPresentation.Kind {
        if metadata?.type == .video || asset?.isVideo == true { return .video }
        if metadata?.type == .livePhoto || asset?.isLivePhoto == true { return .livePhoto }
        return .photo
    }

    private func fallbackMomentTitle(kind: StoryMomentPresentation.Kind, index: Int) -> String {
        switch index {
        case 0:
            return "Opening moment"
        case 1:
            return "The next memory"
        case 2:
            return "A favorite part"
        default:
            return "Moment \(index + 1)"
        }
    }

    private func momentSubtitle(kind: StoryMomentPresentation.Kind, asset: MediaAsset?) -> String {
        switch kind {
        case .photo:
            return "Photo"
        case .video:
            if let duration = asset?.duration, duration > 0 {
                return "Video · \(Int(duration.rounded())) sec"
            }
            return "Video"
        case .livePhoto:
            return "Live Photo"
        }
    }

    private func momentSummary(for story: Story, moments: [StoryMomentPresentation]) -> String? {
        guard !moments.isEmpty else { return nil }
        let photoCount = moments.filter { $0.kind == .photo || $0.kind == .livePhoto }.count
        let videoCount = moments.filter { $0.kind == .video }.count
        var parts = [moments.count == 1 ? "1 moment" : "\(moments.count) moments"]
        if photoCount > 0 {
            parts.append(photoCount == 1 ? "1 photo" : "\(photoCount) photos")
        }
        if videoCount > 0 {
            parts.append(videoCount == 1 ? "1 video" : "\(videoCount) videos")
        }
        return parts.joined(separator: " · ")
    }

    private func storyDisplayTitle(_ story: Story) -> String {
        nonEmpty(story.title) ?? "Your Day"
    }

    private func storyDateDuration(_ story: Story) -> String {
        let date = story.createdAt.formatted(.dateTime.month(.abbreviated).day())
        let seconds = max(1, Int(story.duration.rounded()))
        return "\(date) · \(seconds) sec"
    }

    private func placeholderIcon(for status: StoryStatus) -> String {
        switch status {
        case .failed:
            "exclamationmark.triangle.fill"
        case .ready:
            "film"
        case .queued, .analyzing, .rendering:
            "sparkles"
        }
    }

    private func placeholderTitle(for story: Story) -> String {
        switch story.status {
        case .failed:
            "Something went wrong creating your story."
        case .ready:
            "Preparing your film..."
        case .queued, .analyzing, .rendering:
            "Creating your story..."
        }
    }

    private func placeholderMessage(for story: Story) -> String {
        switch story.status {
        case .failed:
            "Try again when you’re ready."
        case .ready:
            "The video link is almost ready."
        case .queued, .analyzing, .rendering:
            "FrameMio is finding the moments that belong together."
        }
    }

    private func nonEmpty(_ value: String?) -> String? {
        guard let trimmed = value?.trimmingCharacters(in: .whitespacesAndNewlines), !trimmed.isEmpty else {
            return nil
        }
        return trimmed
    }

    private func actionTitle(_ action: StoryPreviewAction, fallback: String) -> String {
        viewModel.activeAction == action ? action.title : fallback
    }

    private var errorBinding: Binding<Bool> {
        Binding(
            get: { viewModel.errorMessage != nil },
            set: { if !$0 { viewModel.errorMessage = nil } }
        )
    }

    private var confirmationBinding: Binding<Bool> {
        Binding(
            get: { viewModel.confirmationMessage != nil },
            set: { if !$0 { viewModel.confirmationMessage = nil } }
        )
    }
}

private struct StoryMomentPresentation: Identifiable, Hashable {
    enum Kind: Hashable {
        case photo
        case video
        case livePhoto

        var iconName: String {
            switch self {
            case .photo:
                "photo.fill"
            case .video:
                "play.fill"
            case .livePhoto:
                "livephoto"
            }
        }

        var placeholderName: String {
            switch self {
            case .photo:
                "photo.fill"
            case .video:
                "video.fill"
            case .livePhoto:
                "livephoto"
            }
        }

        var importedSubtitle: String {
            switch self {
            case .photo:
                "Photo"
            case .video:
                "Video"
            case .livePhoto:
                "Live Photo"
            }
        }
    }

    let id: String
    let assetID: String
    let title: String
    let subtitle: String
    let description: String?
    let kind: Kind
    let duration: TimeInterval?

    init(id: String, assetID: String, title: String, subtitle: String, description: String?, kind: Kind, duration: TimeInterval?) {
        self.id = id
        self.assetID = assetID
        self.title = title
        self.subtitle = subtitle
        self.description = description
        self.kind = kind
        self.duration = duration
    }

    init(imported: ImportedStoryMedia, index: Int) {
        let kind: Kind = imported.sourceAsset.isVideo ? .video : (imported.sourceAsset.isLivePhoto ? .livePhoto : .photo)
        self.init(
            id: imported.mediaID,
            assetID: imported.mediaID,
            title: Self.importedTitle(kind: kind, index: index),
            subtitle: kind == .video && imported.sourceAsset.duration > 0 ? "Video · \(Int(imported.sourceAsset.duration.rounded())) sec" : kind.importedSubtitle,
            description: "New from Photos. FrameMio will decide where it belongs.",
            kind: kind,
            duration: kind == .video ? imported.sourceAsset.duration : nil
        )
    }

    private static func importedTitle(kind: Kind, index: Int) -> String {
        switch kind {
        case .photo: "New photo"
        case .video: "New video"
        case .livePhoto: "New Live Photo"
        }
    }
}

private struct StoryPillActionLabel: View {
    let title: String
    let systemImage: String

    var body: some View {
        HStack(spacing: FrameMioSpacing.xs) {
            Image(systemName: systemImage)
                .font(FrameMioTypography.headline)
                .accessibilityHidden(true)
            Text(title)
                .font(FrameMioTypography.headline)
        }
        .foregroundStyle(FrameMioColors.ink)
        .padding(.horizontal, FrameMioSpacing.lg)
        .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.buttonHeight)
        .background(
            FrameMioColors.surface.opacity(0.86),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
    }
}

private struct StoryMomentCard: View {
    let moment: StoryMomentPresentation
    let image: Image?
    let action: () -> Void

    var body: some View {
        Button {
            FrameMioHaptics.secondaryAction()
            action()
        } label: {
            VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                MediaThumbnail(
                    image: image,
                    duration: moment.duration,
                    isLivePhoto: moment.kind == .livePhoto,
                    aspectRatio: 1,
                    placeholderSystemImage: moment.kind.placeholderName,
                    accessibilityTitle: moment.title
                )
                .overlay(alignment: .center) {
                    if moment.kind == .video {
                        Image(systemName: moment.kind.iconName)
                            .font(.headline.weight(.semibold))
                            .foregroundStyle(FrameMioColors.inverseInk)
                            .frame(width: 36, height: 36)
                            .background(FrameMioColors.mediaScrim, in: Circle())
                            .accessibilityHidden(true)
                    }
                }

                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text(moment.title)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.ink)
                        .lineLimit(2)
                    Text(moment.subtitle)
                        .font(FrameMioTypography.caption)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .lineLimit(1)
                }
            }
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .accessibilityLabel(moment.title)
        .accessibilityHint("Opens moment details")
    }
}

private struct MomentDetailSheet: View {
    let moment: StoryMomentPresentation
    let image: Image?
    let isPerformingAction: Bool
    let onRemove: () -> Void
    let onKeep: () -> Void

    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                MediaThumbnail(
                    image: image,
                    duration: moment.duration,
                    isLivePhoto: moment.kind == .livePhoto,
                    aspectRatio: FrameMioMetrics.thumbnailAspectRatio,
                    placeholderSystemImage: moment.kind.placeholderName,
                    accessibilityTitle: moment.title
                )
                .overlay(alignment: .center) {
                    if moment.kind == .video {
                        Image(systemName: "play.fill")
                            .font(.headline.weight(.semibold))
                            .foregroundStyle(FrameMioColors.inverseInk)
                            .frame(width: 42, height: 42)
                            .background(FrameMioColors.mediaScrim, in: Circle())
                            .accessibilityHidden(true)
                    }
                }

                VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                    Text(moment.title)
                        .font(FrameMioTypography.title)
                        .foregroundStyle(FrameMioColors.ink)
                    Text(moment.description ?? "This is one of the moments FrameMio used in your story.")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .fixedSize(horizontal: false, vertical: true)
                }

                Spacer(minLength: FrameMioSpacing.md)

                VStack(spacing: FrameMioSpacing.sm) {
                    Button(role: .destructive, action: onRemove) {
                        Label("Remove from Story", systemImage: "minus.circle")
                            .font(FrameMioTypography.headline)
                            .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.compactButtonHeight)
                    }
                    .disabled(isPerformingAction)
                    .accessibilityLabel("Remove \(moment.title) from story")

                    SecondaryButton(title: "Keep", systemImage: "checkmark", action: onKeep)
                }
            }
            .padding(FrameMioSpacing.lg)
            .background(FrameMioColors.canvas.ignoresSafeArea())
            .navigationTitle(moment.title)
            .navigationBarTitleDisplayMode(.inline)
        }
    }
}

private struct EditStorySheet: View {
    @Environment(\.dismiss) private var dismiss
    let story: Story
    let moments: [StoryMomentPresentation]
    let availableMoments: [StoryMomentPresentation]
    let activeAction: StoryPreviewAction?
    let isPerformingAction: Bool
    let imageProvider: (String) -> Image?
    let onAppearMoment: (String) -> Void
    let onImportPhotos: ([PhotosPickerItem]) async throws -> [ImportedStoryMedia]
    let onSave: (StoryEditRequest) -> Void
    let onRetry: () -> Void

    @State private var draft = EditStoryDraft()
    @State private var selectedMoment: StoryMomentPresentation?
    @State private var replacingMoment: StoryMomentPresentation?
    @State private var isShowingAddMoment = false
    @State private var importedMoments: [StoryMomentPresentation] = []

    private let styleOptions = ["Natural", "Cinematic", "Warm", "Playful"]
    private let subtitleStyleOptions = ["Bold", "Cinematic", "Minimal", "Playful"]

    private var lengthOptions: [Int] {
        Array(Set([15, max(8, Int(story.duration.rounded())), 30])).sorted()
    }

    private var displayedMoments: [StoryMomentPresentation] {
        let removed = draft.removedMomentIDs.union(draft.removedMediaIDs)
        let replacementSources = Set(draft.replacements.compactMap { $0.fromMomentID } + draft.replacements.compactMap { $0.fromMediaID })
        let base = moments.filter { !removed.contains($0.id) && !removed.contains($0.assetID) && !replacementSources.contains($0.id) && !replacementSources.contains($0.assetID) }
        let existingIDs = Set(base.map(\.assetID))
        let addableMoments = availableMoments + importedMoments
        let added = addableMoments.filter { draft.addedMediaIDs.contains($0.assetID) && !existingIDs.contains($0.assetID) }
        return base + added
    }

    private var selectableMoments: [StoryMomentPresentation] {
        let visibleIDs = Set(displayedMoments.map(\.assetID))
        return (availableMoments + importedMoments).filter { !visibleIDs.contains($0.assetID) }
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: FrameMioSpacing.xl) {
                    if story.status == .failed {
                        failedState
                    } else if isPerformingAction && activeAction == .editStory {
                        updatingState
                    }
                    momentsSection
                    addMomentButton
                    styleSection
                    subtitleSection
                    lengthSection
                    anotherVersionSection
                }
                .padding(FrameMioSpacing.lg)
            }
            .background(FrameMioColors.canvas.ignoresSafeArea())
            .navigationTitle("Edit Story")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                        .disabled(isPerformingAction)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(activeAction == .editStory ? "Saving" : "Save") {
                        onSave(draft.request)
                    }
                    .disabled(!draft.hasChanges || isPerformingAction)
                }
            }
            .sheet(item: $selectedMoment) { moment in
                EditMomentSheet(
                    moment: moment,
                    image: imageProvider(moment.assetID),
                    isPerformingAction: isPerformingAction,
                    onRemove: {
                        draft.remove(moment)
                        selectedMoment = nil
                    },
                    onReplace: {
                        replacingMoment = moment
                        selectedMoment = nil
                    },
                    onKeep: { selectedMoment = nil }
                )
                .presentationDetents([.medium, .large])
                .presentationDragIndicator(.visible)
            }
            .sheet(isPresented: $isShowingAddMoment) {
                AddMomentSelectionSheet(
                    moments: selectableMoments,
                    imageProvider: imageProvider,
                    onAppearMoment: onAppearMoment,
                    onImportPhotos: { items in
                        let imported = try await onImportPhotos(items)
                        let presentations = imported.map { StoryMomentPresentation(imported: $0, index: importedMoments.count) }
                        importedMoments.append(contentsOf: presentations)
                        draft.addAnalysis(imported)
                        return presentations
                    },
                    onAdd: { selectedMoments in
                        selectedMoments.forEach { draft.add($0) }
                        isShowingAddMoment = false
                    },
                    onCancel: { isShowingAddMoment = false }
                )
                .presentationDetents([.large])
                .presentationDragIndicator(.visible)
            }
            .sheet(item: $replacingMoment) { source in
                AddMomentSelectionSheet(
                    title: "Replace Moment",
                    moments: selectableMoments,
                    allowsMultipleSelection: false,
                    imageProvider: imageProvider,
                    onAppearMoment: onAppearMoment,
                    onImportPhotos: { items in
                        let imported = try await onImportPhotos(items)
                        let presentations = imported.map { StoryMomentPresentation(imported: $0, index: importedMoments.count) }
                        importedMoments.append(contentsOf: presentations)
                        draft.addAnalysis(imported)
                        return presentations
                    },
                    onAdd: { selectedMoments in
                        if let replacement = selectedMoments.first {
                            draft.replace(source, with: replacement)
                        }
                        replacingMoment = nil
                    },
                    onCancel: { replacingMoment = nil }
                )
                .presentationDetents([.large])
                .presentationDragIndicator(.visible)
            }
        }
    }

    private var failedState: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Text("Couldn't create this version.")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)
            Text("Your previous story stays available. Try again when you're ready.")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)
            SecondaryButton(title: "Try Again", systemImage: "arrow.clockwise", action: onRetry)
        }
    }

    private var updatingState: some View {
        HStack(spacing: FrameMioSpacing.sm) {
            ProgressView()
            VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                Text("Creating your new story")
                    .font(FrameMioTypography.headline)
                Text("Finding the right rhythm and rendering your video.")
                    .font(FrameMioTypography.caption)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }
        }
        .padding(FrameMioSpacing.md)
        .background(FrameMioColors.surface.opacity(0.72), in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
    }

    private var momentsSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Moments", subtitle: draft.hasChanges ? "Unsaved changes" : "In this story")
            if displayedMoments.isEmpty {
                Text("Choose a few moments and FrameMio will shape them into a new version.")
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            } else {
                LazyVStack(spacing: FrameMioSpacing.sm) {
                    ForEach(displayedMoments) { moment in
                        StoryMomentRow(moment: moment, image: imageProvider(moment.assetID)) {
                            selectedMoment = moment
                        }
                        .onAppear { onAppearMoment(moment.assetID) }
                    }
                }
            }
        }
    }

    private var addMomentButton: some View {
        Button {
            FrameMioHaptics.secondaryAction()
            isShowingAddMoment = true
        } label: {
            Label("Add moment", systemImage: "plus.circle.fill")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.accent)
        }
        .disabled(isPerformingAction)
        .accessibilityLabel("Add moment")
    }

    private var styleSection: some View {
        optionSection(title: "Style", options: styleOptions, selected: draft.style ?? "Natural") { option in
            draft.style = option.lowercased()
        }
    }

    private var subtitleSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Subtitles", subtitle: "Show spoken dialogue as captions")
            Toggle(isOn: Binding(
                get: { draft.subtitlesEnabled ?? true },
                set: { draft.subtitlesEnabled = $0 }
            )) {
                Label("Show subtitles", systemImage: "captions.bubble")
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.ink)
            }
            .tint(FrameMioColors.accent)
            .disabled(isPerformingAction)

            if draft.subtitlesEnabled ?? true {
                LazyVGrid(columns: optionGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                    ForEach(subtitleStyleOptions, id: \.self) { option in
                        optionChip(title: option, isSelected: option.caseInsensitiveCompare(draft.subtitleStyle ?? "Cinematic") == .orderedSame) {
                            draft.subtitleStyle = option.lowercased()
                        }
                    }
                }
            }
        }
    }

    private var lengthSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Length", subtitle: "FrameMio decides what to keep")
            LazyVGrid(columns: optionGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                ForEach(lengthOptions, id: \.self) { seconds in
                    optionChip(title: "\(seconds)s", isSelected: Int((draft.targetDuration ?? story.duration).rounded()) == seconds) {
                        draft.targetDuration = TimeInterval(seconds)
                    }
                }
            }
        }
    }

    private var anotherVersionSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Make Another Version", subtitle: "Tell FrameMio what to change")
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 140), spacing: FrameMioSpacing.sm)], spacing: FrameMioSpacing.sm) {
                ForEach(["More cinematic", "More emotional", "More fun", "Focus on people", "Focus on places"], id: \.self) { prompt in
                    optionChip(title: prompt, isSelected: draft.instruction == prompt) {
                        draft.instruction = prompt
                    }
                }
            }
            TextField("Tell FrameMio what you want", text: Binding(
                get: { draft.instruction ?? "" },
                set: { draft.instruction = $0 }
            ), axis: .vertical)
            .lineLimit(2...4)
            .font(FrameMioTypography.body)
            .padding(FrameMioSpacing.md)
            .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
            .accessibilityLabel("Tell FrameMio what you want")
        }
    }

    private func optionSection(title: String, options: [String], selected: String, onSelect: @escaping (String) -> Void) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: title)
            LazyVGrid(columns: optionGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                ForEach(options, id: \.self) { option in
                    optionChip(title: option, isSelected: option.caseInsensitiveCompare(selected) == .orderedSame) {
                        onSelect(option)
                    }
                }
            }
        }
    }

    private var optionGridColumns: [GridItem] {
        [GridItem(.adaptive(minimum: 108), spacing: FrameMioSpacing.sm, alignment: .leading)]
    }

    private func optionChip(title: String, isSelected: Bool, action: @escaping () -> Void) -> some View {
        Button {
            FrameMioHaptics.secondaryAction()
            action()
        } label: {
            Text(title)
                .font(FrameMioTypography.footnote)
                .foregroundStyle(isSelected ? FrameMioColors.inverseInk : FrameMioColors.ink)
                .padding(.horizontal, FrameMioSpacing.md)
                .frame(minHeight: FrameMioMetrics.compactButtonHeight)
                .background(isSelected ? FrameMioColors.accent : FrameMioColors.surface.opacity(0.78), in: Capsule())
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .disabled(isPerformingAction)
    }
}

private struct EditStoryDraft {
    var removedMomentIDs = Set<String>()
    var removedMediaIDs = Set<String>()
    var addedMediaIDs = Set<String>()
    var replacements: [StoryMomentReplacement] = []
    var analysesByMediaID: [String: MediaAnalysis] = [:]
    var style: String?
    var subtitlesEnabled: Bool?
    var subtitleStyle: String?
    var targetDuration: TimeInterval?
    var instruction: String?

    var hasChanges: Bool { request.hasChanges }

    var request: StoryEditRequest {
        StoryEditRequest(
            removeMomentIDs: Array(removedMomentIDs).sorted(),
            removeMediaIDs: Array(removedMediaIDs).sorted(),
            addMediaIDs: Array(addedMediaIDs).sorted(),
            replace: replacements,
            analyses: Array(analysesByMediaID.values).sorted { $0.assetID < $1.assetID },
            style: style,
            subtitlesEnabled: subtitlesEnabled,
            subtitleStyle: subtitleStyle,
            targetDuration: targetDuration,
            instruction: instruction?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == true ? nil : instruction
        )
    }

    mutating func remove(_ moment: StoryMomentPresentation) {
        removedMomentIDs.insert(moment.id)
        addedMediaIDs.remove(moment.assetID)
    }

    mutating func add(_ moment: StoryMomentPresentation) {
        addedMediaIDs.insert(moment.assetID)
        removedMediaIDs.remove(moment.assetID)
    }

    mutating func add(_ imported: [ImportedStoryMedia]) {
        for item in imported {
            addedMediaIDs.insert(item.mediaID)
            removedMediaIDs.remove(item.mediaID)
            if let analysis = item.analysis {
                analysesByMediaID[item.mediaID] = analysis
            }
        }
    }

    mutating func replace(_ source: StoryMomentPresentation, with replacement: StoryMomentPresentation) {
        remove(source)
        addedMediaIDs.insert(replacement.assetID)
        replacements.append(StoryMomentReplacement(fromMomentID: source.id, fromMediaID: source.assetID, toMediaID: replacement.assetID))
    }

    mutating func addAnalysis(_ imported: [ImportedStoryMedia]) {
        for item in imported {
            if let analysis = item.analysis {
                analysesByMediaID[item.mediaID] = analysis
            }
        }
    }
}

private struct StoryMomentRow: View {
    let moment: StoryMomentPresentation
    let image: Image?
    let action: () -> Void

    var body: some View {
        Button {
            FrameMioHaptics.secondaryAction()
            action()
        } label: {
            HStack(spacing: FrameMioSpacing.md) {
                MediaThumbnail(
                    image: image,
                    duration: moment.duration,
                    isLivePhoto: moment.kind == .livePhoto,
                    aspectRatio: 1,
                    placeholderSystemImage: moment.kind.placeholderName,
                    accessibilityTitle: moment.title
                )
                .frame(width: 72, height: 72)
                .overlay(alignment: .center) {
                    if moment.kind == .video {
                        Image(systemName: moment.kind.iconName)
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(FrameMioColors.inverseInk)
                            .frame(width: 30, height: 30)
                            .background(FrameMioColors.mediaScrim, in: Circle())
                            .accessibilityHidden(true)
                    }
                }

                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text(moment.title)
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                        .lineLimit(2)
                    Text(moment.description ?? moment.subtitle)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .lineLimit(2)
                }
                .frame(maxWidth: .infinity, alignment: .leading)

                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(FrameMioColors.tertiaryInk)
                    .accessibilityHidden(true)
            }
            .padding(FrameMioSpacing.sm)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous)
                    .stroke(FrameMioColors.divider.opacity(0.42), lineWidth: FrameMioMetrics.borderWidth)
            }
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .accessibilityLabel(moment.title)
    }
}

private struct EditMomentSheet: View {
    let moment: StoryMomentPresentation
    let image: Image?
    let isPerformingAction: Bool
    let onRemove: () -> Void
    let onReplace: () -> Void
    let onKeep: () -> Void

    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                MediaThumbnail(
                    image: image,
                    duration: moment.duration,
                    isLivePhoto: moment.kind == .livePhoto,
                    aspectRatio: 1,
                    placeholderSystemImage: moment.kind.placeholderName,
                    accessibilityTitle: moment.title
                )
                .frame(maxWidth: 260)
                .frame(maxWidth: .infinity)

                VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                    Text(moment.title)
                        .font(FrameMioTypography.title)
                        .foregroundStyle(FrameMioColors.ink)
                    Text(moment.description ?? "FrameMio used this moment in your story.")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }

                Spacer(minLength: FrameMioSpacing.md)

                VStack(spacing: FrameMioSpacing.sm) {
                    SecondaryButton(title: "Replace Moment", systemImage: "arrow.triangle.2.circlepath", action: onReplace, isDisabled: isPerformingAction)
                    Button(role: .destructive, action: onRemove) {
                        Label("Remove from Story", systemImage: "minus.circle")
                            .font(FrameMioTypography.headline)
                            .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.compactButtonHeight)
                    }
                    .disabled(isPerformingAction)
                    SecondaryButton(title: "Keep", systemImage: "checkmark", action: onKeep, isDisabled: isPerformingAction)
                }
            }
            .padding(FrameMioSpacing.lg)
            .background(FrameMioColors.canvas.ignoresSafeArea())
            .navigationTitle(moment.title)
            .navigationBarTitleDisplayMode(.inline)
        }
    }
}

private struct AddMomentSelectionSheet: View {
    let title: String
    let moments: [StoryMomentPresentation]
    let allowsMultipleSelection: Bool
    let imageProvider: (String) -> Image?
    let onAppearMoment: (String) -> Void
    let onImportPhotos: ([PhotosPickerItem]) async throws -> [StoryMomentPresentation]
    let onAdd: ([StoryMomentPresentation]) -> Void
    let onCancel: () -> Void

    init(
        title: String = "Add to Story",
        moments: [StoryMomentPresentation],
        allowsMultipleSelection: Bool = true,
        imageProvider: @escaping (String) -> Image?,
        onAppearMoment: @escaping (String) -> Void,
        onImportPhotos: @escaping ([PhotosPickerItem]) async throws -> [StoryMomentPresentation],
        onAdd: @escaping ([StoryMomentPresentation]) -> Void,
        onCancel: @escaping () -> Void
    ) {
        self.title = title
        self.moments = moments
        self.allowsMultipleSelection = allowsMultipleSelection
        self.imageProvider = imageProvider
        self.onAppearMoment = onAppearMoment
        self.onImportPhotos = onImportPhotos
        self.onAdd = onAdd
        self.onCancel = onCancel
    }

    @State private var selectedAssetIDs = Set<String>()
    @State private var importedMoments: [StoryMomentPresentation] = []
    @State private var selectedPhotoItems: [PhotosPickerItem] = []
    @State private var filter = PhotoLibraryMediaFilter.all
    @State private var isImportingPhotos = false
    @State private var importErrorMessage: String?

    private var allMoments: [StoryMomentPresentation] {
        var seen = Set<String>()
        return (moments + importedMoments).filter { seen.insert($0.assetID).inserted }
    }

    private var displayedMoments: [StoryMomentPresentation] {
        switch filter {
        case .all:
            return allMoments
        case .photos:
            return allMoments.filter { $0.kind == .photo || $0.kind == .livePhoto }
        case .videos:
            return allMoments.filter { $0.kind == .video }
        }
    }

    private var selectedMoments: [StoryMomentPresentation] {
        allMoments.filter { selectedAssetIDs.contains($0.assetID) }
    }

    private var selectedCount: Int {
        selectedAssetIDs.count
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                    intro
                    filterPicker
                    sourceMediaGrid
                    photosImportSection
                }
                .padding(FrameMioSpacing.lg)
            }
            .safeAreaInset(edge: .bottom) {
                addAction
            }
            .background(FrameMioColors.canvas.ignoresSafeArea())
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", action: onCancel)
                }
            }
            .onChange(of: selectedPhotoItems) { _, items in
                importPhotos(items)
            }
        }
    }

    @ViewBuilder
    private var sourceMediaGrid: some View {
        if allMoments.isEmpty {
            EmptyState(
                systemImage: "photo.on.rectangle.angled",
                title: "No unused project media yet",
                message: "Choose Add from Photos to import new photos or videos into this story."
            )
            .frame(maxWidth: .infinity)
        } else if displayedMoments.isEmpty {
            EmptyState(
                systemImage: filter == .videos ? "video.slash" : "photo.on.rectangle.angled",
                title: "Nothing in this filter",
                message: "Try another media filter or import from Photos."
            )
            .frame(maxWidth: .infinity)
        } else {
            LazyVGrid(columns: gridColumns, spacing: FrameMioSpacing.xxs) {
                ForEach(displayedMoments) { moment in
                    mediaCell(for: moment)
                }
            }
        }
    }

    private var intro: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Text(title == "Replace Moment" ? "Choose a replacement" : "Choose more source media")
                .font(FrameMioTypography.title)
                .foregroundStyle(FrameMioColors.ink)
            Text("Select unused project media or import new photos and videos. FrameMio will decide the best placement and pacing.")
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var filterPicker: some View {
        Picker(
            "Media type",
            selection: Binding(
                get: { filter },
                set: { filter = $0 }
            )
        ) {
            ForEach(PhotoLibraryMediaFilter.allCases, id: \.self) { filter in
                Text(filter.title).tag(filter)
            }
        }
        .pickerStyle(.segmented)
        .tint(FrameMioColors.accent)
        .padding(FrameMioSpacing.xs)
        .background(FrameMioColors.surface.opacity(0.72), in: RoundedRectangle(cornerRadius: FrameMioRadius.pill, style: .continuous))
        .accessibilityLabel("Filter media")
    }

    private var photosImportSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Divider().overlay(FrameMioColors.divider.opacity(0.5))
            PhotosPicker(
                selection: $selectedPhotoItems,
                maxSelectionCount: allowsMultipleSelection ? 20 : 1,
                matching: .any(of: [.images, .videos]),
                preferredItemEncoding: .current
            ) {
                Label("Add from Photos", systemImage: "plus.circle.fill")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.accent)
                    .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.compactButtonHeight, alignment: .leading)
            }
            .disabled(isImportingPhotos)

            if isImportingPhotos {
                Label("Uploading and preparing new media…", systemImage: "arrow.up.circle")
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }
            if let importErrorMessage {
                Text(importErrorMessage)
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(.red)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private func mediaCell(for moment: StoryMomentPresentation) -> some View {
        MediaThumbnail(
            image: imageProvider(moment.assetID),
            duration: moment.kind == .video ? moment.duration : nil,
            isLivePhoto: moment.kind == .livePhoto,
            isSelected: selectedAssetIDs.contains(moment.assetID),
            aspectRatio: FrameMioMetrics.thumbnailAspectRatio,
            placeholderSystemImage: moment.kind.placeholderName,
            accessibilityTitle: moment.title,
            action: { toggle(moment) }
        )
        .onAppear { onAppearMoment(moment.assetID) }
    }

    private var addAction: some View {
        VStack(spacing: FrameMioSpacing.xs) {
            Text(selectionSummary)
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)

            PrimaryButton(title: addButtonTitle, systemImage: "checkmark") {
                onAdd(selectedMoments)
            }
            .disabled(selectedAssetIDs.isEmpty || isImportingPhotos)
        }
        .padding(.horizontal, FrameMioSpacing.lg)
        .padding(.top, FrameMioSpacing.md)
        .padding(.bottom, FrameMioSpacing.md)
        .background(.ultraThinMaterial)
        .overlay(alignment: .top) {
            Rectangle()
                .fill(FrameMioColors.divider.opacity(0.5))
                .frame(height: FrameMioMetrics.borderWidth)
        }
    }

    private var selectionSummary: String {
        switch selectedCount {
        case 0: allowsMultipleSelection ? "Select project media or import from Photos" : "Select a replacement"
        case 1: allowsMultipleSelection ? "1 moment selected" : "Replacement selected"
        default: "\(selectedCount) moments selected"
        }
    }

    private var addButtonTitle: String {
        if !allowsMultipleSelection { return "Replace Moment" }
        switch selectedCount {
        case 0: return "Add Moments"
        case 1: return "Add 1 Moment"
        default: return "Add \(selectedCount) Moments"
        }
    }

    private var gridColumns: [GridItem] {
        Array(
            repeating: GridItem(.flexible(), spacing: FrameMioSpacing.xxs),
            count: FrameMioMetrics.mediaGridColumnCount
        )
    }

    private func toggle(_ moment: StoryMomentPresentation) {
        if selectedAssetIDs.contains(moment.assetID) {
            selectedAssetIDs.remove(moment.assetID)
        } else {
            if !allowsMultipleSelection {
                selectedAssetIDs.removeAll()
            }
            selectedAssetIDs.insert(moment.assetID)
        }
        FrameMioHaptics.selection()
    }

    private func importPhotos(_ items: [PhotosPickerItem]) {
        guard !items.isEmpty, !isImportingPhotos else { return }
        isImportingPhotos = true
        importErrorMessage = nil
        Task {
            do {
                let imported = try await onImportPhotos(items)
                importedMoments.append(contentsOf: imported)
                if !allowsMultipleSelection {
                    selectedAssetIDs.removeAll()
                }
                for moment in imported {
                    selectedAssetIDs.insert(moment.assetID)
                    onAppearMoment(moment.assetID)
                }
                selectedPhotoItems = []
                isImportingPhotos = false
            } catch is CancellationError {
                selectedPhotoItems = []
                isImportingPhotos = false
            } catch {
                selectedPhotoItems = []
                importErrorMessage = AppError.from(error).localizedDescription
                isImportingPhotos = false
            }
        }
    }
}

private struct FullScreenStoryVideo: Identifiable {
    let id = UUID()
    let url: URL
}

private struct FullScreenStoryVideoPlayer: View {
    @Environment(\.dismiss) private var dismiss
    let url: URL

    var body: some View {
        ZStack(alignment: .topTrailing) {
            Color.black
                .ignoresSafeArea()

            StoryVideoPlayer(url: url, autoplays: true, accessibilityTitle: "Fullscreen story")
                .aspectRatio(9.0 / 16.0, contentMode: .fit)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.black)

            Button {
                FrameMioHaptics.secondaryAction()
                dismiss()
            } label: {
                Image(systemName: "xmark")
                    .font(.headline.weight(.semibold))
                    .foregroundStyle(.white)
                    .frame(width: FrameMioMetrics.minimumTouchTarget, height: FrameMioMetrics.minimumTouchTarget)
                    .background(.black.opacity(0.56), in: Circle())
                    .overlay {
                        Circle().stroke(.white.opacity(0.18), lineWidth: FrameMioMetrics.borderWidth)
                    }
            }
            .buttonStyle(FrameMioPressButtonStyle())
            .padding(FrameMioSpacing.lg)
            .accessibilityLabel("Close fullscreen story")
        }
        .background(Color.black)
    }
}


private extension StoryStatus {
    var shouldPoll: Bool {
        switch self {
        case .queued, .analyzing, .rendering:
            true
        case .ready, .failed:
            false
        }
    }
}
