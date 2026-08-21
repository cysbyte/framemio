import AVFoundation
import CoreImage
import Foundation
import Photos
import UIKit
import Vision

protocol MediaIntelligenceExecutionPolicy: Sendable {
    func waitUntilProcessingIsAllowed() async throws
}

struct ForegroundMediaIntelligenceExecutionPolicy: MediaIntelligenceExecutionPolicy {
    func waitUntilProcessingIsAllowed() async throws {
        try Task.checkCancellation()
        let isActive = await MainActor.run {
            UIApplication.shared.applicationState == .active
        }
        guard !isActive else {
            return
        }

        while true {
            try await Task.sleep(for: .milliseconds(500))
            try Task.checkCancellation()
            let becameActive = await MainActor.run {
                UIApplication.shared.applicationState == .active
            }
            if becameActive {
                return
            }
        }
    }
}

struct AlwaysAvailableMediaIntelligenceExecutionPolicy: MediaIntelligenceExecutionPolicy {
    func waitUntilProcessingIsAllowed() async throws {
        try Task.checkCancellation()
    }
}

actor PhotoKitMediaIntelligenceService: MediaIntelligenceService {
    private enum Constants {
        static let analysisImageSize = CGSize(width: 640, height: 640)
        static let minimumClassificationConfidence: Float = 0.15
    }

    private let imageManager: PHImageManager
    private let store: MediaIntelligenceStore
    private let executionPolicy: MediaIntelligenceExecutionPolicy
    private let imageProcessor: LightweightPhotoProcessor

    init(
        imageManager: PHImageManager = PHCachingImageManager(),
        store: MediaIntelligenceStore,
        executionPolicy: MediaIntelligenceExecutionPolicy = ForegroundMediaIntelligenceExecutionPolicy()
    ) {
        self.imageManager = imageManager
        self.store = store
        self.executionPolicy = executionPolicy
        self.imageProcessor = LightweightPhotoProcessor()
    }

    func analyze(
        assets: [MediaAsset],
        options: MediaIntelligenceOptions = MediaIntelligenceOptions()
    ) async throws -> [MediaAssetIntelligence] {
        try Task.checkCancellation()
        let authorization = PHPhotoLibrary.authorizationStatus(for: .readWrite)
        guard authorization == .authorized || authorization == .limited else {
            throw AppError.notAuthorized
        }

        var results: [MediaAssetIntelligence] = []
        results.reserveCapacity(assets.count)
        var resultsByAssetID: [String: MediaAssetIntelligence] = [:]

        for asset in assets {
            try Task.checkCancellation()
            try await executionPolicy.waitUntilProcessingIsAllowed()

            if let existing = resultsByAssetID[asset.id] {
                results.append(existing)
                continue
            }
            if options.usesCachedResults,
               let cached = try await store.intelligence(for: asset.id) {
                resultsByAssetID[asset.id] = cached
                results.append(cached)
                continue
            }

            let intelligence = try await analyze(asset: asset, options: options)
            try Task.checkCancellation()
            try await store.save(intelligence)
            resultsByAssetID[asset.id] = intelligence
            results.append(intelligence)
        }

        return results
    }

    private func analyze(
        asset: MediaAsset,
        options: MediaIntelligenceOptions
    ) async throws -> MediaAssetIntelligence {
        guard let localIdentifier = asset.localIdentifier,
              let photoAsset = PHAsset.fetchAssets(
                withLocalIdentifiers: [localIdentifier],
                options: nil
              ).firstObject else {
            throw AppError.unsupportedMedia
        }

        switch photoAsset.mediaType {
        case .image:
            let metadata = try await analyzePhoto(
                asset: photoAsset,
                model: asset,
                options: options
            )
            return MediaAssetIntelligence(
                assetID: asset.id,
                analyzedAt: Date(),
                photoMetadata: metadata,
                videoMetadata: nil
            )
        case .video:
            let metadata = try await analyzeVideo(
                asset: photoAsset,
                model: asset,
                options: options
            )
            return MediaAssetIntelligence(
                assetID: asset.id,
                analyzedAt: Date(),
                photoMetadata: nil,
                videoMetadata: metadata
            )
        default:
            throw AppError.unsupportedMedia
        }
    }

    private func analyzePhoto(
        asset: PHAsset,
        model: MediaAsset,
        options: MediaIntelligenceOptions
    ) async throws -> PhotoMetadata {
        let resource = Self.preferredPhotoResource(for: asset)
        let visualAnalysis: LightweightPhotoAnalysis?

        do {
            let image = try await requestAnalysisImage(
                for: asset,
                allowsNetworkAccess: options.allowsNetworkAccess
            )
            try Task.checkCancellation()
            visualAnalysis = try imageProcessor.analyze(
                image: image,
                maximumClassificationCount: options.maximumClassificationCount,
                minimumClassificationConfidence: Constants.minimumClassificationConfidence
            )
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            visualAnalysis = nil
        }

        return PhotoMetadata(
            assetID: model.id,
            creationDate: asset.creationDate,
            location: Self.coordinate(from: asset),
            pixelWidth: asset.pixelWidth,
            pixelHeight: asset.pixelHeight,
            orientation: .from(pixelWidth: asset.pixelWidth, pixelHeight: asset.pixelHeight),
            fileType: resource?.uniformTypeIdentifier,
            isLivePhoto: asset.mediaSubtypes.contains(.photoLive),
            quality: visualAnalysis?.quality,
            faces: visualAnalysis?.faces ?? [],
            peopleCount: visualAnalysis?.peopleCount ?? 0,
            classifications: visualAnalysis?.classifications ?? [],
            perceptualHash: visualAnalysis?.perceptualHash
        )
    }

    private func analyzeVideo(
        asset: PHAsset,
        model: MediaAsset,
        options: MediaIntelligenceOptions
    ) async throws -> VideoMetadata {
        let resource = Self.preferredVideoResource(for: asset)
        let avAsset = try await requestAVAsset(
            for: asset,
            allowsNetworkAccess: options.allowsNetworkAccess
        )
        try Task.checkCancellation()

        var width = asset.pixelWidth
        var height = asset.pixelHeight
        var framesPerSecond: Double?
        var hasAudio: Bool?
        var fileSizeBytes: Int64?

        if let avAsset {
            do {
                let videoTracks = try await avAsset.loadTracks(withMediaType: .video)
                let audioTracks = try await avAsset.loadTracks(withMediaType: .audio)
                try Task.checkCancellation()
                hasAudio = !audioTracks.isEmpty

                if let videoTrack = videoTracks.first {
                    let naturalSize = try await videoTrack.load(.naturalSize)
                    let preferredTransform = try await videoTrack.load(.preferredTransform)
                    let nominalFrameRate = try await videoTrack.load(.nominalFrameRate)
                    let transformedSize = naturalSize.applying(preferredTransform)
                    width = Int(abs(transformedSize.width).rounded())
                    height = Int(abs(transformedSize.height).rounded())
                    framesPerSecond = nominalFrameRate > 0 ? Double(nominalFrameRate) : nil
                }

                if let urlAsset = avAsset as? AVURLAsset,
                   urlAsset.url.isFileURL,
                   let size = try? urlAsset.url.resourceValues(forKeys: [.fileSizeKey]).fileSize {
                    fileSizeBytes = Int64(size)
                }
            } catch is CancellationError {
                throw CancellationError()
            } catch {
                try Task.checkCancellation()
            }
        }

        let quality = Self.videoQuality(
            pixelWidth: width,
            pixelHeight: height,
            framesPerSecond: framesPerSecond
        )
        return VideoMetadata(
            assetID: model.id,
            creationDate: asset.creationDate,
            location: Self.coordinate(from: asset),
            duration: asset.duration,
            pixelWidth: width,
            pixelHeight: height,
            orientation: .from(pixelWidth: width, pixelHeight: height),
            framesPerSecond: framesPerSecond,
            fileSizeBytes: fileSizeBytes,
            hasAudio: hasAudio,
            fileType: resource?.uniformTypeIdentifier,
            quality: quality
        )
    }

    private func requestAnalysisImage(
        for asset: PHAsset,
        allowsNetworkAccess: Bool
    ) async throws -> CGImage {
        let requestOptions = PHImageRequestOptions()
        requestOptions.deliveryMode = .highQualityFormat
        requestOptions.resizeMode = .exact
        requestOptions.isNetworkAccessAllowed = allowsNetworkAccess
        requestOptions.version = .current

        let requestState = PhotoKitRequestState<CGImage>()
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                requestState.install(continuation)
                let requestID = imageManager.requestImage(
                    for: asset,
                    targetSize: Constants.analysisImageSize,
                    contentMode: .aspectFit,
                    options: requestOptions
                ) { image, info in
                    if let degraded = info?[PHImageResultIsDegradedKey] as? Bool, degraded {
                        return
                    }
                    if let cancelled = info?[PHImageCancelledKey] as? Bool, cancelled {
                        requestState.resolve(.failure(CancellationError()))
                    } else if let error = info?[PHImageErrorKey] as? Error {
                        requestState.resolve(.failure(error))
                    } else if let cgImage = image?.cgImage {
                        requestState.resolve(.success(cgImage))
                    } else {
                        requestState.resolve(.failure(AppError.unsupportedMedia))
                    }
                }
                requestState.setRequestID(requestID, imageManager: imageManager)
            }
        } onCancel: {
            requestState.cancel(imageManager: imageManager)
        }
    }

    private func requestAVAsset(
        for asset: PHAsset,
        allowsNetworkAccess: Bool
    ) async throws -> AVAsset? {
        let requestOptions = PHVideoRequestOptions()
        requestOptions.deliveryMode = .automatic
        requestOptions.isNetworkAccessAllowed = allowsNetworkAccess
        requestOptions.version = .current

        let requestState = PhotoKitRequestState<AVAsset?>()
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                requestState.install(continuation)
                let requestID = imageManager.requestAVAsset(
                    forVideo: asset,
                    options: requestOptions
                ) { avAsset, _, info in
                    if let cancelled = info?[PHImageCancelledKey] as? Bool, cancelled {
                        requestState.resolve(.failure(CancellationError()))
                    } else if let error = info?[PHImageErrorKey] as? Error {
                        if allowsNetworkAccess {
                            requestState.resolve(.failure(error))
                        } else {
                            requestState.resolve(.success(nil))
                        }
                    } else {
                        requestState.resolve(.success(avAsset))
                    }
                }
                requestState.setRequestID(requestID, imageManager: imageManager)
            }
        } onCancel: {
            requestState.cancel(imageManager: imageManager)
        }
    }

    private static func preferredPhotoResource(for asset: PHAsset) -> PHAssetResource? {
        let resources = PHAssetResource.assetResources(for: asset)
        return resources.first(where: { $0.type == .fullSizePhoto })
            ?? resources.first(where: { $0.type == .photo })
    }

    private static func preferredVideoResource(for asset: PHAsset) -> PHAssetResource? {
        let resources = PHAssetResource.assetResources(for: asset)
        return resources.first(where: { $0.type == .fullSizeVideo })
            ?? resources.first(where: { $0.type == .video })
    }

    private static func coordinate(from asset: PHAsset) -> GeoCoordinate? {
        guard let coordinate = asset.location?.coordinate else {
            return nil
        }
        return GeoCoordinate(latitude: coordinate.latitude, longitude: coordinate.longitude)
    }

    private static func videoQuality(
        pixelWidth: Int,
        pixelHeight: Int,
        framesPerSecond: Double?
    ) -> VideoQualityIndicators {
        let shortEdge = Double(min(pixelWidth, pixelHeight))
        let resolutionScore = min(1, max(0, shortEdge / 1_080))
        let frameRateScore = framesPerSecond.map { min(1, max(0, $0 / 30)) }
        let overallScore = frameRateScore.map { resolutionScore * 0.8 + $0 * 0.2 }
            ?? resolutionScore
        return VideoQualityIndicators(
            resolutionScore: resolutionScore,
            frameRateScore: frameRateScore,
            overallScore: overallScore
        )
    }
}

