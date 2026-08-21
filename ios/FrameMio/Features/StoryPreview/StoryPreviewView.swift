import SwiftUI

struct StoryPreviewView: View {
    @ObservedObject private var viewModel: StoryPreviewViewModel

    init(viewModel: StoryPreviewViewModel) {
        _viewModel = ObservedObject(wrappedValue: viewModel)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.section) {
                if viewModel.isLoading && viewModel.story == nil {
                    LoadingState(title: "Preparing your memories...", message: "Your story is being assembled securely in the background.")
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
        .background(FrameMioColors.canvas)
        .navigationTitle("Your Story")
        .navigationBarTitleDisplayMode(.inline)
        .task { viewModel.load() }
        .alert("Couldn’t update story", isPresented: errorBinding) {
            Button("Dismiss", role: .cancel) { viewModel.errorMessage = nil }
        } message: {
            Text(viewModel.errorMessage ?? "")
        }
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
        ZStack {
            RoundedRectangle(cornerRadius: FrameMioRadius.media, style: .continuous)
                .fill(FrameMioColors.memoryEarth)
                .aspectRatio(FrameMioMetrics.storyAspectRatio, contentMode: .fit)
                .overlay(alignment: .bottomLeading) {
                    LinearGradient(
                        colors: [.clear, FrameMioColors.mediaScrim],
                        startPoint: .center,
                        endPoint: .bottom
                    )
                    .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.media, style: .continuous))
                }

            VStack(spacing: FrameMioSpacing.sm) {
                Image(systemName: story.status == .ready ? "play.circle.fill" : "sparkles")
                    .font(FrameMioTypography.memorySymbol)
                    .foregroundStyle(FrameMioColors.inverseInk)
                    .accessibilityHidden(true)
                Text(viewModel.progressTitle)
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.inverseInk)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Story preview, \(story.status.rawValue)")
    }

    private func storySummary(_ story: Story) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Tag(story.context.rawValue, systemImage: story.context.icon)
            Text(story.title)
                .font(FrameMioTypography.display)
                .foregroundStyle(FrameMioColors.ink)
            Text("\(story.assetIDs.count) moments · \(Int(story.duration.rounded())) seconds")
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)

            if story.status != .ready {
                FrameMioProgressView(value: nil, title: viewModel.progressTitle)
                Text("Your story is finished outside the app, so the interface stays calm and responsive.")
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }
        }
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
                    .background(FrameMioColors.surface, in: Circle())
                    .overlay {
                        Circle().stroke(FrameMioColors.divider, lineWidth: FrameMioMetrics.borderWidth)
                    }
            }
            .disabled(viewModel.isPerformingAction)
            .accessibilityLabel("Edit moment \(index + 1)")
        }
        .padding(FrameMioSpacing.sm)
        .background(
            FrameMioColors.elevatedSurface,
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider, lineWidth: FrameMioMetrics.borderWidth)
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
            FrameMioColors.surface,
            in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                .stroke(FrameMioColors.divider, lineWidth: FrameMioMetrics.borderWidth)
        }
    }

    private var errorBinding: Binding<Bool> {
        Binding(
            get: { viewModel.errorMessage != nil },
            set: { if !$0 { viewModel.errorMessage = nil } }
        )
    }
}
