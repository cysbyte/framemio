import Foundation
import Vision
import UIKit

final class VisionMediaAnalysisService: MediaAnalysisService, @unchecked Sendable {
    private let photoLibrary: PhotoLibraryService

    init(photoLibrary: PhotoLibraryService) {
        self.photoLibrary = photoLibrary
    }

    func analyze(asset: MediaAsset) async throws -> MediaAnalysis {
        try Task.checkCancellation()
        let data = try await photoLibrary.requestThumbnail(for: asset, targetSize: CGSize(width: 640, height: 640))
        guard let image = UIImage(data: data), let cgImage = image.cgImage else {
            throw AppError.unsupportedMedia
        }

        return try await Task.detached(priority: .userInitiated) {
            try Task.checkCancellation()
            let handler = VNImageRequestHandler(cgImage: cgImage, options: [:])
            let classificationRequest = VNClassifyImageRequest()
            let peopleRequest = VNDetectHumanRectanglesRequest()
            try handler.perform([classificationRequest, peopleRequest])
            try Task.checkCancellation()

            let labels = (classificationRequest.results ?? [])
                .prefix(3)
                .filter { $0.confidence >= 0.15 }
                .map(\.identifier)
            let peopleCount = peopleRequest.results?.count ?? 0
            let confidence = classificationRequest.results?.first?.confidence ?? 0

            return MediaAnalysis(
                assetID: asset.id,
                labels: labels,
                peopleCount: peopleCount,
                confidence: Double(confidence)
            )
        }.value
    }
}

final class MockMediaAnalysisService: MediaAnalysisService, @unchecked Sendable {
    func analyze(asset: MediaAsset) async throws -> MediaAnalysis {
        try await Task.sleep(for: .milliseconds(35))
        try Task.checkCancellation()
        return MediaAnalysis(
            assetID: asset.id,
            labels: asset.isVideo ? ["video", "memory"] : ["photo", "memory"],
            peopleCount: 1,
            confidence: 0.92
        )
    }
}
