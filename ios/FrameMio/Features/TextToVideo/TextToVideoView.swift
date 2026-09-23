import SwiftUI
import UIKit

struct TextToVideoView: View {
    let service: TextToVideoService
    let creditService: CreditService?
    let promptTranscriptionService: PromptTranscriptionService?
    let onOpenPricing: (PricingEntryPoint) -> Void
    let onCreditsUpdated: (() -> Void)?

    @State private var prompt = ""
    @State private var duration = TextVideoDurationOption.five
    @State private var resolution = TextVideoResolutionOption.sevenTwenty
    @State private var aspectRatio = TextVideoAspectRatioOption.nineSixteen
    @State private var isGenerating = false
    @State private var statusText: String?
    @State private var generations: [TextVideoGeneration] = []
    @State private var errorMessage: String?
    @State private var creditCost: Int
    @State private var isCheckingCredits = false
    @State private var isShowingSamplePrompts = false

    private let samplePrompts = [
        "A cinematic vertical video of a small sailboat gliding across calm turquoise water at sunrise, soft golden light reflecting on the waves, gentle ocean mist, smooth aerial camera movement, realistic details, peaceful and inspiring mood, natural colors, no text or logos.",
        "A cozy cabin in a snowy forest at blue hour, warm light glowing through the windows, smoke rising from the chimney, slow push-in camera movement, soft snowfall, cinematic realism, peaceful winter atmosphere, no text or logos.",
        "A bustling night market in Tokyo after rain, colorful neon reflections on wet pavement, people moving through food stalls, smooth handheld camera movement, vibrant cinematic lighting, realistic details, no text or logos."
    ]

    init(
        service: TextToVideoService,
        creditService: CreditService? = nil,
        promptTranscriptionService: PromptTranscriptionService? = nil,
        onOpenPricing: @escaping (PricingEntryPoint) -> Void = { _ in },
        onCreditsUpdated: (() -> Void)? = nil
    ) {
        self.service = service
        self.creditService = creditService
        self.promptTranscriptionService = promptTranscriptionService
        self.onOpenPricing = onOpenPricing
        self.onCreditsUpdated = onCreditsUpdated
        _prompt = State(initialValue: "")
        _creditCost = State(initialValue: CreditPricing.price(for: .createVideo))
    }

