import SwiftUI

private func storyCreationViewDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][StoryCreationView] \(message())")
#endif
}

struct StoryCreationView: View {
    @ObservedObject private var viewModel: StoryCreationViewModel
    let onCreate: (StoryGenerationOptions) -> Void
    let onCreated: (Story) -> Void
    let onOpenPricing: (PricingEntryPoint) -> Void
    @State private var generationOptions = StoryGenerationOptions.automatic
    @State private var isShowingCustomization = false

    init(
        viewModel: StoryCreationViewModel,
        onCreate: @escaping (StoryGenerationOptions) -> Void,
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
                    customizationEntry
                    PrimaryButton(
                        title: "Create Story (\(viewModel.createStoryCreditCost) credits)",
                        systemImage: "sparkles",
                        action: {
                            storyCreationViewDebugLog("create_tapped style=\(generationOptions.style ?? "automatic") scene_detail=\(generationOptions.sceneDetailStyle ?? "balanced") subtitles=\(generationOptions.subtitlesEnabled.map(String.init) ?? "automatic") target_duration=\(generationOptions.targetDuration.map { String(Int($0.rounded())) } ?? "auto")")
                            viewModel.requestCreateStory { onCreate(generationOptions) }
                        },
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
        .sheet(isPresented: $isShowingCustomization) {
            StoryGenerationOptionsSheet(options: $generationOptions, assets: viewModel.assets)
        }
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

    private var customizationEntry: some View {
        Button {
            guard !viewModel.isCheckingCredits else { return }
            isShowingCustomization = true
        } label: {
            HStack(spacing: FrameMioSpacing.md) {
                Image(systemName: "slider.horizontal.3")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.accent)
                    .frame(width: FrameMioMetrics.minimumTouchTarget)
                    .accessibilityHidden(true)

                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text("Customize")
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                    Text(customizationSummary)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .lineLimit(2)
                        .fixedSize(horizontal: false, vertical: true)
                }

                Spacer(minLength: FrameMioSpacing.sm)

                Image(systemName: "chevron.right")
                    .font(FrameMioTypography.caption)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, FrameMioSpacing.md)
            .padding(.vertical, FrameMioSpacing.sm)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                    .stroke(FrameMioColors.divider.opacity(0.55), lineWidth: FrameMioMetrics.borderWidth)
            }
        }
        .buttonStyle(FrameMioPressButtonStyle())
        .disabled(viewModel.isCheckingCredits)
        .accessibilityLabel("Customize story")
        .accessibilityValue(customizationSummary)
    }

    private var customizationSummary: String {
        let style = (generationOptions.style ?? "cinematic").capitalized
        let sceneDetail: String
        switch generationOptions.sceneDetailStyle {
        case "moreDetails":
            sceneDetail = "More details"
        case "mainMoments":
            sceneDetail = "Main moments"
        default:
            sceneDetail = "Balanced"
        }
        let subtitles = generationOptions.subtitlesEnabled == false ? "Subtitles off" : "Subtitles on"
        let length = generationOptions.targetDuration.map { "\(Int($0.rounded()))s" } ?? "Auto length"
        return "\(style) · \(sceneDetail) · \(subtitles) · \(length)"
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

private struct StoryGenerationOptionsSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Binding var options: StoryGenerationOptions
    let assets: [MediaAsset]

    private let styleOptions = ["Natural", "Cinematic", "Warm", "Playful"]
    private let subtitleStyleOptions = ["Bold", "Cinematic", "Minimal", "Playful"]
    private let sceneDetailOptions: [(title: String, value: String?, subtitle: String?)] = [
        ("Balanced", nil, "Default"),
        ("More details", "moreDetails", "Extra cutaways"),
        ("Main moments", "mainMoments", "No cutaways")
    ]
    private let lengthOptions = [15, 30, 45, 60]

    private var hasPhotoLikeSource: Bool {
        assets.contains { !$0.isVideo }
    }

    private var hasUnknownVideoLengthSource: Bool {
        assets.contains { $0.isVideo && $0.duration <= 0 }
    }

    private var totalVideoSourceDuration: TimeInterval {
        assets.reduce(0) { total, asset in
            guard asset.isVideo else { return total }
            return total + max(0, asset.duration)
        }
    }

    private var maxSelectableLengthSeconds: Int {
        if hasPhotoLikeSource || hasUnknownVideoLengthSource {
            return 60
        }
        return max(0, Int(totalVideoSourceDuration.rounded(.down)))
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                    optionSection(title: "Style", options: styleOptions, selected: options.style ?? "cinematic") { option in
                        options.style = option.lowercased()
                    }

                    Divider().overlay(FrameMioColors.divider.opacity(0.58))

                    sceneDetailSection

                    Divider().overlay(FrameMioColors.divider.opacity(0.58))

                    subtitleSection

                    Divider().overlay(FrameMioColors.divider.opacity(0.58))

                    lengthSection
                }
                .padding(FrameMioSpacing.lg)
            }
            .background(FrameMioColors.canvas.ignoresSafeArea())
            .navigationTitle("Customize Story")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") {
                        dismiss()
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    private var sceneDetailSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Scene Details", subtitle: "Choose how much supporting footage to include")
            LazyVGrid(columns: optionGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                ForEach(sceneDetailOptions, id: \.title) { option in
                    optionChip(
                        title: option.title,
                        subtitle: option.subtitle,
                        isSelected: options.sceneDetailStyle == option.value
                    ) {
                        options.sceneDetailStyle = option.value
                    }
                }
            }
        }
    }

    private var subtitleSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Subtitles", subtitle: "Show spoken dialogue as captions")
            Toggle(isOn: Binding(
                get: { options.subtitlesEnabled ?? true },
                set: { options.subtitlesEnabled = $0 }
            )) {
                Label("Show subtitles", systemImage: "captions.bubble")
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.ink)
            }
            .tint(FrameMioColors.accent)

            if options.subtitlesEnabled ?? true {
                LazyVGrid(columns: optionGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                    ForEach(subtitleStyleOptions, id: \.self) { option in
                        optionChip(title: option, isSelected: option.caseInsensitiveCompare(options.subtitleStyle ?? "cinematic") == .orderedSame) {
                            options.subtitleStyle = option.lowercased()
                        }
                    }
                }
            }
        }
    }

    private var lengthSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Length", subtitle: "Auto fits your selected media")
            LazyVGrid(columns: optionGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                optionChip(title: "Auto", isSelected: options.targetDuration == nil) {
                    options.targetDuration = nil
                }
                ForEach(lengthOptions, id: \.self) { seconds in
                    let disabled = seconds > maxSelectableLengthSeconds
                    optionChip(
                        title: "\(seconds)s",
                        subtitle: disabled ? "Not enough source media" : nil,
                        isSelected: Int((options.targetDuration ?? 0).rounded()) == seconds,
                        isDisabled: disabled
                    ) {
                        options.targetDuration = TimeInterval(seconds)
                    }
                }
            }
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
        .disabled(isDisabled)
    }
}
