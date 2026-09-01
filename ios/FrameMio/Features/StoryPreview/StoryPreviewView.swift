import AVFoundation
import UIKit
import PhotosUI
import SwiftUI
import UniformTypeIdentifiers

private func storyPreviewUIDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][StoryPreviewUI] \(message())")
#endif
}

struct StoryPreviewView: View {
    @Environment(\.dismiss) private var dismiss
    @StateObject private var viewModel: StoryPreviewViewModel
    @State private var fullScreenVideo: FullScreenStoryVideo?
    @State private var selectedMoment: StoryMomentPresentation?
    @State private var isShowingEditStory = false
    @State private var isShowingDeleteConfirmation = false

    private let generatedVideoAspectRatio: CGFloat = 9.0 / 16.0
    private let storyDescriptionMaxLength = 180
    private let onOpenPricing: (PricingEntryPoint) -> Void

    init(viewModel: StoryPreviewViewModel, onOpenPricing: @escaping (PricingEntryPoint) -> Void) {
        _viewModel = StateObject(wrappedValue: viewModel)
        self.onOpenPricing = onOpenPricing
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
        .task {
            viewModel.load()
            viewModel.refreshCreditBalance()
        }
        .onChange(of: viewModel.didDeleteStory) { _, didDelete in
            if didDelete {
                dismiss()
            }
        }
        .onChange(of: viewModel.insufficientCredits) { _, issue in
            guard issue != nil else { return }
            viewModel.insufficientCredits = nil
            onOpenPricing(.insufficientCredits)
        }
        .fullScreenCover(item: $fullScreenVideo) { video in
            FullScreenStoryVideoPlayer(url: video.url)
        }
        .sheet(item: $selectedMoment) { moment in
            MomentDetailSheet(
                moment: moment,
                image: viewModel.image(forStoryAssetID: moment.assetID),
                videoURLProvider: { assetID in
                    await viewModel.playableMomentVideoURL(forStoryAssetID: assetID)
                },
                isPerformingAction: viewModel.isPerformingAction,
                onRemove: {
                    viewModel.removeMoment(assetID: moment.assetID)
                    selectedMoment = nil
                },
                onKeep: { selectedMoment = nil }
            )
            .presentationDetents([.large])
            .presentationDragIndicator(.visible)
        }
        .fullScreenCover(isPresented: $isShowingEditStory) {
            if let story = viewModel.story {
                EditStorySheet(
                    story: story,
                    moments: momentPresentations(for: story),
                    availableMoments: availableMomentPresentations(for: story),
                    thumbnailRevision: viewModel.thumbnailRevision,
                    activeAction: viewModel.activeAction,
                    isPerformingAction: viewModel.isPerformingAction,
                    creditBalance: viewModel.creditBalance,
                    imageProvider: { viewModel.image(forStoryAssetID: $0) },
                    videoURLProvider: { assetID in
                        await viewModel.playableMomentVideoURL(forStoryAssetID: assetID)
                    },
                    onAppearMoment: { assetID in
                        viewModel.loadThumbnail(
                            forStoryAssetID: assetID,
                            targetSize: CGSize(width: 220, height: 220)
                        )
                    },
                    onImportPhotos: { items, progress, excludedSourceIDs in
                        try await viewModel.importMediaForEdit(items, progress: progress, excludedSourceIDs: excludedSourceIDs)
                    },
                    onRegenerate: { request in
                        if request.hasChanges {
                            viewModel.saveStoryEdits(request)
                        } else {
                            viewModel.regenerateStory()
                        }
                        isShowingEditStory = false
                    },
                    onRetry: {
                        viewModel.retryProcessing()
                        isShowingEditStory = false
                    }
                )
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

                progressDetail
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
                progressDetail
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

                Text(displayStoryDescription(description))
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
        .disabled(viewModel.activeAction == .deleteStory)
        .accessibilityLabel("Remove story")
        .accessibilityHint("Removes this story from FrameMio.")
    }

    private var progressDetail: some View {
        Group {
            if let detail = viewModel.progressDetail {
                Text(detail)
                    .font(FrameMioTypography.caption)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityLabel(detail)
            }
        }
    }

    private var processingSteps: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
            processingStep("Media prepared", isActive: viewModel.progressValue >= 0.28)
            processingStep("Highlights found", isActive: viewModel.progressValue >= 0.55)
            processingStep("Story written", isActive: viewModel.progressValue >= 0.72)
            processingStep("Film rendered", isActive: viewModel.progressValue >= 0.88)
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
                    duration: kind == .video ? asset?.duration : nil,
                    captureDate: metadata.creationDate ?? asset?.creationDate,
                    sourceAssetID: metadata.sourceAssetID ?? asset?.localIdentifier
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
                duration: kind == .video ? asset?.duration : nil,
                captureDate: metadata?.creationDate ?? asset?.creationDate,
                sourceAssetID: metadata?.sourceAssetID ?? asset?.localIdentifier
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
                    duration: kind == .video ? asset?.duration : nil,
                    captureDate: metadata.creationDate ?? asset?.creationDate,
                    sourceAssetID: metadata.sourceAssetID ?? asset?.localIdentifier
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
                duration: kind == .video ? asset?.duration : nil,
                captureDate: metadata?.creationDate ?? asset?.creationDate,
                sourceAssetID: metadata?.sourceAssetID ?? asset?.localIdentifier
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

    private func displayStoryDescription(_ value: String) -> String {
        guard value.count > storyDescriptionMaxLength else {
            return value
        }
        return String(value.prefix(storyDescriptionMaxLength))
            .trimmingCharacters(in: CharacterSet.whitespacesAndNewlines.union(CharacterSet(charactersIn: ".…")))
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
    let captureDate: Date?
    let sourceAssetID: String?
    let isImportedFromPhotos: Bool

    var sourceIdentityKey: String {
        if let source = sourceAssetID?.trimmingCharacters(in: .whitespacesAndNewlines), !source.isEmpty {
            return source
        }
        return assetID
    }

    init(
        id: String,
        assetID: String,
        title: String,
        subtitle: String,
        description: String?,
        kind: Kind,
        duration: TimeInterval?,
        captureDate: Date? = nil,
        sourceAssetID: String? = nil,
        isImportedFromPhotos: Bool = false
    ) {
        self.id = id
        self.assetID = assetID
        self.title = title
        self.subtitle = subtitle
        self.description = description
        self.kind = kind
        self.duration = duration
        self.captureDate = captureDate
        self.sourceAssetID = sourceAssetID
        self.isImportedFromPhotos = isImportedFromPhotos
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
            duration: kind == .video ? imported.sourceAsset.duration : nil,
            captureDate: imported.sourceAsset.creationDate,
            sourceAssetID: imported.sourceAsset.localIdentifier ?? imported.sourceAsset.id,
            isImportedFromPhotos: true
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
                .frame(width: 132, height: 132)
                .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.media, style: .continuous))
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
            .frame(width: 132, height: 196, alignment: .topLeading)
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .accessibilityLabel(moment.title)
        .accessibilityHint("Opens moment details")
    }
}

private struct MomentDetailSheet: View {
    let moment: StoryMomentPresentation
    let image: Image?
    let videoURLProvider: (String) async -> URL?
    let isPerformingAction: Bool
    let onRemove: () -> Void
    let onKeep: () -> Void

