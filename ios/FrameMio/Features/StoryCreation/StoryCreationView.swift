import AVKit
import SwiftUI
import UIKit

private func storyCreationViewDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][StoryCreationView] \(message())")
#endif
}

struct StoryCreationView: View {
    @Environment(\.scenePhase) private var scenePhase
    @StateObject private var viewModel: StoryCreationViewModel
    let draft: VideoProjectDraft
    let storyService: StoryService
    let onCreate: (VideoProjectDraft) -> Void
    let onCreated: (Story) -> Void
    let onOpenPricing: (PricingEntryPoint) -> Void
    let promptTranscriptionService: PromptTranscriptionService?
    let hasTemplateAccess: Bool
    @State private var generationOptions = StoryGenerationOptions.automatic
    @State private var isShowingCustomization = false
    @State private var templates: [VideoTemplate] = []
    @State private var selectedTemplateID: String?
    @State private var templateConfiguration: VideoTemplateConfiguration?
    @State private var hasExplicitDurationSelection = false

    init(
        viewModel: StoryCreationViewModel,
        draft: VideoProjectDraft,
        storyService: StoryService,
        promptTranscriptionService: PromptTranscriptionService? = nil,
        hasTemplateAccess: Bool = false,
        onCreate: @escaping (VideoProjectDraft) -> Void,
        onCreated: @escaping (Story) -> Void,
        onOpenPricing: @escaping (PricingEntryPoint) -> Void
    ) {
        _viewModel = StateObject(wrappedValue: viewModel)
        self.draft = draft
        self.storyService = storyService
        _generationOptions = State(initialValue: draft.storyOptions)
        _selectedTemplateID = State(initialValue: draft.templateID)
        _templateConfiguration = State(initialValue: draft.templateConfiguration)
        self.promptTranscriptionService = promptTranscriptionService
        self.hasTemplateAccess = hasTemplateAccess
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
                            isLoading: viewModel.isThumbnailLoading(for: asset),
                            duration: asset.isVideo ? asset.duration : nil,
                            isLivePhoto: asset.isLivePhoto,
                            accessibilityTitle: asset.isVideo ? "Selected video" : "Selected photo"
                        )
                        .onAppear {
                            storyCreationViewDebugLog("thumbnail cell appeared asset=\(asset.id) cached=\(viewModel.image(for: asset) != nil) loading=\(viewModel.isThumbnailLoading(for: asset))")
                            viewModel.loadThumbnail(
                                for: asset,
                                targetSize: FrameMioMetrics.mediaThumbnailRequestSize
                            )
                        }
                        .onDisappear {
                            storyCreationViewDebugLog("thumbnail cell disappeared asset=\(asset.id) cached=\(viewModel.image(for: asset) != nil) loading=\(viewModel.isThumbnailLoading(for: asset))")
                            viewModel.cancelThumbnail(for: asset)
                        }
                        .id(asset.id)
                    }
                }

                templateSection

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
        .safeAreaInset(edge: .bottom) {
            if !viewModel.isCreating && viewModel.story == nil {
                fixedCreateButton
            }
        }
        .task { await loadTemplates() }
        .onAppear {
            storyCreationViewDebugLog("create view appeared assets=\(viewModel.assets.count) cached_thumbnails=\(viewModel.thumbnails.count)")
            viewModel.loadMissingThumbnails(targetSize: FrameMioMetrics.mediaThumbnailRequestSize)
        }
        .task {
            viewModel.refreshCreditBalance()
        }
        .navigationBarTitleDisplayMode(.inline)
        .sheet(isPresented: $isShowingCustomization) {
            StoryGenerationOptionsSheet(
                options: $generationOptions,
                templateConfiguration: $templateConfiguration,
                hasExplicitDurationSelection: $hasExplicitDurationSelection,
                assets: viewModel.assets,
                promptTranscriptionService: promptTranscriptionService,
                template: selectedTemplate
            )
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
        .onChange(of: scenePhase) { _, newPhase in
            guard newPhase == .active else { return }
            storyCreationViewDebugLog("create view scene became active cached_thumbnails=\(viewModel.thumbnails.count)")
            viewModel.loadMissingThumbnails(targetSize: FrameMioMetrics.mediaThumbnailRequestSize)
        }
    }

    private var fixedCreateButton: some View {
        VStack(spacing: 0) {
            Divider()
                .overlay(FrameMioColors.divider.opacity(0.7))
            PrimaryButton(
                title: "Create Story (\(viewModel.createStoryCreditCost) credits)",
                systemImage: "sparkles",
                action: {
                    let instruction = generationOptions.instruction?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
                    storyCreationViewDebugLog("create_tapped style=\(generationOptions.style ?? "automatic") scene_detail=\(generationOptions.sceneDetailStyle ?? "balanced") subtitles=\(generationOptions.subtitlesEnabled.map(String.init) ?? "automatic") duration_mode=\(generationOptions.durationMode.rawValue) target_duration=\(generationOptions.targetDuration.map { String(Int($0.rounded())) } ?? "auto") remove_filler_and_pauses=\(generationOptions.removeFillerAndPauses) prompt_present=\(!instruction.isEmpty) prompt_length=\(instruction.count)")
                    viewModel.requestCreateStory { onCreate(configuredDraft) }
                },
                isDisabled: viewModel.isCheckingCredits,
                isLoading: viewModel.isCheckingCredits
            )
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.md)
            .padding(.bottom, FrameMioSpacing.md)
            .background(FrameMioColors.canvas.opacity(0.96))
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
        case "more", "moreDetails":
            sceneDetail = "More details"
        case "none", "mainMoments":
            sceneDetail = "Main moments"
        default:
            sceneDetail = "Balanced"
        }
        let subtitles = generationOptions.subtitlesEnabled == false ? "Subtitles off" : "Subtitles on"
        let length: String
        switch generationOptions.durationMode {
        case .fullSource:
            length = generationOptions.removeFillerAndPauses ? "Full length, cleaned" : "Full length"
        case .fixed:
            length = generationOptions.targetDuration.map { "\(Int($0.rounded()))s" } ?? "Fixed length"
        case .auto:
            length = "Auto length"
        }
        let overlay: String? = {
            guard let selectedTemplate,
                  selectedTemplate.editableOptions.contains("overlay_placement"),
                  let configuration = templateConfiguration else {
                return nil
            }
            let placement = configuration.overlayPlacement?
                .replacingOccurrences(of: "_", with: " ")
                .capitalized
            let size = configuration.overlaySize?.capitalized
            return [placement, size].compactMap { $0 }.joined(separator: " ")
        }()
        let prompt = generationOptions.instruction?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false ? "Prompt added" : nil
        return [selectedTemplate?.name, style, sceneDetail, subtitles, length, overlay, prompt].compactMap { $0 }.joined(separator: " · ")
    }

    private var templateSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            Text("Choose a visual template")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)
            Text("Preview the edit style. FrameMio will choose your media for the template.")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)

            if templates.isEmpty {
                ProgressView()
                    .frame(maxWidth: .infinity, minHeight: 110)
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(alignment: .top, spacing: FrameMioSpacing.md) {
                        ForEach(Array(templates.enumerated()), id: \.element.id) { index, template in
                            let isLocked = templateIsLocked(at: index)
                            TemplateCard(
                                template: template,
                                isSelected: selectedTemplateID == template.id,
                                isLocked: isLocked,
                                cardWidth: 184,
                                previewHeight: 122,
                                minHeight: 268
                            ) {
                                guard !isLocked else {
                                    onOpenPricing(.settings)
                                    return
                                }
                                select(template)
                            }
                        }
                    }
                }
            }

        }
        .padding(FrameMioSpacing.md)
        .background(FrameMioColors.surface.opacity(0.74), in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))
    }

    private var selectedTemplate: VideoTemplate? {
        templates.first(where: { $0.id == selectedTemplateID })
    }

    private var configuredDraft: VideoProjectDraft {
        var configured = draft
        configured.storyOptions = resolvedStoryOptions
        configured.templateID = selectedTemplateID
        configured.templateVersion = selectedTemplate?.version
        configured.templateConfiguration = resolvedTemplateConfiguration
        return configured
    }

    private var resolvedStoryOptions: StoryGenerationOptions {
        var options = generationOptions
        if options.durationMode == .fullSource
            && (selectedTemplate?.supportsFullSourceDuration == false || viewModel.assets.filter(\.isVideo).isEmpty) {
            options.durationMode = .auto
            options.targetDuration = nil
            options.removeFillerAndPauses = false
        }
        if options.durationMode != .fullSource {
            options.removeFillerAndPauses = false
        }
        return options
    }

    private var resolvedTemplateConfiguration: VideoTemplateConfiguration? {
        guard let template = selectedTemplate else { return templateConfiguration }
        var resolved = templateConfiguration ?? template.defaults
        let options = resolvedStoryOptions
        if template.editableOptions.contains("duration_seconds") {
            resolved.durationSeconds = options.durationMode == .fixed
                ? options.targetDuration.map { Int($0.rounded()) } ?? resolved.durationSeconds
                : 0
        }
        if template.editableOptions.contains("duration_mode") {
            resolved.durationMode = options.durationMode
            resolved.removeFillerAndPauses = options.durationMode == .fullSource && options.removeFillerAndPauses
            if options.durationMode != .fixed {
                resolved.durationSeconds = nil
            }
        }
        if template.editableOptions.contains("visual_style") {
            resolved.visualStyle = options.style
        }
        if template.editableOptions.contains("captions_enabled") {
            resolved.captionsEnabled = options.subtitlesEnabled
        }
        if template.editableOptions.contains("caption_style") {
            resolved.captionStyle = options.subtitleStyle
        }
        if template.editableOptions.contains("scene_detail") {
            resolved.sceneDetail = options.sceneDetailStyle
        }
        return resolved.sanitized(for: template)
    }

    private func loadTemplates() async {
        do {
            let loaded = try await storyService.videoTemplates(for: .story)
            templates = loaded.isEmpty
                ? VideoTemplate.localFallbackCatalog.filter { $0.supportedProjectTypes.contains(.story) }
                : loaded
            if selectedTemplateID == nil, let first = templates.first {
                select(first)
            } else if let selectedTemplate {
                templateConfiguration = resolvedTemplateConfiguration?.sanitized(for: selectedTemplate)
            }
        } catch {
            templates = VideoTemplate.localFallbackCatalog.filter { $0.supportedProjectTypes.contains(.story) }
            if selectedTemplateID == nil, let first = templates.first {
                select(first)
            } else if let selectedTemplate {
                templateConfiguration = resolvedTemplateConfiguration?.sanitized(for: selectedTemplate)
            }
            storyCreationViewDebugLog("template_catalog_fallback error=\(String(describing: error))")
        }
    }

    private func select(_ template: VideoTemplate) {
        guard let index = templates.firstIndex(where: { $0.id == template.id }), !templateIsLocked(at: index) else { return }
        selectedTemplateID = template.id
        templateConfiguration = compatibleConfiguration(templateConfiguration, with: template)
        applyTemplateDefaultsToStoryOptions(templateConfiguration)
    }

    private func templateIsLocked(at index: Int) -> Bool {
        TemplateAccessPolicy.isLocked(index: index, hasTemplateAccess: hasTemplateAccess)
    }

    private func compatibleConfiguration(_ previous: VideoTemplateConfiguration?, with template: VideoTemplate) -> VideoTemplateConfiguration {
        var resolved = template.defaults
        guard let previous else { return resolved }
        if template.editableOptions.contains("duration_seconds") { resolved.durationSeconds = previous.durationSeconds }
        if template.supportsFullSourceDuration && template.editableOptions.contains("duration_mode") {
            resolved.durationMode = previous.durationMode
            resolved.removeFillerAndPauses = previous.durationMode == .fullSource && previous.removeFillerAndPauses == true
        } else {
            resolved.durationMode = resolved.durationSeconds == nil ? .auto : .fixed
            resolved.removeFillerAndPauses = false
        }
        if template.editableOptions.contains("visual_style") { resolved.visualStyle = previous.visualStyle }
        if template.editableOptions.contains("captions_enabled") { resolved.captionsEnabled = previous.captionsEnabled }
        if template.editableOptions.contains("caption_style") { resolved.captionStyle = previous.captionStyle }
        if template.editableOptions.contains("scene_detail") { resolved.sceneDetail = previous.sceneDetail }
        if template.editableOptions.contains("overlay_placement") { resolved.overlayPlacement = previous.overlayPlacement }
        if template.editableOptions.contains("overlay_size") { resolved.overlaySize = previous.overlaySize }
        if template.editableOptions.contains("overlay_opacity") { resolved.overlayOpacity = previous.overlayOpacity }
        if template.editableOptions.contains("overlay_frame") { resolved.overlayFrame = previous.overlayFrame }
        if template.editableOptions.contains("overlay_animation"),
           let previousAnimation = previous.overlayAnimation,
           previousAnimation == "auto" ? template.id == "photo-over-video" : template.rendererCapabilities.contains(previousAnimation) {
            resolved.overlayAnimation = previousAnimation
        }
        return resolved.sanitized(for: template)
    }

    private func applyTemplateDefaultsToStoryOptions(_ configuration: VideoTemplateConfiguration?) {
        guard let configuration else { return }
        if let style = configuration.visualStyle {
            generationOptions.style = style
        }
        generationOptions.subtitlesEnabled = configuration.captionsEnabled
        generationOptions.subtitleStyle = configuration.captionStyle
        generationOptions.sceneDetailStyle = configuration.sceneDetail
        guard !hasExplicitDurationSelection else { return }
        let templateDurationMode = configuration.durationMode ?? (configuration.durationSeconds == nil ? .auto : .fixed)
        generationOptions.durationMode = templateDurationMode == .fullSource ? .fullSource : .auto
        generationOptions.targetDuration = nil
        generationOptions.removeFillerAndPauses = generationOptions.durationMode == .fullSource && configuration.removeFillerAndPauses == true
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

struct FrameMioPromptInputSection: View {
    @Environment(\.openURL) private var openURL
    let title: String
    let subtitle: String
    let placeholder: String
    let quickPrompts: [String]
    @Binding var prompt: String
    let promptTranscriptionService: PromptTranscriptionService?
    let transcriptLogContext: String
    let showsHeader: Bool
    let inputMinHeight: CGFloat?
    let promptActions: AnyView?
    @State private var isShowingMicrophoneSettingsAlert = false
    @StateObject private var voicePromptRecorder = VoicePromptRecorder()

    init(
        title: String,
        subtitle: String,
        placeholder: String,
        quickPrompts: [String],
        prompt: Binding<String>,
        promptTranscriptionService: PromptTranscriptionService?,
        transcriptLogContext: String,
        showsHeader: Bool = true,
        inputMinHeight: CGFloat? = nil,
        promptActions: AnyView? = nil
    ) {
        self.title = title
        self.subtitle = subtitle
        self.placeholder = placeholder
        self.quickPrompts = quickPrompts
        self._prompt = prompt
        self.promptTranscriptionService = promptTranscriptionService
        self.transcriptLogContext = transcriptLogContext
        self.showsHeader = showsHeader
        self.inputMinHeight = inputMinHeight
        self.promptActions = promptActions
    }

    var body: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            if showsHeader {
                SectionHeader(title: title, subtitle: subtitle)
            }

            if !quickPrompts.isEmpty {
                VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                    ForEach(quickPrompts, id: \.self) { suggestion in
                        suggestionChip(title: suggestion, isSelected: prompt == suggestion) {
                            prompt = suggestion
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            }

            if let promptActions {
                HStack(alignment: .center) {
                    promptActions
                    Spacer(minLength: FrameMioSpacing.sm)
                    clearPromptButton
                }
            }

            promptInput

            if promptActions == nil {
                HStack {
                    Spacer()
                    clearPromptButton
                }
            }

            voicePromptControl
                .onChange(of: voicePromptRecorder.currentTranscript) { _, transcript in
                    let trimmed = transcript.trimmingCharacters(in: .whitespacesAndNewlines)
                    if !trimmed.isEmpty {
                        prompt = trimmed
                        debugLog("voice prompt live transcript applied transcript_length=\(trimmed.count)")
                    }
            }
        }
        .overlay {
            if voicePromptRecorder.isListening {
                voicePromptListeningOverlay
            }
        }
        .onChange(of: voicePromptRecorder.shouldOpenMicrophoneSettings) { _, shouldOpen in
            guard shouldOpen else { return }
            isShowingMicrophoneSettingsAlert = true
            voicePromptRecorder.clearMicrophoneSettingsRequest()
        }
        .alert("Microphone Access Needed", isPresented: $isShowingMicrophoneSettingsAlert) {
            Button("Open Settings") {
                guard let settingsURL = URL(string: UIApplication.openSettingsURLString) else { return }
                openURL(settingsURL)
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Microphone access is turned off for FrameMio. Enable it in Settings under Privacy & Security > Microphone.")
        }
        .onDisappear {
            Task { await stopVoicePromptRecording() }
        }
    }

    private var clearPromptButton: some View {
        Button {
            clearVoicePromptTranscript()
        } label: {
            Label("Clear prompt", systemImage: "trash")
                .font(FrameMioTypography.footnote)
        }
        .buttonStyle(.borderless)
        .foregroundStyle(FrameMioColors.secondaryInk)
        .disabled(isPromptClearDisabled)
    }

    @ViewBuilder
    private var promptInput: some View {
        if let inputMinHeight {
            ZStack(alignment: .topLeading) {
                TextEditor(text: $prompt)
                    .font(FrameMioTypography.body)
                    .foregroundStyle(FrameMioColors.ink)
                    .scrollContentBackground(.hidden)
                    .frame(maxWidth: .infinity, minHeight: inputMinHeight, alignment: .topLeading)
                    .padding(FrameMioSpacing.md)

                if prompt.isEmpty {
                    Text(placeholder)
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.tertiaryInk)
                        .padding(.horizontal, FrameMioSpacing.lg)
                        .padding(.vertical, FrameMioSpacing.lg)
                        .allowsHitTesting(false)
                }
            }
            .frame(minHeight: inputMinHeight, alignment: .topLeading)
            .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
                    .stroke(FrameMioColors.divider.opacity(0.45), lineWidth: FrameMioMetrics.borderWidth)
            }
            .accessibilityLabel(title)
        } else {
            TextField(placeholder, text: $prompt, axis: .vertical)
                .lineLimit(2...4)
                .font(FrameMioTypography.body)
                .padding(FrameMioSpacing.md)
                .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
                        .stroke(FrameMioColors.divider.opacity(0.45), lineWidth: FrameMioMetrics.borderWidth)
                }
                .accessibilityLabel(title)
        }
    }

    private func suggestionChip(title: String, isSelected: Bool, action: @escaping () -> Void) -> some View {
        Button {
            FrameMioHaptics.secondaryAction()
            action()
        } label: {
            Text(title)
                .font(FrameMioTypography.footnote)
                .foregroundStyle(isSelected ? FrameMioColors.inverseInk : FrameMioColors.ink)
                .frame(maxWidth: .infinity, alignment: .leading)
                .frame(minHeight: FrameMioMetrics.compactButtonHeight)
                .padding(.horizontal, FrameMioSpacing.sm)
                .background(
                    isSelected ? FrameMioColors.accent : FrameMioColors.surface.opacity(0.78),
                    in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                )
                .overlay {
                    RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                        .stroke(isSelected ? FrameMioColors.accent : FrameMioColors.divider.opacity(0.45), lineWidth: FrameMioMetrics.borderWidth)
                }
        }
        .buttonStyle(FrameMioPressButtonStyle())
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
            Text("Connecting... tap again to stop.")
                .foregroundStyle(FrameMioColors.secondaryInk)
        } else if voicePromptRecorder.isRecording {
            Text("Listening live... auto-stops after silence.")
                .foregroundStyle(FrameMioColors.secondaryInk)
        } else {
            Text("Tap Voice Prompt and speak; the text field updates live.")
                .foregroundStyle(FrameMioColors.secondaryInk)
        }
    }

    private var voicePromptButtonDisabled: Bool {
        voicePromptRecorder.isTranscribing || promptTranscriptionService == nil
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
        let value = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        return transcript.isEmpty && value.isEmpty
    }

    @MainActor
    private func startVoicePromptRecording() async {
        guard let promptTranscriptionService else { return }
        await voicePromptRecorder.startRecording(using: promptTranscriptionService)
    }

    private func clearVoicePromptTranscript() {
        voicePromptRecorder.clearTranscript()
        if !prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            prompt = ""
            debugLog("voice prompt transcript cleared")
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
            prompt = transcript
            debugLog("voice prompt applied transcript_length=\(transcript.count)")
        }
    }

    private func debugLog(_ message: @autoclosure () -> String) {
#if DEBUG
        print("[FrameMio][\(transcriptLogContext)] \(message())")
#endif
    }
}

