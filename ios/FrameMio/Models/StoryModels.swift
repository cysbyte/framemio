import Foundation

enum StoryStatus: String, Codable, Hashable, Sendable {
    case queued
    case analyzing
    case rendering
    case ready
    case failed
}

enum StoryMomentType: String, Codable, Hashable, Sendable {
    case photo
    case video
    case livePhoto = "live_photo"
}

struct StoryMoment: Identifiable, Codable, Hashable, Sendable {
    let id: String
    let mediaID: String
    let sourceAssetID: String?
    let type: StoryMomentType?
    let thumbnailURL: URL?
    let title: String?
    let description: String?
    let creationDate: Date?
    let isSelected: Bool?

    enum CodingKeys: String, CodingKey {
        case id
        case momentID = "momentId"
        case mediaID = "mediaId"
        case sourceAssetID = "sourceAssetId"
        case type
        case thumbnailURL = "thumbnailUrl"
        case title
        case description
        case creationDate
        case isSelected = "selected"
    }

    init(
        id: String,
        mediaID: String,
        sourceAssetID: String? = nil,
        type: StoryMomentType? = nil,
        thumbnailURL: URL? = nil,
        title: String? = nil,
        description: String? = nil,
        creationDate: Date? = nil,
        isSelected: Bool? = nil
    ) {
        self.id = id
        self.mediaID = mediaID
        self.sourceAssetID = sourceAssetID
        self.type = type
        self.thumbnailURL = thumbnailURL
        self.title = title
        self.description = description
        self.creationDate = creationDate
        self.isSelected = isSelected
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let decodedMediaID = try container.decodeIfPresent(String.self, forKey: .mediaID) ?? ""
        let decodedID = try container.decodeIfPresent(String.self, forKey: .id)
            ?? container.decodeIfPresent(String.self, forKey: .momentID)
            ?? decodedMediaID
        id = decodedID
        mediaID = decodedMediaID.isEmpty ? decodedID : decodedMediaID
        sourceAssetID = try container.decodeIfPresent(String.self, forKey: .sourceAssetID)
        type = try container.decodeIfPresent(StoryMomentType.self, forKey: .type)
        thumbnailURL = try container.decodeIfPresent(URL.self, forKey: .thumbnailURL)
        title = try container.decodeIfPresent(String.self, forKey: .title)
        description = try container.decodeIfPresent(String.self, forKey: .description)
        creationDate = try container.decodeIfPresent(Date.self, forKey: .creationDate)
        isSelected = try container.decodeIfPresent(Bool.self, forKey: .isSelected)
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(mediaID, forKey: .mediaID)
        try container.encodeIfPresent(sourceAssetID, forKey: .sourceAssetID)
        try container.encodeIfPresent(type, forKey: .type)
        try container.encodeIfPresent(thumbnailURL, forKey: .thumbnailURL)
        try container.encodeIfPresent(title, forKey: .title)
        try container.encodeIfPresent(description, forKey: .description)
        try container.encodeIfPresent(creationDate, forKey: .creationDate)
        try container.encodeIfPresent(isSelected, forKey: .isSelected)
    }
}

struct Story: Identifiable, Codable, Hashable, Sendable {
    let id: String
    var title: String
    let context: MomentContext
    let createdAt: Date
    var duration: TimeInterval
    var assetIDs: [String]
    var status: StoryStatus
    var videoURL: URL?
    var description: String? = nil
    var progressTitle: String? = nil
    var progressDetail: String? = nil
    var progressValue: Double? = nil
    var storyPlanVersion: Int? = nil
    var moments: [StoryMoment]? = nil
    var availableMoments: [StoryMoment]? = nil
}

struct StoryGenerationOptions: Codable, Hashable, Sendable {
    var style: String?
    var subtitlesEnabled: Bool?
    var subtitleStyle: String?
    var sceneDetailStyle: String?
    var targetDuration: TimeInterval?
    var instruction: String?

    static let automatic = StoryGenerationOptions(
        style: "cinematic",
        subtitlesEnabled: true,
        subtitleStyle: "cinematic",
        sceneDetailStyle: nil,
        targetDuration: nil,
        instruction: nil
    )
}

struct StoryMomentReplacement: Codable, Hashable, Sendable {
    let fromMomentID: String?
    let fromMediaID: String?
    let toMediaID: String