    @State private var fullScreenVideo: FullScreenStoryVideo?
    @State private var sourceVideoURL: URL?
    @State private var isLoadingVideo = false
    @State private var videoErrorMessage: String?

    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                momentPreview
                    .task(id: moment.assetID) {
                        await loadSourceVideoIfNeeded()
                    }

                if let videoErrorMessage {
                    Text(videoErrorMessage)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(.red)
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
            .fullScreenCover(item: $fullScreenVideo) { video in
                FullScreenStoryVideoPlayer(url: video.url)
            }
        }
    }

    private var momentPreview: some View {
        let previewWidth = max(0, UIScreen.main.bounds.width - FrameMioSpacing.lg * 2)
        let previewHeight = previewWidth / FrameMioMetrics.thumbnailAspectRatio

        return ZStack {
            if moment.kind == .video, let sourceVideoURL {
                AspectFillVideoPreview(url: sourceVideoURL)
                    .frame(width: previewWidth, height: previewHeight)
                    .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.media, style: .continuous))
                    .overlay {
                        RoundedRectangle(cornerRadius: FrameMioRadius.media, style: .continuous)
                            .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
                    }
            } else {
                MediaThumbnail(
                    image: image,
                    duration: moment.duration,
                    isLivePhoto: moment.kind == .livePhoto,
                    aspectRatio: FrameMioMetrics.thumbnailAspectRatio,
                    placeholderSystemImage: moment.kind.placeholderName,
                    accessibilityTitle: moment.title
                )
                .frame(width: previewWidth, height: previewHeight)
            }

            if moment.kind == .video {
                Button(action: playVideo) {
                    ZStack {
                        Circle()
                            .fill(FrameMioColors.mediaScrim.opacity(0.78))
                            .frame(width: 54, height: 54)
                        if isLoadingVideo {
                            ProgressView()
                                .tint(FrameMioColors.inverseInk)
                        } else {
                            Image(systemName: "play.fill")
                                .font(.title3.weight(.bold))
                                .foregroundStyle(FrameMioColors.inverseInk)
                                .offset(x: 2)
                        }
                    }
                }
                .buttonStyle(.plain)
                .disabled(isLoadingVideo)
                .accessibilityLabel("Play moment video")
            }
        }
        .frame(width: previewWidth, height: previewHeight)
        .frame(maxWidth: .infinity, alignment: .center)
    }

    private func playVideo() {
        guard moment.kind == .video, !isLoadingVideo else { return }
        if let sourceVideoURL {
            fullScreenVideo = FullScreenStoryVideo(url: sourceVideoURL)
            return
        }
        isLoadingVideo = true
        videoErrorMessage = nil
        Task {
            let videoURL = await videoURLProvider(moment.assetID)
            isLoadingVideo = false
            if let videoURL {
                sourceVideoURL = videoURL
                fullScreenVideo = FullScreenStoryVideo(url: videoURL)
            } else {
                videoErrorMessage = "This source video isn't available on this device."
            }
        }
    }

    private func loadSourceVideoIfNeeded() async {
        guard moment.kind == .video, sourceVideoURL == nil else { return }
        sourceVideoURL = await videoURLProvider(moment.assetID)
    }
}

