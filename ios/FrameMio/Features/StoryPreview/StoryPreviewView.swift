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
    private let onOpenPricing: (PricingEntryPoint) -> Void

    private var editorConfiguration: VideoProjectEditorConfiguration {
        viewModel.editorConfiguration
    }

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
                        title: "\(editorConfiguration.terminology.projectName) unavailable",
                        message: viewModel.errorMessage ?? "This project could not be opened.",
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
        .navigationTitle("My \(editorConfiguration.terminology.projectName)")
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
                terminology: editorConfiguration.terminology,
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
                    promptTranscriptionService: viewModel.promptTranscriptionService,
                    configuration: editorConfiguration,
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
        .overlay {
            if let saveState = viewModel.saveVideoState {
                saveVideoModal(state: saveState)
            }
        }
        .alert("Couldn’t update story", isPresented: errorBinding) {
            Button("Dismiss", role: .cancel) { viewModel.errorMessage = nil }
        } message: {
            Text(viewModel.errorMessage ?? "")
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
                Text("Creating your \(editorConfiguration.terminology.projectName.lowercased())...")
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
            Text(editorConfiguration.eyebrow)
                .font(FrameMioTypography.eyebrow)
                .foregroundStyle(FrameMioColors.accent)
                .textCase(.uppercase)
                .accessibilityHidden(true)

            Text(storyDisplayTitle(story))
                .font(FrameMioTypography.display)
                .foregroundStyle(FrameMioColors.ink)
                .lineLimit(2)
                .minimumScaleFactor(0.86)
                .truncationMode(.tail)

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
                        isPlaybackEnabled: fullScreenVideo == nil && !isShowingEditStory,
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
                .accessibilityLabel("Share \(editorConfiguration.terminology.projectName.lowercased())")

                PrimaryButton(
                    title: actionTitle(.saveVideo, fallback: "Save"),
                    systemImage: "arrow.down.circle",
                    action: { viewModel.saveVideoToPhotos() },
                    isLoading: viewModel.activeAction == .saveVideo
                )
                .disabled(viewModel.isPerformingAction)
                .accessibilityLabel("Save \(editorConfiguration.terminology.projectName.lowercased()) to Photos")
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
                Text("About this \(editorConfiguration.terminology.projectName.lowercased())")
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
            SectionHeader(title: editorConfiguration.terminology.plural, subtitle: momentSummary(for: story, moments: moments))

            if moments.isEmpty {
                Text("FrameMio will show the \(editorConfiguration.terminology.plural.lowercased()) that shaped this \(editorConfiguration.terminology.projectName.lowercased()) when they are available.")
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
        PrimaryButton(title: editorConfiguration.editButtonTitle, systemImage: "square.and.pencil") {
            guard !story.status.shouldPoll else { return }
            isShowingEditStory = true
        }
        .disabled(viewModel.isPerformingAction || story.status.shouldPoll)
        .accessibilityLabel(editorConfiguration.editButtonTitle)
        .accessibilityHint(story.status.shouldPoll ? "Editing is unavailable while FrameMio is creating this project." : "Opens project editing options.")
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
                Text(actionTitle(.deleteStory, fallback: editorConfiguration.deleteButtonTitle))
                    .font(FrameMioTypography.headline)
            }
            .foregroundStyle(Color.red)
            .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.compactButtonHeight)
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .disabled(viewModel.activeAction == .deleteStory)
        .accessibilityLabel(editorConfiguration.deleteButtonTitle)
        .accessibilityHint("Removes this project from FrameMio.")
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
            processingStep(editorConfiguration.type == .story ? "Highlights found" : "\(editorConfiguration.terminology.plural) planned", isActive: viewModel.progressValue >= 0.55)
            processingStep(editorConfiguration.type == .story ? "Story written" : "Edit planned", isActive: viewModel.progressValue >= 0.72)
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
            return "\(editorConfiguration.terminology.singular) \(index + 1)"
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
        var parts = [moments.count == 1 ? "1 \(editorConfiguration.terminology.singular.lowercased())" : "\(moments.count) \(editorConfiguration.terminology.plural.lowercased())"]
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
            "Something went wrong creating your \(editorConfiguration.terminology.projectName.lowercased())."
        case .ready:
            "Preparing your film..."
        case .queued, .analyzing, .rendering:
            "Creating your \(editorConfiguration.terminology.projectName.lowercased())..."
        }
    }

    private func placeholderMessage(for story: Story) -> String {
        switch story.status {
        case .failed:
            "Try again when you’re ready."
        case .ready:
            "The video link is almost ready."
        case .queued, .analyzing, .rendering:
            "FrameMio is finding the \(editorConfiguration.terminology.plural.lowercased()) that belong together."
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

    @ViewBuilder
    private func saveVideoModal(state: StoryPreviewSaveVideoState) -> some View {
        ZStack {
            Color.black.opacity(0.28)
                .ignoresSafeArea()

            VStack(spacing: FrameMioSpacing.lg) {
                HStack(spacing: FrameMioSpacing.md) {
                    Image(systemName: state.showsDoneButton ? "checkmark.circle.fill" : "arrow.down.circle.fill")
                        .font(.title2.weight(.semibold))
                        .foregroundStyle(FrameMioColors.accent)
                        .accessibilityHidden(true)

                    VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                        Text(state.title)
                            .font(FrameMioTypography.headline)
                            .foregroundStyle(FrameMioColors.ink)
                        Text(state.showsDoneButton ? "Your story video is ready." : "Saving your story video to Photos.")
                            .font(FrameMioTypography.footnote)
                            .foregroundStyle(FrameMioColors.secondaryInk)
                    }

                    Spacer(minLength: 0)
                }

                if state.showsDoneButton {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.title2.weight(.semibold))
                        .foregroundStyle(FrameMioColors.accent)
                        .accessibilityHidden(true)
                } else {
                    FrameMioProgressView(value: state.progress, title: "Downloading")
                }

                Text(state.message)
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                    .fixedSize(horizontal: false, vertical: true)

                if state.showsDoneButton {
                    PrimaryButton(title: "Done", systemImage: "checkmark") {
                        viewModel.dismissSaveVideoState()
                    }
                }
            }
            .padding(FrameMioSpacing.xl)
            .frame(maxWidth: 360)
            .background(
                FrameMioColors.surface.opacity(0.96),
                in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                    .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
            }
            .frameMioElevation(.subtle)
            .padding(.horizontal, FrameMioSpacing.xl)
        }
    }
}

