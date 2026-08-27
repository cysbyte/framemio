import Photos
import SwiftUI
import UIKit
import UniformTypeIdentifiers

struct MediaPickerView: View {
    @Environment(\.openURL) private var openURL
    @StateObject private var viewModel: MediaPickerViewModel
    @State private var captureMode: MomentCaptureMode?
    @State private var captureErrorMessage: String?
    let onContinue: ([MediaAsset]) -> Void

    init(viewModel: MediaPickerViewModel, onContinue: @escaping ([MediaAsset]) -> Void) {
        _viewModel = StateObject(wrappedValue: viewModel)
        self.onContinue = onContinue
    }

    var body: some View {
        Group {
            if viewModel.isAccessBlocked {
                accessState
            } else if !viewModel.hasCompletedInitialLoad || (viewModel.isLoading && viewModel.totalAssetCount == 0) {
                LoadingState(
                    title: "Finding your moments",
                    message: "Looking through the time and place around \(viewModel.context.rawValue.lowercased())."
                )
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if viewModel.totalAssetCount == 0 {
                emptyMomentsState
            } else {
                mediaBrowser
            }
        }
        .background {
            LinearGradient(
                colors: [FrameMioColors.canvas, FrameMioColors.accentMuted.opacity(0.26)],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )
            .ignoresSafeArea()
        }
        .navigationTitle(viewModel.context.pickerTitle)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Text("\(viewModel.selectedCount)")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.accent)
                    .accessibilityLabel("\(viewModel.selectedCount) selected")
            }
        }
        .safeAreaInset(edge: .bottom) {
            if !viewModel.isAccessBlocked && viewModel.totalAssetCount > 0 {
                createStoryAction
            }
        }
        .task { viewModel.load() }
        .sheet(item: $captureMode) { mode in
            CameraCaptureView(mode: mode) { result in
                captureMode = nil
                handleCaptureResult(result)
            }
        }
        .alert("Photo access needed", isPresented: errorBinding) {
            Button("Try Again") { viewModel.load() }
            Button("Dismiss", role: .cancel) { viewModel.errorMessage = nil }
        } message: {
            Text(viewModel.errorMessage ?? "")
        }
        .alert("Capture unavailable", isPresented: captureErrorBinding) {
            Button("OK", role: .cancel) { captureErrorMessage = nil }
        } message: {
            Text(captureErrorMessage ?? "")
        }
    }

    private var mediaBrowser: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: FrameMioSpacing.xxl) {
                pickerIntro

                if viewModel.authorization == .limited {
                    limitedAccessNotice
                }

                captureMomentActions

                filterPicker

                if viewModel.sections.isEmpty {
                    EmptyState(
                        systemImage: viewModel.filter == .videos ? "video.slash" : "photo.on.rectangle.angled",
                        title: "Nothing in this filter",
                        message: "Try another media filter to keep exploring your moments."
                    )
                    .frame(maxWidth: .infinity)
                } else {
                    ForEach(viewModel.sections) { section in
                        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                            Text(section.title)
                                .font(FrameMioTypography.eyebrow)
                                .foregroundStyle(FrameMioColors.secondaryInk)
                                .textCase(.uppercase)

                            LazyVGrid(columns: gridColumns, spacing: FrameMioSpacing.xxs) {
                                ForEach(section.assets) { asset in
                                    mediaCell(for: asset)
                                }
                            }
                        }
                    }
                }
            }
            .padding(.horizontal, FrameMioSpacing.md)
            .padding(.top, FrameMioSpacing.md)
            .padding(.bottom, FrameMioSpacing.xxl)
        }
        .scrollIndicators(.hidden)
    }

    private var pickerIntro: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Tag(viewModel.context.rawValue, systemImage: viewModel.context.icon)

            Text("Choose the moments that belong together.")
                .font(FrameMioTypography.screenTitle)
                .foregroundStyle(FrameMioColors.ink)
                .fixedSize(horizontal: false, vertical: true)

            Text("Mix photos and videos from your camera roll. FrameMio will use your selection as the emotional starting point for the story.")
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
                .fixedSize(horizontal: false, vertical: true)
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
        .accessibilityElement(children: .combine)
    }

    private func mediaCell(for asset: MediaAsset) -> some View {
        MediaThumbnail(
            image: viewModel.image(for: asset),
            duration: asset.isVideo ? asset.duration : nil,
            isLivePhoto: asset.isLivePhoto,
            isSelected: viewModel.selectedIDs.contains(asset.id),
            aspectRatio: FrameMioMetrics.thumbnailAspectRatio,
            accessibilityTitle: accessibilityTitle(for: asset),
            action: { viewModel.toggleSelection(for: asset) }
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

    private var filterPicker: some View {
        Picker(
            "Media type",
            selection: Binding(
                get: { viewModel.filter },
                set: { viewModel.setFilter($0) }
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

    private var limitedAccessNotice: some View {
        HStack(alignment: .top, spacing: FrameMioSpacing.sm) {
            Image(systemName: "info.circle")
                .foregroundStyle(FrameMioColors.accent)

            Text("Showing the photos and videos you chose to share with FrameMio.")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)
        }
        .padding(FrameMioSpacing.md)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(FrameMioColors.accentMuted.opacity(0.74), in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous))
        .accessibilityElement(children: .combine)
    }

    private var captureMomentActions: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            HStack(alignment: .top, spacing: FrameMioSpacing.sm) {
                Image(systemName: "camera.fill")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.accent)
                    .frame(width: 36, height: 36)
                    .background(FrameMioColors.accentMuted.opacity(0.74), in: Circle())
                    .accessibilityHidden(true)

                VStack(alignment: .leading, spacing: FrameMioSpacing.xxs) {
                    Text("Add a fresh moment")
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)

                    Text("Take one more photo or record a quick clip, then FrameMio will refresh this selection.")
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }

            captureButtons
        }
        .padding(FrameMioSpacing.md)
        .background(
            FrameMioColors.surface.opacity(0.78),
            in: RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.large, style: .continuous)
                .stroke(FrameMioColors.divider.opacity(0.62), lineWidth: FrameMioMetrics.borderWidth)
        }
        .accessibilityElement(children: .contain)
    }

    private var captureButtons: some View {
        VStack(spacing: FrameMioSpacing.sm) {
            PrimaryButton(title: "Take Photo", systemImage: "camera.fill") {
                presentCapture(.photo)
            }

            SecondaryButton(title: "Record Video", systemImage: "video.fill") {
                presentCapture(.video)
            }
        }
    }

    private var emptyMomentsState: some View {
        VStack(spacing: FrameMioSpacing.md) {
            EmptyState(
                systemImage: "camera.fill",
                title: "No moments found",
                message: "Capture something new now. FrameMio will save it to your photo library and refresh this view."
            )

            captureButtons
                .frame(maxWidth: FrameMioMetrics.emptyStateMaxWidth)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding(.horizontal, FrameMioSpacing.lg)
    }

    private var accessState: some View {
        EmptyState(
            systemImage: viewModel.authorization == .restricted ? "lock.fill" : "photo.on.rectangle.angled",
            title: viewModel.accessTitle,
            message: viewModel.accessMessage,
            actionTitle: "Open Settings",
            action: openSettings
        )
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var createStoryAction: some View {
        VStack(spacing: FrameMioSpacing.xs) {
            Text(selectionSummary)
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)

            PrimaryButton(title: "Confirm Selection", systemImage: "checkmark") {
                onContinue(viewModel.selectedAssets)
            }
            .disabled(!viewModel.canContinue)
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
        switch viewModel.selectedCount {
        case 0: "Select the moments that belong together"
        case 1: "1 moment selected"
        default: "\(viewModel.selectedCount) moments selected"
        }
    }

    private var gridColumns: [GridItem] {
        Array(
            repeating: GridItem(.flexible(), spacing: FrameMioSpacing.xxs),
            count: FrameMioMetrics.mediaGridColumnCount
        )
    }

    private func accessibilityTitle(for asset: MediaAsset) -> String {
        if asset.isVideo { return "Video" }
        if asset.isLivePhoto { return "Live Photo" }
        return "Photo"
    }

    private func openSettings() {
        guard let settingsURL = URL(string: UIApplication.openSettingsURLString) else { return }
        openURL(settingsURL)
    }

    private func presentCapture(_ mode: MomentCaptureMode) {
        guard UIImagePickerController.isSourceTypeAvailable(.camera) else {
            captureErrorMessage = "Camera capture is not available on this device."
            return
        }
        captureMode = mode
    }

    private func handleCaptureResult(_ result: Result<Void, Error>) {
        switch result {
        case .success:
            viewModel.load()
        case .failure(let error):
            captureErrorMessage = AppError.from(error).localizedDescription
        }
    }

    private var errorBinding: Binding<Bool> {
        Binding(
            get: { viewModel.errorMessage != nil },
            set: { if !$0 { viewModel.errorMessage = nil } }
        )
    }

    private var captureErrorBinding: Binding<Bool> {
        Binding(
            get: { captureErrorMessage != nil },
            set: { if !$0 { captureErrorMessage = nil } }
        )
    }
}

private enum MomentCaptureMode: String, Identifiable {
    case photo
    case video

    var id: String { rawValue }

    var mediaTypes: [String] {
        switch self {
        case .photo:
            [UTType.image.identifier]
        case .video:
            [UTType.movie.identifier]
        }
    }

    var cameraCaptureMode: UIImagePickerController.CameraCaptureMode {
        switch self {
        case .photo: .photo
        case .video: .video
        }
    }
}

private struct CameraCaptureView: UIViewControllerRepresentable {
    let mode: MomentCaptureMode
    let onComplete: (Result<Void, Error>) -> Void

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let picker = UIImagePickerController()
        picker.sourceType = .camera
        picker.mediaTypes = mode.mediaTypes
        picker.cameraCaptureMode = mode.cameraCaptureMode
        picker.videoQuality = .typeHigh
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ uiViewController: UIImagePickerController, context: Context) {}

    func makeCoordinator() -> Coordinator {
        Coordinator(onComplete: onComplete)
    }

    final class Coordinator: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
        private let onComplete: (Result<Void, Error>) -> Void

        init(onComplete: @escaping (Result<Void, Error>) -> Void) {
            self.onComplete = onComplete
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
            picker.dismiss(animated: true)
            onComplete(.success(()))
        }

        func imagePickerController(
            _ picker: UIImagePickerController,
            didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]
        ) {
            if let image = info[.originalImage] as? UIImage {
                savePhoto(image, picker: picker)
                return
            }
            if let videoURL = info[.mediaURL] as? URL {
                saveVideo(videoURL, picker: picker)
                return
            }
            picker.dismiss(animated: true)
            onComplete(.failure(AppError.invalidResponse))
        }

        private func savePhoto(_ image: UIImage, picker: UIImagePickerController) {
            PHPhotoLibrary.shared().performChanges {
                PHAssetChangeRequest.creationRequestForAsset(from: image)
            } completionHandler: { [onComplete] _, error in
                DispatchQueue.main.async {
                    picker.dismiss(animated: true)
                    if let error {
                        onComplete(.failure(error))
                    } else {
                        onComplete(.success(()))
                    }
                }
            }
        }

        private func saveVideo(_ videoURL: URL, picker: UIImagePickerController) {
            PHPhotoLibrary.shared().performChanges {
                PHAssetChangeRequest.creationRequestForAssetFromVideo(atFileURL: videoURL)
            } completionHandler: { [onComplete] _, error in
                DispatchQueue.main.async {
                    picker.dismiss(animated: true)
                    if let error {
                        onComplete(.failure(error))
                    } else {
                        onComplete(.success(()))
                    }
                }
            }
        }
    }
}