private struct EditStorySheet: View {
    @Environment(\.dismiss) private var dismiss
    let story: Story
    let moments: [StoryMomentPresentation]
    let availableMoments: [StoryMomentPresentation]
    let thumbnailRevision: Int
    let activeAction: StoryPreviewAction?
    let isPerformingAction: Bool
    let creditBalance: Int?
    let imageProvider: (String) -> Image?
    let videoURLProvider: (String) async -> URL?
    let onAppearMoment: (String) -> Void
    let onImportPhotos: ([PhotosPickerItem], StoryImportProgressHandler?, Set<String>) async throws -> [ImportedStoryMedia]
    let onRegenerate: (StoryEditRequest) -> Void
    let onRetry: () -> Void

    @State private var draft = EditStoryDraft()
    @State private var selectedMoment: StoryMomentPresentation?
    @State private var replacingMoment: StoryMomentPresentation?
    @State private var isShowingAddMoment = false
    @State private var importedMoments: [StoryMomentPresentation] = []
    @State private var draggedMomentAssetID: String?

    private let styleOptions = ["Natural", "Cinematic", "Warm", "Playful"]
    private let subtitleStyleOptions = ["Bold", "Cinematic", "Minimal", "Playful"]

    private var lengthOptions: [Int] {
        Array(Set([15, max(8, Int(story.duration.rounded())), 30])).sorted()
    }

    private var displayedMoments: [StoryMomentPresentation] {
        let removed = draft.removedMomentIDs.union(draft.removedMediaIDs)
        let replacementSources = Set(draft.replacements.compactMap { $0.fromMomentID } + draft.replacements.compactMap { $0.fromMediaID })
        let baseCandidates = moments.filter { !removed.contains($0.id) && !removed.contains($0.assetID) && !replacementSources.contains($0.id) && !replacementSources.contains($0.assetID) }
        var visibleSourceKeys = Set<String>()
        let base = baseCandidates.filter { moment in
            let inserted = visibleSourceKeys.insert(moment.sourceIdentityKey).inserted
            if !inserted {
                storyPreviewUIDebugLog("edit sheet hiding duplicate visible moment media=\(moment.assetID) source=\(moment.sourceIdentityKey)")
            }
            return inserted
        }
        let existingIDs = Set(base.map(\.assetID))
        let addableMoments = availableMoments + importedMoments
        let added = addableMoments.filter { moment in
            draft.addedMediaIDs.contains(moment.assetID)
                && !existingIDs.contains(moment.assetID)
                && visibleSourceKeys.insert(moment.sourceIdentityKey).inserted
        }
        guard !draft.orderedMediaIDs.isEmpty else {
            return momentsWithChronologicalAdditions(base: base, added: added)
        }
        return draft.ordered(base + added)
    }

    private var visibleMediaIDs: Set<String> {
        Set(displayedMoments.map(\.assetID))
    }

    private var visibleSourceKeys: Set<String> {
        Set(displayedMoments.map(\.sourceIdentityKey))
    }

    private var selectableMoments: [StoryMomentPresentation] {
        (availableMoments + importedMoments).filter { moment in
            !visibleMediaIDs.contains(moment.assetID) && !visibleSourceKeys.contains(moment.sourceIdentityKey)
        }
    }

    private func newImportedMomentPresentations(from imported: [ImportedStoryMedia]) -> (presentations: [StoryMomentPresentation], media: [ImportedStoryMedia]) {
        var existingSourceKeys = Set((displayedMoments + importedMoments).map(\.sourceIdentityKey))
        var presentations: [StoryMomentPresentation] = []
        var media: [ImportedStoryMedia] = []
        presentations.reserveCapacity(imported.count)
        media.reserveCapacity(imported.count)
        for item in imported {
            let presentation = StoryMomentPresentation(imported: item, index: importedMoments.count + presentations.count)
            guard existingSourceKeys.insert(presentation.sourceIdentityKey).inserted else {
                storyPreviewUIDebugLog("edit sheet ignored duplicate imported source=\(presentation.sourceIdentityKey) media=\(presentation.assetID)")
                continue
            }
            presentations.append(presentation)
            media.append(item)
        }
        return (presentations, media)
    }

    private func momentsWithChronologicalAdditions(base: [StoryMomentPresentation], added: [StoryMomentPresentation]) -> [StoryMomentPresentation] {
        guard !added.isEmpty else { return base }
        var result = base
        for moment in added.sorted(by: momentTimestampPrecedes) {
            guard let captureDate = moment.captureDate else {
                result.append(moment)
                continue
            }
            let insertionIndex = result.firstIndex { existing in
                guard let existingDate = existing.captureDate else { return false }
                return existingDate > captureDate
            } ?? result.count
            result.insert(moment, at: insertionIndex)
        }
        return result
    }

