import SwiftUI

private func storyCreationViewDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][StoryCreationView] \(message())")
#endif
}

struct StoryCreationView: View {
    @ObservedObject private var viewModel: StoryCreationViewModel
    let onCreate: () -> Void
    let onCreated: (Story) -> Void
    let onOpenPricing: (PricingEntryPoint) -> Void

    init(
        viewModel: StoryCreationViewModel,
        onCreate: @escaping () -> Void,
        onCreated: @escaping (Story) -> Void,
        onOpenPricing: @escaping (PricingEntryPoint) -> Void
    ) {
        _viewModel = ObservedObject(wrappedValue: viewModel)
        self.onCreate = onCreate
        self.onCreated = onCreated
        self.onOpenPricing = onOpenPricing
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.section) {
                VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
                    Tag(viewModel.context.rawValue, systemImage: viewModel.context.icon)
                    Text("A cinematic story from \(viewModel.context.rawValue.lowercased()).")
                        .font(FrameMioTypography.screenTitle)
                        .foregroundStyle(FrameMioColors.ink)
                        .fixedSize(horizontal: false, vertical: true)
                    Text("FrameMio will look for the feeling that connects these moments, then shape them into something worth revisiting.")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .fixedSize(horizontal: false, vertical: true)
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
                .frameMioElevation(.subtle)

                SectionHeader(title: "Selected moments", subtitle: "\(viewModel.assets.count) photos and videos")
                LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible()), GridItem(.flexible())], spacing: FrameMioSpacing.sm) {
                    ForEach(viewModel.assets) { asset in
                        MediaThumbnail(
                            image: viewModel.image(for: asset),
                            duration: asset.isVideo ? asset.duration : nil,
                            isLivePhoto: asset.isLivePhoto,
                            accessibilityTitle: asset.isVideo ? "Selected video" : "Selected photo"
                        )
                        .onAppear {
                            viewModel.loadThumbnail(
                                for: asset,
                                targetSize: FrameMioMetrics.mediaThumbnailRequestSize
                            )
                        }
                        .onDisappear {
                            viewModel.cancelThumbnail(for: asset)
                        }
                    }
                }

                if viewModel.isCreating {
                    VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
                        LoadingState(title: viewModel.phaseTitle, message: "You can leave this screen; FrameMio will keep the work focused and quiet.")
                            .frame(maxWidth: .infinity)
                        FrameMioProgressView(value: viewModel.progress, title: viewModel.phaseTitle)
                    }
                } else if let story = viewModel.story {
                    processingStartedCard(story)
                } else {
                    PrimaryButton(
                        title: "Create Story (\(viewModel.createStoryCreditCost) credits)",
                        systemImage: "sparkles",
                        action: { viewModel.requestCreateStory(onReady: onCreate) },
                        isDisabled: viewModel.isCheckingCredits,
                        isLoading: viewModel.isCheckingCredits
                    )
                }

                if let errorMessage = viewModel.errorMessage {
                    Text(errorMessage)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.accent)
                        .accessibilityAddTraits(.isStaticText)
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
        .navigationTitle("Create Story")
        .task { viewModel.refreshCreditBalance() }
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if viewModel.isCreating {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Cancel") { viewModel.cancel() }
                }
            }
        }
        .onChange(of: viewModel.insufficientCredits) { _, issue in
            guard issue != nil else { return }
            storyCreationViewDebugLog("ui_redirect_to_pricing required=\(issue?.required ?? 0) available=\(issue?.available ?? 0)")
            viewModel.insufficientCredits = nil
            onOpenPricing(.insufficientCredits)
        }
    }

    private func processingStartedCard(_ story: Story) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            HStack(alignment: .top, spacing: FrameMioSpacing.md) {
                Image(systemName: "sparkles")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.accent)
                    .accessibilityHidden(true)

                VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
                    Text("Story creation started")
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                    Text(processingMessage(for: story.status))
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }

            FrameMioProgressView(value: 1, title: "Submitted to FrameMio")

            PrimaryButton(title: "View Story Status", systemImage: "arrow.right") {
                onCreated(story)
            }
        }
        .padding(FrameMioSpacing.xl)
        .background(
            FrameMioColors.surface.opacity(0.9),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.subtle)
    }

    private func processingMessage(for status: StoryStatus) -> String {
        switch status {
        case .queued:
            "Your media uploaded successfully. FrameMio will continue processing it in the background."
        case .analyzing:
            "FrameMio is finding meaningful moments. You can open the status view when you want to check progress."
        case .rendering:
            "FrameMio is rendering your story video. You can open the status view to follow along."
        case .ready:
            "Your story is ready to watch."
        case .failed:
            "FrameMio started the story, but it needs another try. Open the status view for details."
        }
    }
}
