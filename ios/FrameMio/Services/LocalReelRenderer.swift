import AVFoundation
import CoreImage
import Foundation
import UIKit

private func localReelDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][LocalReel] \(message())")
#endif
}

private func localReelElapsedMS(since start: Date) -> Int {
    Int(Date().timeIntervalSince(start) * 1000)
}

struct LocalReelRenderResult: Sendable {
    let videoURL: URL
    let duration: TimeInterval
    let templateName: String
}

protocol LocalReelRendering: Sendable {
    func renderReel(
        draft: VideoProjectDraft,
        assets: [MediaAsset],
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> LocalReelRenderResult
}

enum LocalReelPolicy {
    static func shouldRenderLocally(draft: VideoProjectDraft, assets: [MediaAsset]) -> Bool {
        guard draft.type == .reel, !assets.isEmpty else { return false }
        let prompt = draft.reel?.prompt?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let configuration = draft.templateConfiguration
        let durationMode = configuration?.durationMode ?? draft.reel?.duration.durationMode ?? .auto
        let targetDuration = configuration?.durationSeconds.map(TimeInterval.init) ?? draft.reel?.duration.seconds

        if !prompt.isEmpty { return false }
        if durationMode == .fullSource { return false }
        if draft.reel?.removeFillerAndPauses == true || configuration?.removeFillerAndPauses == true { return false }
        if configuration?.captionsEnabled == true { return false }
        if let targetDuration, targetDuration > 20 { return false }
        if assets.count > 24 { return false }
        return true
    }
}

final class AVFoundationLocalReelRenderer: LocalReelRendering, @unchecked Sendable {
    private let source: OriginalMediaSource
    private let canvasSize = CGSize(width: 540, height: 960)

    init(source: OriginalMediaSource) {
        self.source = source
    }