    private func momentTimestampPrecedes(_ left: StoryMomentPresentation, _ right: StoryMomentPresentation) -> Bool {
        switch (left.captureDate, right.captureDate) {
        case let (.some(leftDate), .some(rightDate)) where leftDate != rightDate:
            return leftDate < rightDate
        case (.some, .none):
            return true
        case (.none, .some):
            return false
        default:
            return left.assetID < right.assetID
        }
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
                ToolbarItem(placement: .topBarLeading) {
                    Button {
                        dismiss()
                    } label: {
                        Label("Back", systemImage: "chevron.left")
                    }
                    .disabled(isPerformingAction)
                }
            }
            .sheet(item: $selectedMoment) { moment in
                EditMomentSheet(
                    moment: moment,
                    image: imageProvider(moment.assetID),
                    videoURLProvider: videoURLProvider,
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
                .presentationDetents([.large])
                .presentationDragIndicator(.visible)
            }
            .sheet(isPresented: $isShowingAddMoment) {
                AddMomentSelectionSheet(
                    moments: selectableMoments,
                    excludedSourceIDs: visibleSourceKeys,
                    thumbnailRevision: thumbnailRevision,
                    imageProvider: imageProvider,
                    onAppearMoment: onAppearMoment,
                    onImportPhotos: { items, progress, excludedSourceIDs in
                        let imported = try await onImportPhotos(items, progress, excludedSourceIDs)
                        let filtered = newImportedMomentPresentations(from: imported)
                        importedMoments.append(contentsOf: filtered.presentations)
                        draft.addAnalysis(filtered.media)
                        return filtered.presentations
                    },
                    onRemoveImported: { moment in
                        importedMoments.removeAll { $0.assetID == moment.assetID }
                        draft.removeImported(moment)
                    },
                    onAdd: { selectedMoments in
                        storyPreviewUIDebugLog("edit sheet add selected=\(selectedMoments.map(\.assetID).joined(separator: ",")) sources=\(selectedMoments.map(\.sourceIdentityKey).joined(separator: ",")) before=\(displayedMoments.map(\.assetID).joined(separator: ","))")
                        draft.add(selectedMoments, currentMoments: displayedMoments)
                        storyPreviewUIDebugLog("edit sheet add draft added=\(draft.addedMediaIDs.sorted().joined(separator: ",")) ordered=\(draft.orderedMediaIDs.joined(separator: ",")) after=\(displayedMoments.map(\.assetID).joined(separator: ","))")
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
                    excludedSourceIDs: visibleSourceKeys,
                    thumbnailRevision: thumbnailRevision,
                    imageProvider: imageProvider,
                    onAppearMoment: onAppearMoment,
                    onImportPhotos: { items, progress, excludedSourceIDs in
                        let imported = try await onImportPhotos(items, progress, excludedSourceIDs)
                        let filtered = newImportedMomentPresentations(from: imported)
                        importedMoments.append(contentsOf: filtered.presentations)
                        draft.addAnalysis(filtered.media)
                        return filtered.presentations
                    },
                    onRemoveImported: { moment in
                        importedMoments.removeAll { $0.assetID == moment.assetID }
                        draft.removeImported(moment)
                    },
                    onAdd: { selectedMoments in
                        if let replacement = selectedMoments.first {
                            draft.replace(source, with: replacement, currentMoments: displayedMoments)
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
            SectionHeader(title: "Moments", subtitle: draft.hasChanges ? "Unsaved changes" : "Drag to reorder")
            if displayedMoments.isEmpty {
                Text("Choose a few moments and FrameMio will shape them into a new version.")
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            } else {
                LazyVStack(spacing: FrameMioSpacing.sm) {
                    ForEach(displayedMoments) { moment in
                        StoryMomentRow(moment: moment, image: imageProvider(moment.assetID), showsReorderHandle: displayedMoments.count > 1) {
                            selectedMoment = moment
                        }
                        .onAppear { onAppearMoment(moment.assetID) }
                        .onDrag {
                            draggedMomentAssetID = moment.assetID
                            FrameMioHaptics.selection()
                            return NSItemProvider(object: moment.assetID as NSString)
                        }
                        .onDrop(
                            of: [UTType.text],
                            delegate: StoryMomentDropDelegate(
                                target: moment,
                                draft: $draft,
                                displayedMoments: displayedMoments,
                                draggedMomentAssetID: $draggedMomentAssetID
                            )
                        )
                    }
                }
                Text("Drag moments to suggest the story order. FrameMio still handles pacing and transitions.")
                    .font(FrameMioTypography.caption)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }
        }
    }

    private var addMomentButton: some View {
        HStack {
            Spacer(minLength: 0)
            Button {
                FrameMioHaptics.secondaryAction()
                isShowingAddMoment = true
            } label: {
                Label("Add Moment (\(CreditPricing.addMoment) credits)", systemImage: "plus.circle.fill")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.accent)
                    .padding(.horizontal, FrameMioSpacing.lg)
                    .frame(minHeight: FrameMioMetrics.compactButtonHeight)
                    .background(FrameMioColors.surface.opacity(0.72), in: Capsule())
                    .overlay {
                        Capsule()
                            .stroke(FrameMioColors.accent.opacity(0.42), lineWidth: FrameMioMetrics.borderWidth)
                    }
            }
            .buttonStyle(FrameMioPressButtonStyle())
            .disabled(isPerformingAction)
            .accessibilityLabel("Add moment")
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity)
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
            Text("This will use \(CreditPricing.regenerateStory) credits.")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)
            if let creditBalance {
                Text("Current balance: \(creditBalance)")
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }
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
            PrimaryButton(title: "Regenerate Story (\(CreditPricing.regenerateStory) credits)", systemImage: "sparkles") {
                storyPreviewUIDebugLog("edit sheet regenerate tapped has_changes=\(draft.request.hasChanges) added=\(draft.addedMediaIDs.sorted().joined(separator: ",")) ordered=\(draft.orderedMediaIDs.joined(separator: ",")) visible=\(displayedMoments.map(\.assetID).joined(separator: ","))")
                onRegenerate(draft.request)
            }
            .disabled(isPerformingAction)
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
    var orderedMediaIDs: [String] = []
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
            orderedMediaIDs: orderedMediaIDs,
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
        orderedMediaIDs.removeAll { $0 == moment.assetID }
    }

    mutating func add(_ moments: [StoryMomentPresentation], currentMoments: [StoryMomentPresentation]) {
        guard !moments.isEmpty else { return }
        var nextMoments = orderedMediaIDs.isEmpty ? currentMoments : ordered(currentMoments)
        for moment in moments {
            addedMediaIDs.insert(moment.assetID)
            removedMediaIDs.remove(moment.assetID)
            if !nextMoments.contains(where: { $0.assetID == moment.assetID }) {
                insertMomentByTimestamp(moment, into: &nextMoments)
            }
        }
        orderedMediaIDs = nextMoments.map(\.assetID)
    }

    mutating func add(_ imported: [ImportedStoryMedia]) {
        for item in imported {
            addedMediaIDs.insert(item.mediaID)
            removedMediaIDs.remove(item.mediaID)
            appendToCustomOrderIfNeeded(item.mediaID)
            if let analysis = item.analysis {
                analysesByMediaID[item.mediaID] = analysis
            }
        }
    }

    mutating func replace(_ source: StoryMomentPresentation, with replacement: StoryMomentPresentation, currentMoments: [StoryMomentPresentation]) {
        if orderedMediaIDs.isEmpty {
            orderedMediaIDs = currentMoments.map(\.assetID)
        }
        let sourceOrderIndex = orderedMediaIDs.firstIndex(of: source.assetID)
        remove(source)
        addedMediaIDs.insert(replacement.assetID)
        removedMediaIDs.remove(replacement.assetID)
        if let sourceOrderIndex, !orderedMediaIDs.contains(replacement.assetID) {
            orderedMediaIDs.insert(replacement.assetID, at: min(sourceOrderIndex, orderedMediaIDs.count))
        } else {
            appendToCustomOrderIfNeeded(replacement.assetID)
        }
        replacements.append(StoryMomentReplacement(fromMomentID: source.id, fromMediaID: source.assetID, toMediaID: replacement.assetID))
    }

    mutating func addAnalysis(_ imported: [ImportedStoryMedia]) {
        for item in imported {
            if let analysis = item.analysis {
                analysesByMediaID[item.mediaID] = analysis
            }
        }
    }

    mutating func removeImported(_ moment: StoryMomentPresentation) {
        addedMediaIDs.remove(moment.assetID)
        analysesByMediaID.removeValue(forKey: moment.assetID)
        replacements.removeAll { $0.toMediaID == moment.assetID }
        orderedMediaIDs.removeAll { $0 == moment.assetID }
    }

    func ordered(_ moments: [StoryMomentPresentation]) -> [StoryMomentPresentation] {
        guard !orderedMediaIDs.isEmpty else { return moments }
        let order = Dictionary(uniqueKeysWithValues: orderedMediaIDs.enumerated().map { ($0.element, $0.offset) })
        return moments.enumerated().sorted { left, right in
            let leftOrder = order[left.element.assetID]
            let rightOrder = order[right.element.assetID]
            switch (leftOrder, rightOrder) {
            case let (.some(leftIndex), .some(rightIndex)):
                return leftIndex < rightIndex
            case (.some, .none):
                return true
            case (.none, .some):
                return false
            case (.none, .none):
                return left.offset < right.offset
            }
        }.map(\.element)
    }

    mutating func moveMoment(draggedAssetID: String, before targetAssetID: String, currentMoments: [StoryMomentPresentation]) {
        guard draggedAssetID != targetAssetID else { return }
        var assetIDs = currentMoments.map(\.assetID)
        guard let sourceIndex = assetIDs.firstIndex(of: draggedAssetID), let targetIndex = assetIDs.firstIndex(of: targetAssetID) else { return }
        let moved = assetIDs.remove(at: sourceIndex)
        assetIDs.insert(moved, at: targetIndex)
        orderedMediaIDs = assetIDs
    }

    private mutating func appendToCustomOrderIfNeeded(_ mediaID: String) {
        guard !orderedMediaIDs.isEmpty, !orderedMediaIDs.contains(mediaID) else { return }
        orderedMediaIDs.append(mediaID)
    }

    private func insertMomentByTimestamp(_ moment: StoryMomentPresentation, into moments: inout [StoryMomentPresentation]) {
        guard let captureDate = moment.captureDate else {
            moments.append(moment)
            return
        }
        let insertionIndex = moments.firstIndex { existing in
            guard let existingDate = existing.captureDate else { return false }
            return existingDate > captureDate
        } ?? moments.count
        moments.insert(moment, at: insertionIndex)
    }
}

private struct StoryMomentDropDelegate: DropDelegate {
    let target: StoryMomentPresentation
    @Binding var draft: EditStoryDraft
    let displayedMoments: [StoryMomentPresentation]
    @Binding var draggedMomentAssetID: String?

    func validateDrop(info: DropInfo) -> Bool {
        draggedMomentAssetID != nil
    }

    func dropEntered(info: DropInfo) {
        guard let draggedMomentAssetID else { return }
        draft.moveMoment(draggedAssetID: draggedMomentAssetID, before: target.assetID, currentMoments: displayedMoments)
    }

    func dropUpdated(info: DropInfo) -> DropProposal? {
        DropProposal(operation: .move)
    }

    func performDrop(info: DropInfo) -> Bool {
        draggedMomentAssetID = nil
        return true
    }
}

private struct StoryMomentRow: View {
    let moment: StoryMomentPresentation
    let image: Image?
    var showsReorderHandle = false
    let action: () -> Void

    var body: some View {
        Button {
            FrameMioHaptics.secondaryAction()
            action()
        } label: {
            HStack(spacing: FrameMioSpacing.md) {
                if showsReorderHandle {
                    Image(systemName: "line.3.horizontal")
                        .font(.body.weight(.semibold))
                        .foregroundStyle(FrameMioColors.tertiaryInk)
                        .accessibilityHidden(true)
                }

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
        .accessibilityHint(showsReorderHandle ? "Double tap to edit. Drag to suggest where this moment belongs." : "Double tap to edit this moment.")
    }
}

private struct EditMomentSheet: View {
    let moment: StoryMomentPresentation
    let image: Image?
    let videoURLProvider: (String) async -> URL?
    let isPerformingAction: Bool
    let onRemove: () -> Void
    let onReplace: () -> Void
    let onKeep: () -> Void

    @State private var fullScreenVideo: FullScreenStoryVideo?
    @State private var sourceVideoURL: URL?
    @State private var isLoadingVideo = false
    @State private var videoErrorMessage: String?

    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                momentPreview
                    .task(id: moment.assetID) {
                        await loadSourceVideoIfNeeded()
                    }

                if let videoErrorMessage {
                    Text(videoErrorMessage)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(.red)
                }

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
            .fullScreenCover(item: $fullScreenVideo) { video in
                FullScreenStoryVideoPlayer(url: video.url)
            }
        }
    }

    private var momentPreview: some View {
        let previewWidth = max(0, UIScreen.main.bounds.width - FrameMioSpacing.lg * 2)
        let previewHeight = previewWidth / FrameMioMetrics.thumbnailAspectRatio

        return ZStack {
            if moment.kind == .video, let sourceVideoURL {
                AspectFillVideoPreview(url: sourceVideoURL)
                    .frame(width: previewWidth, height: previewHeight)
                    .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.media, style: .continuous))
                    .overlay {
                        RoundedRectangle(cornerRadius: FrameMioRadius.media, style: .continuous)
                            .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
                    }
            } else {
                MediaThumbnail(
                    image: image,
                    duration: moment.duration,
                    isLivePhoto: moment.kind == .livePhoto,
                    aspectRatio: FrameMioMetrics.thumbnailAspectRatio,
                    placeholderSystemImage: moment.kind.placeholderName,
                    accessibilityTitle: moment.title
                )
                .frame(width: previewWidth, height: previewHeight)
            }

            if moment.kind == .video {
                Button(action: playVideo) {
                    ZStack {
                        Circle()
                            .fill(FrameMioColors.mediaScrim.opacity(0.78))
                            .frame(width: 54, height: 54)
                        if isLoadingVideo {
                            ProgressView()
                                .tint(FrameMioColors.inverseInk)
                        } else {
                            Image(systemName: "play.fill")
                                .font(.title3.weight(.bold))
                                .foregroundStyle(FrameMioColors.inverseInk)
                                .offset(x: 2)
                        }
                    }
                }
                .buttonStyle(.plain)
                .disabled(isLoadingVideo)
                .accessibilityLabel("Play moment video")
            }
        }
        .frame(width: previewWidth, height: previewHeight)
        .frame(maxWidth: .infinity, alignment: .center)
    }