struct StoryMomentPresentation: Identifiable, Hashable {
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
    let terminology: VideoProjectTerminology
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
                    Text(moment.description ?? "This is one of the \(terminology.plural.lowercased()) FrameMio used in your \(terminology.projectName.lowercased()).")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .fixedSize(horizontal: false, vertical: true)
                }

                Spacer(minLength: FrameMioSpacing.md)

                VStack(spacing: FrameMioSpacing.sm) {
                    Button(role: .destructive, action: onRemove) {
                        Label("Remove from \(terminology.projectName)", systemImage: "minus.circle")
                            .font(FrameMioTypography.headline)
                            .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.compactButtonHeight)
                    }
                    .disabled(isPerformingAction)
                    .accessibilityLabel("Remove \(moment.title) from \(terminology.projectName.lowercased())")

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
    let promptTranscriptionService: PromptTranscriptionService?
    let configuration: VideoProjectEditorConfiguration
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
    @StateObject private var voicePromptRecorder = VoicePromptRecorder()

    private let sceneDetailOptions: [(title: String, value: String?, subtitle: String?)] = [
        ("Balanced", nil, "Default"),
        ("More details", "moreDetails", "Extra cutaways"),
        ("Main moments", "mainMoments", "No cutaways")
    ]

    private var lengthOptions: [Int] {
        Array(Set([15, 30, 45, 60, max(8, min(60, Int(story.duration.rounded())))])).sorted()
    }

    private var hasPhotoLikeLengthSource: Bool {
        displayedMoments.contains { $0.kind != .video }
    }

    private var hasUnknownVideoLengthSource: Bool {
        displayedMoments.contains { $0.kind == .video && (($0.duration ?? 0) <= 0) }
    }

    private var totalVideoSourceDuration: TimeInterval {
        displayedMoments.reduce(0) { total, moment in
            guard moment.kind == .video else { return total }
            return total + max(0, moment.duration ?? 0)
        }
    }

    private var maxSelectableLengthSeconds: Int {
        if hasPhotoLikeLengthSource || hasUnknownVideoLengthSource {
            return 60
        }
        return max(0, Int(totalVideoSourceDuration.rounded(.down)))
    }

    private var supportsFullLengthEdit: Bool {
        totalVideoSourceDuration > 0 && !hasUnknownVideoLengthSource
    }

    private var lengthSectionSubtitle: String {
        if hasPhotoLikeLengthSource {
            return "Photos can extend the \(configuration.terminology.projectName.lowercased()) up to 60s"
        }
        if hasUnknownVideoLengthSource {
            return "FrameMio will fit the \(configuration.terminology.projectName.lowercased()) to available footage"
        }
        let seconds = maxSelectableLengthSeconds
        if seconds < 60 {
            return "Video-only \(configuration.terminology.projectName.lowercased()): up to \(seconds)s of source footage available"
        }
        return "FrameMio decides what to keep"
    }

    private func isLengthOptionDisabled(_ seconds: Int) -> Bool {
        seconds > maxSelectableLengthSeconds
    }

    private func lengthDisabledLabel(for seconds: Int) -> String? {
        guard isLengthOptionDisabled(seconds) else { return nil }
        let available = maxSelectableLengthSeconds
        if available <= 0 {
            return "Needs photo or video source"
        }
        return "Only \(available)s available"
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
                    if configuration.type == .promo, !displayedMoments.isEmpty {
                        promoSceneTimelineSection
                    }
                    timelineSection
                    addMomentButton
                    editorControlsSection
                    aiActionSection
                }
                .padding(FrameMioSpacing.lg)
            }
            .background(FrameMioColors.canvas.ignoresSafeArea())
            .overlay {
                if voicePromptRecorder.isListening {
                    voicePromptListeningOverlay
                }
            }
            .navigationTitle(configuration.title)
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
                    terminology: configuration.terminology,
                    configuration: configuration,
                    sceneDraft: Binding(
                        get: { draft.sceneEdit(for: moment.assetID) },
                        set: { draft.setSceneEdit($0, for: moment.assetID) }
                    ),
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
                    title: configuration.addSheetTitle,
                    terminology: configuration.terminology,
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
                    title: configuration.replaceSheetTitle,
                    terminology: configuration.terminology,
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
            Text("Your previous \(configuration.terminology.projectName.lowercased()) stays available. Try again when you're ready.")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)
            SecondaryButton(title: "Try Again", systemImage: "arrow.clockwise", action: onRetry)
        }
    }

    private var updatingState: some View {
        HStack(spacing: FrameMioSpacing.sm) {
            ProgressView()
            VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                Text("Creating your new \(configuration.terminology.projectName.lowercased())")
                    .font(FrameMioTypography.headline)
                Text("Finding the right rhythm and rendering your video.")
                    .font(FrameMioTypography.caption)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }
        }
        .padding(FrameMioSpacing.md)
        .background(FrameMioColors.surface.opacity(0.72), in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
    }

    private var timelineSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: configuration.terminology.plural, subtitle: draft.hasChanges(for: configuration) ? "Unsaved changes" : configuration.timelineSubtitle)
            if displayedMoments.isEmpty {
                Text("Choose a few \(configuration.terminology.plural.lowercased()) and FrameMio will shape them into a new version.")
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            } else {
                LazyVStack(spacing: FrameMioSpacing.sm) {
                    ForEach(displayedMoments) { moment in
                        StoryMomentRow(moment: moment, image: imageProvider(moment.assetID), terminology: configuration.terminology, showsReorderHandle: displayedMoments.count > 1) {
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
                Text(configuration.timelineGuidance)
                    .font(FrameMioTypography.caption)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }
        }
    }

    private var promoSceneTimelineSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Scene timeline", subtitle: "Select, reorder, and shape the promo arc")
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(alignment: .top, spacing: FrameMioSpacing.sm) {
                    ForEach(Array(displayedMoments.enumerated()), id: \.element.id) { index, moment in
                        PromoSceneTimelineCard(
                            moment: moment,
                            index: index,
                            count: displayedMoments.count,
                            purpose: draft.sceneEdit(for: moment.assetID).purpose,
                            image: imageProvider(moment.assetID),
                            canMoveLeft: index > 0,
                            canMoveRight: index < displayedMoments.count - 1,
                            isPerformingAction: isPerformingAction,
                            onSelect: { selectedMoment = moment },
                            onMoveLeft: { moveScene(at: index, by: -1) },
                            onMoveRight: { moveScene(at: index, by: 1) }
                        )
                        .onAppear { onAppearMoment(moment.assetID) }
                    }
                }
                .padding(.vertical, FrameMioSpacing.xs)
            }
        }
    }

    private func moveScene(at index: Int, by offset: Int) {
        let targetIndex = index + offset
        guard displayedMoments.indices.contains(index), displayedMoments.indices.contains(targetIndex) else { return }
        draft.moveMoment(from: index, to: targetIndex, currentMoments: displayedMoments)
    }

    private var addMomentButton: some View {
        HStack {
            Spacer(minLength: 0)
            Button {
                FrameMioHaptics.secondaryAction()
                isShowingAddMoment = true
            } label: {
                Label("\(configuration.addItemTitle) (\(CreditPricing.addMoment) credits)", systemImage: "plus.circle.fill")
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
            .accessibilityLabel(configuration.addItemTitle)
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity)
    }

    private var styleSection: some View {
        optionSection(title: "Style", options: configuration.styleOptions, selected: draft.style ?? configuration.styleOptions.first ?? "Natural") { option in
            draft.style = option.lowercased()
        }
    }

    private var editorControlsSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
            SectionHeader(title: "Project settings", subtitle: settingsSubtitle)
            styleSection
            if configuration.showsSceneDetailControl {
                editControlDivider
                sceneDetailSection
            }
            if configuration.showsSceneDurationControls {
                editControlDivider
                sceneTimingSection
            }
            editControlDivider
            subtitleSection
            if configuration.showsAudioControls {
                editControlDivider
                audioSection
            }
            if configuration.showsBrandControls {
                editControlDivider
                brandSection
            }
            if configuration.showsCTAControls {
                editControlDivider
                ctaSection
            }
            editControlDivider
            lengthSection
        }
        .padding(FrameMioSpacing.lg)
        .background(FrameMioColors.surface.opacity(0.54), in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.34), lineWidth: FrameMioMetrics.borderWidth)
        }
    }

    private var editControlDivider: some View {
        Divider()
            .overlay(FrameMioColors.divider.opacity(0.58))
    }

    private var sceneDetailSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "\(configuration.terminology.singular) details", subtitle: "Choose how much supporting footage to include")
            LazyVGrid(columns: optionGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                ForEach(sceneDetailOptions, id: \.title) { option in
                    optionChip(
                        title: option.title,
                        subtitle: option.subtitle,
                        isSelected: draft.sceneDetailStyle == option.value
                    ) {
                        draft.sceneDetailStyle = option.value
                    }
                }
            }
        }
    }

    private var subtitleSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: configuration.textSectionTitle, subtitle: configuration.textSectionSubtitle)
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
                    ForEach(configuration.subtitleStyleOptions, id: \.self) { option in
                        optionChip(title: option, isSelected: option.caseInsensitiveCompare(draft.subtitleStyle ?? "Cinematic") == .orderedSame) {
                            draft.subtitleStyle = option.lowercased()
                        }
                    }
                }
            }
        }
    }

    private var settingsSubtitle: String {
        switch configuration.type {
        case .story:
            "Style, text, audio, and length"
        case .promo:
            "Text, brand, CTA, scenes, audio, and length"
        case .reel:
            "Hook, captions, clips, audio, social layout, and length"
        }
    }

    private var sceneTimingSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "\(configuration.terminology.singular) editing", subtitle: "Describe trims or timing changes")
            TextField(sceneTimingPlaceholder, text: $draft.sceneTimingInstruction, axis: .vertical)
                .lineLimit(2...4)
                .font(FrameMioTypography.body)
                .padding(FrameMioSpacing.md)
                .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
                .accessibilityLabel("\(configuration.terminology.singular) editing")
        }
    }

    private var sceneTimingPlaceholder: String {
        switch configuration.type {
        case .story:
            "Keep the pacing natural."
        case .promo:
            "Trim the opening tighter and hold product shots longer."
        case .reel:
            "Remove slow sections and keep quick, energetic cuts."
        }
    }

    private var audioSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: configuration.audioTitle, subtitle: configuration.audioSubtitle)
            TextField(audioPlaceholder, text: $draft.audioDirection, axis: .vertical)
                .lineLimit(2...4)
                .font(FrameMioTypography.body)
                .padding(FrameMioSpacing.md)
                .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
                .accessibilityLabel(configuration.audioTitle)
        }
    }

    private var audioPlaceholder: String {
        switch configuration.type {
        case .story:
            "Use warm music and natural audio."
        case .promo:
            "Add a confident voiceover and premium background music."
        case .reel:
            "Use upbeat audio and sync cuts to the strongest beats."
        }
    }

    private var brandSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Brand", subtitle: "Logo, brand name, colors, and style")
            labeledTextField("Brand name", text: $draft.brandName)
            labeledTextField("Logo", placeholder: "Use the imported logo or describe where it should appear", text: $draft.logoInstruction)
            labeledTextField("Brand colors", placeholder: "Black, white, and electric blue", text: $draft.brandColors)
            labeledTextField("Font/style", placeholder: "Premium, clean, modern", text: $draft.brandStyle)
        }
    }

    private var ctaSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "CTA", subtitle: "Call to action text and placement")
            labeledTextField("CTA text", placeholder: "Download now", text: $draft.ctaText)
            labeledTextField("Optional URL", placeholder: "https://example.com", text: $draft.ctaURL)
            labeledTextField("CTA position/style", placeholder: "Prominent end card, bottom safe area", text: $draft.ctaStyle)
        }
    }

    private func labeledTextField(_ title: String, placeholder: String? = nil, text: Binding<String>) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
            Text(title)
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)
            TextField(placeholder ?? title, text: text, axis: .vertical)
                .lineLimit(1...3)
                .font(FrameMioTypography.body)
                .padding(FrameMioSpacing.md)
                .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
        }
    }

    private var lengthSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Length", subtitle: lengthSectionSubtitle)
            LazyVGrid(columns: optionGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                optionChip(title: "Auto", isSelected: draft.durationMode == .auto) {
                    draft.durationMode = .auto
                    draft.targetDuration = nil
                    draft.removeFillerAndPauses = false
                }
                ForEach(lengthOptions, id: \.self) { seconds in
                    let disabledReason = lengthDisabledLabel(for: seconds)
                    optionChip(
                        title: "\(seconds)s",
                        subtitle: disabledReason,
                        isSelected: draft.durationMode == .fixed && Int((draft.targetDuration ?? 0).rounded()) == seconds,
                        isDisabled: disabledReason != nil
                    ) {
                        guard disabledReason == nil else {
                            storyPreviewUIDebugLog("edit sheet length option blocked seconds=\(seconds) max_selectable=\(maxSelectableLengthSeconds) video_source=\(Int(totalVideoSourceDuration.rounded())) has_photo_like=\(hasPhotoLikeLengthSource) unknown_video=\(hasUnknownVideoLengthSource)")
                            return
                        }
                        draft.durationMode = .fixed
                        draft.targetDuration = TimeInterval(seconds)
                        draft.removeFillerAndPauses = false
                        storyPreviewUIDebugLog("edit sheet length selected seconds=\(seconds) max_selectable=\(maxSelectableLengthSeconds) video_source=\(Int(totalVideoSourceDuration.rounded())) has_photo_like=\(hasPhotoLikeLengthSource) unknown_video=\(hasUnknownVideoLengthSource)")
                    }
                }
                optionChip(
                    title: "Full length",
                    subtitle: supportsFullLengthEdit ? nil : "Needs known video length",
                    isSelected: draft.durationMode == .fullSource,
                    isDisabled: !supportsFullLengthEdit
                ) {
                    draft.durationMode = .fullSource
                    draft.targetDuration = nil
                }
            }
            if draft.durationMode == .fullSource {
                Toggle(isOn: $draft.removeFillerAndPauses) {
                    Label("Remove filler words and pauses", systemImage: "scissors")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.ink)
                }
                .tint(FrameMioColors.accent)
            }
        }
        .onAppear {
            normalizeDurationMode()
            storyPreviewUIDebugLog("edit sheet length availability max_selectable=\(maxSelectableLengthSeconds) video_source=\(Int(totalVideoSourceDuration.rounded())) has_photo_like=\(hasPhotoLikeLengthSource) unknown_video=\(hasUnknownVideoLengthSource) options=\(lengthOptions.map(String.init).joined(separator: ","))")
        }
        .onChange(of: maxSelectableLengthSeconds) { _, value in
            storyPreviewUIDebugLog("edit sheet length availability changed max_selectable=\(value) video_source=\(Int(totalVideoSourceDuration.rounded())) has_photo_like=\(hasPhotoLikeLengthSource) unknown_video=\(hasUnknownVideoLengthSource)")
            if let target = draft.targetDuration, Int(target.rounded()) > value {
                draft.targetDuration = nil
                draft.durationMode = .auto
                storyPreviewUIDebugLog("edit sheet length target cleared reason=above_available previous=\(Int(target.rounded())) max_selectable=\(value)")
            }
            normalizeDurationMode()
        }
    }

    private func normalizeDurationMode() {
        if draft.durationMode == .fullSource && !supportsFullLengthEdit {
            draft.durationMode = .auto
            draft.targetDuration = nil
        }
        if draft.durationMode != .fullSource {
            draft.removeFillerAndPauses = false
        }
    }

    private var aiActionSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: configuration.aiSectionTitle, subtitle: configuration.aiSectionSubtitle)
            Text("This will use \(draft.regenerationCreditCost(for: configuration)) credits.")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)
            if let creditBalance {
                Text("Current balance: \(creditBalance)")
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 140), spacing: FrameMioSpacing.sm)], spacing: FrameMioSpacing.sm) {
                ForEach(configuration.aiSuggestions, id: \.self) { prompt in
                    optionChip(title: prompt, isSelected: draft.instruction == prompt) {
                        draft.instruction = prompt
                    }
                }
            }
            TextField(configuration.aiPromptPlaceholder, text: Binding(
                get: { draft.instruction ?? "" },
                set: { draft.instruction = $0 }
            ), axis: .vertical)
            .lineLimit(2...4)
            .font(FrameMioTypography.body)
            .padding(FrameMioSpacing.md)
            .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
            .accessibilityLabel("AI edit instruction")
            HStack {
                Spacer()
                Button {
                    clearVoicePromptTranscript()
                } label: {
                    Label("Clear transcript", systemImage: "trash")
                        .font(FrameMioTypography.footnote)
                }
                .buttonStyle(.borderless)
                .foregroundStyle(FrameMioColors.secondaryInk)
                .disabled(isPromptClearDisabled)
            }
            voicePromptControl
                .onChange(of: voicePromptRecorder.currentTranscript) { _, transcript in
                    let trimmed = transcript.trimmingCharacters(in: .whitespacesAndNewlines)
                    if !trimmed.isEmpty {
                        draft.instruction = trimmed
                        storyPreviewUIDebugLog("voice prompt live transcript applied transcript_length=\(trimmed.count)")
                    }
            }
            PrimaryButton(title: "\(configuration.primaryAIButtonTitle) (\(draft.regenerationCreditCost(for: configuration)) credits)", systemImage: "sparkles") {
                let request = draft.request(for: configuration)
                storyPreviewUIDebugLog("edit sheet regenerate tapped type=\(configuration.type.rawValue) has_changes=\(request.hasChanges) added=\(draft.addedMediaIDs.sorted().joined(separator: ",")) ordered=\(draft.orderedMediaIDs.joined(separator: ",")) visible=\(displayedMoments.map(\.assetID).joined(separator: ",")) scene_detail=\(draft.sceneDetailStyle ?? "balanced") required_credits=\(draft.regenerationCreditCost(for: configuration))")
                Task {
                    await stopVoicePromptRecording()
                    onRegenerate(draft.request(for: configuration))
                }
            }
            .disabled(isPerformingAction || !draft.hasChanges(for: configuration))
        }
    }

    private var voicePromptControl: some View {
        VStack(alignment: .center, spacing: FrameMioSpacing.xs) {
            HStack(spacing: FrameMioSpacing.sm) {
                voicePromptButton
            }
            .frame(maxWidth: .infinity, alignment: .center)

            voicePromptStatusText
                .font(FrameMioTypography.caption)
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity, alignment: .center)
        }
        .frame(maxWidth: .infinity, alignment: .center)
    }

    private var voicePromptListeningOverlay: some View {
        VStack {
            Spacer()
            VoicePromptListeningIndicator()
                .padding(.horizontal, FrameMioSpacing.xl)
                .padding(.vertical, FrameMioSpacing.lg)
                .background(.clear)
            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .allowsHitTesting(false)
    }

    private var voicePromptButton: some View {
        Label(voicePromptButtonTitle, systemImage: voicePromptButtonIcon)
            .font(FrameMioTypography.footnote)
            .foregroundStyle(voicePromptRecorder.isListening ? .white : FrameMioColors.accent)
            .padding(.horizontal, FrameMioSpacing.lg)
            .frame(minHeight: FrameMioMetrics.compactButtonHeight)
            .background(
                voicePromptRecorder.isListening ? FrameMioColors.accent : FrameMioColors.surface.opacity(0.78),
                in: Capsule()
            )
            .overlay {
                Capsule()
                    .stroke(FrameMioColors.accent.opacity(0.35), lineWidth: FrameMioMetrics.borderWidth)
            }
            .scaleEffect(voicePromptRecorder.isListening ? 1.04 : 1)
            .opacity(voicePromptButtonDisabled ? 0.55 : 1)
            .animation(.easeInOut(duration: 0.18), value: voicePromptRecorder.isListening)
            .contentShape(Capsule())
            .onTapGesture {
                guard !voicePromptButtonDisabled else { return }
                FrameMioHaptics.secondaryAction()
                Task { await toggleVoicePromptRecording() }
            }
            .accessibilityLabel("Voice prompt")
            .accessibilityHint("Tap to start live transcription. Tap again to stop.")
    }

    @ViewBuilder
    private var voicePromptStatusText: some View {
        if promptTranscriptionService == nil {
            Text("Voice prompt is unavailable in this build.")
                .foregroundStyle(FrameMioColors.secondaryInk)
        } else if let error = voicePromptRecorder.errorMessage {
            Text(error)
                .foregroundStyle(.red)
        } else if voicePromptRecorder.didAutoStopForSilence {
            Text("Stopped after a few quiet seconds.")
                .foregroundStyle(FrameMioColors.secondaryInk)
        } else if voicePromptRecorder.isPreparing {
            Text("Connecting… tap again to stop.")
                .foregroundStyle(FrameMioColors.secondaryInk)
        } else if voicePromptRecorder.isRecording {
            Text("Listening live… auto-stops after silence.")
                .foregroundStyle(FrameMioColors.secondaryInk)
        } else {
            Text("Tap Voice Prompt and speak; the AI instruction updates live.")
                .foregroundStyle(FrameMioColors.secondaryInk)
        }
    }

    private var voicePromptButtonDisabled: Bool {
        isPerformingAction || voicePromptRecorder.isTranscribing || promptTranscriptionService == nil
    }

    private var voicePromptButtonTitle: String {
        if voicePromptRecorder.isPreparing {
            return "Connecting"
        }
        if voicePromptRecorder.isRecording {
            return "Stop Voice Prompt"
        }
        return "Voice Prompt"
    }

    private var voicePromptButtonIcon: String {
        voicePromptRecorder.isListening ? "waveform" : "mic.fill"
    }

    private var isPromptClearDisabled: Bool {
        let transcript = voicePromptRecorder.currentTranscript.trimmingCharacters(in: .whitespacesAndNewlines)
        let instruction = draft.instruction?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return transcript.isEmpty && instruction.isEmpty
    }

    @MainActor
    private func startVoicePromptRecording() async {
        guard let promptTranscriptionService else { return }
        await voicePromptRecorder.startRecording(using: promptTranscriptionService)
    }

    private func clearVoicePromptTranscript() {
        voicePromptRecorder.clearTranscript()
        if let instruction = draft.instruction?.trimmingCharacters(in: .whitespacesAndNewlines), !instruction.isEmpty {
            draft.instruction = ""
        }
    }

    @MainActor
    private func toggleVoicePromptRecording() async {
        if voicePromptRecorder.isListening {
            await stopVoicePromptRecording()
        } else {
            await startVoicePromptRecording()
        }
    }

    @MainActor
    private func stopVoicePromptRecording() async {
        await voicePromptRecorder.stopRecording()
        let transcript = voicePromptRecorder.currentTranscript.trimmingCharacters(in: .whitespacesAndNewlines)
        if !transcript.isEmpty {
            draft.instruction = transcript
            storyPreviewUIDebugLog("voice prompt applied transcript_length=\(transcript.count)")
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

    private func optionChip(title: String, subtitle: String? = nil, isSelected: Bool, isDisabled: Bool = false, action: @escaping () -> Void) -> some View {
        Button {
            guard !isDisabled else { return }
            FrameMioHaptics.secondaryAction()
            action()
        } label: {
            VStack(spacing: 2) {
                Text(title)
                    .font(FrameMioTypography.footnote)
                if let subtitle {
                    Text(subtitle)
                        .font(FrameMioTypography.caption)
                        .lineLimit(1)
                        .minimumScaleFactor(0.72)
                }
            }
            .foregroundStyle(isSelected ? FrameMioColors.inverseInk : (isDisabled ? FrameMioColors.secondaryInk : FrameMioColors.ink))
            .padding(.horizontal, FrameMioSpacing.md)
            .frame(minHeight: FrameMioMetrics.compactButtonHeight)
            .background(isSelected ? FrameMioColors.accent : FrameMioColors.surface.opacity(isDisabled ? 0.42 : 0.78), in: Capsule())
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .opacity(isDisabled ? FrameMioOpacity.disabled : 1)
        .disabled(isPerformingAction || isDisabled)
    }
}

struct VideoProjectEditorDraft: Hashable {
    var removedMomentIDs = Set<String>()
    var removedMediaIDs = Set<String>()
    var addedMediaIDs = Set<String>()
    var orderedMediaIDs: [String] = []
    var replacements: [StoryMomentReplacement] = []
    var analysesByMediaID: [String: MediaAnalysis] = [:]
    var style: String?
    var subtitlesEnabled: Bool?
    var subtitleStyle: String?
    var sceneDetailStyle: String?
    var durationMode: VideoDurationMode = .auto
    var targetDuration: TimeInterval?
    var removeFillerAndPauses = false
    var instruction: String?
    var sceneTimingInstruction = ""
    var audioDirection = ""
    var brandName = ""
    var logoInstruction = ""
    var brandColors = ""
    var brandStyle = ""
    var ctaText = ""
    var ctaURL = ""
    var ctaStyle = ""
    var sceneEditsByAssetID: [String: PromoSceneEditDraft] = [:]

    func hasChanges(for configuration: VideoProjectEditorConfiguration) -> Bool {
        request(for: configuration).hasChanges
    }

    func regenerationCreditCost(for configuration: VideoProjectEditorConfiguration) -> Int {
        request(for: configuration).regenerationCreditCost
    }

    func request(for configuration: VideoProjectEditorConfiguration) -> StoryEditRequest {
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
            sceneDetailStyle: sceneDetailStyle,
            durationMode: durationMode,
            targetDuration: targetDuration,
            removeFillerAndPauses: removeFillerAndPauses,
            instruction: composedInstruction(for: configuration)
        )
    }

    private func composedInstruction(for configuration: VideoProjectEditorConfiguration) -> String? {
        var parts: [String] = []
        append("AI instruction", instruction, to: &parts)
        append("\(configuration.terminology.singular) editing", sceneTimingInstruction, to: &parts)
        append(configuration.audioTitle, audioDirection, to: &parts)
        if configuration.showsBrandControls {
            append("Brand name", brandName, to: &parts)
            append("Logo", logoInstruction, to: &parts)
            append("Brand colors", brandColors, to: &parts)
            append("Brand font/style", brandStyle, to: &parts)
        }
        if configuration.showsCTAControls {
            append("CTA text", ctaText, to: &parts)
            append("CTA URL", ctaURL, to: &parts)
            append("CTA position/style", ctaStyle, to: &parts)
        }
        if configuration.type == .promo {
            let sceneLines = sceneEditsByAssetID
                .sorted { $0.key < $1.key }
                .compactMap { assetID, edit -> String? in
                    edit.composedInstruction(assetID: assetID)
                }
            if !sceneLines.isEmpty {
                parts.append("Promo scene edits:\n" + sceneLines.joined(separator: "\n"))
            }
        }
        guard !parts.isEmpty else { return nil }
        parts.insert("Project type: \(configuration.type.title).", at: 0)
        let text = parts.joined(separator: "\n")
        return text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : text
    }

    private func append(_ label: String, _ value: String?, to parts: inout [String]) {
        let trimmed = value?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !trimmed.isEmpty else { return }
        parts.append("\(label): \(trimmed)")
    }

    mutating func remove(_ moment: StoryMomentPresentation) {
        removedMomentIDs.insert(moment.id)
        addedMediaIDs.remove(moment.assetID)
        orderedMediaIDs.removeAll { $0 == moment.assetID }
        sceneEditsByAssetID.removeValue(forKey: moment.assetID)
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
        sceneEditsByAssetID.removeValue(forKey: moment.assetID)
    }

    func sceneEdit(for assetID: String) -> PromoSceneEditDraft {
        sceneEditsByAssetID[assetID] ?? PromoSceneEditDraft()
    }

    mutating func setSceneEdit(_ edit: PromoSceneEditDraft, for assetID: String) {
        if edit.isEmpty {
            sceneEditsByAssetID.removeValue(forKey: assetID)
        } else {
            sceneEditsByAssetID[assetID] = edit
        }
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

    mutating func moveMoment(from sourceIndex: Int, to targetIndex: Int, currentMoments: [StoryMomentPresentation]) {
        var assetIDs = currentMoments.map(\.assetID)
        guard assetIDs.indices.contains(sourceIndex), assetIDs.indices.contains(targetIndex), sourceIndex != targetIndex else { return }
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

private typealias EditStoryDraft = VideoProjectEditorDraft

struct PromoSceneEditDraft: Hashable {
    var purpose: PromoScenePurpose = .auto
    var durationSeconds: Int?
    var headline = ""
    var supportingText = ""
    var ctaText = ""
    var aiAction = ""

    var isEmpty: Bool {
        purpose == .auto
            && durationSeconds == nil
            && headline.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && supportingText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && ctaText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && aiAction.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    func composedInstruction(assetID: String) -> String? {
        var parts: [String] = ["asset \(assetID)"]
        if purpose != .auto {
            parts.append("purpose \(purpose.rawValue)")
        }
        if let durationSeconds {
            parts.append("duration \(durationSeconds)s")
        }
        append("headline", headline, to: &parts)
        append("supporting text", supportingText, to: &parts)
        append("CTA", ctaText, to: &parts)
        append("AI action", aiAction, to: &parts)
        guard parts.count > 1 else { return nil }
        return "- " + parts.joined(separator: "; ")
    }

    private func append(_ label: String, _ value: String, to parts: inout [String]) {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        parts.append("\(label) \(trimmed)")
    }
}

enum PromoScenePurpose: String, CaseIterable, Hashable {
    case auto
    case hook
    case problem
    case product
    case feature
    case benefit
    case proof
    case testimonial
    case cta
    case outro

    var title: String {
        switch self {
        case .auto: "Auto"
        case .hook: "Hook"
        case .problem: "Problem"
        case .product: "Product"
        case .feature: "Feature"
        case .benefit: "Benefit"
        case .proof: "Proof"
        case .testimonial: "Testimonial"
        case .cta: "CTA"
        case .outro: "Outro"
        }
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
    let terminology: VideoProjectTerminology
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
        .accessibilityHint(showsReorderHandle ? "Double tap to edit. Drag to suggest where this \(terminology.singular.lowercased()) belongs." : "Double tap to edit this \(terminology.singular.lowercased()).")
    }
}

private struct PromoSceneTimelineCard: View {
    let moment: StoryMomentPresentation
    let index: Int
    let count: Int
    let purpose: PromoScenePurpose
    let image: Image?
    let canMoveLeft: Bool
    let canMoveRight: Bool
    let isPerformingAction: Bool
    let onSelect: () -> Void
    let onMoveLeft: () -> Void
    let onMoveRight: () -> Void

    private var durationText: String {
        guard let duration = moment.duration, duration > 0 else { return "--" }
        return "\(Int(duration.rounded()))s"
    }

    private var purposeTitle: String {
        purpose == .auto ? suggestedPurpose.title : purpose.title
    }

    private var suggestedPurpose: PromoScenePurpose {
        if count <= 1 { return .cta }
        if index == 0 { return .hook }
        if index == count - 1 { return .cta }
        let sequence: [PromoScenePurpose] = [.product, .feature, .benefit, .proof]
        return sequence[(index - 1) % sequence.count]
    }

    var body: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
            Button(action: onSelect) {
                VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
                    MediaThumbnail(
                        image: image,
                        duration: moment.duration,
                        isLivePhoto: moment.kind == .livePhoto,
                        aspectRatio: 1,
                        placeholderSystemImage: moment.kind.placeholderName,
                        accessibilityTitle: moment.title
                    )
                    .frame(width: 104, height: 104)
                    .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
                    Text(String(format: "%02d %@", index + 1, purposeTitle))
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.ink)
                        .lineLimit(1)
                        .minimumScaleFactor(0.72)
                    Text(durationText)
                        .font(FrameMioTypography.caption)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }
                .frame(width: 116, alignment: .leading)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Scene \(index + 1), \(purposeTitle), \(durationText)")

            HStack(spacing: FrameMioSpacing.xs) {
                Button(action: onMoveLeft) {
                    Image(systemName: "chevron.left")
                        .frame(width: 34, height: 32)
                }
                .buttonStyle(.borderless)
                .disabled(isPerformingAction || !canMoveLeft)
                Button(action: onMoveRight) {
                    Image(systemName: "chevron.right")
                        .frame(width: 34, height: 32)
                }
                .buttonStyle(.borderless)
                .disabled(isPerformingAction || !canMoveRight)
            }
            .foregroundStyle(FrameMioColors.accent)
        }
        .padding(FrameMioSpacing.sm)
        .background(FrameMioColors.surface.opacity(0.72), in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.42), lineWidth: FrameMioMetrics.borderWidth)
        }
    }
}

private struct EditMomentSheet: View {
    let moment: StoryMomentPresentation
    let image: Image?
    let terminology: VideoProjectTerminology
    let configuration: VideoProjectEditorConfiguration
    @Binding var sceneDraft: PromoSceneEditDraft
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
                    Text(moment.description ?? "FrameMio used this \(terminology.singular.lowercased()) in your \(terminology.projectName.lowercased()).")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }

                if configuration.type == .promo {
                    promoSceneControls
                }

                Spacer(minLength: FrameMioSpacing.md)

                VStack(spacing: FrameMioSpacing.sm) {
                    SecondaryButton(title: "Replace \(terminology.singular)", systemImage: "arrow.triangle.2.circlepath", action: onReplace, isDisabled: isPerformingAction)
                    Button(role: .destructive, action: onRemove) {
                        Label("Remove from \(terminology.projectName)", systemImage: "minus.circle")
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

    private var promoSceneControls: some View {
        ScrollView(.vertical, showsIndicators: false) {
            VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
                SectionHeader(title: "Scene purpose", subtitle: "Helps AI shape the promo arc")
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 92), spacing: FrameMioSpacing.sm)], alignment: .leading, spacing: FrameMioSpacing.sm) {
                    ForEach(PromoScenePurpose.allCases, id: \.self) { purpose in
                        Button {
                            sceneDraft.purpose = purpose
                        } label: {
                            Text(purpose.title)
                                .font(FrameMioTypography.caption)
                                .foregroundStyle(sceneDraft.purpose == purpose ? FrameMioColors.inverseInk : FrameMioColors.ink)
                                .frame(minHeight: 34)
                                .padding(.horizontal, FrameMioSpacing.sm)
                                .background(sceneDraft.purpose == purpose ? FrameMioColors.accent : FrameMioColors.surface.opacity(0.78), in: Capsule())
                        }
                        .buttonStyle(FrameMioPressButtonStyle())
                        .disabled(isPerformingAction)
                    }
                }

                SectionHeader(title: "Duration", subtitle: "Keep creation simple; AI handles exact trims")
                HStack(spacing: FrameMioSpacing.sm) {
                    ForEach([3, 5, 8, 10], id: \.self) { seconds in
                        Button {
                            sceneDraft.durationSeconds = sceneDraft.durationSeconds == seconds ? nil : seconds
                        } label: {
                            Text("\(seconds)s")
                                .font(FrameMioTypography.footnote)
                                .foregroundStyle(sceneDraft.durationSeconds == seconds ? FrameMioColors.inverseInk : FrameMioColors.ink)
                                .frame(minHeight: 36)
                                .padding(.horizontal, FrameMioSpacing.md)
                                .background(sceneDraft.durationSeconds == seconds ? FrameMioColors.accent : FrameMioColors.surface.opacity(0.78), in: Capsule())
                        }
                        .buttonStyle(FrameMioPressButtonStyle())
                        .disabled(isPerformingAction)
                    }
                }

                SectionHeader(title: "Text overlays", subtitle: "Headline, supporting text, and optional CTA")
                promoTextField("Headline", text: $sceneDraft.headline)
                promoTextField("Supporting text", text: $sceneDraft.supportingText)
                promoTextField("CTA", text: $sceneDraft.ctaText)

                SectionHeader(title: "Scene AI", subtitle: "Modify the edit plan for this scene")
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 140), spacing: FrameMioSpacing.sm)], alignment: .leading, spacing: FrameMioSpacing.sm) {
                    ForEach(["Improve this scene.", "Find a better caption.", "Shorten this scene.", "Make this scene more persuasive."], id: \.self) { action in
                        Button {
                            sceneDraft.aiAction = action
                        } label: {
                            Text(action)
                                .font(FrameMioTypography.caption)
                                .foregroundStyle(sceneDraft.aiAction == action ? FrameMioColors.inverseInk : FrameMioColors.ink)
                                .multilineTextAlignment(.leading)
                                .frame(maxWidth: .infinity, minHeight: 38, alignment: .leading)
                                .padding(.horizontal, FrameMioSpacing.sm)
                                .background(sceneDraft.aiAction == action ? FrameMioColors.accent : FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
                        }
                        .buttonStyle(FrameMioPressButtonStyle())
                        .disabled(isPerformingAction)
                    }
                }
            }
            .padding(.vertical, FrameMioSpacing.sm)
        }
        .frame(maxHeight: 360)
    }

    private func promoTextField(_ title: String, text: Binding<String>) -> some View {
        TextField(title, text: text, axis: .vertical)
            .lineLimit(1...2)
            .font(FrameMioTypography.body)
            .padding(FrameMioSpacing.md)
            .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
            .accessibilityLabel(title)
            .disabled(isPerformingAction)
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
                .accessibilityLabel("Play \(terminology.singular.lowercased()) video")
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
    let terminology: VideoProjectTerminology
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
        terminology: VideoProjectTerminology = .forType(.story),
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
        self.terminology = terminology
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
                message: "Choose Add from Photos to import new photos or videos into this \(terminology.projectName.lowercased())."
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
            Text(title == "Replace \(terminology.singular)" ? "Choose a replacement" : "Choose more source media")
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
                    .padding(.horizontal, FrameMioSpacing.lg)
                    .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.buttonHeight, alignment: .center)
                    .overlay {
                        RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                            .stroke(FrameMioColors.accent.opacity(0.85), lineWidth: FrameMioMetrics.borderWidth)
                    }
                }
                .buttonStyle(FrameMioPressButtonStyle())
                .frame(maxWidth: .infinity, alignment: .center)
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
        case 1: allowsMultipleSelection ? "1 \(terminology.singular.lowercased()) selected" : "Replacement selected"
        default: "\(selectedCount) \(terminology.plural.lowercased()) selected"
        }
    }

    private var addButtonTitle: String {
        if !allowsMultipleSelection { return "Replace \(terminology.singular)" }
        switch selectedCount {
        case 0: return "Add \(terminology.singular) (+\(CreditPricing.addMoment) credits)"
        case 1: return "Add \(terminology.singular) (+\(CreditPricing.addMoment) credits)"
        default: return "Add \(selectedCount) \(terminology.plural) (+\(CreditPricing.addMoment) credits)"
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
                    importErrorMessage = "Those items are already in this \(terminology.projectName.lowercased())."
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

@MainActor
final class VoicePromptRecorder: ObservableObject {
    @Published private(set) var isRecording = false
    @Published private(set) var isPreparing = false
    @Published private(set) var isTranscribing = false
    @Published private(set) var currentTranscript = ""
    @Published private(set) var didAutoStopForSilence = false

    var isListening: Bool {
        isPreparing || isRecording
    }
    @Published var errorMessage: String?

    private var audioEngine: AVAudioEngine?
    private var webSocketTask: URLSessionWebSocketTask?
    private var receiveTask: Task<Void, Never>?
    private var silenceMonitorTask: Task<Void, Never>?
    private var lastVoiceInputAt = ContinuousClock.now
    private var stopRequestedAfterStart = false
    private var isEndingLiveTranscription = false
    private var finalTranscript = ""
    private var interimTranscript = ""
    private let silenceAutoStopDelay: Duration = .seconds(30)
    private let voiceActivityThreshold: Float = 0.012

    func startRecording(using service: PromptTranscriptionService) async {
        guard !isRecording, !isPreparing, !isTranscribing else { return }
        isPreparing = true
        stopRequestedAfterStart = false
        isEndingLiveTranscription = false
        errorMessage = nil
        didAutoStopForSilence = false
        currentTranscript = ""
        finalTranscript = ""
        interimTranscript = ""
        guard await requestRecordPermission() else {
            isPreparing = false
            errorMessage = "Microphone access is needed for voice prompts."
            return
        }

        do {
            let session = AVAudioSession.sharedInstance()
            try session.setCategory(.playAndRecord, mode: .spokenAudio, options: [.allowBluetooth, .defaultToSpeaker])
            try session.setActive(true)

            let engine = AVAudioEngine()
            let inputNode = engine.inputNode
            let inputFormat = inputNode.outputFormat(forBus: 0)
            let sampleRate = max(8_000, Int(inputFormat.sampleRate.rounded()))
            let channels = max(1, Int(inputFormat.channelCount))
            let configuration = try await service.livePromptConfiguration()
            let task = try makeWebSocketTask(configuration: configuration, sampleRate: sampleRate, channels: channels)
            webSocketTask = task
            task.resume()
            startReceiving(from: task)
            lastVoiceInputAt = .now
            startSilenceMonitor()

            inputNode.installTap(onBus: 0, bufferSize: 1_024, format: inputFormat) { [weak self, weak task] buffer, _ in
                guard let task, let data = Self.linearPCMData(from: buffer), !data.isEmpty else { return }
                if Self.containsVoiceActivity(in: buffer, threshold: self?.voiceActivityThreshold ?? 0.012) {
                    Task { @MainActor [weak self] in
                        self?.lastVoiceInputAt = .now
                    }
                }
                task.send(.data(data)) { error in
                    if let error {
                        Task { @MainActor [weak self] in
                            self?.handleStreamingError(error)
                        }
                    }
                }
            }
            try engine.start()
            audioEngine = engine
            isPreparing = false
            isRecording = true
            storyPreviewUIDebugLog("voice prompt live transcription start sample_rate=\(sampleRate) channels=\(channels)")
            if stopRequestedAfterStart {
                stopRequestedAfterStart = false
                await stopRecording()
        }
    } catch {
            cleanupActiveRecording()
            isPreparing = false
            stopRequestedAfterStart = false
            errorMessage = AppError.from(error).localizedDescription
            storyPreviewUIDebugLog("voice prompt live transcription start failed error=\(error.localizedDescription)")
        }
    }

    func stopRecording() async {
        if isPreparing {
            stopRequestedAfterStart = true
            return
        }
        guard isRecording || isTranscribing || audioEngine != nil || webSocketTask != nil else { return }
        isEndingLiveTranscription = true
        isRecording = false
        isPreparing = false
        isTranscribing = true
        if let webSocketTask {
            webSocketTask.send(.string("{\"type\":\"CloseStream\"}")) { _ in }
            try? await Task.sleep(nanoseconds: 500_000_000)
        }
        cleanupActiveRecording()
        isTranscribing = false
        isEndingLiveTranscription = false
    }

    private func stopRecordingForSilence() async {
        didAutoStopForSilence = true
        storyPreviewUIDebugLog("voice prompt auto-stopped after silence")
        await stopRecording()
    }

    func clearTranscript() {
        currentTranscript = ""
        finalTranscript = ""
        interimTranscript = ""
        errorMessage = nil
        didAutoStopForSilence = false
    }

    private func cleanupActiveRecording() {
        audioEngine?.inputNode.removeTap(onBus: 0)
        audioEngine?.stop()
        audioEngine = nil
        webSocketTask?.cancel(with: .normalClosure, reason: nil)
        webSocketTask = nil
        receiveTask?.cancel()
        receiveTask = nil
        silenceMonitorTask?.cancel()
        silenceMonitorTask = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    private func startSilenceMonitor() {
        silenceMonitorTask?.cancel()
        silenceMonitorTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(1))
                guard let self, self.isRecording else { continue }
                if .now - self.lastVoiceInputAt >= self.silenceAutoStopDelay {
                    await self.stopRecordingForSilence()
                    return
                }
            }
        }
    }

    private func makeWebSocketTask(
        configuration: PromptLiveTranscriptionConfiguration,
        sampleRate: Int,
        channels: Int
    ) throws -> URLSessionWebSocketTask {
        guard var components = URLComponents(url: configuration.websocketURL, resolvingAgainstBaseURL: false) else {
            throw AppError.invalidConfiguration
        }
        var items = components.queryItems ?? []
        items.append(contentsOf: [
            URLQueryItem(name: "model", value: configuration.model),
            URLQueryItem(name: "encoding", value: "linear16"),
            URLQueryItem(name: "sample_rate", value: String(sampleRate)),
            URLQueryItem(name: "channels", value: String(channels)),
            URLQueryItem(name: "interim_results", value: "true"),
            URLQueryItem(name: "smart_format", value: "true"),
            URLQueryItem(name: "endpointing", value: "300")
        ])
        if let language = configuration.language?.trimmingCharacters(in: .whitespacesAndNewlines), !language.isEmpty {
            items.append(URLQueryItem(name: "language", value: language))
        }
        components.queryItems = items
        guard let url = components.url else {
            throw AppError.invalidConfiguration
        }
        var request = URLRequest(url: url)
        let scheme = configuration.authorizationScheme?.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if scheme == "subprotocol" {
            request.setValue("token, \(configuration.accessToken)", forHTTPHeaderField: "Sec-WebSocket-Protocol")
        } else {
            let authorizationScheme = scheme?.isEmpty == false ? (configuration.authorizationScheme ?? "Bearer") : "Bearer"
            request.setValue("\(authorizationScheme) \(configuration.accessToken)", forHTTPHeaderField: "Authorization")
        }
        return URLSession.shared.webSocketTask(with: request)
    }

    private func startReceiving(from task: URLSessionWebSocketTask) {
        receiveTask?.cancel()
        receiveTask = Task { [weak self, weak task] in
            while !Task.isCancelled {
                guard let task else { return }
                do {
                    let message = try await task.receive()
                    switch message {
                    case let .string(text):
                        self?.handleLiveMessage(Data(text.utf8))
                    case let .data(data):
                        self?.handleLiveMessage(data)
                    @unknown default:
                        break
                    }
                } catch is CancellationError {
                    return
                } catch {
                    self?.handleStreamingError(error)
                    return
                }
            }
        }
    }

    private func handleLiveMessage(_ data: Data) {
        guard let message = try? JSONDecoder().decode(DeepgramLiveTranscriptionMessage.self, from: data),
              let transcript = message.channel?.alternatives.first?.transcript.trimmingCharacters(in: .whitespacesAndNewlines),
              !transcript.isEmpty else {
            return
        }
        lastVoiceInputAt = .now
        if message.isFinal == true {
            if !finalTranscript.isEmpty {
                finalTranscript += " "
            }
            finalTranscript += transcript
            interimTranscript = ""
        } else {
            interimTranscript = transcript
        }
        currentTranscript = [finalTranscript, interimTranscript]
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
            .joined(separator: " ")
    }

    private func handleStreamingError(_ error: Error) {
        guard !isEndingLiveTranscription else { return }
        guard isRecording || isTranscribing else { return }
        let errorMessageText = error.localizedDescription
        if errorMessageText.contains("Socket is not connected") || errorMessageText.contains("cancelled") {
            return
        }
        errorMessage = AppError.from(error).localizedDescription
        storyPreviewUIDebugLog("voice prompt live transcription failed error=\(errorMessageText)")
    }

    private func requestRecordPermission() async -> Bool {
        await AVAudioApplication.requestRecordPermission()
    }

    nonisolated private static func linearPCMData(from buffer: AVAudioPCMBuffer) -> Data? {
        let frameLength = Int(buffer.frameLength)
        guard frameLength > 0 else { return nil }
        let channelCount = max(1, Int(buffer.format.channelCount))
        var data = Data(capacity: frameLength * channelCount * MemoryLayout<Int16>.size)

        if let floatChannels = buffer.floatChannelData {
            for frame in 0..<frameLength {
                for channel in 0..<channelCount {
                    let sample = floatChannels[min(channel, Int(buffer.format.channelCount) - 1)][frame]
                    appendInt16Sample(sample, to: &data)
                }
            }
            return data
        }
        if let int16Channels = buffer.int16ChannelData {
            for frame in 0..<frameLength {
                for channel in 0..<channelCount {
                    var sample = int16Channels[min(channel, Int(buffer.format.channelCount) - 1)][frame].littleEndian
                    data.append(Data(bytes: &sample, count: MemoryLayout<Int16>.size))
                }
            }
            return data
        }
        return nil
    }

    nonisolated private static func containsVoiceActivity(in buffer: AVAudioPCMBuffer, threshold: Float) -> Bool {
        let frameLength = Int(buffer.frameLength)
        guard frameLength > 0 else { return false }
        let channelCount = max(1, Int(buffer.format.channelCount))
        var squareSum: Float = 0
        var sampleCount = 0

        if let floatChannels = buffer.floatChannelData {
            for channel in 0..<channelCount {
                let samples = floatChannels[min(channel, Int(buffer.format.channelCount) - 1)]
                for frame in 0..<frameLength {
                    let sample = samples[frame]
                    squareSum += sample * sample
                    sampleCount += 1
                }
            }
        } else if let int16Channels = buffer.int16ChannelData {
            for channel in 0..<channelCount {
                let samples = int16Channels[min(channel, Int(buffer.format.channelCount) - 1)]
                for frame in 0..<frameLength {
                    let sample = Float(samples[frame]) / Float(Int16.max)
                    squareSum += sample * sample
                    sampleCount += 1
                }
            }
        }

        guard sampleCount > 0 else { return false }
        return sqrt(squareSum / Float(sampleCount)) >= threshold
    }

    nonisolated private static func appendInt16Sample(_ value: Float, to data: inout Data) {
        let clamped = min(1, max(-1, value))
        var sample = Int16(clamped * Float(Int16.max)).littleEndian
        data.append(Data(bytes: &sample, count: MemoryLayout<Int16>.size))
    }
}

private struct DeepgramLiveTranscriptionMessage: Decodable {
    let type: String?
    let isFinal: Bool?
    let channel: Channel?

    enum CodingKeys: String, CodingKey {
        case type
        case isFinal = "is_final"
        case channel
    }

    struct Channel: Decodable {
        let alternatives: [Alternative]
    }

    struct Alternative: Decodable {
        let transcript: String
    }
}


struct VoicePromptListeningIndicator: View {
    @State private var isAnimating = false

    var body: some View {
        HStack(spacing: 4) {
            ForEach(0..<3, id: \.self) { index in
                Capsule()
                    .fill(FrameMioColors.accent.opacity(0.78))
                    .frame(width: 4, height: isAnimating ? 18 : 8)
                    .animation(
                        .easeInOut(duration: 0.42)
                            .repeatForever(autoreverses: true)
                            .delay(Double(index) * 0.12),
                        value: isAnimating
                    )
            }
        }
        .frame(height: 22)
        .onAppear { isAnimating = true }
        .onDisappear { isAnimating = false }
        .accessibilityHidden(true)
    }
}
