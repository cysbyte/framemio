import Combine
import Foundation
import SwiftUI
import UIKit

private func storyCreationDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][StoryCreation] \(message())")
#endif
}

private enum StoryCreationAnalysisTimeoutError: Error {
    case timedOut
}

@MainActor
final class StoryCreationViewModel: ObservableObject {
    @Published private(set) var progress: Double = 0
    @Published private(set) var phaseTitle = "Preparing your memories..."
    @Published private(set) var isCreating = false
    @Published private(set) var story: Story?
    @Published private(set) var thumbnails: [String: UIImage] = [:]
    @Published var errorMessage: String?

    let context: MomentContext
    let assets: [MediaAsset]
    private let photoLibrary: PhotoLibraryService
    private let analysisService: MediaAnalysisService
    private let mediaUploadService: MediaUploadService
    private let storyService: StoryService
    private var creationTask: Task<Void, Never>?
    private var thumbnailTasks: [String: Task<Void, Never>] = [:]

    init(
        context: MomentContext,
        assets: [MediaAsset],
        photoLibrary: PhotoLibraryService,
        analysisService: MediaAnalysisService,
        mediaUploadService: MediaUploadService,
        storyService: StoryService
    ) {
        self.context = context
        self.assets = assets
        self.photoLibrary = photoLibrary
        self.analysisService = analysisService
        self.mediaUploadService = mediaUploadService
        self.storyService = storyService
    }

    func image(for asset: MediaAsset) -> Image? {
        guard let image = thumbnails[asset.id] else { return nil }
        return Image(uiImage: image)
    }

    func loadThumbnail(for asset: MediaAsset, targetSize: CGSize) {
        guard thumbnails[asset.id] == nil, thumbnailTasks[asset.id] == nil else { return }

        let assetID = asset.id
        thumbnailTasks[assetID] = Task { [weak self, photoLibrary] in
            do {
                let data = try await photoLibrary.requestThumbnail(for: asset, targetSize: targetSize)
                try Task.checkCancellation()
                if let image = UIImage(data: data) {
                    self?.thumbnails[assetID] = image
                }
            } catch is CancellationError {
            } catch {
            }
            self?.thumbnailTasks[assetID] = nil
        }
    }

    func cancelThumbnail(for asset: MediaAsset) {
        thumbnailTasks[asset.id]?.cancel()
        thumbnailTasks[asset.id] = nil
    }

    func createStory() {
        guard !isCreating else {
            storyCreationDebugLog("ignored duplicate create request while already creating")
            return
        }
        storyCreationDebugLog("createStory requested assets=\(assets.count) videos=\(assets.filter(\.isVideo).count)")
        creationTask?.cancel()
        creationTask = Task { [weak self] in
            guard let self else { return }
            isCreating = true
            progress = 0
            story = nil
            errorMessage = nil
            defer { isCreating = false }

            do {
                storyCreationDebugLog("phase=upload_start progress=\(progress)")
                phaseTitle = "Uploading your photos and videos..."
                let selection = MediaSelection(explicitlySelectedAssetIDs: assets.map(\.id))
                _ = try await mediaUploadService.upload(
                    selection: selection,
                    assets: assets,
                    options: MediaUploadOptions(),
                    progress: { [weak self] uploadProgress in
                        Task { @MainActor [weak self] in
                            guard let self else { return }
                            let displayProgress = min(
                                0.28,
                                uploadProgress.fractionCompleted * 0.28
                            )
                            let previousPercent = Int(progress * 100)
                            progress = displayProgress
                            let currentPercent = Int(displayProgress * 100)
                            if currentPercent != previousPercent || currentPercent >= 28 {
                                storyCreationDebugLog("phase=upload_progress app_percent=\(currentPercent) upload_fraction=\(uploadProgress.fractionCompleted) bytes=\(uploadProgress.uploadedBytes)/\(uploadProgress.totalBytes) completed_uploads=\(uploadProgress.completedUploads)/\(uploadProgress.totalUploads) asset=\(uploadProgress.assetID) upload=\(uploadProgress.uploadID)")
                            }
                        }
                    }
                )
                storyCreationDebugLog("phase=upload_complete progress=\(progress)")
                try Task.checkCancellation()
                phaseTitle = "Finding meaningful moments..."
                storyCreationDebugLog("phase=analysis_start assets=\(assets.count)")
                let analyses = try await analyzeAssets()
                storyCreationDebugLog("phase=analysis_complete count=\(analyses.count)")
                try Task.checkCancellation()
                phaseTitle = "Building your story..."
                progress = max(progress, 0.86)
                storyCreationDebugLog("phase=story_create_start app_percent=\(Int(progress * 100)) analyses=\(analyses.count)")
                story = try await storyService.createStory(
                    request: CreateStoryRequest(context: context, assets: assets, analyses: analyses)
                )
                progress = 1
                storyCreationDebugLog("phase=story_create_complete story=\(story?.id ?? "nil") status=\(story?.status.rawValue ?? "nil")")
                phaseTitle = "Story is processing."
                FrameMioHaptics.success()
            } catch is CancellationError {
                storyCreationDebugLog("phase=cancelled progress=\(progress)")
                phaseTitle = "Preparing your memories..."
            } catch {
                let appError = AppError.from(error)
                storyCreationDebugLog("phase=failed progress=\(progress) error=\(appError.localizedDescription) raw=\(error.localizedDescription)")
                errorMessage = appError.localizedDescription
                phaseTitle = "Something went wrong"
            }
        }
    }