    private func playVideo() {
        guard moment.kind == .video, !isLoadingVideo else { return }
        if let sourceVideoURL {
            fullScreenVideo = FullScreenStoryVideo(url: sourceVideoURL)
            return
        }
        isLoadingVideo = true
        videoErrorMessage = nil
        Task {
            let videoURL = await videoURLProvider(moment.assetID)
            isLoadingVideo = false
            if let videoURL {
                sourceVideoURL = videoURL
                fullScreenVideo = FullScreenStoryVideo(url: videoURL)
            } else {
                videoErrorMessage = "This source video isn't available on this device."
            }
        }
    }

    private func loadSourceVideoIfNeeded() async {
        guard moment.kind == .video, sourceVideoURL == nil else { return }
        sourceVideoURL = await videoURLProvider(moment.assetID)
    }
}

private struct AspectFillVideoPreview: UIViewRepresentable {
    let url: URL

    func makeUIView(context: Context) -> AspectFillVideoPreviewView {
        let view = AspectFillVideoPreviewView()
        view.update(url: url)
        return view
    }

    func updateUIView(_ uiView: AspectFillVideoPreviewView, context: Context) {
        uiView.update(url: url)
    }

    final class AspectFillVideoPreviewView: UIView {
        private let player = AVPlayer()
        private var currentURL: URL?

