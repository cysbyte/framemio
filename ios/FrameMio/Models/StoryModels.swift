import Foundation

enum VideoProjectType: String, Codable, CaseIterable, Hashable, Sendable {
    case story
    case promo
    case reel

    var title: String {
        switch self {
        case .story: "Story"
        case .promo: "Promo"
        case .reel: "Reel"
        }
    }

    var description: String {
        switch self {
        case .story: "For trips, memories and everyday moments"
        case .promo: "For products, apps and businesses"
        case .reel: "For short-form social content"
        }
    }

    var icon: String {
        switch self {
        case .story: "sparkles"
        case .promo: "megaphone.fill"
        case .reel: "play.rectangle.on.rectangle.fill"
        }
    }
}

struct VideoProjectTerminology: Hashable, Sendable {
    let singular: String
    let plural: String
    let projectName: String

    static func forType(_ type: VideoProjectType) -> VideoProjectTerminology {
        switch type {
        case .story:
            VideoProjectTerminology(singular: "Moment", plural: "Moments", projectName: "Story")
        case .promo:
            VideoProjectTerminology(singular: "Scene", plural: "Scenes", projectName: "Promo")
        case .reel:
            VideoProjectTerminology(singular: "Clip", plural: "Clips", projectName: "Reel")
        }
    }
}

struct VideoProjectEditorConfiguration: Hashable, Sendable {
    let type: VideoProjectType
    let terminology: VideoProjectTerminology
    let title: String
    let eyebrow: String
    let editButtonTitle: String
    let deleteButtonTitle: String
    let addItemTitle: String
    let addSheetTitle: String
    let replaceSheetTitle: String
    let timelineSubtitle: String
    let timelineGuidance: String
    let textSectionTitle: String
    let textSectionSubtitle: String
    let aiSectionTitle: String
    let aiSectionSubtitle: String
    let aiPromptPlaceholder: String
    let aiSuggestions: [String]
    let primaryAIButtonTitle: String
    let styleOptions: [String]
    let subtitleStyleOptions: [String]
    let showsSceneDetailControl: Bool
    let showsBrandControls: Bool
    let showsCTAControls: Bool
    let showsSceneDurationControls: Bool
    let showsAudioControls: Bool
    let audioTitle: String
    let audioSubtitle: String
    let defaultTargetDuration: TimeInterval?

