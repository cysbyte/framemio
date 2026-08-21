import Foundation

enum MediaKind: String, Codable, Hashable, Sendable {
    case image
    case video
    case livePhoto
}

struct MediaAsset: Identifiable, Codable, Hashable, Sendable {
    let id: String
    let localIdentifier: String?
    let kind: MediaKind
    let creationDate: Date?
    let duration: TimeInterval
    let pixelWidth: Int
    let pixelHeight: Int

    var isVideo: Bool {
        kind == .video
    }

    var isLivePhoto: Bool {
        kind == .livePhoto
    }
}

struct MediaAnalysis: Codable, Hashable, Sendable {
    let assetID: String
    let labels: [String]
    let peopleCount: Int
    let confidence: Double
}

enum MediaOrientation: String, Codable, Hashable, Sendable {
    case portrait
    case landscape
    case square

    static func from(pixelWidth: Int, pixelHeight: Int) -> MediaOrientation {
        if pixelWidth == pixelHeight {
            return .square
        }
        return pixelWidth > pixelHeight ? .landscape : .portrait
    }
}

struct GeoCoordinate: Codable, Hashable, Sendable {
    let latitude: Double
    let longitude: Double
}

struct NormalizedRect: Codable, Hashable, Sendable {
    let x: Double
    let y: Double
    let width: Double
    let height: Double
}

struct FaceMetadata: Codable, Hashable, Sendable {
    let boundingBox: NormalizedRect
    let confidence: Double
}

struct MediaClassification: Codable, Hashable, Sendable {
    let identifier: String
    let confidence: Double
}

struct PerceptualHash: Codable, Hashable, Sendable {
    let hexString: String

    init(bits: UInt64) {
        self.hexString = String(format: "%016llx", bits)
    }

    init?(hexString: String) {
        guard hexString.count == 16, UInt64(hexString, radix: 16) != nil else {
            return nil
        }
        self.hexString = hexString.lowercased()
    }

    func hammingDistance(to other: PerceptualHash) -> Int {
        guard let lhs = UInt64(hexString, radix: 16),
              let rhs = UInt64(other.hexString, radix: 16) else {
            return .max
        }
        return (lhs ^ rhs).nonzeroBitCount
    }
}

struct PhotoQualityIndicators: Codable, Hashable, Sendable {
    let blurScore: Double
    let sharpnessScore: Double
    let brightness: Double
    let exposureScore: Double
    let overallScore: Double
}

struct PhotoMetadata: Codable, Hashable, Sendable {
    let assetID: String
    let creationDate: Date?
    let location: GeoCoordinate?
    let pixelWidth: Int
    let pixelHeight: Int
    let orientation: MediaOrientation
    let fileType: String?
    let isLivePhoto: Bool
    let quality: PhotoQualityIndicators?
    let faces: [FaceMetadata]
    let peopleCount: Int
    let classifications: [MediaClassification]
    let perceptualHash: PerceptualHash?
}

struct VideoQualityIndicators: Codable, Hashable, Sendable {
    let resolutionScore: Double
    let frameRateScore: Double?
    let overallScore: Double
}

struct VideoMetadata: Codable, Hashable, Sendable {
    let assetID: String?
    let creationDate: Date?
    let location: GeoCoordinate?
    let duration: TimeInterval
    let pixelWidth: Int
    let pixelHeight: Int
    let orientation: MediaOrientation
    let framesPerSecond: Double?
    let fileSizeBytes: Int64?
    let hasAudio: Bool?
    let fileType: String?
    let quality: VideoQualityIndicators?

    var naturalSize: CGSizeCodable {
        CGSizeCodable(width: Double(pixelWidth), height: Double(pixelHeight))
    }

    init(duration: TimeInterval, naturalSize: CGSizeCodable) {
        self.init(
            assetID: nil,
            creationDate: nil,
            location: nil,
            duration: duration,
            pixelWidth: Int(naturalSize.width.rounded()),
            pixelHeight: Int(naturalSize.height.rounded()),
            orientation: MediaOrientation.from(
                pixelWidth: Int(naturalSize.width.rounded()),
                pixelHeight: Int(naturalSize.height.rounded())
            ),
            framesPerSecond: nil,
            fileSizeBytes: nil,
            hasAudio: nil,
            fileType: nil,
            quality: nil
        )
    }

    init(
        assetID: String?,
        creationDate: Date?,
        location: GeoCoordinate?,
        duration: TimeInterval,
        pixelWidth: Int,
        pixelHeight: Int,
        orientation: MediaOrientation,
        framesPerSecond: Double?,
        fileSizeBytes: Int64?,
        hasAudio: Bool?,
        fileType: String?,
        quality: VideoQualityIndicators?
    ) {
        self.assetID = assetID
        self.creationDate = creationDate
        self.location = location
        self.duration = duration
        self.pixelWidth = pixelWidth
        self.pixelHeight = pixelHeight
        self.orientation = orientation
        self.framesPerSecond = framesPerSecond
        self.fileSizeBytes = fileSizeBytes
        self.hasAudio = hasAudio
        self.fileType = fileType
        self.quality = quality
    }
}

struct CGSizeCodable: Codable, Hashable, Sendable {
    let width: Double
    let height: Double
}

struct MediaAssetIntelligence: Identifiable, Codable, Hashable, Sendable {
    var id: String { assetID }

    let assetID: String
    let analyzedAt: Date
    let photoMetadata: PhotoMetadata?
    let videoMetadata: VideoMetadata?
}

struct MediaSelection: Codable, Hashable, Sendable {
    let explicitlySelectedAssetIDs: [String]
    let automaticMemoryAssetIDs: [String]
    let automaticMemoryEnabled: Bool

    init(
        explicitlySelectedAssetIDs: [String],
        automaticMemoryAssetIDs: [String] = [],
        automaticMemoryEnabled: Bool = false
    ) {
        self.explicitlySelectedAssetIDs = Self.unique(explicitlySelectedAssetIDs)
        self.automaticMemoryAssetIDs = Self.unique(automaticMemoryAssetIDs)
        self.automaticMemoryEnabled = automaticMemoryEnabled
    }

    var uploadEligibleAssetIDs: [String] {
        guard automaticMemoryEnabled else {
            return explicitlySelectedAssetIDs
        }
        return Self.unique(explicitlySelectedAssetIDs + automaticMemoryAssetIDs)
    }

    func permitsUpload(of assetID: String) -> Bool {
        uploadEligibleAssetIDs.contains(assetID)
    }

    private static func unique(_ assetIDs: [String]) -> [String] {
        var seen = Set<String>()
        return assetIDs.filter { !$0.isEmpty && seen.insert($0).inserted }
    }
}

struct MediaCluster: Identifiable, Codable, Hashable, Sendable {
    let id: String
    let assetIDs: [String]
    let representativeAssetID: String
    let startDate: Date?
    let endDate: Date?
    let location: GeoCoordinate?
    let similarityConfidence: Double
}

struct MediaIntelligenceOptions: Hashable, Sendable {
    let allowsNetworkAccess: Bool
    let usesCachedResults: Bool
    let maximumClassificationCount: Int

    init(
        allowsNetworkAccess: Bool = false,
        usesCachedResults: Bool = true,
        maximumClassificationCount: Int = 5
    ) {
        self.allowsNetworkAccess = allowsNetworkAccess
        self.usesCachedResults = usesCachedResults
        self.maximumClassificationCount = max(0, maximumClassificationCount)
    }
}
