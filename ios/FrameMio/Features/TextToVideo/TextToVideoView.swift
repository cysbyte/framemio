import AVFoundation
import PhotosUI
import SwiftUI
import UIKit

enum TextToVideoMode: Hashable, Sendable {
    case textToVideo
    case imageToVideo

    var navigationTitle: String {
        switch self {
        case .textToVideo:
            "Text to Video"
        case .imageToVideo:
            "Image to Video"
        }
    }

    var promptPlaceholder: String {
        switch self {
        case .textToVideo:
            "Example: A cinematic golden-hour walk along the coast..."
        case .imageToVideo:
            "Example: Gently move the camera forward while the light flickers..."
        }
    }

    var promptSubtitle: String {
        switch self {
        case .textToVideo:
            "Describe the scene, mood, or story you want to generate."
        case .imageToVideo:
            "Describe the motion, camera movement, or visual changes for this image."
        }
    }
}

struct TextToVideoView: View {
    let mode: TextToVideoMode
    let service: TextToVideoService
    let creditService: CreditService?
    let promptTranscriptionService: PromptTranscriptionService?
    let photoLibrary: PhotoLibraryService?
    let mediaUploadService: MediaUploadService?
    let onOpenPricing: (PricingEntryPoint) -> Void
    let onCreditsUpdated: (() -> Void)?

    @State private var prompt = ""
    @State private var duration = TextVideoDurationOption.five
    @State private var resolution = TextVideoResolutionOption.sevenTwenty
    @State private var aspectRatio = TextVideoAspectRatioOption.nineSixteen
    @State private var isGenerating = false
    @State private var isSubmittingGeneration = false
    @State private var generations: [TextVideoGeneration] = []
    @State private var activeGeneration: TextVideoGeneration?
    @State private var recentlyCompletedGeneration: TextVideoGeneration?
    @State private var submittedGenerationID: String?
    @State private var pendingPrompt: String?
    @State private var pendingDuration: Int?
    @State private var pendingResolution: String?
    @State private var pendingAspectRatio: String?
    @State private var pendingImageMediaID: String?
    @State private var pendingIdempotencyKey: String?
    @State private var errorMessage: String?
    @State private var creditCost: Int
    @State private var isCheckingCredits = false
    @State private var isShowingSamplePrompts = false
    @State private var isShowingAllHistory = false
    @State private var selectedPhotoItem: PhotosPickerItem?
    @State private var selectedImageAsset: MediaAsset?
    @State private var selectedImageThumbnail: UIImage?
    @State private var isResolvingImage = false
    @State private var isUploadingSourceImage = false

    private let samplePrompts = [
        "A cinematic vertical video of a small sailboat gliding across calm turquoise water at sunrise, soft golden light reflecting on the waves, gentle ocean mist, smooth aerial camera movement, realistic details, peaceful and inspiring mood, natural colors, no text or logos.",
        "A cozy cabin in a snowy forest at blue hour, warm light glowing through the windows, smoke rising from the chimney, slow push-in camera movement, soft snowfall, cinematic realism, peaceful winter atmosphere, no text or logos.",
        "A bustling night market in Tokyo after rain, colorful neon reflections on wet pavement, people moving through food stalls, smooth handheld camera movement, vibrant cinematic lighting, realistic details, no text or logos."
    ]

    init(
        mode: TextToVideoMode = .textToVideo,
        service: TextToVideoService,
        creditService: CreditService? = nil,
        promptTranscriptionService: PromptTranscriptionService? = nil,
        photoLibrary: PhotoLibraryService? = nil,
        mediaUploadService: MediaUploadService? = nil,
        onOpenPricing: @escaping (PricingEntryPoint) -> Void = { _ in },
        onCreditsUpdated: (() -> Void)? = nil
    ) {
        self.mode = mode
        self.service = service
        self.creditService = creditService
        self.promptTranscriptionService = promptTranscriptionService
        self.photoLibrary = photoLibrary
        self.mediaUploadService = mediaUploadService
        self.onOpenPricing = onOpenPricing
        self.onCreditsUpdated = onCreditsUpdated
        _creditCost = State(initialValue: CreditPricing.price(for: .createVideo))
    }