    func cancel() {
        creationTask?.cancel()
    }

    private func analyzeAssets() async throws -> [MediaAnalysis] {
        guard !assets.isEmpty else { throw AppError.unsupportedMedia }
        var analyses: [MediaAnalysis] = []
        analyses.reserveCapacity(assets.count)

        try await withThrowingTaskGroup(of: MediaAnalysis.self) { group in
            for asset in assets {
                storyCreationDebugLog("phase=analysis_enqueue asset=\(asset.id) kind=\(asset.kind.rawValue)")
                group.addTask { [analysisService] in
                    try Task.checkCancellation()
                    return try await Self.analyzeWithFallback(
                        asset: asset,
                        analysisService: analysisService,
                        timeout: .seconds(8)
                    )
                }
            }

            for try await analysis in group {
                analyses.append(analysis)
                progress = min(0.82, Double(analyses.count) / Double(assets.count) * 0.82)
                storyCreationDebugLog("phase=analysis_progress app_percent=\(Int(progress * 100)) completed=\(analyses.count)/\(assets.count) asset=\(analysis.assetID) labels=\(analysis.labels.joined(separator: ","))")
            }
        }
        return analyses.sorted { $0.assetID < $1.assetID }
    }

    private static func analyzeWithFallback(
        asset: MediaAsset,
        analysisService: MediaAnalysisService,
        timeout: Duration
    ) async throws -> MediaAnalysis {
        do {
            return try await withThrowingTaskGroup(of: MediaAnalysis.self) { group in
                group.addTask {
                    try await analysisService.analyze(asset: asset)
                }
                group.addTask {
                    try await Task.sleep(for: timeout)
                    throw StoryCreationAnalysisTimeoutError.timedOut
                }

                do {
                    guard let analysis = try await group.next() else {
                        group.cancelAll()
                        return fallbackAnalysis(for: asset)
                    }
                    group.cancelAll()
                    return analysis
                } catch is CancellationError {
                    group.cancelAll()
                    throw CancellationError()
                } catch {
                    group.cancelAll()
                    storyCreationDebugLog("phase=analysis_fallback asset=\(asset.id) kind=\(asset.kind.rawValue) error=\(error.localizedDescription)")
                    return fallbackAnalysis(for: asset)
                }
            }
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            storyCreationDebugLog("phase=analysis_fallback asset=\(asset.id) kind=\(asset.kind.rawValue) error=\(error.localizedDescription)")
            return fallbackAnalysis(for: asset)
        }
    }

    private static func fallbackAnalysis(for asset: MediaAsset) -> MediaAnalysis {
        MediaAnalysis(
            assetID: asset.id,
            labels: asset.isVideo ? ["video", "memory"] : ["photo", "memory"],
            peopleCount: 0,
            confidence: 0.35
        )
    }

    deinit {
        creationTask?.cancel()
        thumbnailTasks.values.forEach { $0.cancel() }
    }
}
