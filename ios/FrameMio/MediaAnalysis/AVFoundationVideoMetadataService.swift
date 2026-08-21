import AVFoundation
import Foundation

final class AVFoundationVideoMetadataService: VideoMetadataService, @unchecked Sendable {
    func inspectVideo(at url: URL) async throws -> VideoMetadata {
        try Task.checkCancellation()
        let asset = AVURLAsset(url: url)
        let duration = try await asset.load(.duration)
        let videoTracks = try await asset.loadTracks(withMediaType: .video)
        let audioTracks = try await asset.loadTracks(withMediaType: .audio)
        try Task.checkCancellation()

        guard let videoTrack = videoTracks.first else {
            throw AppError.unsupportedMedia
        }
        let naturalSize = try await videoTrack.load(.naturalSize)
        let preferredTransform = try await videoTrack.load(.preferredTransform)
        let nominalFrameRate = try await videoTrack.load(.nominalFrameRate)
        let displaySize = naturalSize.applying(preferredTransform)
        let width = Int(abs(displaySize.width).rounded())
        let height = Int(abs(displaySize.height).rounded())
        let framesPerSecond = nominalFrameRate > 0 ? Double(nominalFrameRate) : nil
        let fileSizeBytes = (try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize)
            .flatMap { $0.map(Int64.init) }
        let resolutionScore = min(1, max(0, Double(min(width, height)) / 1_080))
        let frameRateScore = framesPerSecond.map { min(1, max(0, $0 / 30)) }
        let overallScore = frameRateScore.map { resolutionScore * 0.8 + $0 * 0.2 }
            ?? resolutionScore

        return VideoMetadata(
            assetID: nil,
            creationDate: nil,
            location: nil,
            duration: duration.seconds,
            pixelWidth: width,
            pixelHeight: height,
            orientation: .from(pixelWidth: width, pixelHeight: height),
            framesPerSecond: framesPerSecond,
            fileSizeBytes: fileSizeBytes,
            hasAudio: !audioTracks.isEmpty,
            fileType: nil,
            quality: VideoQualityIndicators(
                resolutionScore: resolutionScore,
                frameRateScore: frameRateScore,
                overallScore: overallScore
            )
        )
    }
}

final class MockVideoMetadataService: VideoMetadataService, @unchecked Sendable {
    func inspectVideo(at url: URL) async throws -> VideoMetadata {
        try Task.checkCancellation()
        return VideoMetadata(
            assetID: nil,
            creationDate: nil,
            location: nil,
            duration: 12,
            pixelWidth: 1_920,
            pixelHeight: 1_080,
            orientation: .landscape,
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
    }
}
