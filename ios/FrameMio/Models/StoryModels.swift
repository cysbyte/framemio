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
    let type: StoryMomentType?
    let thumbnailURL: URL?
    let title: String?
    let description: String?
    let isSelected: Bool?

    enum CodingKeys: String, CodingKey {
        case id
        case momentID = "momentId"
        case mediaID = "mediaId"
        case type
        case thumbnailURL = "thumbnailUrl"
        case title
        case description
        case isSelected = "selected"
    }

    init(
        id: String,
        mediaID: String,
        type: StoryMomentType? = nil,
        thumbnailURL: URL? = nil,
        title: String? = nil,
        description: String? = nil,
        isSelected: Bool? = nil
    ) {
        self.id = id
        self.mediaID = mediaID
        self.type = type
        self.thumbnailURL = thumbnailURL
        self.title = title
        self.description = description
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
        type = try container.decodeIfPresent(StoryMomentType.self, forKey: .type)
        thumbnailURL = try container.decodeIfPresent(URL.self, forKey: .thumbnailURL)
        title = try container.decodeIfPresent(String.self, forKey: .title)
        description = try container.decodeIfPresent(String.self, forKey: .description)
        isSelected = try container.decodeIfPresent(Bool.self, forKey: .isSelected)
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(mediaID, forKey: .mediaID)
        try container.encodeIfPresent(type, forKey: .type)
        try container.encodeIfPresent(thumbnailURL, forKey: .thumbnailURL)
        try container.encodeIfPresent(title, forKey: .title)
        try container.encodeIfPresent(description, forKey: .description)
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
    var moments: [StoryMoment]? = nil
    var availableMoments: [StoryMoment]? = nil
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
    let replace: [StoryMomentReplacement]
    let analyses: [MediaAnalysis]
    let style: String?
    let subtitlesEnabled: Bool?
    let subtitleStyle: String?
    let targetDuration: TimeInterval?
    let instruction: String?

    init(
        removeMomentIDs: [String],
        removeMediaIDs: [String],
        addMediaIDs: [String],
        replace: [StoryMomentReplacement],
        analyses: [MediaAnalysis] = [],
        style: String? = nil,
        subtitlesEnabled: Bool? = nil,
        subtitleStyle: String? = nil,
        targetDuration: TimeInterval? = nil,
        instruction: String? = nil
    ) {
        self.removeMomentIDs = removeMomentIDs
        self.removeMediaIDs = removeMediaIDs
        self.addMediaIDs = addMediaIDs
        self.replace = replace
        self.analyses = analyses
        self.style = style
        self.subtitlesEnabled = subtitlesEnabled
        self.subtitleStyle = subtitleStyle
        self.targetDuration = targetDuration
        self.instruction = instruction
    }

    enum CodingKeys: String, CodingKey {
        case removeMomentIDs = "removeMomentIds"
        case removeMediaIDs = "removeMediaIds"
        case addMediaIDs = "addMediaIds"
        case replace
        case analyses
        case style
        case subtitlesEnabled
        case subtitleStyle
        case targetDuration
        case instruction
    }

    var hasChanges: Bool {
        !removeMomentIDs.isEmpty
            || !removeMediaIDs.isEmpty
            || !addMediaIDs.isEmpty
            || !replace.isEmpty
            || style != nil
            || subtitlesEnabled != nil
            || subtitleStyle != nil
            || targetDuration != nil
            || instruction?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false
    }
}

struct CreateStoryRequest: Codable, Hashable, Sendable {
    let context: MomentContext
    let assets: [MediaAsset]
    let analyses: [MediaAnalysis]
}