    static func forType(_ type: VideoProjectType) -> VideoProjectEditorConfiguration {
        let terminology = VideoProjectTerminology.forType(type)
        switch type {
        case .story:
            return VideoProjectEditorConfiguration(
                type: type,
                terminology: terminology,
                title: "Edit Story",
                eyebrow: "YOUR STORY",
                editButtonTitle: "Edit Story",
                deleteButtonTitle: "Remove Story",
                addItemTitle: "Add Moment",
                addSheetTitle: "Add to Story",
                replaceSheetTitle: "Replace Moment",
                timelineSubtitle: "Drag to reorder",
                timelineGuidance: "Drag moments to suggest the story order. FrameMio still handles pacing and transitions.",
                textSectionTitle: "Text",
                textSectionSubtitle: "Show spoken dialogue as captions",
                aiSectionTitle: "Edit with AI",
                aiSectionSubtitle: "Tell FrameMio what to change",
                aiPromptPlaceholder: "Make this trip feel more cinematic and nostalgic.",
                aiSuggestions: ["More cinematic", "More emotional", "More fun", "Focus on people", "Focus on places"],
                primaryAIButtonTitle: "Edit with AI",
                styleOptions: ["Natural", "Cinematic", "Warm", "Playful"],
                subtitleStyleOptions: ["Bold", "Cinematic", "Minimal", "Playful"],
                showsSceneDetailControl: true,
                showsBrandControls: false,
                showsCTAControls: false,
                showsSceneDurationControls: false,
                showsAudioControls: true,
                audioTitle: "Music/audio",
                audioSubtitle: "FrameMio chooses music and audio balance for this story.",
                defaultTargetDuration: nil
            )
        case .promo:
            return VideoProjectEditorConfiguration(
                type: type,
                terminology: terminology,
                title: "Edit Promo",
                eyebrow: "PROMO",
                editButtonTitle: "Edit Promo",
                deleteButtonTitle: "Remove Promo",
                addItemTitle: "Add Scene",
                addSheetTitle: "Add Scene",
                replaceSheetTitle: "Replace Scene",
                timelineSubtitle: "Reorder and select scenes",
                timelineGuidance: "Reorder scenes and set the brief for timing, text, CTA, brand, and audio. FrameMio applies the detailed edit.",
                textSectionTitle: "Text & captions",
                textSectionSubtitle: "Control captions and short overlay text",
                aiSectionTitle: "Improve with AI",
                aiSectionSubtitle: "Ask FrameMio to improve the promotional edit",
                aiPromptPlaceholder: "Make this feel like a premium product advertisement.",
                aiSuggestions: [
                    "Make the opening more attention grabbing.",
                    "Make this feel like a premium product advertisement.",
                    "Shorten this to 15 seconds.",
                    "Make the CTA more prominent."
                ],
                primaryAIButtonTitle: "Improve Promo",
                styleOptions: ["Clean", "Premium", "Bold", "Playful"],
                subtitleStyleOptions: ["Bold", "Clean", "Minimal", "Premium"],
                showsSceneDetailControl: true,
                showsBrandControls: true,
                showsCTAControls: true,
                showsSceneDurationControls: true,
                showsAudioControls: true,
                audioTitle: "Voiceover/audio",
                audioSubtitle: "Describe voiceover or audio direction for the draft.",
                defaultTargetDuration: 30
            )
        case .reel:
            return VideoProjectEditorConfiguration(
                type: type,
                terminology: terminology,
                title: "Edit Reel",
                eyebrow: "REEL",
                editButtonTitle: "Edit Reel",
                deleteButtonTitle: "Remove Reel",
                addItemTitle: "Add Clip",
                addSheetTitle: "Add Clip",
                replaceSheetTitle: "Replace Clip",
                timelineSubtitle: "Reorder clips for pacing",
                timelineGuidance: "Keep the strongest clips, tighten pacing, and tell FrameMio how to handle hook, captions, and vertical framing.",
                textSectionTitle: "Captions",
                textSectionSubtitle: "Make dialogue and key beats readable in short-form feeds",
                aiSectionTitle: "Edit with AI",
                aiSectionSubtitle: "Ask FrameMio to make this more engaging",
                aiPromptPlaceholder: "Make the first 2 seconds stronger.",
                aiSuggestions: [
                    "Make this more engaging.",
                    "Strengthen the hook.",
                    "Make this 15 seconds.",
                    "Remove boring parts.",
                    "Make the captions more readable.",
                    "Make this more cinematic."
                ],
                primaryAIButtonTitle: "Edit Reel",
                styleOptions: ["Dynamic", "Cinematic", "Clean", "Talking-head"],
                subtitleStyleOptions: ["Bold", "Clean", "Minimal", "Social"],
                showsSceneDetailControl: false,
                showsBrandControls: false,
                showsCTAControls: false,
                showsSceneDurationControls: true,
                showsAudioControls: true,
                audioTitle: "Audio",
                audioSubtitle: "Describe music, voice, or sync direction for the reel.",
                defaultTargetDuration: 15
            )
        }
    }
}

enum ProjectDurationOption: String, Codable, CaseIterable, Hashable, Sendable {
    case fifteen
    case thirty
    case sixty
    case automatic

    var title: String {
        switch self {
        case .fifteen: "15 sec"
        case .thirty: "30 sec"
        case .sixty: "60 sec"
        case .automatic: "Auto"
        }
    }

    var seconds: TimeInterval? {
        switch self {
        case .fifteen: 15
        case .thirty: 30
        case .sixty: 60
        case .automatic: nil
        }
    }
}

enum PromoSubject: String, Codable, CaseIterable, Hashable, Sendable {
    case product
    case app
    case business
    case event
    case other

    var title: String {
        switch self {
        case .product: "Product"
        case .app: "App"
        case .business: "Business"
        case .event: "Event"
        case .other: "Other"
        }
    }
}

enum PromoGoal: String, Codable, CaseIterable, Hashable, Sendable {
    case sell
    case explain
    case announce
    case driveDownloads
    case getAttention

    var title: String {
        switch self {
        case .sell: "Sell"
        case .explain: "Explain"
        case .announce: "Announce"
        case .driveDownloads: "Drive downloads"
        case .getAttention: "Get attention"
        }
    }
}

enum ReelPlatform: String, Codable, CaseIterable, Hashable, Sendable {
    case instagram
    case tiktok
    case youtubeShorts
    case other

    var title: String {
        switch self {
        case .instagram: "Instagram"
        case .tiktok: "TikTok"
        case .youtubeShorts: "YouTube Shorts"
        case .other: "Other"
        }
    }