        override class var layerClass: AnyClass { AVPlayerLayer.self }

        private var playerLayer: AVPlayerLayer {
            layer as! AVPlayerLayer
        }

        override init(frame: CGRect) {
            super.init(frame: frame)
            player.isMuted = true
            playerLayer.player = player
            playerLayer.videoGravity = .resizeAspectFill
        }

        required init?(coder: NSCoder) {
            fatalError("init(coder:) has not been implemented")
        }

        func update(url: URL) {
            guard currentURL != url else { return }
            currentURL = url
            let item = AVPlayerItem(url: url)
            player.replaceCurrentItem(with: item)
            player.pause()
            player.seek(to: CMTime(seconds: 0.1, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
        }
    }
}

private struct AddMomentSelectionSheet: View {
    let title: String
    let moments: [StoryMomentPresentation]
    let allowsMultipleSelection: Bool
    let excludedSourceIDs: Set<String>
    let thumbnailRevision: Int
    let imageProvider: (String) -> Image?
    let onAppearMoment: (String) -> Void
    let onImportPhotos: ([PhotosPickerItem], StoryImportProgressHandler?, Set<String>) async throws -> [StoryMomentPresentation]
    let onRemoveImported: (StoryMomentPresentation) -> Void
    let onAdd: ([StoryMomentPresentation]) -> Void
    let onCancel: () -> Void