    private var canGenerate: Bool {
        !prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
            !isGenerating &&
            !isCheckingCredits
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.xl) {
                promptSection
                settingsSection
                generationStatusSection
                historySection
            }
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.lg)
            .padding(.bottom, FrameMioSpacing.xxxl)
            .background {
                Color.clear
                    .contentShape(Rectangle())
                    .onTapGesture(perform: dismissKeyboard)
            }
        }
        .background(FrameMioColors.canvas.ignoresSafeArea())
        .navigationTitle("Text to Video")
        .navigationBarTitleDisplayMode(.inline)
        .scrollDismissesKeyboard(.immediately)
        .safeAreaInset(edge: .bottom) {
            fixedGenerateButton
        }
        .task {
            await refreshCreditQuote()
            await refreshGenerationHistoryLoop()
        }
        .alert("Couldn’t generate video", isPresented: errorBinding) {
            Button("Dismiss", role: .cancel) { errorMessage = nil }
        } message: {
            Text(errorMessage ?? "")
        }
        .sheet(isPresented: $isShowingSamplePrompts) {
            TextToVideoSamplePromptsSheet(prompts: samplePrompts) { samplePrompt in
                prompt = samplePrompt
            }
            .presentationDetents([.large])
            .presentationDragIndicator(.visible)
        }
    }

    private var promptSection: some View {
        FrameMioPromptInputSection(
            title: "Prompt",
            subtitle: "Describe the scene, mood, or story you want to generate.",
            placeholder: "Example: A cinematic golden-hour walk along the coast...",
            quickPrompts: [],
            prompt: $prompt,
            promptTranscriptionService: promptTranscriptionService,
            transcriptLogContext: "text_to_video",
            showsHeader: false,
            inputMinHeight: 180,
            promptActions: AnyView(
                Button {
                    FrameMioHaptics.secondaryAction()
                    dismissKeyboard()
                    isShowingSamplePrompts = true
                } label: {
                    Label("Sample Prompt", systemImage: "text.quote")
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.accent)
                }
                .buttonStyle(.borderless)
            )
        )
    }

    private var settingsSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            Text("Video settings")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)

            settingPicker(title: "Duration", selection: $duration) {
                ForEach(TextVideoDurationOption.allCases) { option in
                    Text(option.title).tag(option)
                }
            }

            settingPicker(title: "Resolution", selection: $resolution) {
                ForEach(TextVideoResolutionOption.allCases) { option in
                    Text(option.title).tag(option)
                }
            }

            settingPicker(title: "Aspect ratio", selection: $aspectRatio) {
                ForEach(TextVideoAspectRatioOption.allCases) { option in
                    Text(option.title).tag(option)
                }
            }
        }
        .onTapGesture(perform: dismissKeyboard)
        .padding(FrameMioSpacing.lg)
        .background(
            FrameMioColors.surface.opacity(0.9),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.72), lineWidth: FrameMioMetrics.borderWidth)
        }
    }

    private func settingPicker<Selection: Hashable, Content: View>(
        title: String,
        selection: Binding<Selection>,
        @ViewBuilder content: () -> Content
    ) -> some View {
        HStack(spacing: FrameMioSpacing.md) {
            Text(title)
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.ink)

            Spacer(minLength: FrameMioSpacing.sm)

            Picker(title, selection: selection, content: content)
                .pickerStyle(.menu)
                .tint(FrameMioColors.accent)
        }
        .frame(minHeight: FrameMioMetrics.minimumTouchTarget)
    }

    private var generationStatusSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            if let statusText {
                HStack(spacing: FrameMioSpacing.sm) {
                    if isGenerating {
                        ProgressView()
                            .controlSize(.small)
                    }
                    Text(statusText)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }
            }
        }
        .onTapGesture(perform: dismissKeyboard)
    }

    private var historySection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            Text("Generated videos")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)

            if generations.isEmpty {
                Text("Your generated videos will appear here.")
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            } else {
                LazyVStack(alignment: .leading, spacing: FrameMioSpacing.md) {
                    ForEach(generations) { generation in
                        TextVideoGenerationRow(generation: generation, service: service)
                    }
                }
            }
        }
        .onTapGesture(perform: dismissKeyboard)
    }

    private var fixedGenerateButton: some View {
        VStack(spacing: 0) {
            Divider()
                .overlay(FrameMioColors.divider.opacity(0.7))
            PrimaryButton(
                title: "Generate Video (\(creditCost) credits)",
                systemImage: "wand.and.stars",
                action: requestGenerate,
                isDisabled: !canGenerate,
                isLoading: isCheckingCredits || isGenerating
            )
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.md)
            .padding(.bottom, FrameMioSpacing.md)
            .background(FrameMioColors.canvas.opacity(0.96))
            .onTapGesture(perform: dismissKeyboard)
        }
    }

    private func dismissKeyboard() {
        UIApplication.shared.sendAction(
            #selector(UIResponder.resignFirstResponder),
            to: nil,
            from: nil,
            for: nil
        )
    }

    private func refreshCreditQuote() async {
        guard let creditService else { return }
        do {
            let quote = try await creditService.quote(action: .createVideo, stats: CreditMediaStats())
            await MainActor.run {
                creditCost = quote.credits
            }
        } catch is CancellationError {
            // The initial quote is best-effort; the Generate action retries it before creating a video.
        } catch {
            // Keep the configured default price visible when the quote service is temporarily unavailable.
        }
    }

    private func requestGenerate() {
        guard canGenerate else { return }
        guard let creditService else {
            generate()
            return
        }

        isCheckingCredits = true
        errorMessage = nil
        Task { @MainActor in
            defer { isCheckingCredits = false }
            do {
                let quote = try await creditService.quote(action: .createVideo, stats: CreditMediaStats())
                creditCost = quote.credits
                guard quote.eligible else {
                    errorMessage = AppError.message(quote.reason ?? "Video generation is not available for this request.").localizedDescription
                    return
                }
                guard quote.balance >= quote.credits else {
                    onOpenPricing(.insufficientCredits)
                    return
                }
                generate()
            } catch {
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    private func generate() {
        guard !isGenerating else { return }
        isGenerating = true
        errorMessage = nil
        statusText = "Preparing your video..."

        let requestPrompt = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        Task {
            do {
                _ = try await service.create(
                    prompt: requestPrompt,
                    duration: duration.seconds,
                    resolution: resolution.rawValue,
                    aspectRatio: aspectRatio.rawValue
                )
                await MainActor.run {
                    statusText = "Your video is being generated. You can keep using this screen."
                    isGenerating = false
                    onCreditsUpdated?()
                }
                await refreshGenerations()
                if let creditService {
                    _ = try? await creditService.refreshBalance()
                }
            } catch is CancellationError {
                await MainActor.run {
                    isGenerating = false
                    statusText = nil
                }
            } catch {
                await MainActor.run {
                    isGenerating = false
                    statusText = nil
                    let appError = AppError.from(error)
                    if case .insufficientCredits = appError {
                        onOpenPricing(.insufficientCredits)
                    } else {
                        errorMessage = appError.localizedDescription
                    }
                }
            }
        }
    }

    private func refreshGenerationHistoryLoop() async {
        while !Task.isCancelled {
            await refreshGenerations()
            try? await Task.sleep(nanoseconds: 2_500_000_000)
        }
    }

    private func refreshGenerations() async {
        do {
            let latest = try await service.list()
            await MainActor.run {
                generations = latest
                if latest.contains(where: { $0.status == .queued || $0.status == .running }) {
                    if !isGenerating {
                        statusText = "Your video is being generated..."
                    }
                } else if !isGenerating &&
                            (statusText == "Your video is being generated..." ||
                             statusText == "Your video is being generated. You can keep using this screen.") {
                    statusText = nil
                }
            }
        } catch is CancellationError {
        } catch {
            // History refresh is best-effort while the view is open.
        }
    }

    private var errorBinding: Binding<Bool> {
        Binding(
            get: { errorMessage != nil },
            set: { if !$0 { errorMessage = nil } }
        )
    }
}

private struct TextVideoGenerationRow: View {
    let generation: TextVideoGeneration
    let service: TextToVideoService

    @State private var playableURL: URL?
    @State private var isLoadingContent = false
    @State private var contentError: String?

    var body: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            Text(generation.prompt)
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.ink)
                .lineLimit(3)
                .fixedSize(horizontal: false, vertical: true)

            switch generation.status {
            case .queued, .running:
                generationPlaceholder
            case .completed:
                if let playableURL {
                    StoryVideoPlayer(
                        url: playableURL,
                        autoplays: false,
                        accessibilityTitle: "Generated video"
                    )
                    .aspectRatio(videoAspectRatio, contentMode: .fit)
                    .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous))
                } else {
                    generationPlaceholder
                }
            case .failed:
                Label(
                    generation.errorMessage ?? "Video generation failed.",
                    systemImage: "exclamationmark.triangle"
                )
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)
            }

            HStack(spacing: FrameMioSpacing.sm) {
                Text(statusTitle)
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)

                Spacer(minLength: FrameMioSpacing.sm)

                Text("\(generation.seconds)s · \(generation.resolution) · \(generation.aspectRatio)")
                    .font(FrameMioTypography.caption)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                    .lineLimit(1)
            }
        }
        .padding(FrameMioSpacing.lg)
        .background(
            FrameMioColors.surface.opacity(0.9),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.72), lineWidth: FrameMioMetrics.borderWidth)
        }
        .task(id: "\(generation.id)-\(generation.updatedAt.timeIntervalSince1970)-\(generation.status.rawValue)") {
            guard generation.status == .completed, playableURL == nil else { return }
            isLoadingContent = true
            contentError = nil
            defer { isLoadingContent = false }
            do {
                playableURL = try await service.downloadContent(for: generation.id)
            } catch is CancellationError {
            } catch {
                contentError = AppError.from(error).localizedDescription
            }
        }
    }

    private var generationPlaceholder: some View {
        ZStack {
            RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous)
                .fill(FrameMioColors.canvas)

            VStack(spacing: FrameMioSpacing.sm) {
                if isLoadingContent || generation.status == .queued || generation.status == .running {
                    ProgressView(value: max(0, min(1, generation.progress)))
                        .progressViewStyle(.circular)
                }
                Text(placeholderTitle)
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                if let contentError {
                    Text(contentError)
                        .font(FrameMioTypography.caption)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .multilineTextAlignment(.center)
                }
            }
            .padding(FrameMioSpacing.lg)
        }
        .aspectRatio(videoAspectRatio, contentMode: .fit)
    }

    private var placeholderTitle: String {
        switch generation.status {
        case .queued:
            "Queued"
        case .running:
            generation.progress > 0
                ? "Generating \(Int(generation.progress * 100))%"
                : "Generating..."
        case .completed:
            "Loading video..."
        case .failed:
            "Generation failed"
        }
    }

    private var statusTitle: String {
        switch generation.status {
        case .queued: "Queued"
        case .running: "Generating"
        case .completed: "Ready"
        case .failed: "Failed"
        }
    }

    private var videoAspectRatio: CGFloat {
        let parts = generation.aspectRatio.split(separator: ":")
        guard parts.count == 2,
              let width = Double(parts[0]),
              let height = Double(parts[1]),
              width > 0,
              height > 0 else {
            return 9.0 / 16.0
        }
        return width / height
    }
}

