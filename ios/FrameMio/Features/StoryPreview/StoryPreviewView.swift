import SwiftUI

struct StoryPreviewView: View {
    @StateObject private var viewModel: StoryPreviewViewModel

    init(viewModel: StoryPreviewViewModel) {
        _viewModel = StateObject(wrappedValue: viewModel)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.section) {
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
        .background {
            LinearGradient(
                colors: [FrameMioColors.canvas, FrameMioColors.accentMuted.opacity(0.26)],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )
            .ignoresSafeArea()
        }
        .navigationTitle("Your Story")
        .navigationBarTitleDisplayMode(.inline)
        .task { viewModel.load() }
        .alert("Couldn’t update story", isPresented: errorBinding) {
            Button("Dismiss", role: .cancel) { viewModel.errorMessage = nil }
        } message: {
            Text(viewModel.errorMessage ?? "")
        }
    }

    private var initialLoadingProgress: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            StoryWaitingAnimation()

            FrameMioProgressView(value: viewModel.progressValue, title: viewModel.progressTitle)

            Text("Your story is being assembled securely in the background.")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity)
        }
        .frame(maxWidth: FrameMioMetrics.loadingStateMaxWidth)
        .padding(FrameMioSpacing.xl)
        .background(
            FrameMioColors.surface.opacity(0.78),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.subtle)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private func storyContent(_ story: Story) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xl) {
            previewHero(story)
            storySummary(story)
            actionPanel(story)
            selectedMoments(story)
        }
    }

    private func previewHero(_ story: Story) -> some View {
        ZStack(alignment: .bottomLeading) {
            if let videoURL = story.videoURL, story.status == .ready {
                // The render finished and the server returned a playable URL, so
                // this is the final film and autoplay is the whole point.
                StoryVideoPlayer(url: videoURL, autoplays: true)
                    .aspectRatio(FrameMioMetrics.storyAspectRatio, contentMode: .fit)
                    .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
            } else {
                // Still rendering or failed, so show the decorative gradient
                // placeholder with progress overlay.
                heroPlaceholder(story)
            }
        }
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.inverseInk.opacity(0.2), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.floating)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Story preview, \(story.status.rawValue)")
    }

    private func heroPlaceholder(_ story: Story) -> some View {
        ZStack(alignment: .bottomLeading) {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .fill(
                    LinearGradient(
                        colors: [FrameMioColors.memoryLight, FrameMioColors.accent, FrameMioColors.memoryEarth],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    )
                )
                .aspectRatio(FrameMioMetrics.storyAspectRatio, contentMode: .fit)

            Circle()
                .fill(FrameMioColors.inverseInk.opacity(0.18))
                .frame(width: 170, height: 170)
                .offset(x: -44, y: -70)
                .accessibilityHidden(true)

            Circle()
                .fill(FrameMioColors.ink.opacity(0.16))
                .frame(width: 220, height: 220)
                .offset(x: 168, y: 92)
                .accessibilityHidden(true)

            VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                Image(systemName: heroPlaceholderIcon(for: story.status))
                    .font(FrameMioTypography.memorySymbol)
                    .foregroundStyle(FrameMioColors.inverseInk)
                    .frame(width: 76, height: 76)
                    .background(FrameMioColors.mediaScrim, in: Circle())
                    .accessibilityHidden(true)

                Text(viewModel.progressTitle)
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.inverseInk)
            }
            .padding(FrameMioSpacing.xl)
        }
    }

    private func heroPlaceholderIcon(for status: StoryStatus) -> String {
        switch status {
        case .failed:
            "exclamationmark.triangle.fill"
        case .ready:
            "play.fill"
        case .queued, .analyzing, .rendering:
            "sparkles"
        }
    }

    private func storySummary(_ story: Story) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Tag(story.context.rawValue, systemImage: story.context.icon)
            Text(story.title)
                .font(FrameMioTypography.display)
                .foregroundStyle(FrameMioColors.ink)
                .fixedSize(horizontal: false, vertical: true)
            Text("\(story.assetIDs.count) moments · \(Int(story.duration.rounded())) seconds")
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)

            switch story.status {
            case .queued, .analyzing, .rendering:
                StoryWaitingAnimation()
                    .padding(.top, FrameMioSpacing.xs)

                FrameMioProgressView(value: viewModel.progressValue, title: viewModel.progressTitle)
                Text("Your story is finished outside the app, so the interface stays calm and responsive.")
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            case .failed:
                Label(
                    "Rendering failed. Tap Retry, or choose different media and create a new story.",
                    systemImage: "exclamationmark.triangle.fill"
                )
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            case .ready:
                EmptyView()
            }
        }
        .padding(FrameMioSpacing.xl)
        .background(
            FrameMioColors.surface.opacity(0.82),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.subtle)
    }

    private func actionPanel(_ story: Story) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Make it yours", subtitle: "Small changes without a timeline")

            VStack(spacing: FrameMioSpacing.sm) {
                if story.status == .failed {
                    PrimaryButton(title: actionTitle(.retry, fallback: "Retry"), systemImage: "arrow.clockwise") {
                        viewModel.retryProcessing()
                    }
                    .disabled(viewModel.isPerformingAction)
                }

                SecondaryButton(title: actionTitle(.regenerate, fallback: "Regenerate Story"), systemImage: "sparkles") {
                    viewModel.regenerateStory()
                }
                .disabled(viewModel.isPerformingAction)

                SecondaryButton(title: actionTitle(.renderAgain, fallback: "Render Again"), systemImage: "film") {
                    viewModel.renderAgain()
                }
                .disabled(viewModel.isPerformingAction || story.status == .failed)

                if let videoURL = story.videoURL {
                    ShareLink(item: videoURL) {
                        shareLabel
                    }
                    .accessibilityLabel("Share Story")
                }
            }
        }
        .padding(FrameMioSpacing.lg)
        .background(
            FrameMioColors.surface.opacity(0.82),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.subtle)
    }

    private func selectedMoments(_ story: Story) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Selected moments", subtitle: "Replace or remove a moment")

            if story.assetIDs.isEmpty {
                Text("No moments are selected for this story.")
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            } else {
                VStack(spacing: FrameMioSpacing.sm) {
                    ForEach(Array(story.assetIDs.enumerated()), id: \.element) { index, assetID in
                        momentRow(index: index, assetID: assetID)
                    }
                }
            }
        }
        .padding(FrameMioSpacing.lg)
        .background(
            FrameMioColors.surface.opacity(0.78),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.subtle)
    }

    private func momentRow(index: Int, assetID: String) -> some View {
        HStack(spacing: FrameMioSpacing.md) {
            MediaThumbnail(
                duration: nil,
                aspectRatio: 1,
                placeholderSystemImage: "photo.fill",
                accessibilityTitle: "Moment \(index + 1)"
            )
            .frame(width: 72)

            VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                Text("Moment \(index + 1)")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.ink)
                Text("Used in your story")
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }

            Spacer(minLength: FrameMioSpacing.sm)

            Menu {
                Button("Replace Moment") {
                    FrameMioHaptics.secondaryAction()
                    viewModel.replaceMoment(assetID: assetID)
                }
                Button("Remove Moment", role: .destructive) {
                    FrameMioHaptics.secondaryAction()
                    viewModel.removeMoment(assetID: assetID)
                }
            } label: {
                Image(systemName: "ellipsis")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.ink)
                    .frame(width: FrameMioMetrics.minimumTouchTarget, height: FrameMioMetrics.minimumTouchTarget)
                    .background(FrameMioColors.surface.opacity(0.78), in: Circle())
                    .overlay {
                        Circle().stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
                    }
            }
            .disabled(viewModel.isPerformingAction)
            .accessibilityLabel("Edit moment \(index + 1)")
        }
        .padding(FrameMioSpacing.sm)
        .background(
            FrameMioColors.elevatedSurface.opacity(0.86),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
    }

    private func actionTitle(_ action: StoryPreviewAction, fallback: String) -> String {
        viewModel.activeAction == action ? action.title : fallback
    }

    private var shareLabel: some View {
        HStack(spacing: FrameMioSpacing.xs) {
            Image(systemName: "square.and.arrow.up")
                .font(FrameMioTypography.headline)
                .accessibilityHidden(true)
            Text("Share Story")
                .font(FrameMioTypography.headline)
        }
        .foregroundStyle(FrameMioColors.ink)
        .padding(.horizontal, FrameMioSpacing.lg)
        .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.compactButtonHeight)
        .background(
            FrameMioColors.surface.opacity(0.78),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
    }

    private var errorBinding: Binding<Bool> {
        Binding(
            get: { viewModel.errorMessage != nil },
            set: { if !$0 { viewModel.errorMessage = nil } }
        )
    }
}