    var apiValue: String {
        switch self {
        case .instagram: "instagram"
        case .tiktok: "tiktok"
        case .youtubeShorts: "youtube_shorts"
        case .other: "other"
        }
    }
}

enum ReelStyle: String, Codable, CaseIterable, Hashable, Sendable {
    case dynamic
    case cinematic
    case clean
    case talkingHead
    case automatic

    var title: String {
        switch self {
        case .dynamic: "Dynamic"
        case .cinematic: "Cinematic"
        case .clean: "Clean"
        case .talkingHead: "Talking-head"
        case .automatic: "Auto"
        }
    }

    var apiValue: String {
        switch self {
        case .dynamic: "dynamic"
        case .cinematic: "cinematic"
        case .clean: "clean"
        case .talkingHead: "talking-head"
        case .automatic: "auto"
        }
    }

    var hookPreference: String {
        switch self {
        case .dynamic: "strong opening hook"
        case .cinematic: "cinematic opening"
        case .clean: "clear opening context"
        case .talkingHead: "lead with the strongest spoken line"
        case .automatic: "auto"
        }
    }

    var captionStylePreset: String {
        switch self {
        case .clean: "clean_minimal"
        case .cinematic: "cinematic_subtitle"
        default: "bold_social"
        }
    }
}

struct PromoCreationConfiguration: Codable, Hashable, Sendable {
    var subject: PromoSubject
    var goal: PromoGoal
    var duration: ProjectDurationOption
    var prompt: String?

    static let automatic = PromoCreationConfiguration(subject: .product, goal: .sell, duration: .automatic)
}

struct ReelCreationConfiguration: Codable, Hashable, Sendable {
    var platform: ReelPlatform
    var duration: ProjectDurationOption
    var style: ReelStyle
    var prompt: String?

    static let automatic = ReelCreationConfiguration(platform: .instagram, duration: .automatic, style: .automatic)
}

struct VideoProjectDraft: Codable, Hashable, Sendable {
    var type: VideoProjectType
    var context: MomentContext
    var storyOptions: StoryGenerationOptions
    var promo: PromoCreationConfiguration?
    var reel: ReelCreationConfiguration?

    static func story(context: MomentContext = .specialMoments, options: StoryGenerationOptions = .automatic) -> VideoProjectDraft {
        VideoProjectDraft(type: .story, context: context, storyOptions: options)
    }

    static func promo(configuration: PromoCreationConfiguration = .automatic) -> VideoProjectDraft {
        VideoProjectDraft(type: .promo, context: .specialMoments, storyOptions: .automatic, promo: configuration)
    }

    static func reel(configuration: ReelCreationConfiguration = .automatic) -> VideoProjectDraft {
        VideoProjectDraft(type: .reel, context: .specialMoments, storyOptions: .automatic, reel: configuration)
    }