    init(
        title: String = "Add to Story",
        moments: [StoryMomentPresentation],
        allowsMultipleSelection: Bool = true,
        excludedSourceIDs: Set<String>,
        thumbnailRevision: Int,
        imageProvider: @escaping (String) -> Image?,
        onAppearMoment: @escaping (String) -> Void,
        onImportPhotos: @escaping ([PhotosPickerItem], StoryImportProgressHandler?, Set<String>) async throws -> [StoryMomentPresentation],
        onRemoveImported: @escaping (StoryMomentPresentation) -> Void,
        onAdd: @escaping ([StoryMomentPresentation]) -> Void,
        onCancel: @escaping () -> Void
    ) {
        self.title = title
        self.moments = moments
        self.allowsMultipleSelection = allowsMultipleSelection
        self.excludedSourceIDs = excludedSourceIDs
        self.thumbnailRevision = thumbnailRevision
        self.imageProvider = imageProvider
        self.onAppearMoment = onAppearMoment
        self.onImportPhotos = onImportPhotos
        self.onRemoveImported = onRemoveImported
        self.onAdd = onAdd
        self.onCancel = onCancel
    }

    @State private var selectedAssetIDs = Set<String>()
    @State private var importedMoments: [StoryMomentPresentation] = []
    @State private var selectedPhotoItems: [PhotosPickerItem] = []
    @State private var filter = PhotoLibraryMediaFilter.all
    @State private var isImportingPhotos = false
    @State private var importProgress: Double?
    @State private var importStatusMessage = "Preparing media…"
    @State private var importErrorMessage: String?