    enum CodingKeys: String, CodingKey {
        case fromMomentID = "fromMomentId"
        case fromMediaID = "fromMediaId"
        case toMediaID = "toMediaId"
    }
}

struct StoryEditRequest: Codable, Hashable, Sendable {
    let removeMomentIDs: [String]
    let removeMediaIDs: [String]
    let addMediaIDs: [String]
    let orderedMediaIDs: [String]
    let replace: [StoryMomentReplacement]
    let analyses: [MediaAnalysis]
    let style: String?
    let subtitlesEnabled: Bool?
    let subtitleStyle: String?
    let sceneDetailStyle: String?
    let targetDuration: TimeInterval?
    let instruction: String?
    let baseVersion: Int?

    init(
        removeMomentIDs: [String],
        removeMediaIDs: [String],
        addMediaIDs: [String],
        orderedMediaIDs: [String] = [],
        replace: [StoryMomentReplacement],
        analyses: [MediaAnalysis] = [],
        style: String? = nil,
        subtitlesEnabled: Bool? = nil,
        subtitleStyle: String? = nil,
        sceneDetailStyle: String? = nil,
        targetDuration: TimeInterval? = nil,
        instruction: String? = nil,
        baseVersion: Int? = nil
    ) {
        self.removeMomentIDs = removeMomentIDs
        self.removeMediaIDs = removeMediaIDs
        self.addMediaIDs = addMediaIDs
        self.orderedMediaIDs = orderedMediaIDs
        self.replace = replace
        self.analyses = analyses
        self.style = style
        self.subtitlesEnabled = subtitlesEnabled
        self.subtitleStyle = subtitleStyle
        self.sceneDetailStyle = sceneDetailStyle
        self.targetDuration = targetDuration
        self.instruction = instruction
        self.baseVersion = baseVersion
    }

    enum CodingKeys: String, CodingKey {
        case removeMomentIDs = "removeMomentIds"
        case removeMediaIDs = "removeMediaIds"
        case addMediaIDs = "addMediaIds"
        case orderedMediaIDs = "orderedMediaIds"
        case replace
        case analyses
        case style
        case subtitlesEnabled
        case subtitleStyle
        case sceneDetailStyle
        case targetDuration
        case instruction
        case baseVersion
    }

    var hasChanges: Bool {
        !removeMomentIDs.isEmpty
            || !removeMediaIDs.isEmpty
            || !addMediaIDs.isEmpty
            || !orderedMediaIDs.isEmpty
            || !replace.isEmpty
            || style != nil
            || subtitlesEnabled != nil
            || subtitleStyle != nil
            || sceneDetailStyle != nil
            || targetDuration != nil
            || instruction?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false
    }

    func withBaseVersion(_ version: Int?) -> StoryEditRequest {
        StoryEditRequest(
            removeMomentIDs: removeMomentIDs,
            removeMediaIDs: removeMediaIDs,
            addMediaIDs: addMediaIDs,
            orderedMediaIDs: orderedMediaIDs,
            replace: replace,
            analyses: analyses,
            style: style,
            subtitlesEnabled: subtitlesEnabled,
            subtitleStyle: subtitleStyle,
            sceneDetailStyle: sceneDetailStyle,
            targetDuration: targetDuration,
            instruction: instruction,
            baseVersion: version ?? baseVersion
        )
    }
}

struct CreateStoryRequest: Codable, Hashable, Sendable {
    let context: MomentContext
    let assets: [MediaAsset]
    let analyses: [MediaAnalysis]
    let style: String?
    let subtitlesEnabled: Bool?
    let subtitleStyle: String?
    let sceneDetailStyle: String?
    let targetDuration: TimeInterval?
    let instruction: String?

    init(
        context: MomentContext,
        assets: [MediaAsset],
        analyses: [MediaAnalysis],
        options: StoryGenerationOptions = .automatic
    ) {
        self.context = context
        self.assets = assets
        self.analyses = analyses
        self.style = options.style
        self.subtitlesEnabled = options.subtitlesEnabled
        self.subtitleStyle = options.subtitleStyle
        self.sceneDetailStyle = options.sceneDetailStyle
        self.targetDuration = options.targetDuration
        let trimmedInstruction = options.instruction?.trimmingCharacters(in: .whitespacesAndNewlines)
        self.instruction = trimmedInstruction?.isEmpty == true ? nil : trimmedInstruction
    }
}