    var prompt: String? {
        switch type {
        case .story:
            storyOptions.instruction
        case .promo:
            promo?.prompt
        case .reel:
            reel?.prompt
        }
    }
}

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
    var type: VideoProjectType? = nil
    var title: String
    let context: MomentContext
    let createdAt: Date
    var updatedAt: Date?
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

    var projectType: VideoProjectType {
        type ?? .story
    }

    var lastModifiedAt: Date {
        updatedAt ?? createdAt
    }

    enum CodingKeys: String, CodingKey {
        case id
        case type
        case title
        case context
        case createdAt
        case createdAtSnake = "created_at"
        case updatedAt
        case updatedAtSnake = "updated_at"
        case duration
        case assetIDs
        case assetIDsSnake = "asset_ids"
        case mediaIDs = "media_ids"
        case status
        case videoURL
        case videoURLSnake = "video_url"
        case description
        case progressTitle
        case progressTitleSnake = "progress_title"
        case progressDetail
        case progressDetailSnake = "progress_detail"
        case progressValue
        case progressValueSnake = "progress_value"
        case storyPlanVersion
        case storyPlanVersionSnake = "story_plan_version"
        case moments
        case availableMoments
        case availableMomentsSnake = "available_moments"
    }

    init(
        id: String,
        type: VideoProjectType? = nil,
        title: String,
        context: MomentContext,
        createdAt: Date,
        updatedAt: Date? = nil,
        duration: TimeInterval,
        assetIDs: [String],
        status: StoryStatus,
        videoURL: URL? = nil,
        description: String? = nil,
        progressTitle: String? = nil,
        progressDetail: String? = nil,
        progressValue: Double? = nil,
        storyPlanVersion: Int? = nil,
        moments: [StoryMoment]? = nil,
        availableMoments: [StoryMoment]? = nil
    ) {
        self.id = id
        self.type = type
        self.title = title
        self.context = context
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.duration = duration
        self.assetIDs = assetIDs
        self.status = status
        self.videoURL = videoURL
        self.description = description
        self.progressTitle = progressTitle
        self.progressDetail = progressDetail
        self.progressValue = progressValue
        self.storyPlanVersion = storyPlanVersion
        self.moments = moments
        self.availableMoments = availableMoments
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        type = try container.decodeIfPresent(VideoProjectType.self, forKey: .type)
        title = try container.decode(String.self, forKey: .title)
        context = try container.decode(MomentContext.self, forKey: .context)
        createdAt = try container.decodeIfPresent(Date.self, forKey: .createdAt)
            ?? container.decode(Date.self, forKey: .createdAtSnake)
        updatedAt = try container.decodeIfPresent(Date.self, forKey: .updatedAt)
            ?? container.decodeIfPresent(Date.self, forKey: .updatedAtSnake)
        duration = try container.decodeIfPresent(TimeInterval.self, forKey: .duration) ?? 0
        assetIDs = try container.decodeIfPresent([String].self, forKey: .assetIDs)
            ?? container.decodeIfPresent([String].self, forKey: .assetIDsSnake)
            ?? container.decodeIfPresent([String].self, forKey: .mediaIDs)
            ?? []
        status = try container.decode(StoryStatus.self, forKey: .status)
        videoURL = try container.decodeIfPresent(URL.self, forKey: .videoURL)
            ?? container.decodeIfPresent(URL.self, forKey: .videoURLSnake)
        description = try container.decodeIfPresent(String.self, forKey: .description)
        progressTitle = try container.decodeIfPresent(String.self, forKey: .progressTitle)
            ?? container.decodeIfPresent(String.self, forKey: .progressTitleSnake)
        progressDetail = try container.decodeIfPresent(String.self, forKey: .progressDetail)
            ?? container.decodeIfPresent(String.self, forKey: .progressDetailSnake)
        progressValue = try container.decodeIfPresent(Double.self, forKey: .progressValue)
            ?? container.decodeIfPresent(Double.self, forKey: .progressValueSnake)
        storyPlanVersion = try container.decodeIfPresent(Int.self, forKey: .storyPlanVersion)
            ?? container.decodeIfPresent(Int.self, forKey: .storyPlanVersionSnake)
        moments = try container.decodeIfPresent([StoryMoment].self, forKey: .moments)
        availableMoments = try container.decodeIfPresent([StoryMoment].self, forKey: .availableMoments)
            ?? container.decodeIfPresent([StoryMoment].self, forKey: .availableMomentsSnake)
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encodeIfPresent(type, forKey: .type)
        try container.encode(title, forKey: .title)
        try container.encode(context, forKey: .context)
        try container.encode(createdAt, forKey: .createdAt)
        try container.encodeIfPresent(updatedAt, forKey: .updatedAt)
        try container.encode(duration, forKey: .duration)
        try container.encode(assetIDs, forKey: .assetIDs)
        try container.encode(status, forKey: .status)
        try container.encodeIfPresent(videoURL, forKey: .videoURL)
        try container.encodeIfPresent(description, forKey: .description)
        try container.encodeIfPresent(progressTitle, forKey: .progressTitle)
        try container.encodeIfPresent(progressDetail, forKey: .progressDetail)
        try container.encodeIfPresent(progressValue, forKey: .progressValue)
        try container.encodeIfPresent(storyPlanVersion, forKey: .storyPlanVersion)
        try container.encodeIfPresent(moments, forKey: .moments)
        try container.encodeIfPresent(availableMoments, forKey: .availableMoments)
    }
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

struct CreateVideoProjectRequest: Hashable, Sendable {
    let draft: VideoProjectDraft
    let assets: [MediaAsset]
    let analyses: [MediaAnalysis]
    let mediaIDsByAssetID: [String: String]

    init(
        draft: VideoProjectDraft,
        assets: [MediaAsset],
        analyses: [MediaAnalysis],
        mediaIDsByAssetID: [String: String] = [:]
    ) {
        self.draft = draft
        self.assets = assets
        self.analyses = analyses
        self.mediaIDsByAssetID = mediaIDsByAssetID
    }
}