actor MockMediaIntelligenceService: MediaIntelligenceService {
    private let perAssetDelay: Duration
    private let store: MediaIntelligenceStore

    init(
        perAssetDelay: Duration = .milliseconds(5),
        store: MediaIntelligenceStore = MockMediaIntelligenceStore()
    ) {
        self.perAssetDelay = perAssetDelay
        self.store = store
    }

    func analyze(
        assets: [MediaAsset],
        options: MediaIntelligenceOptions = MediaIntelligenceOptions()
    ) async throws -> [MediaAssetIntelligence] {
        var results: [MediaAssetIntelligence] = []
        results.reserveCapacity(assets.count)

        for asset in assets {
            try Task.checkCancellation()
            if options.usesCachedResults,
               let cached = try await store.intelligence(for: asset.id) {
                results.append(cached)
                continue
            }
            try await Task.sleep(for: perAssetDelay)
            let result = Self.makeIntelligence(for: asset)
            try await store.save(result)
            results.append(result)
        }
        return results
    }

    private static func makeIntelligence(for asset: MediaAsset) -> MediaAssetIntelligence {
        if asset.isVideo {
            let metadata = VideoMetadata(
                assetID: asset.id,
                creationDate: asset.creationDate,
                location: nil,
                duration: asset.duration,
                pixelWidth: asset.pixelWidth,
                pixelHeight: asset.pixelHeight,
                orientation: .from(pixelWidth: asset.pixelWidth, pixelHeight: asset.pixelHeight),
                framesPerSecond: 30,
                fileSizeBytes: nil,
                hasAudio: true,
                fileType: "public.mpeg-4",
                quality: VideoQualityIndicators(
                    resolutionScore: 1,
                    frameRateScore: 1,
                    overallScore: 1
                )
            )
            return MediaAssetIntelligence(
                assetID: asset.id,
                analyzedAt: Date(),
                photoMetadata: nil,
                videoMetadata: metadata
            )
        }

        let metadata = PhotoMetadata(
            assetID: asset.id,
            creationDate: asset.creationDate,
            location: nil,
            pixelWidth: asset.pixelWidth,
            pixelHeight: asset.pixelHeight,
            orientation: .from(pixelWidth: asset.pixelWidth, pixelHeight: asset.pixelHeight),
            fileType: "public.jpeg",
            isLivePhoto: asset.isLivePhoto,
            quality: nil,
            faces: [],
            peopleCount: 0,
            classifications: [],
            perceptualHash: nil
        )
        return MediaAssetIntelligence(
            assetID: asset.id,
            analyzedAt: Date(),
            photoMetadata: metadata,
            videoMetadata: nil
        )
    }
}