    private var allMoments: [StoryMomentPresentation] {
        var seen = Set<String>()
        let merged = (moments + importedMoments).filter { seen.insert($0.sourceIdentityKey).inserted }
        return merged.sorted(by: momentPrecedes)
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

    private func momentPrecedes(_ left: StoryMomentPresentation, _ right: StoryMomentPresentation) -> Bool {
        switch (left.captureDate, right.captureDate) {
        case let (.some(leftDate), .some(rightDate)) where leftDate != rightDate:
            return leftDate < rightDate
        case (.some, .none):
            return true
        case (.none, .some):
            return false
        default:
            return left.assetID < right.assetID
        }
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
            if isImportingPhotos {
                importProgressView
            } else {
                PhotosPicker(
                    selection: $selectedPhotoItems,
                    maxSelectionCount: allowsMultipleSelection ? 20 : 1,
                    matching: .any(of: [.images, .videos]),
                    preferredItemEncoding: .current
                ) {
                    HStack(spacing: FrameMioSpacing.sm) {
                        Image(systemName: "plus.circle.fill")
                        Text("Add from Photos")
                    }
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.accent)
                    .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.compactButtonHeight, alignment: .leading)
                }
                .accessibilityLabel("Add from Photos")
            }

            if let importErrorMessage {
                Text(importErrorMessage)
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(.red)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var importProgressView: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
            HStack(spacing: FrameMioSpacing.xs) {
                ProgressView()
                    .controlSize(.small)
                    .tint(FrameMioColors.accent)
                Text(importStatusMessage)
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                Spacer()
                if let importProgress {
                    Text(importProgress, format: .percent.precision(.fractionLength(0)))
                        .font(FrameMioTypography.caption)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }
            }

            if let importProgress {
                ProgressView(value: importProgress)
                    .tint(FrameMioColors.accent)
                    .accessibilityValue(importProgress.formatted(.percent.precision(.fractionLength(0))))
            } else {
                ProgressView()
                    .tint(FrameMioColors.accent)
            }
        }
        .padding(FrameMioSpacing.sm)
        .background(FrameMioColors.surface.opacity(0.72), in: RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous))
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Adding from Photos")
    }

    private func mediaCell(for moment: StoryMomentPresentation) -> some View {
        let _ = thumbnailRevision
        return ZStack(alignment: .topTrailing) {
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

        }
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
        case 0: return "Add Moment (\(CreditPricing.addMoment) credits)"
        case 1: return "Add Moment (\(CreditPricing.addMoment) credits)"
        default: return "Add \(selectedCount) Moments (\(CreditPricing.addMoment) credits)"
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

    private func removeImported(_ moment: StoryMomentPresentation) {
        importedMoments.removeAll { $0.assetID == moment.assetID }
        selectedAssetIDs.remove(moment.assetID)
        selectedPhotoItems.removeAll()
        onRemoveImported(moment)
        FrameMioHaptics.secondaryAction()
        storyPreviewUIDebugLog("add sheet removed imported media=\(moment.assetID) imported_total=\(importedMoments.count) selected=\(selectedAssetIDs.count)")
    }

    private func importPhotos(_ items: [PhotosPickerItem]) {
        storyPreviewUIDebugLog("add sheet picker changed items=\(items.count) importing=\(isImportingPhotos) current_imported=\(importedMoments.count) selected=\(selectedAssetIDs.count)")
        guard !items.isEmpty, !isImportingPhotos else {
            storyPreviewUIDebugLog("add sheet picker ignored empty=\(items.isEmpty) importing=\(isImportingPhotos)")
            return
        }
        isImportingPhotos = true
        importProgress = 0.04
        importStatusMessage = "Reading your selection…"
        importErrorMessage = nil
        Task {
            do {
                let imported = try await onImportPhotos(items, { value, message in
                    importProgress = value
                    importStatusMessage = message
                }, excludedSourceIDs)
                storyPreviewUIDebugLog("add sheet import returned count=\(imported.count) ids=\(imported.map(\.assetID).joined(separator: ","))")
                guard !imported.isEmpty else {
                    importErrorMessage = "Those items are already in this story."
                    selectedPhotoItems = []
                    importProgress = nil
                    importStatusMessage = "Preparing media…"
                    isImportingPhotos = false
                    storyPreviewUIDebugLog("add sheet import skipped all duplicates")
                    return
                }
                importedMoments.append(contentsOf: imported)
                if !allowsMultipleSelection {
                    selectedAssetIDs.removeAll()
                }
                for moment in imported {
                    selectedAssetIDs.insert(moment.assetID)
                    onAppearMoment(moment.assetID)
                }
                selectedPhotoItems = []
                importProgress = nil
                importStatusMessage = "Preparing media…"
                isImportingPhotos = false
                storyPreviewUIDebugLog("add sheet import applied imported_total=\(importedMoments.count) selected=\(selectedAssetIDs.count)")
            } catch is CancellationError {
                selectedPhotoItems = []
                importProgress = nil
                importStatusMessage = "Preparing media…"
                isImportingPhotos = false
                storyPreviewUIDebugLog("add sheet import cancelled")
            } catch {
                selectedPhotoItems = []
                importProgress = nil
                importStatusMessage = "Preparing media…"
                importErrorMessage = AppError.from(error).localizedDescription
                isImportingPhotos = false
                storyPreviewUIDebugLog("add sheet import failed error=\(importErrorMessage ?? error.localizedDescription)")
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
