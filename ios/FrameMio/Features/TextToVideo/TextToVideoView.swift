import SwiftUI

struct TextToVideoView: View {
    let service: TextToVideoService
    let creditService: CreditService?
    let promptTranscriptionService: PromptTranscriptionService?
    let onOpenPricing: (PricingEntryPoint) -> Void
    let onCreditsUpdated: (() -> Void)?

    @Environment(\.openURL) private var openURL
    @State private var prompt = ""
    @State private var duration = TextVideoDurationOption.five
    @State private var resolution = TextVideoResolutionOption.sevenTwenty
    @State private var aspectRatio = TextVideoAspectRatioOption.nineSixteen
    @State private var isGenerating = false
    @State private var statusText: String?
    @State private var generatedVideoURL: URL?
    @State private var errorMessage: String?
    @State private var creditCost: Int
    @State private var isCheckingCredits = false

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
                header
                promptSection
                settingsSection
                generationStatusSection
            }
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.lg)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background(FrameMioColors.canvas.ignoresSafeArea())
        .navigationTitle("Text to Video")
        .navigationBarTitleDisplayMode(.inline)
        .scrollDismissesKeyboard(.interactively)
        .safeAreaInset(edge: .bottom) {
            fixedGenerateButton
        }
        .task {
            await refreshCreditQuote()
        }
        .alert("Couldn’t generate video", isPresented: errorBinding) {
            Button("Dismiss", role: .cancel) { errorMessage = nil }
        } message: {
            Text(errorMessage ?? "")
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
            Text("Write a scene, mood, or story and FrameMio will turn it into a short video.")
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
                .fixedSize(horizontal: false, vertical: true)
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
            transcriptLogContext: "text_to_video"
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
                    ProgressView()
                        .controlSize(.small)
                    Text(statusText)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }
            }

            if let generatedVideoURL {
                Button {
                    openURL(generatedVideoURL)
                } label: {
                    Label("Open generated video", systemImage: "play.rectangle")
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.accent)
                        .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.minimumTouchTarget)
                }
                .buttonStyle(FrameMioPressButtonStyle())
            }
        }
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
        }
    }

    private func refreshCreditQuote() async {
        guard let creditService else { return }
        do {
            let quote = try await creditService.quote(action: .createVideo, stats: CreditMediaStats())
            await MainActor.run {
                creditCost = quote.credits
            }
        } catch {
            await MainActor.run {
                if errorMessage == nil {
                    errorMessage = AppError.from(error).localizedDescription
                }
            }
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
        generatedVideoURL = nil
        errorMessage = nil
        statusText = "Preparing your video..."

        let requestPrompt = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        Task {
            do {
                let result = try await service.generate(
                    prompt: requestPrompt,
                    duration: duration.seconds,
                    resolution: resolution.rawValue,
                    aspectRatio: aspectRatio.rawValue,
                    progress: { update in
                        Task { @MainActor in
                            statusText = statusDescription(for: update)
                        }
                    }
                )
                await MainActor.run {
                    generatedVideoURL = result.videoURL
                    statusText = "Your video is ready."
                    isGenerating = false
                    onCreditsUpdated?()
                }
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

    private func statusDescription(for update: TextVideoGenerationStatus) -> String {
        if let progress = update.progress, !progress.isEmpty {
            return "Generating your video... \(progress)%"
        }
        switch update.status {
        case "queued", "pending":
            return "Your video is queued..."
        case "running", "processing", "in_progress":
            return "Generating your video..."
        default:
            return "Checking video status..."
        }
    }

    private var errorBinding: Binding<Bool> {
        Binding(
            get: { errorMessage != nil },
            set: { if !$0 { errorMessage = nil } }
        )
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