    private var canGenerate: Bool {
        !prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
            (mode == .textToVideo || selectedImageAsset != nil) &&
            !isGenerating &&
            !isCheckingCredits
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.xl) {
                if isGenerating {
                    generatingVideoSection
                }
                if let recentlyCompletedGeneration {
                    completedVideoSection(for: recentlyCompletedGeneration)
                }
                if mode == .imageToVideo {
                    imageSourceSection
                }
                promptSection
                settingsSection
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
        .navigationTitle(mode.navigationTitle)
        .navigationBarTitleDisplayMode(.inline)
        .scrollDismissesKeyboard(.immediately)
        .navigationDestination(isPresented: $isShowingAllHistory) {
            TextVideoHistoryView(
                generations: historyGenerations,
                service: service,
                onDeleted: {
                    Task { await refreshGenerations() }
                }
            )
        }
        .safeAreaInset(edge: .bottom) {
            fixedGenerateButton
        }
        .task {
            await refreshCreditQuote()
            await refreshGenerationHistoryLoop()
        }
        .alert("Couldn’t generate video", isPresented: errorBinding) {
            Button("Try Again") {
                retryGeneration()
            }
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
        .onChange(of: selectedPhotoItem) { _, item in
            resolveSelectedImage(item)
        }
    }

    private var generatingVideoSection: some View {
        let progress = activeGeneration.map { max(0, min(1, $0.progress)) }

        return VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            HStack(spacing: FrameMioSpacing.sm) {
                ProgressView()
                    .controlSize(.small)
                Text(isUploadingSourceImage ? "Uploading image" : "Generating video")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.ink)
                Spacer(minLength: FrameMioSpacing.sm)
                Text(progress.map { "\(Int($0 * 100))%" } ?? "Preparing")
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(FrameMioColors.secondaryInk)
            }

            FrameMioProgressView(
                value: progress,
                title: isUploadingSourceImage
                    ? "Preparing your image"
                    : activeGeneration?.status == .queued
                        ? "Queued"
                        : "FrameMio is creating your video"
            )

            Text(
                "\(activeGeneration?.seconds ?? pendingDuration ?? duration.seconds)s · " +
                "\(activeGeneration?.resolution ?? pendingResolution ?? resolution.rawValue) · " +
                "\(activeGeneration?.aspectRatio ?? pendingAspectRatio ?? aspectRatio.rawValue)"
            )
                .font(FrameMioTypography.caption)
                .foregroundStyle(FrameMioColors.secondaryInk)
        }
        .padding(FrameMioSpacing.lg)
        .background(
            FrameMioColors.surface.opacity(0.92),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
                .stroke(FrameMioColors.accent.opacity(0.4), lineWidth: FrameMioMetrics.borderWidth)
        }
    }

    private var imageSourceSection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            Text("Source image")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)

            if let thumbnail = selectedImageThumbnail {
                Image(uiImage: thumbnail)
                    .resizable()
                    .scaledToFill()
                    .frame(maxWidth: .infinity)
                    .aspectRatio(imageAspectRatio, contentMode: .fit)
                    .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous))
                    .overlay(alignment: .topTrailing) {
                        Button {
                            selectedPhotoItem = nil
                            selectedImageAsset = nil
                            self.selectedImageThumbnail = nil
                            pendingImageMediaID = nil
                        } label: {
                            Image(systemName: "xmark")
                                .font(.caption.weight(.bold))
                                .foregroundStyle(FrameMioColors.inverseInk)
                                .frame(width: 30, height: 30)
                                .background(FrameMioColors.mediaScrim, in: Circle())
                        }
                        .padding(FrameMioSpacing.sm)
                        .disabled(isGenerating)
                        .accessibilityLabel("Remove source image")
                    }
            } else if isResolvingImage {
                ProgressView("Preparing image")
                    .frame(maxWidth: .infinity, minHeight: 180)
            } else {
                PhotosPicker(
                    selection: $selectedPhotoItem,
                    matching: .images,
                    preferredItemEncoding: .current
                ) {
                    Label("Choose Image", systemImage: "photo.on.rectangle.angled")
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.accent)
                        .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.buttonHeight)
                        .overlay {
                            RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                                .stroke(FrameMioColors.accent.opacity(0.85), lineWidth: FrameMioMetrics.borderWidth)
                        }
                }
                .buttonStyle(FrameMioPressButtonStyle())
                .disabled(isGenerating)
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

    private var imageAspectRatio: CGFloat {
        guard let selectedImageAsset,
              selectedImageAsset.pixelWidth > 0,
              selectedImageAsset.pixelHeight > 0 else {
            return 9.0 / 16.0
        }
        return CGFloat(selectedImageAsset.pixelWidth) / CGFloat(selectedImageAsset.pixelHeight)
    }

    private func completedVideoSection(for generation: TextVideoGeneration) -> some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            HStack {
                Text("Video ready")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.ink)
                Spacer()
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(FrameMioColors.accent)
            }

            TextVideoThumbnail(generation: generation, service: service)
                .frame(maxWidth: .infinity)

            NavigationLink {
                TextVideoDetailView(
                    generation: generation,
                    service: service,
                    onDeleted: {
                        recentlyCompletedGeneration = nil
                        Task { await refreshGenerations() }
                    }
                )
            } label: {
                Label("Play video", systemImage: "play.fill")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.inverseInk)
                    .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.compactButtonHeight)
                    .background(
                        FrameMioColors.accent,
                        in: RoundedRectangle(cornerRadius: FrameMioRadius.control, style: .continuous)
                    )
            }
            .buttonStyle(FrameMioPressButtonStyle())
        }
        .padding(FrameMioSpacing.lg)
        .background(
            FrameMioColors.surface.opacity(0.92),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
                .stroke(FrameMioColors.accent.opacity(0.4), lineWidth: FrameMioMetrics.borderWidth)
        }
    }

    private var promptSection: some View {
        FrameMioPromptInputSection(
            title: "Prompt",
            subtitle: mode.promptSubtitle,
            placeholder: mode.promptPlaceholder,
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

    private var historySection: some View {
        let history = historyGenerations
        let recentHistory = Array(history.prefix(7))

        return VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(
                title: "Generated videos",
                subtitle: history.isEmpty ? nil : "\(history.count) \(history.count == 1 ? "video" : "videos")",
                actionTitle: history.isEmpty ? nil : "See all",
                action: history.isEmpty ? nil : { isShowingAllHistory = true }
            )

            if history.isEmpty {
                EmptyState(
                    systemImage: "film.stack",
                    title: "No generated videos yet",
                    message: "Your finished videos will appear here."
                )
            } else {
                LazyVStack(alignment: .leading, spacing: FrameMioSpacing.md) {
                    ForEach(recentHistory) { generation in
                        NavigationLink {
                            TextVideoDetailView(
                                generation: generation,
                                service: service,
                                onDeleted: {
                                    Task { await refreshGenerations() }
                                }
                            )
                        } label: {
                            TextVideoHistoryRow(generation: generation, service: service)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
        .onTapGesture(perform: dismissKeyboard)
    }

    private var historyGenerations: [TextVideoGeneration] {
        generations
            .filter { $0.status == .completed || $0.status == .failed }
            .sorted { left, right in
                if left.createdAt == right.createdAt {
                    return left.id > right.id
                }
                return left.createdAt > right.createdAt
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
            await MainActor.run { creditCost = quote.credits }
        } catch is CancellationError {
        } catch {
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

    private func generate(reusePendingRequest: Bool = false) {
        guard !isGenerating else { return }
        isGenerating = true
        isSubmittingGeneration = true
        activeGeneration = nil
        recentlyCompletedGeneration = nil
        if !reusePendingRequest || pendingPrompt == nil {
            pendingPrompt = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
            pendingDuration = duration.seconds
            pendingResolution = resolution.rawValue
            pendingAspectRatio = aspectRatio.rawValue
            pendingIdempotencyKey = "ios-text-video-\(UUID().uuidString)"
        }
        errorMessage = nil

        let requestPrompt = pendingPrompt ?? ""
        let requestDuration = pendingDuration ?? duration.seconds
        let requestResolution = pendingResolution ?? resolution.rawValue
        let requestAspectRatio = pendingAspectRatio ?? aspectRatio.rawValue
        let requestIdempotencyKey = pendingIdempotencyKey ?? "ios-text-video-\(UUID().uuidString)"
        let requestImageMediaID = pendingImageMediaID
        Task {
            do {
                let imageMediaID = try await uploadSourceImageIfNeeded(existingMediaID: requestImageMediaID)
                let created = try await service.create(
                    prompt: requestPrompt,
                    duration: requestDuration,
                    resolution: requestResolution,
                    aspectRatio: requestAspectRatio,
                    idempotencyKey: requestIdempotencyKey,
                    imageMediaID: imageMediaID
                )
                await MainActor.run {
                    isSubmittingGeneration = false
                    activeGeneration = created.status == .queued || created.status == .running ? created : nil
                    isGenerating = created.status == .queued || created.status == .running
                    submittedGenerationID = created.id
                    recentlyCompletedGeneration = created.status == .completed ? created : nil
                    if !isGenerating {
                        clearPendingGeneration()
                    }
                    onCreditsUpdated?()
                }
                await refreshGenerations()
                if let creditService {
                    _ = try? await creditService.refreshBalance()
                }
            } catch is CancellationError {
                await MainActor.run {
                    isSubmittingGeneration = false
                    isGenerating = false
                    activeGeneration = nil
                    clearPendingGeneration()
                }
            } catch {
                await MainActor.run {
                    isSubmittingGeneration = false
                    isGenerating = false
                    activeGeneration = nil
                    let appError = AppError.from(error)
                    if case .insufficientCredits = appError {
                        clearPendingGeneration()
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
                if let active = latest.first(where: { $0.status == .queued || $0.status == .running }) {
                    activeGeneration = active
                    isGenerating = true
                    recentlyCompletedGeneration = nil
                } else if let submittedGenerationID,
                          let completed = latest.first(where: {
                              $0.id == submittedGenerationID && $0.status == .completed
                          }) {
                    activeGeneration = nil
                    isGenerating = false
                    recentlyCompletedGeneration = completed
                    clearPendingGeneration()
                } else if !isSubmittingGeneration {
                    activeGeneration = nil
                    isGenerating = false
                    if errorMessage == nil {
                        clearPendingGeneration()
                    }
                }
            }
        } catch is CancellationError {
        } catch {
        }
    }

    private var errorBinding: Binding<Bool> {
        Binding(
            get: { errorMessage != nil },
            set: { if !$0 { errorMessage = nil } }
        )
    }

    private func clearPendingGeneration() {
        pendingPrompt = nil
        pendingDuration = nil
        pendingResolution = nil
        pendingAspectRatio = nil
        pendingImageMediaID = nil
        pendingIdempotencyKey = nil
    }

    private func resolveSelectedImage(_ item: PhotosPickerItem?) {
        guard mode == .imageToVideo, let item else { return }
        isResolvingImage = true
        errorMessage = nil
        pendingImageMediaID = nil
        Task { @MainActor in
            defer { isResolvingImage = false }
            guard let photoLibrary else {
                errorMessage = "Photo access is not configured for this environment."
                return
            }
            guard let identifier = item.itemIdentifier, !identifier.isEmpty else {
                errorMessage = "This photo could not be linked to your library. Try selecting it again."
                selectedPhotoItem = nil
                return
            }
            do {
                let assets = try await photoLibrary.resolveAssets(localIdentifiers: [identifier])
                guard let asset = assets.first, asset.kind == .image else {
                    throw AppError.unsupportedMedia
                }
                let thumbnailData = try await photoLibrary.requestThumbnail(
                    for: asset,
                    targetSize: CGSize(width: 900, height: 900)
                )
                guard let thumbnail = UIImage(data: thumbnailData) else {
                    throw AppError.invalidResponse
                }
                selectedImageAsset = asset
                selectedImageThumbnail = thumbnail
            } catch {
                selectedPhotoItem = nil
                selectedImageAsset = nil
                selectedImageThumbnail = nil
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    private func uploadSourceImageIfNeeded(existingMediaID: String?) async throws -> String? {
        guard mode == .imageToVideo else { return nil }
        if let existingMediaID, !existingMediaID.isEmpty {
            return existingMediaID
        }
        guard let selectedImageAsset,
              let mediaUploadService else {
            throw AppError.message("Choose an image before generating a video.")
        }

        await MainActor.run { isUploadingSourceImage = true }
        defer {
            Task { @MainActor in
                isUploadingSourceImage = false
            }
        }

        let uploads = try await mediaUploadService.upload(
            selection: MediaSelection(explicitlySelectedAssetIDs: [selectedImageAsset.id]),
            assets: [selectedImageAsset],
            options: MediaUploadOptions(),
            progress: { _ in }
        )
        guard let mediaID = uploads.first(where: { $0.status == .completed })?.mediaID,
              !mediaID.isEmpty else {
            throw AppError.message("The source image could not be uploaded. Try again.")
        }
        await MainActor.run {
            pendingImageMediaID = mediaID
        }
        return mediaID
    }

    private func retryGeneration() {
        guard pendingPrompt != nil else {
            requestGenerate()
            return
        }
        generate(reusePendingRequest: true)
    }
}

private struct TextVideoThumbnail: View {
    let generation: TextVideoGeneration
    let service: TextToVideoService

    @State private var image: Image?
    @State private var isLoading = false

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous)
                .fill(FrameMioColors.canvas)

            if let image {
                image
                    .resizable()
                    .scaledToFill()
            } else if isLoading {
                ProgressView()
                    .tint(FrameMioColors.accent)
            } else {
                Image(systemName: generation.status == .failed ? "exclamationmark.triangle" : "film")
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(generation.status == .failed ? .red : FrameMioColors.accent)
            }

            if generation.status == .completed {
                Image(systemName: "play.fill")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(FrameMioColors.inverseInk)
                    .frame(width: 32, height: 32)
                    .background(FrameMioColors.mediaScrim, in: Circle())
            }
        }
        .aspectRatio(videoAspectRatio, contentMode: .fit)
        .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .clipped()
        .task(id: generation.id) {
            await loadThumbnail()
        }
        .accessibilityLabel(generation.status == .completed ? "Generated video thumbnail" : "Video generation status")
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

    private func loadThumbnail() async {
        guard generation.status == .completed, image == nil, !isLoading else { return }
        isLoading = true
        defer { isLoading = false }

        do {
            let videoURL = try await service.downloadContent(for: generation.id)
            defer { try? FileManager.default.removeItem(at: videoURL) }

            let asset = AVURLAsset(url: videoURL)
            let generator = AVAssetImageGenerator(asset: asset)
            generator.appliesPreferredTrackTransform = true
            let result = try await generator.image(at: .zero)
            guard !Task.isCancelled else { return }
            image = Image(uiImage: UIImage(cgImage: result.image))
        } catch is CancellationError {
        } catch {
        }
    }
}

private struct TextVideoHistoryRow: View {
    let generation: TextVideoGeneration
    let service: TextToVideoService

    var body: some View {
        HStack(spacing: FrameMioSpacing.md) {
            TextVideoThumbnail(generation: generation, service: service)
                .frame(width: 92)

            VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
                Text(generation.prompt)
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.ink)
                    .lineLimit(3)
                    .fixedSize(horizontal: false, vertical: true)
                Text(generation.status == .completed ? "Ready to play" : "Generation failed")
                    .font(FrameMioTypography.footnote)
                    .foregroundStyle(generation.status == .failed ? .red : FrameMioColors.secondaryInk)
                Text("\(generation.seconds)s · \(generation.resolution) · \(generation.aspectRatio)")
                    .font(FrameMioTypography.caption)
                    .foregroundStyle(FrameMioColors.secondaryInk)
                    .lineLimit(1)
                Text(generation.createdAt.formatted(.dateTime.month(.abbreviated).day().year().hour().minute()))
                    .font(FrameMioTypography.caption)
                    .foregroundStyle(FrameMioColors.tertiaryInk)
                    .lineLimit(1)
            }

            Spacer(minLength: FrameMioSpacing.xs)
            Image(systemName: "chevron.right")
                .font(FrameMioTypography.caption.weight(.semibold))
                .foregroundStyle(FrameMioColors.tertiaryInk)
        }
        .padding(FrameMioSpacing.md)
        .background(
            FrameMioColors.surface.opacity(0.86),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.subtle)
    }
}

private struct TextVideoHistoryView: View {
    let generations: [TextVideoGeneration]
    let service: TextToVideoService
    let onDeleted: () -> Void

    @State private var pageIndex = 0

    private let pageSize = 10

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
                SectionHeader(
                    title: "Video History",
                    subtitle: pageSubtitle
                )

                if pageGenerations.isEmpty {
                    EmptyState(
                        systemImage: "film.stack",
                        title: "No generated videos yet",
                        message: "Your finished videos will appear here."
                    )
                } else {
                    LazyVStack(alignment: .leading, spacing: FrameMioSpacing.md) {
                        ForEach(pageGenerations) { generation in
                            NavigationLink {
                                TextVideoDetailView(
                                    generation: generation,
                                    service: service,
                                    onDeleted: onDeleted
                                )
                            } label: {
                                TextVideoHistoryRow(generation: generation, service: service)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }

                if pageCount > 1 {
                    paginationControls
                }
            }
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.lg)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background(FrameMioColors.canvas.ignoresSafeArea())
        .navigationTitle("Video History")
        .navigationBarTitleDisplayMode(.inline)
        .onChange(of: generations.count) { _, _ in
            pageIndex = min(pageIndex, max(pageCount - 1, 0))
        }
    }

    private var pageGenerations: [TextVideoGeneration] {
        let startIndex = pageIndex * pageSize
        guard startIndex < generations.count else { return [] }
        let endIndex = min(startIndex + pageSize, generations.count)
        return Array(generations[startIndex..<endIndex])
    }

    private var pageCount: Int {
        max(1, Int(ceil(Double(generations.count) / Double(pageSize))))
    }

    private var pageSubtitle: String? {
        guard !generations.isEmpty else { return nil }
        let firstItem = pageIndex * pageSize + 1
        let lastItem = min(firstItem + pageGenerations.count - 1, generations.count)
        return "Showing \(firstItem)-\(lastItem) of \(generations.count)"
    }

    private var paginationControls: some View {
        HStack(spacing: FrameMioSpacing.md) {
            Button {
                FrameMioHaptics.secondaryAction()
                pageIndex = max(pageIndex - 1, 0)
            } label: {
                Label("Previous", systemImage: "chevron.left")
            }
            .disabled(pageIndex == 0)

            Spacer()

            Text("Page \(pageIndex + 1) of \(pageCount)")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)

            Spacer()

            Button {
                FrameMioHaptics.secondaryAction()
                pageIndex = min(pageIndex + 1, pageCount - 1)
            } label: {
                Label("Next", systemImage: "chevron.right")
                    .labelStyle(.titleAndIcon)
            }
            .disabled(pageIndex >= pageCount - 1)
        }
        .font(FrameMioTypography.headline)
        .foregroundStyle(FrameMioColors.accent)
        .padding(FrameMioSpacing.md)
        .background(
            FrameMioColors.surface.opacity(0.86),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
    }
}

private struct TextVideoDetailView: View {
    let generation: TextVideoGeneration
    let service: TextToVideoService
    let onDeleted: () -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var playableURL: URL?
    @State private var isLoadingContent = false
    @State private var errorMessage: String?
    @State private var isShowingDeleteConfirmation = false
    @State private var isDeleting = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.xl) {
                if generation.status == .completed, let playableURL {
                    StoryVideoPlayer(
                        url: playableURL,
                        autoplays: true,
                        accessibilityTitle: "Generated video"
                    )
                    .aspectRatio(videoAspectRatio, contentMode: .fit)
                    .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous))
                } else {
                    detailPlaceholder
                }

                VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
                    Text("Prompt")
                        .font(FrameMioTypography.title)
                        .foregroundStyle(FrameMioColors.ink)
                    Text(generation.prompt)
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.ink)
                        .fixedSize(horizontal: false, vertical: true)
                }

                VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                    Text("Video settings")
                        .font(FrameMioTypography.title)
                        .foregroundStyle(FrameMioColors.ink)
                    Text("\(generation.seconds) seconds · \(generation.resolution) · \(generation.aspectRatio)")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                    Text("Created \(generation.createdAt.formatted(.dateTime.month(.wide).day().year().hour().minute()))")
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.tertiaryInk)
                }

                Button(role: .destructive) {
                    isShowingDeleteConfirmation = true
                } label: {
                    Label(
                        isDeleting ? "Deleting..." : "Delete video",
                        systemImage: isDeleting ? "hourglass" : "trash"
                    )
                    .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.compactButtonHeight)
                }
                .buttonStyle(FrameMioPressButtonStyle())
                .disabled(isDeleting)
            }
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.lg)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background(FrameMioColors.canvas.ignoresSafeArea())
        .navigationTitle("Generated Video")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            guard generation.status == .completed, playableURL == nil else { return }
            isLoadingContent = true
            defer { isLoadingContent = false }
            do {
                playableURL = try await service.downloadContent(for: generation.id)
            } catch is CancellationError {
            } catch {
                errorMessage = AppError.from(error).localizedDescription
            }
        }
        .alert("Couldn’t load video", isPresented: errorBinding) {
            Button("Dismiss", role: .cancel) { errorMessage = nil }
        } message: {
            Text(errorMessage ?? "")
        }
        .alert("Delete video?", isPresented: $isShowingDeleteConfirmation) {
            Button("Cancel", role: .cancel) {}
            Button("Delete", role: .destructive) {
                deleteVideo()
            }
        } message: {
            Text("This permanently removes the generated video from FrameMio.")
        }
    }

    private var detailPlaceholder: some View {
        VStack(spacing: FrameMioSpacing.md) {
            if isLoadingContent {
                ProgressView()
            } else {
                Image(systemName: generation.status == .failed ? "exclamationmark.triangle" : "film")
                    .font(.title2)
                    .foregroundStyle(generation.status == .failed ? .red : FrameMioColors.accent)
            }
            Text(generation.status == .failed ? (generation.errorMessage ?? "Generation failed") : "Loading video...")
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
        }
        .frame(maxWidth: .infinity)
        .aspectRatio(videoAspectRatio, contentMode: .fit)
        .background(
            FrameMioColors.surface,
            in: RoundedRectangle(cornerRadius: FrameMioRadius.container, style: .continuous)
        )
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

    private var errorBinding: Binding<Bool> {
        Binding(
            get: { errorMessage != nil },
            set: { if !$0 { errorMessage = nil } }
        )
    }

    private func deleteVideo() {
        guard !isDeleting else { return }
        isDeleting = true
        Task {
            do {
                try await service.delete(generationID: generation.id)
                await MainActor.run {
                    FrameMioHaptics.success()
                    onDeleted()
                    dismiss()
                }
            } catch is CancellationError {
                await MainActor.run { isDeleting = false }
            } catch {
                await MainActor.run {
                    isDeleting = false
                    errorMessage = AppError.from(error).localizedDescription
                }
            }
        }
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
                    Button("Cancel") { dismiss() }
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
