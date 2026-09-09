import SwiftUI

private func storyCreationViewDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][StoryCreationView] \(message())")
#endif
}

struct StoryCreationView: View {
    @Environment(\.scenePhase) private var scenePhase
    @StateObject private var viewModel: StoryCreationViewModel
    let onCreate: (StoryGenerationOptions) -> Void
    let onCreated: (Story) -> Void
    let onOpenPricing: (PricingEntryPoint) -> Void
    let promptTranscriptionService: PromptTranscriptionService?
    @State private var generationOptions = StoryGenerationOptions.automatic
    @State private var isShowingCustomization = false

    init(
        viewModel: StoryCreationViewModel,
        promptTranscriptionService: PromptTranscriptionService? = nil,
        onCreate: @escaping (StoryGenerationOptions) -> Void,
        onCreated: @escaping (Story) -> Void,
        onOpenPricing: @escaping (PricingEntryPoint) -> Void
    ) {
        _viewModel = StateObject(wrappedValue: viewModel)
        self.promptTranscriptionService = promptTranscriptionService
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
                            let instruction = generationOptions.instruction?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
                            storyCreationViewDebugLog("create_tapped style=\(generationOptions.style ?? "automatic") scene_detail=\(generationOptions.sceneDetailStyle ?? "balanced") subtitles=\(generationOptions.subtitlesEnabled.map(String.init) ?? "automatic") target_duration=\(generationOptions.targetDuration.map { String(Int($0.rounded())) } ?? "auto") prompt_present=\(!instruction.isEmpty) prompt_length=\(instruction.count)")
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
                assets: viewModel.assets,
                promptTranscriptionService: promptTranscriptionService
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
        let prompt = generationOptions.instruction?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false ? "Prompt added" : nil
        return [style, sceneDetail, subtitles, length, prompt].compactMap { $0 }.joined(separator: " · ")
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
    let promptTranscriptionService: PromptTranscriptionService?
    @StateObject private var voicePromptRecorder = VoicePromptRecorder()

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

                    Divider().overlay(FrameMioColors.divider.opacity(0.58))

                    promptSection
                }
                .padding(FrameMioSpacing.lg)
            }
            .background(FrameMioColors.canvas.ignoresSafeArea())
            .overlay {
                if voicePromptRecorder.isListening {
                    voicePromptListeningOverlay
                }
            }
            .navigationTitle("Customize Story")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") {
                        Task {
                            await stopVoicePromptRecording()
                            dismiss()
                        }
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .onDisappear {
            Task { await stopVoicePromptRecording() }
        }
    }

    private var promptSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(title: "Prompt", subtitle: "Guide the story's feeling, focus, or pacing")

            LazyVGrid(columns: promptGridColumns, alignment: .leading, spacing: FrameMioSpacing.sm) {
                ForEach(["More cinematic", "More emotional", "More fun", "Focus on people", "Focus on places"], id: \.self) { prompt in
                    optionChip(title: prompt, isSelected: options.instruction == prompt) {
                        options.instruction = prompt
                    }
                }
            }

            TextField("Tell FrameMio what you want", text: Binding(
                get: { options.instruction ?? "" },
                set: { options.instruction = $0 }
            ), axis: .vertical)
            .lineLimit(2...4)
            .font(FrameMioTypography.body)
            .padding(FrameMioSpacing.md)
            .background(FrameMioColors.surface.opacity(0.78), in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                    .stroke(FrameMioColors.divider.opacity(0.45), lineWidth: FrameMioMetrics.borderWidth)
            }
            .accessibilityLabel("Tell FrameMio what you want")

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
                        options.instruction = trimmed
                        storyCreationViewDebugLog("voice prompt live transcript applied transcript_length=\(trimmed.count)")
                    }
                }
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
        let instruction = options.instruction?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return transcript.isEmpty && instruction.isEmpty
    }

    @MainActor
    private func startVoicePromptRecording() async {
        guard let promptTranscriptionService else { return }
        await voicePromptRecorder.startRecording(using: promptTranscriptionService)
    }

    private func clearVoicePromptTranscript() {
        voicePromptRecorder.clearTranscript()
        if let instruction = options.instruction?.trimmingCharacters(in: .whitespacesAndNewlines), !instruction.isEmpty {
            options.instruction = ""
            storyCreationViewDebugLog("voice prompt transcript cleared")
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
            options.instruction = transcript
            storyCreationViewDebugLog("voice prompt applied transcript_length=\(transcript.count)")
        }
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

    private var promptGridColumns: [GridItem] {
        [GridItem(.adaptive(minimum: 140), spacing: FrameMioSpacing.sm, alignment: .leading)]
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