private struct LightweightPhotoAnalysis {
    let quality: PhotoQualityIndicators
    let faces: [FaceMetadata]
    let peopleCount: Int
    let classifications: [MediaClassification]
    let perceptualHash: PerceptualHash
}

private final class LightweightPhotoProcessor: @unchecked Sendable {
    private let context = CIContext(options: [.cacheIntermediates: false])

    func analyze(
        image: CGImage,
        maximumClassificationCount: Int,
        minimumClassificationConfidence: Float
    ) throws -> LightweightPhotoAnalysis {
        try Task.checkCancellation()
        let visionResults = try analyzeWithVision(
            image: image,
            maximumClassificationCount: maximumClassificationCount,
            minimumClassificationConfidence: minimumClassificationConfidence
        )
        try Task.checkCancellation()
        let ciImage = CIImage(cgImage: image)
        let brightness = averageLuminance(of: ciImage)
        let sharpness = min(1, max(0, averageEdgeLuminance(of: ciImage) * 4))
        let exposure = min(1, max(0, 1 - abs(brightness - 0.5) / 0.5))
        let quality = PhotoQualityIndicators(
            blurScore: 1 - sharpness,
            sharpnessScore: sharpness,
            brightness: brightness,
            exposureScore: exposure,
            overallScore: sharpness * 0.6 + exposure * 0.4
        )

        return LightweightPhotoAnalysis(
            quality: quality,
            faces: visionResults.faces,
            peopleCount: visionResults.peopleCount,
            classifications: visionResults.classifications,
            perceptualHash: perceptualHash(of: ciImage)
        )
    }