    func renderReel(
        draft: VideoProjectDraft,
        assets: [MediaAsset],
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> LocalReelRenderResult {
        let renderStartedAt = Date()
        let template = Self.template(for: draft)
        let duration = Self.duration(for: draft, assetCount: assets.count)
        let outputURL = try Self.outputURL()
        let frameCount = max(1, Int(duration * Double(template.frameRate)))
        localReelDebugLog(
            "render start template_id=\(draft.templateID ?? "none") template=\(template.name) layout=\(template.layoutName) " +
            "assets=\(assets.count) videos=\(assets.filter(\.isVideo).count) photos=\(assets.filter { !$0.isVideo }.count) " +
            "duration=\(String(format: "%.2f", duration)) fps=\(template.frameRate) frames=\(frameCount) " +
            "source_sample_rate=\(String(format: "%.2f", template.sourceFrameSampleRate)) " +
            "canvas=\(Int(canvasSize.width))x\(Int(canvasSize.height)) output=\(outputURL.lastPathComponent)"
        )

        do {
            let materialized = try await materialize(assets: assets, progress: progress)
            try Task.checkCancellation()
            progress(0.42)

            let silentOutputURL = try Self.outputURL(suffix: "video")
            localReelDebugLog("silent video output=\(silentOutputURL.lastPathComponent)")
            try await Task.detached(priority: .utility) {
                try await self.render(
                    materialized: materialized,
                    template: template,
                    duration: duration,
                    outputURL: silentOutputURL,
                    progress: progress
                )
            }.value

            if let musicURL = Self.musicURL(for: draft) {
                localReelDebugLog("music selected file=\(musicURL.lastPathComponent)")
                try await Task.detached(priority: .utility) {
                    try await Self.mixBackgroundMusic(
                        videoURL: silentOutputURL,
                        musicURL: musicURL,
                        outputURL: outputURL,
                        duration: duration
                    )
                }.value
                do {
                    try FileManager.default.removeItem(at: silentOutputURL)
                    localReelDebugLog("silent video removed file=\(silentOutputURL.lastPathComponent)")
                } catch {
                    localReelDebugLog("silent video cleanup failed file=\(silentOutputURL.lastPathComponent) error=\(error.localizedDescription)")
                }
            } else {
                do {
                    try FileManager.default.removeItem(at: outputURL)
                } catch {
                    localReelDebugLog("old final output cleanup skipped file=\(outputURL.lastPathComponent) error=\(error.localizedDescription)")
                }
                try FileManager.default.moveItem(at: silentOutputURL, to: outputURL)
                localReelDebugLog("silent video moved to final output file=\(outputURL.lastPathComponent)")
            }

            await Self.logFinalOutput(outputURL, expectedDuration: duration)
            progress(1)
            localReelDebugLog(
                "render complete file=\(outputURL.lastPathComponent) bytes=\(Self.fileSize(at: outputURL)) " +
                "elapsed_ms=\(localReelElapsedMS(since: renderStartedAt))"
            )
            return LocalReelRenderResult(videoURL: outputURL, duration: duration, templateName: template.name)
        } catch {
            localReelDebugLog(
                "render failed template_id=\(draft.templateID ?? "none") error=\(error.localizedDescription) " +
                "elapsed_ms=\(localReelElapsedMS(since: renderStartedAt))"
            )
            throw error
        }
    }

    private func materialize(
        assets: [MediaAsset],
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> [LocalReelSource] {
        var result: [LocalReelSource] = []
        result.reserveCapacity(assets.count)
        for (index, asset) in assets.enumerated() {
            try Task.checkCancellation()
            let startedAt = Date()
            localReelDebugLog(
                "materialize start index=\(index) asset=\(asset.id) kind=\(asset.kind.rawValue) " +
                "duration=\(String(format: "%.2f", asset.duration)) dimensions=\(asset.pixelWidth)x\(asset.pixelHeight)"
            )
            do {
                let file = try await source.materializeOriginalMedia(
                    for: asset,
                    allowsNetworkAccess: true,
                    progress: nil
                )
                result.append(LocalReelSource(asset: asset, fileURL: file.url))
                localReelDebugLog(
                    "materialize complete index=\(index) asset=\(asset.id) file=\(file.url.lastPathComponent) " +
                    "bytes=\(file.fileSizeBytes) content_type=\(file.contentType) elapsed_ms=\(localReelElapsedMS(since: startedAt))"
                )
                progress(0.12 + Double(index + 1) / Double(max(assets.count, 1)) * 0.28)
            } catch {
                localReelDebugLog(
                    "materialize failed index=\(index) asset=\(asset.id) error=\(error.localizedDescription) " +
                    "elapsed_ms=\(localReelElapsedMS(since: startedAt))"
                )
                throw error
            }
        }
        return result
    }

    private func render(
        materialized: [LocalReelSource],
        template: LocalReelTemplate,
        duration: TimeInterval,
        outputURL: URL,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws {
        let frameRate = template.frameRate
        let frameCount = max(1, Int(duration * Double(frameRate)))
        let renderStartedAt = Date()
        localReelDebugLog(
            "video render start layout=\(template.layoutName) duration=\(String(format: "%.2f", duration)) " +
            "frames=\(frameCount) fps=\(frameRate) output=\(outputURL.lastPathComponent)"
        )
        let writer = try AVAssetWriter(outputURL: outputURL, fileType: .mp4)
        let settings: [String: Any] = [
            AVVideoCodecKey: AVVideoCodecType.h264,
            AVVideoWidthKey: Int(canvasSize.width),
            AVVideoHeightKey: Int(canvasSize.height),
            AVVideoCompressionPropertiesKey: [
                AVVideoAverageBitRateKey: template.bitRate,
                AVVideoProfileLevelKey: AVVideoProfileLevelH264HighAutoLevel
            ]
        ]
        let input = AVAssetWriterInput(mediaType: .video, outputSettings: settings)
        input.expectsMediaDataInRealTime = false
        let adaptor = AVAssetWriterInputPixelBufferAdaptor(
            assetWriterInput: input,
            sourcePixelBufferAttributes: [
                kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA,
                kCVPixelBufferWidthKey as String: Int(canvasSize.width),
                kCVPixelBufferHeightKey as String: Int(canvasSize.height)
            ]
        )
        guard writer.canAdd(input) else {
            localReelDebugLog("writer cannot add video input status=\(writer.status.rawValue)")
            throw AppError.persistenceFailure
        }
        writer.add(input)
        guard writer.startWriting() else {
            localReelDebugLog(
                "writer start failed status=\(writer.status.rawValue) error=\(writer.error?.localizedDescription ?? "none")"
            )
            throw writer.error ?? AppError.persistenceFailure
        }
        writer.startSession(atSourceTime: .zero)
        localReelDebugLog(
            "writer started status=\(writer.status.rawValue) codec=h264 bitrate=\(template.bitRate) " +
            "size=\(Int(canvasSize.width))x\(Int(canvasSize.height))"
        )

        let providers = materialized.enumerated().map { index, source in
            LocalFrameProvider(source: source, index: index)
        }
        if let panelVisualOrder = template.panelVisualOrder {
            localReelDebugLog(
                "panel schedule layout=\(template.layoutName) panel_count=\(panelVisualOrder.count) " +
                "visual_order=\(panelVisualOrder.map(String.init).joined(separator: ",")) " +
                "transition_step_seconds=\(String(format: "%.3f", template.panelStepDuration(for: duration)))"
            )
        }
        let context = CIContext(options: [CIContextOption.cacheIntermediates: false])
        var frameIndex = 0
        var writerStallCount = 0
        var stallStartedAt: Date?
        var previousVisiblePanels: [Int]?
        var lastProgressBucket = -1
        while frameIndex < frameCount {
            try Task.checkCancellation()
            if input.isReadyForMoreMediaData {
                if let stallStartedAt {
                    localReelDebugLog(
                        "writer backpressure recovered frame=\(frameIndex) stalled_ms=\(localReelElapsedMS(since: stallStartedAt))"
                    )
                }
                stallStartedAt = nil
                let time = CMTime(value: CMTimeValue(frameIndex), timescale: frameRate)
                guard let pixelBuffer = adaptor.pixelBufferPool.flatMap(Self.makePixelBuffer(from:)) else {
                    localReelDebugLog(
                        "pixel buffer allocation failed frame=\(frameIndex) pool_present=\(adaptor.pixelBufferPool != nil)"
                    )
                    throw AppError.persistenceFailure
                }
                try autoreleasepool {
                    let seconds = Double(frameIndex) / Double(frameRate)
                    let visiblePanels = visiblePanelIndices(at: seconds, duration: duration, template: template)
                    if let visiblePanels, visiblePanels != previousVisiblePanels {
                        localReelDebugLog(
                            "panel visibility changed frame=\(frameIndex) time=\(String(format: "%.3f", seconds)) " +
                            "visible=\(visiblePanels.map(String.init).joined(separator: ","))"
                        )
                        previousVisiblePanels = visiblePanels
                    }
                    let image = try frameImage(
                        at: seconds,
                        duration: duration,
                        template: template,
                        providers: providers
                    )
                    context.render(image, to: pixelBuffer, bounds: CGRect(origin: .zero, size: canvasSize), colorSpace: CGColorSpaceCreateDeviceRGB())
                    guard adaptor.append(pixelBuffer, withPresentationTime: time) else {
                        localReelDebugLog(
                            "writer append failed frame=\(frameIndex) time=\(String(format: "%.3f", seconds)) " +
                            "status=\(writer.status.rawValue) error=\(writer.error?.localizedDescription ?? "none")"
                        )
                        throw writer.error ?? AppError.persistenceFailure
                    }
                }
                frameIndex += 1
                if frameIndex % Int(frameRate) == 0 || frameIndex == frameCount {
                    progress(0.42 + Double(frameIndex) / Double(frameCount) * 0.55)
                }
                let progressBucket = min(4, Int(Double(frameIndex) / Double(frameCount) * 4))
                if progressBucket != lastProgressBucket {
                    lastProgressBucket = progressBucket
                    localReelDebugLog(
                        "video render progress milestone=\(progressBucket * 25)% frames=\(frameIndex)/\(frameCount) " +
                        "elapsed_ms=\(localReelElapsedMS(since: renderStartedAt))"
                    )
                }
                if frameIndex % 8 == 0 {
                    await Task.yield()
                }
            } else {
                writerStallCount += 1
                if stallStartedAt == nil {
                    stallStartedAt = Date()
                    localReelDebugLog(
                        "writer backpressure started frame=\(frameIndex) stalls=\(writerStallCount) status=\(writer.status.rawValue)"
                    )
                } else if writerStallCount % 30 == 0 {
                    localReelDebugLog(
                        "writer backpressure ongoing frame=\(frameIndex) stalls=\(writerStallCount) " +
                        "stalled_ms=\(localReelElapsedMS(since: stallStartedAt!))"
                    )
                }
                try await Task.sleep(for: .milliseconds(16))
            }
        }

        input.markAsFinished()
        localReelDebugLog(
            "writer input finished frames=\(frameIndex)/\(frameCount) stalls=\(writerStallCount) status=\(writer.status.rawValue)"
        )
        await Self.finishWriting(writer)
        guard writer.status == .completed else {
            localReelDebugLog(
                "writer finish failed status=\(writer.status.rawValue) error=\(writer.error?.localizedDescription ?? "none")"
            )
            throw writer.error ?? AppError.persistenceFailure
        }
        localReelDebugLog(
            "video render complete file=\(outputURL.lastPathComponent) bytes=\(Self.fileSize(at: outputURL)) " +
            "elapsed_ms=\(localReelElapsedMS(since: renderStartedAt))"
        )
    }

    private func visiblePanelIndices(
        at seconds: TimeInterval,
        duration: TimeInterval,
        template: LocalReelTemplate
    ) -> [Int]? {
        guard let visualOrder = template.panelVisualOrder else { return nil }
        return visualOrder.filter {
            panelIsVisible(
                index: $0,
                panelCount: visualOrder.count,
                visualOrder: visualOrder,
                at: seconds,
                duration: duration
            )
        }.sorted()
    }

    private func frameImage(
        at seconds: TimeInterval,
        duration: TimeInterval,
        template: LocalReelTemplate,
        providers: [LocalFrameProvider]
    ) throws -> CIImage {
        let background = CIImage(color: CIColor(red: 0, green: 0, blue: 0))
            .cropped(to: CGRect(origin: .zero, size: canvasSize))
        guard !providers.isEmpty else { return background }

        switch template.layout {
        case .split:
            return try splitFrame(
                at: seconds,
                duration: duration,
                providers: providers,
                background: background,
                sampleRate: template.sourceFrameSampleRate
            )
        case .grid:
            return try gridFrame(
                at: seconds,
                duration: duration,
                providers: providers,
                background: background,
                sampleRate: template.sourceFrameSampleRate
            )
        case .montage:
            return try montageFrame(at: seconds, duration: duration, providers: providers, background: background, sampleRate: template.sourceFrameSampleRate)
        }
    }

    private func montageFrame(at seconds: TimeInterval, duration: TimeInterval, providers: [LocalFrameProvider], background: CIImage, sampleRate: Double) throws -> CIImage {
        let slotDuration = max(0.45, duration / Double(providers.count))
        let index = min(providers.count - 1, max(0, Int(seconds / slotDuration)))
        let sourceTime = localSourceTime(seconds: seconds, slotDuration: slotDuration, provider: providers[index])
        let frame = try providers[index].image(at: sourceTime, sampleRate: sampleRate)
        return composite(frame, in: CGRect(origin: .zero, size: canvasSize), over: background)
    }

    private func splitFrame(
        at seconds: TimeInterval,
        duration: TimeInterval,
        providers: [LocalFrameProvider],
        background: CIImage,
        sampleRate: Double
    ) throws -> CIImage {
        var output = background
        let frames = [
            CGRect(x: 0, y: 0, width: canvasSize.width / 2, height: canvasSize.height),
            CGRect(x: canvasSize.width / 2, y: 0, width: canvasSize.width / 2, height: canvasSize.height)
        ]
        for index in 0..<2 {
            guard panelIsVisible(
                index: index,
                panelCount: frames.count,
                visualOrder: [0, 1],
                at: seconds,
                duration: duration
            ) else {
                continue
            }
            let provider = providers[index % providers.count]
            let frame = try provider.image(at: loopedTime(seconds, duration: provider.duration), sampleRate: sampleRate)
            output = composite(frame, in: frames[index], over: output)
        }
        return output
    }

    private func gridFrame(
        at seconds: TimeInterval,
        duration: TimeInterval,
        providers: [LocalFrameProvider],
        background: CIImage,
        sampleRate: Double
    ) throws -> CIImage {
        var output = background
        let width = canvasSize.width / 2
        let height = canvasSize.height / 2
        let frames = [
            CGRect(x: 0, y: height, width: width, height: height),
            CGRect(x: width, y: height, width: width, height: height),
            CGRect(x: 0, y: 0, width: width, height: height),
            CGRect(x: width, y: 0, width: width, height: height)
        ]
        // Reveal panels in visual reading order: top-left, top-right,
        // bottom-left, bottom-right. The closing sequence is reversed.
        let visualOrder = [2, 3, 0, 1]
        for index in 0..<4 {
            guard panelIsVisible(
                index: index,
                panelCount: frames.count,
                visualOrder: visualOrder,
                at: seconds,
                duration: duration
            ) else {
                continue
            }
            let provider = providers[index % providers.count]
            let frame = try provider.image(at: loopedTime(seconds + Double(index) * 0.25, duration: provider.duration), sampleRate: sampleRate)
            output = composite(frame, in: frames[index], over: output)
        }
        return output
    }

    private func panelIsVisible(
        index: Int,
        panelCount: Int,
        visualOrder: [Int],
        at seconds: TimeInterval,
        duration: TimeInterval
    ) -> Bool {
        guard panelCount > 0,
              visualOrder.count == panelCount,
              let visualIndex = visualOrder.firstIndex(of: index) else {
            return true
        }

        // Keep the transitions short enough that the actual reel content
        // remains dominant, while guaranteeing an initial and final black
        // frame for multi-panel layouts.
        let stepDuration = min(0.32, max(0.16, duration * 0.04))
        let sequenceDuration = stepDuration * Double(panelCount)
        let openingEnd = sequenceDuration
        let closingStart = max(openingEnd, duration - sequenceDuration)

        if seconds < openingEnd {
            return seconds >= stepDuration * Double(visualIndex + 1)
        }
        if seconds >= closingStart {
            let closingElapsed = seconds - closingStart
            let reverseIndex = panelCount - 1 - visualIndex
            return closingElapsed < stepDuration * Double(reverseIndex + 1)
        }
        return true
    }

    private func composite(_ image: CIImage, in rect: CGRect, over background: CIImage) -> CIImage {
        let scaled = aspectFill(image, in: rect.size).transformed(by: CGAffineTransform(translationX: rect.minX, y: rect.minY))
        return scaled.composited(over: background)
    }

    private func aspectFill(_ image: CIImage, in size: CGSize) -> CIImage {
        let extent = image.extent
        guard extent.width > 0, extent.height > 0 else { return image }
        let scale = max(size.width / extent.width, size.height / extent.height)
        let scaledSize = CGSize(width: extent.width * scale, height: extent.height * scale)
        let dx = (size.width - scaledSize.width) / 2 - extent.minX * scale
        let dy = (size.height - scaledSize.height) / 2 - extent.minY * scale
        return image
            .transformed(by: CGAffineTransform(scaleX: scale, y: scale).translatedBy(x: dx / scale, y: dy / scale))
            .cropped(to: CGRect(origin: .zero, size: size))
    }

    private func localSourceTime(seconds: TimeInterval, slotDuration: TimeInterval, provider: LocalFrameProvider) -> TimeInterval {
        guard provider.duration > 0 else { return 0 }
        let start = max(0, (provider.duration - slotDuration) * 0.45)
        return min(provider.duration - 0.05, start + seconds.truncatingRemainder(dividingBy: slotDuration))
    }

    private func loopedTime(_ seconds: TimeInterval, duration: TimeInterval) -> TimeInterval {
        guard duration > 0.1 else { return 0 }
        return min(duration - 0.05, seconds.truncatingRemainder(dividingBy: duration))
    }

    private static func makePixelBuffer(from pool: CVPixelBufferPool) -> CVPixelBuffer? {
        var pixelBuffer: CVPixelBuffer?
        guard CVPixelBufferPoolCreatePixelBuffer(nil, pool, &pixelBuffer) == kCVReturnSuccess else {
            return nil
        }
        return pixelBuffer
    }

    private static func finishWriting(_ writer: AVAssetWriter) async {
        await withCheckedContinuation { continuation in
            writer.finishWriting {
                continuation.resume()
            }
        }
    }

    private static func outputURL(suffix: String? = nil) throws -> URL {
        let directory = (FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first ?? FileManager.default.temporaryDirectory)
            .appendingPathComponent("FrameMio", isDirectory: true)
            .appendingPathComponent("LocalReels", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let filenameSuffix = suffix.map { "-\($0)" } ?? ""
        return directory.appendingPathComponent("reel-\(UUID().uuidString)\(filenameSuffix).mp4")
    }

    private static func fileSize(at url: URL) -> Int64 {
        (try? FileManager.default.attributesOfItem(atPath: url.path)[.size] as? NSNumber)?.int64Value ?? 0
    }

    private static func logFinalOutput(_ url: URL, expectedDuration: TimeInterval) async {
        let asset = AVURLAsset(url: url)
        do {
            let videoTracks = try await asset.loadTracks(withMediaType: .video)
            let audioTracks = try await asset.loadTracks(withMediaType: .audio)
            let actualDuration = try await asset.load(.duration).seconds
            localReelDebugLog(
                "final output file=\(url.lastPathComponent) bytes=\(fileSize(at: url)) " +
                "duration=\(String(format: "%.3f", actualDuration)) expected_duration=\(String(format: "%.3f", expectedDuration)) " +
                "video_tracks=\(videoTracks.count) audio_tracks=\(audioTracks.count) " +
                "audio_policy=source_audio_excluded_generated_music_only"
            )
        } catch {
            localReelDebugLog(
                "final output inspection failed file=\(url.lastPathComponent) bytes=\(fileSize(at: url)) " +
                "error=\(error.localizedDescription)"
            )
        }
    }

    private static func duration(for draft: VideoProjectDraft, assetCount: Int) -> TimeInterval {
        if let duration = draft.templateConfiguration?.durationSeconds, duration > 0 {
            return TimeInterval(min(duration, 20))
        }
        if let duration = draft.reel?.duration.seconds, duration > 0 {
            return TimeInterval(min(Int(duration), 20))
        }
        return min(12, max(6, Double(assetCount) * 1.1))
    }

    private static func template(for draft: VideoProjectDraft) -> LocalReelTemplate {
        let templateID = draft.templateID ?? "fast-montage"
        let name = VideoTemplate.localFallbackCatalog.first { $0.id == templateID }?.name ?? "Local Reel"
        let layout = switch templateID {
        case "fast-montage", "reel-picture-in-picture":
            LocalReelTemplate.Layout.grid
        case "before-after-split":
            LocalReelTemplate.Layout.split
        default:
            LocalReelTemplate.Layout.montage
        }
        return LocalReelTemplate(name: name, layout: layout)
    }

    private static func musicURL(for draft: VideoProjectDraft) -> URL? {
        let templateID = draft.templateID ?? "fast-montage"
        let trackName = switch templateID {
        case "fast-montage", "travel-highlight-reel", "reel-picture-in-picture":
            "free-fall"
        default:
            "city-loop"
        }
        return Bundle.main.url(forResource: trackName, withExtension: "mp3", subdirectory: "Music")
    }

    private static func mixBackgroundMusic(
        videoURL: URL,
        musicURL: URL,
        outputURL: URL,
        duration: TimeInterval
    ) async throws {
        let mixStartedAt = Date()
        let composition = AVMutableComposition()
        let videoAsset = AVURLAsset(url: videoURL)
        let musicAsset = AVURLAsset(url: musicURL)
        let reelDuration = CMTime(seconds: max(0.1, duration), preferredTimescale: 600)
        let sourceAudioTracks = try await videoAsset.loadTracks(withMediaType: .audio)
        let sourceVideoDuration = try await videoAsset.load(.duration).seconds
        localReelDebugLog(
            "music mix start video=\(videoURL.lastPathComponent) music=\(musicURL.lastPathComponent) " +
            "video_duration=\(String(format: "%.3f", sourceVideoDuration)) target_duration=\(String(format: "%.3f", duration)) " +
            "source_audio_tracks=\(sourceAudioTracks.count)"
        )

        guard let videoSourceTrack = try await videoAsset.loadTracks(withMediaType: .video).first,
              let videoTrack = composition.addMutableTrack(withMediaType: .video, preferredTrackID: kCMPersistentTrackID_Invalid) else {
            localReelDebugLog("music mix failed missing video track video=\(videoURL.lastPathComponent)")
            throw AppError.persistenceFailure
        }
        try videoTrack.insertTimeRange(
            CMTimeRange(start: .zero, duration: reelDuration),
            of: videoSourceTrack,
            at: .zero
        )
        videoTrack.preferredTransform = try await videoSourceTrack.load(.preferredTransform)

        var insertedMusicSegments = 0
        if let musicSourceTrack = try await musicAsset.loadTracks(withMediaType: .audio).first,
           let audioTrack = composition.addMutableTrack(withMediaType: .audio, preferredTrackID: kCMPersistentTrackID_Invalid) {
            let musicDuration = try await musicAsset.load(.duration)
            localReelDebugLog(
                "music source ready file=\(musicURL.lastPathComponent) duration=\(String(format: "%.3f", musicDuration.seconds)) " +
                "volume=0.28 fade_out_seconds=1.20"
            )
            var insertAt = CMTime.zero
            while insertAt < reelDuration {
                let remaining = reelDuration - insertAt
                let segmentDuration = min(musicDuration, remaining)
                guard segmentDuration > .zero else { break }
                try audioTrack.insertTimeRange(
                    CMTimeRange(start: .zero, duration: segmentDuration),
                    of: musicSourceTrack,
                    at: insertAt
                )
                insertAt = insertAt + segmentDuration
                insertedMusicSegments += 1
            }
            localReelDebugLog(
                "music inserted segments=\(insertedMusicSegments) duration=\(String(format: "%.3f", insertAt.seconds))"
            )
        } else {
            localReelDebugLog("music mix has no usable audio track file=\(musicURL.lastPathComponent)")
        }

        let audioMix = AVMutableAudioMix()
        if let compositionAudioTrack = composition.tracks(withMediaType: .audio).first {
            let parameters = AVMutableAudioMixInputParameters(track: compositionAudioTrack)
            parameters.setVolume(0.28, at: .zero)
            parameters.setVolumeRamp(
                fromStartVolume: 0.28,
                toEndVolume: 0,
                timeRange: CMTimeRange(
                    start: max(.zero, reelDuration - CMTime(seconds: 1.2, preferredTimescale: 600)),
                    duration: CMTime(seconds: 1.2, preferredTimescale: 600)
                )
            )
            audioMix.inputParameters = [parameters]
        }

        try? FileManager.default.removeItem(at: outputURL)
        guard let export = AVAssetExportSession(asset: composition, presetName: AVAssetExportPresetHighestQuality) else {
            throw AppError.persistenceFailure
        }
        export.outputURL = outputURL
        export.outputFileType = .mp4
        export.timeRange = CMTimeRange(start: .zero, duration: reelDuration)
        export.audioMix = audioMix
        await export.export()
        guard export.status == .completed else {
            localReelDebugLog(
                "music export failed status=\(export.status.rawValue) error=\(export.error?.localizedDescription ?? "none") " +
                "elapsed_ms=\(localReelElapsedMS(since: mixStartedAt))"
            )
            throw export.error ?? AppError.persistenceFailure
        }
        localReelDebugLog(
            "music mix complete output=\(outputURL.lastPathComponent) bytes=\(fileSize(at: outputURL)) " +
            "audio_tracks=\(composition.tracks(withMediaType: .audio).count) elapsed_ms=\(localReelElapsedMS(since: mixStartedAt))"
        )
    }
}

final class MockLocalReelRenderer: LocalReelRendering, @unchecked Sendable {
    func renderReel(
        draft: VideoProjectDraft,
        assets: [MediaAsset],
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> LocalReelRenderResult {
        progress(1)
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("mock-local-reel.mp4")
        return LocalReelRenderResult(videoURL: url, duration: 8, templateName: "Local Reel")
    }
}

private struct LocalReelSource {
    let asset: MediaAsset
    let fileURL: URL
}

private struct LocalReelTemplate {
    enum Layout {
        case montage
        case split
        case grid
    }

    let name: String
    let layout: Layout

    var layoutName: String {
        switch layout {
        case .montage:
            "montage"
        case .split:
            "split"
        case .grid:
            "four_panel_reveal"
        }
    }

    var panelVisualOrder: [Int]? {
        switch layout {
        case .montage:
            nil
        case .split:
            [0, 1]
        case .grid:
            [2, 3, 0, 1]
        }
    }

    func panelStepDuration(for duration: TimeInterval) -> TimeInterval {
        min(0.32, max(0.16, duration * 0.04))
    }

    var frameRate: Int32 {
        switch layout {
        case .montage:
            24
        case .split, .grid:
            24
        }
    }

    var bitRate: Int {
        switch layout {
        case .montage:
            2_400_000
        case .split, .grid:
            2_000_000
        }
    }

    var sourceFrameSampleRate: Double {
        Double(frameRate)
    }
}

private final class LocalFrameProvider {
    private let sourceID: String
    private let sourceIndex: Int
    let duration: TimeInterval
    private let image: CIImage?
    private let generator: AVAssetImageGenerator?
    private var cachedVideoFrame: (bucket: Int, image: CIImage)?
    private var lastLoggedBucket: Int?

    init(source: LocalReelSource, index: Int) {
        sourceID = source.asset.id
        sourceIndex = index
        if source.asset.isVideo {
            let asset = AVURLAsset(url: source.fileURL)
            duration = max(0, asset.duration.seconds.isFinite ? asset.duration.seconds : source.asset.duration)
            image = nil
            let generator = AVAssetImageGenerator(asset: asset)
            generator.appliesPreferredTrackTransform = true
            generator.maximumSize = CGSize(width: 720, height: 1280)
            // The renderer requests every output frame. Allowing a large
            // timestamp tolerance makes AVAssetImageGenerator reuse nearby
            // keyframes, which is visible as dropped or repeated motion.
            generator.requestedTimeToleranceBefore = .zero
            generator.requestedTimeToleranceAfter = .zero
            self.generator = generator
            let naturalSize = asset.tracks(withMediaType: .video).first?.naturalSize ?? .zero
            localReelDebugLog(
                "provider init index=\(sourceIndex) asset=\(sourceID) kind=video file=\(source.fileURL.lastPathComponent) " +
                "duration=\(String(format: "%.3f", duration)) declared_duration=\(String(format: "%.3f", source.asset.duration)) " +
                "declared_dimensions=\(source.asset.pixelWidth)x\(source.asset.pixelHeight) " +
                "track_dimensions=\(Int(naturalSize.width))x\(Int(naturalSize.height)) " +
                "generator_max=720x1280 tolerance_before=0 tolerance_after=0"
            )
        } else {
            duration = 0
            generator = nil
            image = UIImage(contentsOfFile: source.fileURL.path)
                .flatMap { CIImage(image: $0) }
            localReelDebugLog(
                "provider init index=\(sourceIndex) asset=\(sourceID) kind=\(source.asset.kind.rawValue) " +
                "file=\(source.fileURL.lastPathComponent) image_loaded=\(image != nil) " +
                "declared_dimensions=\(source.asset.pixelWidth)x\(source.asset.pixelHeight)"
            )
        }
    }

    func image(at seconds: TimeInterval, sampleRate: Double) throws -> CIImage {
        if let image {
            return image
        }
        guard let generator else { throw AppError.unsupportedMedia }
        let bucket = max(0, Int((seconds * sampleRate).rounded(.down)))
        if let cachedVideoFrame, cachedVideoFrame.bucket == bucket {
            return cachedVideoFrame.image
        }
        let sampledSeconds = Double(bucket) / sampleRate
        let time = CMTime(seconds: max(0, sampledSeconds), preferredTimescale: 600)
        var actualTime = CMTime.zero
        let cgImage: CGImage
        do {
            cgImage = try generator.copyCGImage(at: time, actualTime: &actualTime)
        } catch {
            localReelDebugLog(
                "frame sample failed index=\(sourceIndex) asset=\(sourceID) requested_seconds=\(String(format: "%.3f", sampledSeconds)) " +
                "error=\(error.localizedDescription)"
            )
            throw error
        }
        let image = CIImage(cgImage: cgImage)
        cachedVideoFrame = (bucket, image)
        let logBucketInterval = max(1, Int(sampleRate.rounded()))
        if bucket % logBucketInterval == 0, lastLoggedBucket != bucket {
            lastLoggedBucket = bucket
            let actualSeconds = actualTime.seconds.isFinite ? actualTime.seconds : -1
            let driftMS = actualSeconds >= 0 ? (actualSeconds - sampledSeconds) * 1000 : -1
            localReelDebugLog(
                "frame sample index=\(sourceIndex) asset=\(sourceID) requested_seconds=\(String(format: "%.3f", sampledSeconds)) " +
                "actual_seconds=\(String(format: "%.3f", actualSeconds)) drift_ms=\(String(format: "%.1f", driftMS)) " +
                "bucket=\(bucket) cache=miss"
            )
        }
        return image
    }
}