private struct TextToVideoSamplePromptsSheet: View {
    let prompts: [String]
    let onSelect: (String) -> Void

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(prompts.enumerated()), id: \.element) { index, prompt in
                        Button {
                            onSelect(prompt)
                            dismiss()
                        } label: {
                            Text(prompt)
                                .font(FrameMioTypography.body)
                                .foregroundStyle(FrameMioColors.ink)
                                .multilineTextAlignment(.leading)
                                .fixedSize(horizontal: false, vertical: true)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(.vertical, FrameMioSpacing.lg)
                        }
                        .buttonStyle(.plain)

                        if index < prompts.count - 1 {
                            Divider()
                                .overlay(FrameMioColors.divider.opacity(0.72))
                        }
                    }
                }
                .padding(.horizontal, FrameMioSpacing.lg)
            }
            .background(FrameMioColors.canvas.ignoresSafeArea())
            .navigationTitle("Sample Prompts")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        dismiss()
                    }
                }
            }
        }
    }
}

private enum TextVideoDurationOption: Int, CaseIterable, Identifiable {
    case five = 5
    case eight = 8
    case ten = 10
    case fifteen = 15

    var id: Int { rawValue }
    var seconds: Int { rawValue }
    var title: String { "\(rawValue) seconds" }
}

private enum TextVideoResolutionOption: String, CaseIterable, Identifiable {
    case fourEighty = "480p"
    case sevenTwenty = "720p"
    case tenEighty = "1080p"

    var id: String { rawValue }
    var title: String { rawValue }
}

private enum TextVideoAspectRatioOption: String, CaseIterable, Identifiable {
    case threeFour = "3:4"
    case square = "1:1"
    case fourThree = "4:3"
    case nineSixteen = "9:16"
    case sixteenNine = "16:9"

    var id: String { rawValue }
    var title: String { rawValue }
}