    private func analyzeWithVision(
        image: CGImage,
        maximumClassificationCount: Int,
        minimumClassificationConfidence: Float
    ) throws -> (faces: [FaceMetadata], peopleCount: Int, classifications: [MediaClassification]) {
        let classificationRequest = VNClassifyImageRequest()
        let faceRequest = VNDetectFaceRectanglesRequest()
        let peopleRequest = VNDetectHumanRectanglesRequest()
        let handler = VNImageRequestHandler(cgImage: image, orientation: .up, options: [:])
        try handler.perform([classificationRequest, faceRequest, peopleRequest])

        let classifications = (classificationRequest.results ?? [])
            .filter { $0.confidence >= minimumClassificationConfidence }
            .prefix(maximumClassificationCount)
            .map {
                MediaClassification(
                    identifier: $0.identifier,
                    confidence: Double($0.confidence)
                )
            }
        let faces = (faceRequest.results ?? []).map {
            FaceMetadata(
                boundingBox: NormalizedRect(
                    x: $0.boundingBox.origin.x,
                    y: $0.boundingBox.origin.y,
                    width: $0.boundingBox.width,
                    height: $0.boundingBox.height
                ),
                confidence: Double($0.confidence)
            )
        }
        let peopleCount = max(faces.count, peopleRequest.results?.count ?? 0)
        return (faces, peopleCount, classifications)
    }