private struct StoryGenerationOptionsSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Binding var options: StoryGenerationOptions
    @Binding var templateConfiguration: VideoTemplateConfiguration?
    @Binding var hasExplicitDurationSelection: Bool
    let assets: [MediaAsset]
    let promptTranscriptionService: PromptTranscriptionService?
    let template: VideoTemplate?

    private let styleOptions = ["Natural", "Cinematic", "Warm", "Minimal", "Playful"]
    private let subtitleStyleOptions: [(title: String, value: String)] = [
        ("Bold", "bold_social"),
        ("Cinematic", "cinematic_subtitle"),
        ("Minimal", "clean_minimal"),
        ("Playful", "playful_pop")
    ]
    private let sceneDetailOptions: [(title: String, value: String?, subtitle: String?)] = [
        ("Balanced", "balanced", "Default"),
        ("More details", "more", "Extra cutaways"),
        ("Main moments", "none", "No cutaways")
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
                    if let template {
                        templateSummary(template)
                    }

                    if supports("visual_style") {
                        optionSection(title: "Style", options: styleOptions, selected: options.style ?? "cinematic") { option in
                            options.style = option.lowercased()
                        }
                        optionDivider
                    }

                    if supports("aspect_ratio") {
                        aspectRatioSection
                        optionDivider
                    }

                    if supports("scene_detail") {
                        sceneDetailSection
                        optionDivider
                    }

                    if supports("captions_enabled") || supports("caption_style") {
                        subtitleSection
                        optionDivider
                    }

                    if supports("duration_seconds") {
                        lengthSection
                        optionDivider
                    }

                    if supportsAny(["overlay_placement", "overlay_size", "overlay_opacity", "overlay_frame", "overlay_animation"]) {
                        overlaySection
                        optionDivider
                }

                promptSection
            }
            .padding(FrameMioSpacing.lg)
            .background {
                Color.clear
                    .contentShape(Rectangle())
                    .onTapGesture(perform: dismissKeyboard)
            }
        }
        .background(FrameMioColors.canvas.ignoresSafeArea())
        .navigationTitle(template.map { "Customize \($0.name)" } ?? "Customize Story")
        .navigationBarTitleDisplayMode(.inline)
        .scrollDismissesKeyboard(.immediately)
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

    private var optionDivider: some View {
        Divider().overlay(FrameMioColors.divider.opacity(0.58))
    }

    private func supports(_ option: String) -> Bool {
        template?.editableOptions.contains(option) ?? true
    }

    private var supportsFullLength: Bool {
        template?.supportsFullSourceDuration ?? true
    }

    private func supportsAny(_ options: [String]) -> Bool {
        guard template != nil else { return false }
        return options.contains(where: supports)
    }

    private func templateSummary(_ template: VideoTemplate) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Template", subtitle: templateLayoutDescription(template))
            HStack(alignment: .top, spacing: FrameMioSpacing.sm) {
                Image(systemName: templateLayoutIcon(template))
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.accent)
                    .frame(width: FrameMioMetrics.minimumTouchTarget)
                    .accessibilityHidden(true)

                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text(template.name)
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                    Text(template.description)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .padding(FrameMioSpacing.md)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
        }
    }

    private var promptSection: some View {
        FrameMioPromptInputSection(
            title: "Prompt",
            subtitle: "Guide the story's feeling, focus, or pacing",
            placeholder: "Tell FrameMio what you want",
            quickPrompts: ["More cinematic", "More emotional", "More fun", "Focus on people", "Focus on places"],
            prompt: Binding(
                get: { options.instruction ?? "" },
                set: { options.instruction = $0 }
            ),
            promptTranscriptionService: promptTranscriptionService,
            transcriptLogContext: "StoryGenerationOptionsSheet",
            inputMinHeight: 180
        )
    }

    private func dismissKeyboard() {
        UIApplication.shared.sendAction(
            #selector(UIResponder.resignFirstResponder),
            to: nil,
            from: nil,
            for: nil
        )
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
            if supports("captions_enabled") {
                Toggle(isOn: Binding(
                    get: { options.subtitlesEnabled ?? true },
                    set: { options.subtitlesEnabled = $0 }
                )) {
                    Label("Show subtitles", systemImage: "captions.bubble")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.ink)
                }
                .tint(FrameMioColors.accent)
            }

            if supports("caption_style") && (options.subtitlesEnabled ?? true) {
                LazyVGrid(columns: optionGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                    ForEach(subtitleStyleOptions, id: \.value) { option in
                        optionChip(title: option.title, isSelected: option.value == normalizedSubtitleStyle(options.subtitleStyle)) {
                            options.subtitleStyle = option.value
                        }
                    }
                }
            }
        }
    }

    private var overlaySection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(
                title: "Overlay",
                subtitle: template?.defaults.layout == "picture_in_picture"
                    ? "Keep the main video playing while supporting media appears above it."
                    : "Adjust the bounded supporting-media treatment."
            )

            if supports("overlay_placement") {
                TemplateOptionMenu(
                    title: "Position",
                    selection: templateConfiguration?.overlayPlacement ?? "bottom_right",
                    options: ["top_left", "top_right", "bottom_left", "bottom_right", "center"]
                ) { templateConfiguration?.overlayPlacement = $0 }
            }

            if supports("overlay_size") {
                TemplateOptionMenu(
                    title: "Size",
                    selection: templateConfiguration?.overlaySize ?? "medium",
                    options: ["small", "medium", "large"]
                ) { templateConfiguration?.overlaySize = $0 }
            }

            if supports("overlay_opacity") {
                HStack(spacing: FrameMioSpacing.md) {
                    Text("Opacity")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.ink)
                    Spacer()
                    Text("\(Int((templateConfiguration?.overlayOpacity ?? 1) * 100))%")
                        .monospacedDigit()
                        .foregroundStyle(FrameMioColors.secondaryInk)
                    Stepper(
                        "",
                        value: Binding(
                            get: { templateConfiguration?.overlayOpacity ?? 1 },
                            set: { templateConfiguration?.overlayOpacity = min(1, max(0.35, $0)) }
                        ),
                        in: 0.35...1,
                        step: 0.05
                    )
                }
            }

            if supports("overlay_frame") {
                TemplateOptionMenu(
                    title: "Frame",
                    selection: templateConfiguration?.overlayFrame ?? "subtle",
                    options: ["none", "subtle", "bold"]
                ) { templateConfiguration?.overlayFrame = $0 }
            }

            if supports("overlay_animation") {
                TemplateOptionMenu(
                    title: "Motion",
                    selection: templateConfiguration?.overlayAnimation ?? "fade",
                    options: template?.id == "photo-over-video"
                        ? ["auto", "none", "fade", "scale", "move"]
                        : ["none", "fade", "subtle_scale"]
                ) { templateConfiguration?.overlayAnimation = $0 }
            }
        }
    }

    private var aspectRatioSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Frame", subtitle: "Choose the output shape for this template")
            TemplateOptionMenu(
                title: "Aspect ratio",
                selection: templateConfiguration?.aspectRatio ?? "9:16",
                options: ["9:16", "16:9", "1:1"]
            ) { templateConfiguration?.aspectRatio = $0 }
        }
    }

    private var lengthSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Length", subtitle: "Auto fits your selected media")
            LazyVGrid(columns: optionGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                optionChip(title: "Auto", isSelected: options.durationMode == .auto) {
                    hasExplicitDurationSelection = true
                    options.durationMode = .auto
                    options.targetDuration = nil
                    options.removeFillerAndPauses = false
                }
                ForEach(lengthOptions, id: \.self) { seconds in
                    let disabled = seconds > maxSelectableLengthSeconds
                    optionChip(
                        title: "\(seconds)s",
                        subtitle: disabled ? "Not enough source media" : nil,
                        isSelected: options.durationMode == .fixed && Int((options.targetDuration ?? 0).rounded()) == seconds,
                        isDisabled: disabled
                    ) {
                        hasExplicitDurationSelection = true
                        options.durationMode = .fixed
                        options.targetDuration = TimeInterval(seconds)
                        options.removeFillerAndPauses = false
                    }
                }
                if supportsFullLength {
                    let disabled = totalVideoSourceDuration <= 0
                    optionChip(
                        title: "Full length",
                        subtitle: disabled ? "Needs a video" : nil,
                        isSelected: options.durationMode == .fullSource,
                        isDisabled: disabled
                    ) {
                        hasExplicitDurationSelection = true
                        options.durationMode = .fullSource
                        options.targetDuration = nil
                    }
                }
            }
            if options.durationMode == .fullSource {
                Toggle(isOn: $options.removeFillerAndPauses) {
                    Label("Remove filler words and pauses", systemImage: "scissors")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.ink)
                }
                .tint(FrameMioColors.accent)
            }
        }
        .onAppear(perform: normalizeDurationSelection)
        .onChange(of: template?.id) { _, _ in normalizeDurationSelection() }
    }

    private func normalizeDurationSelection() {
        if options.durationMode == .fullSource && (!supportsFullLength || totalVideoSourceDuration <= 0) {
            options.durationMode = .auto
            options.targetDuration = nil
            options.removeFillerAndPauses = false
        }
        if options.durationMode != .fullSource {
            options.removeFillerAndPauses = false
        }
        if options.durationMode == .fixed && options.targetDuration == nil {
            options.durationMode = .auto
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

    private func normalizedSubtitleStyle(_ value: String?) -> String {
        switch value?.lowercased() {
        case "bold", "bold_social":
            return "bold_social"
        case "cinematic", "cinematic_subtitle":
            return "cinematic_subtitle"
        case "minimal", "clean_minimal":
            return "clean_minimal"
        case "playful", "playful_pop":
            return "playful_pop"
        default:
            return "cinematic_subtitle"
        }
    }

    private func templateLayoutIcon(_ template: VideoTemplate) -> String {
        switch template.defaults.layout {
        case "picture_in_picture":
            return "rectangle.on.rectangle"
        case "split_screen":
            return "rectangle.split.2x1"
        case "montage":
            return "square.grid.2x2"
        default:
            return "rectangle.fill"
        }
    }

    private func templateLayoutDescription(_ template: VideoTemplate) -> String {
        switch template.defaults.layout {
        case "picture_in_picture":
            return "Picture over continuing video"
        case "split_screen":
            return "Two moments share the frame"
        case "montage":
            return "Several moments cut together quickly"
        default:
            return "Full-screen cinematic moments"
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