    private func averageLuminance(of image: CIImage) -> Double {
        guard let averageFilter = CIFilter(name: "CIAreaAverage") else {
            return 0.5
        }
        averageFilter.setValue(image, forKey: kCIInputImageKey)
        averageFilter.setValue(CIVector(cgRect: image.extent), forKey: kCIInputExtentKey)
        guard let output = averageFilter.outputImage else {
            return 0.5
        }

        var pixel = [UInt8](repeating: 0, count: 4)
        context.render(
            output,
            toBitmap: &pixel,
            rowBytes: 4,
            bounds: CGRect(x: 0, y: 0, width: 1, height: 1),
            format: .RGBA8,
            colorSpace: CGColorSpaceCreateDeviceRGB()
        )
        return (0.2126 * Double(pixel[0]) + 0.7152 * Double(pixel[1]) + 0.0722 * Double(pixel[2])) / 255
    }

    private func averageEdgeLuminance(of image: CIImage) -> Double {
        guard let edgesFilter = CIFilter(name: "CIEdges") else {
            return 0
        }
        edgesFilter.setValue(image, forKey: kCIInputImageKey)
        edgesFilter.setValue(1, forKey: kCIInputIntensityKey)
        guard let edges = edgesFilter.outputImage else {
            return 0
        }
        return averageLuminance(of: edges)
    }

    private func perceptualHash(of image: CIImage) -> PerceptualHash {
        let grayscale = image.applyingFilter(
            "CIColorControls",
            parameters: [kCIInputSaturationKey: 0]
        )
        let extent = grayscale.extent
        let transform = CGAffineTransform(
            scaleX: 8 / max(extent.width, 1),
            y: 8 / max(extent.height, 1)
        ).translatedBy(x: -extent.origin.x, y: -extent.origin.y)
        let resized = grayscale.transformed(by: transform)
        var pixels = [UInt8](repeating: 0, count: 64)
        context.render(
            resized,
            toBitmap: &pixels,
            rowBytes: 8,
            bounds: CGRect(x: 0, y: 0, width: 8, height: 8),
            format: .L8,
            colorSpace: CGColorSpaceCreateDeviceGray()
        )
        let average = pixels.reduce(0) { $0 + Int($1) } / pixels.count
        var bits: UInt64 = 0
        for (index, pixel) in pixels.enumerated() where Int(pixel) >= average {
            bits |= UInt64(1) << UInt64(index)
        }
        return PerceptualHash(bits: bits)
    }
}

private final class PhotoKitRequestState<Value>: @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<Value, Error>?
    private var requestID: PHImageRequestID?
    private var result: Result<Value, Error>?
    private var isCancelled = false
    private var isResolved = false

    func install(_ continuation: CheckedContinuation<Value, Error>) {
        lock.lock()
        if isResolved {
            let result = self.result ?? .failure(CancellationError())
            lock.unlock()
            continuation.resume(with: result)
            return
        }
        self.continuation = continuation
        let shouldCancel = isCancelled
        lock.unlock()

        if shouldCancel {
            resolve(.failure(CancellationError()))
        }
    }

    func setRequestID(_ requestID: PHImageRequestID, imageManager: PHImageManager) {
        lock.lock()
        self.requestID = requestID
        let shouldCancel = isCancelled
        lock.unlock()
        if shouldCancel {
            imageManager.cancelImageRequest(requestID)
        }
    }

    func resolve(_ result: Result<Value, Error>) {
        lock.lock()
        guard !isResolved else {
            lock.unlock()
            return
        }
        isResolved = true
        self.result = result
        let continuation = self.continuation
        self.continuation = nil
        lock.unlock()
        continuation?.resume(with: result)
    }

    func cancel(imageManager: PHImageManager) {
        lock.lock()
        isCancelled = true
        let requestID = self.requestID
        lock.unlock()
        if let requestID {
            imageManager.cancelImageRequest(requestID)
        }
        resolve(.failure(CancellationError()))
    }
}
