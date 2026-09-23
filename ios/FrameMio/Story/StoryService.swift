import Foundation

private func storyServiceDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][StoryService] \(message())")
#endif
}

final class RemoteStoryService: StoryService, @unchecked Sendable {
    private let apiClient: APIClient
    private let persistence: StoryPersistence
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder

    init(apiClient: APIClient, persistence: StoryPersistence) {
        self.apiClient = apiClient
        self.persistence = persistence
        encoder = FrameMioJSONCoding.apiEncoder()
        decoder = FrameMioJSONCoding.apiDecoder()
    }

    func videoTemplates(for projectType: VideoProjectType? = nil) async throws -> [VideoTemplate] {
        let path = projectType.map { "/v1/video-templates?project_type=\($0.rawValue)" } ?? "/v1/video-templates"
        let data = try await apiClient.data(for: APIRequest(method: .get, path: path))
        do {
            return try decoder.decode([VideoTemplate].self, from: data)
        } catch {
            throw AppError.invalidResponse
        }
    }

    func cachedStories() async throws -> [Story] {
        try await persistence.loadStories()
    }

    func listStories() async throws -> [Story] {
        do {
            let data = try await apiClient.data(for: APIRequest(method: .get, path: "/v1/stories"))
            let stories = try decoder.decode([Story].self, from: data)
            try await persistence.saveStories(stories)
            return stories
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            let cachedStories = try await persistence.loadStories()
            let appError = AppError.from(error)
            if !cachedStories.isEmpty || appError == .networkUnavailable || appError == .invalidConfiguration {
                return cachedStories
            }
            throw appError
        }
    }

    func story(id: String) async throws -> Story {
        do {
            let data = try await apiClient.data(for: APIRequest(method: .get, path: "/v1/stories/\(id)"))
            let story = try decoder.decode(Story.self, from: data)
            let existing = try await persistence.loadStories()
            try await persistence.saveStories([story] + existing.filter { $0.id != story.id })
            return story
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            let appError = AppError.from(error)
            let cachedStories = try await persistence.loadStories()
            if (appError == .networkUnavailable || appError == .invalidConfiguration),
               let cachedStory = cachedStories.first(where: { $0.id == id }) {
                return cachedStory
            }
            throw appError
        }
    }

    func createStory(request: CreateStoryRequest) async throws -> Story {
        let body: Data
        do {
            body = try encoder.encode(request)
        } catch {
            throw AppError.invalidResponse
        }
        storyServiceDebugLog("create_story_request duration_mode=\(request.durationMode.rawValue) target_duration=\(request.targetDuration.map { String(Int($0.rounded())) } ?? "auto") assets=\(request.assets.count) videos=\(request.assets.filter(\.isVideo).count) total_video_source_seconds=\(request.assets.reduce(0.0) { total, asset in asset.isVideo ? total + max(0, asset.duration) : total }) template_id=\(request.templateID ?? "none") template_duration_mode=\(request.templateConfiguration?.durationMode?.rawValue ?? "none") template_duration_seconds=\(request.templateConfiguration?.durationSeconds.map(String.init) ?? "none")")

        let data = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/v1/stories",
                body: body,
                headers: ["Idempotency-Key": "ios-story-\(UUID().uuidString)"]
            )
        )
        do {
            let story = try decoder.decode(Story.self, from: data)
            let existing = try await persistence.loadStories()
            try await persistence.saveStories([story] + existing.filter { $0.id != story.id })
            return story
        } catch {
            throw AppError.invalidResponse
        }
    }

    func createProject(request: CreateVideoProjectRequest) async throws -> Story {
        guard request.draft.type != .story else {
            return try await createStory(
                request: CreateStoryRequest(
                    context: request.draft.context,
                    assets: request.assets,
                    analyses: request.analyses,
                    options: request.draft.storyOptions,
                    templateID: request.draft.templateID,
                    templateVersion: request.draft.templateVersion,
                    templateConfiguration: request.draft.templateConfiguration
                )
            )
        }

        let mediaIDs = request.assets.compactMap { request.mediaIDsByAssetID[$0.id] }
        guard mediaIDs.count == request.assets.count, !mediaIDs.isEmpty else {
            throw AppError.invalidResponse
        }

        let createBody: Data
        do {
            createBody = try encoder.encode(GenericProjectCreateRequest(from: request, mediaIDs: mediaIDs))
        } catch {
            throw AppError.invalidResponse
        }

        let idempotencyKey = "ios-\(request.draft.type.rawValue)-\(UUID().uuidString)"
        storyServiceDebugLog("generic_project_create_start type=\(request.draft.type.rawValue) media_count=\(mediaIDs.count) idempotency_key_present=true")
        let projectData = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/projects",
                body: createBody,
                headers: ["Idempotency-Key": idempotencyKey]
            )
        )

        let project: GenericProjectResponse
        do {
            project = try decoder.decode(GenericProjectResponse.self, from: projectData)
        } catch {
            throw AppError.invalidResponse
        }
        storyServiceDebugLog("generic_project_create_complete type=\(project.type?.rawValue ?? request.draft.type.rawValue) project_id=\(project.id) status=\(project.status) media_count=\(project.mediaIDs.count)")

        _ = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/projects/\(project.id)/analyze",
                headers: ["Idempotency-Key": "\(idempotencyKey)-analysis"]
            )
        )
        storyServiceDebugLog("generic_project_analyze_accepted type=\(request.draft.type.rawValue) project_id=\(project.id)")

        let story = project.storyFallback
        let existing = try await persistence.loadStories()
        try await persistence.saveStories([story] + existing.filter { $0.id != story.id })
        return story
    }

    func retryProcessing(id: String) async throws -> Story {
        try await requestRender(id: id)
    }

    func regenerateStory(id: String) async throws -> Story {
        let data = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/v1/stories/\(id)/regenerate",
                headers: ["Idempotency-Key": "ios-story-regenerate-\(id)-\(UUID().uuidString)"]
            )
        )
        do {
            let updated = try decoder.decode(Story.self, from: data)
            let existing = try await persistence.loadStories()
            try await persistence.saveStories([updated] + existing.filter { $0.id != updated.id })
            return updated
        } catch {
            throw AppError.invalidResponse
        }
    }

    func renderAgain(id: String) async throws -> Story {
        try await requestRender(id: id)
    }

    func saveStoryEdits(storyID: String, request: StoryEditRequest) async throws -> Story {
        guard request.hasChanges else {
            return try await story(id: storyID)
        }
        let body: Data
        do {
            body = try encoder.encode(request)
        } catch {
            throw AppError.invalidResponse
        }
        let data = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/v1/stories/\(storyID)/revisions",
                body: body,
                headers: ["Idempotency-Key": "ios-story-edit-\(storyID)-\(UUID().uuidString)"]
            )
        )
        do {
            let updated = try decoder.decode(Story.self, from: data)
            let existing = try await persistence.loadStories()
            try await persistence.saveStories([updated] + existing.filter { $0.id != updated.id })
            return updated
        } catch {
            throw AppError.invalidResponse
        }
    }

    func deleteStory(id: String) async throws {
        _ = try await apiClient.data(for: APIRequest(method: .delete, path: "/v1/stories/\(id)"))
        try await persistence.removeStory(id: id)
    }

    private func requestRender(id: String) async throws -> Story {
        let body: Data
        do {
            body = try encoder.encode(["output_preset": "social"])
        } catch {
            throw AppError.invalidResponse
        }
        _ = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/projects/\(id)/render",
                body: body,
                headers: ["Idempotency-Key": "ios-render-\(id)-\(UUID().uuidString)"]
            )
        )
        return try await story(id: id)
    }

    func replaceMoment(storyID: String, assetID: String) async throws -> Story {
        try await updateCachedStory(id: storyID) { story in
            if let firstAssetID = story.assetIDs.first {
                story.assetIDs = story.assetIDs.map { $0 == firstAssetID ? assetID : $0 }
            } else {
                story.assetIDs = [assetID]
            }
            story.assetIDs = deduplicated(story.assetIDs)
            story.status = .queued
        }
    }

    func removeMoment(storyID: String, assetID: String) async throws -> Story {
        try await updateCachedStory(id: storyID) { story in
            story.assetIDs.removeAll { $0 == assetID }
            story.duration = max(8, Double(max(story.assetIDs.count, 1)) * 4)
            story.status = story.assetIDs.isEmpty ? .failed : .queued
        }
    }

    private func updateCachedStory(id: String, transform: (inout Story) -> Void) async throws -> Story {
        let existing = try await persistence.loadStories()
        guard let story = existing.first(where: { $0.id == id }) else {
            throw AppError.invalidResponse
        }
        var updated = story
        transform(&updated)
        try await persistence.saveStories([updated] + existing.filter { $0.id != id })
        return updated
    }

    private func deduplicated(_ assetIDs: [String]) -> [String] {
        var seen = Set<String>()
        return assetIDs.filter { !$0.isEmpty && seen.insert($0).inserted }
    }
}


final class RemotePromptTranscriptionService: PromptTranscriptionService, @unchecked Sendable {
    private let apiClient: APIClient
    private let decoder: JSONDecoder

    init(apiClient: APIClient) {
        self.apiClient = apiClient
        decoder = FrameMioJSONCoding.apiDecoder()
    }

    func transcribePromptAudio(_ recording: PromptAudioRecording) async throws -> String {
        let data = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/v1/transcriptions",
                body: recording.data,
                headers: [
                    "Content-Type": recording.contentType,
                    "X-File-Name": recording.fileName
                ]
            )
        )
        do {
            let response = try decoder.decode(PromptTranscriptionResponse.self, from: data)
            return response.transcript
        } catch {
            throw AppError.invalidResponse
        }
    }

    func livePromptConfiguration() async throws -> PromptLiveTranscriptionConfiguration {
        if let apiKey = AppConfiguration.deepgramAPIKey {
            return PromptLiveTranscriptionConfiguration(
                accessToken: apiKey,
                expiresIn: 60,
                websocketURL: URL(string: "wss://api.deepgram.com/v1/listen")!,
                model: "nova-3",
                language: AppConfiguration.deepgramLanguage,
                authorizationScheme: "Subprotocol"
            )
        }
        let data = try await apiClient.data(
            for: APIRequest(
                method: .post,
                path: "/v1/transcriptions/token"
            )
        )
        do {
            return try decoder.decode(PromptLiveTranscriptionConfiguration.self, from: data)
        } catch {
            throw AppError.invalidResponse
        }
    }
}

private struct GenericProjectCreateRequest: Encodable {
    let type: VideoProjectType
    let title: String
    let context: String
    let mediaIDs: [String]
    let configuration: GenericProjectConfiguration
    let analyses: [GenericProjectAnalysis]

    enum CodingKeys: String, CodingKey {
        case type
        case title
        case context
        case mediaIDs = "media_ids"
        case configuration
        case analyses
    }

    init(from request: CreateVideoProjectRequest, mediaIDs: [String]) {
        type = request.draft.type
        title = Self.title(for: request.draft)
        context = Self.context(for: request.draft)
        self.mediaIDs = mediaIDs
        configuration = GenericProjectConfiguration(draft: request.draft)
        analyses = request.analyses.compactMap { analysis in
            guard let mediaID = request.mediaIDsByAssetID[analysis.assetID] else { return nil }
            return GenericProjectAnalysis(mediaID: mediaID, analysis: analysis)
        }
    }

    private static func title(for draft: VideoProjectDraft) -> String {
        switch draft.type {
        case .story:
            draft.context.rawValue
        case .promo:
            "Preparing Promo"
        case .reel:
            "Preparing Reel"
        }
    }

    private static func context(for draft: VideoProjectDraft) -> String {
        switch draft.type {
        case .story:
            draft.context.rawValue
        case .promo:
            [
                "Promo",
                draft.promo?.subject.title,
                draft.promo?.goal.title,
                draft.promo?.prompt
            ]
            .compactMap { $0?.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
            .joined(separator: " · ")
        case .reel:
            [
                "Reel",
                draft.reel?.platform.title,
                draft.reel?.style.title,
                draft.reel?.prompt
            ]
            .compactMap { $0?.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
            .joined(separator: " · ")
        }
    }
}

private struct GenericProjectConfiguration: Encodable {
    let aiInstructions: String?
    let promo: PromoConfigurationPayload?
    let reel: ReelConfigurationPayload?
    let composition: CompositionConfigurationPayload
    let templateID: String?
    let templateVersion: Int?
    let templateConfiguration: VideoTemplateConfiguration?

    enum CodingKeys: String, CodingKey {
        case aiInstructions = "ai_instructions"
        case promo
        case reel
        case composition
        case templateID = "template_id"
        case templateVersion = "template_version"
        case templateConfiguration = "template_configuration"
    }

    init(draft: VideoProjectDraft) {
        aiInstructions = draft.prompt?.trimmingCharacters(in: .whitespacesAndNewlines).nilIfEmpty
        promo = draft.type == .promo ? PromoConfigurationPayload(configuration: draft.promo ?? .automatic) : nil
        reel = draft.type == .reel ? ReelConfigurationPayload(configuration: draft.reel ?? .automatic) : nil
        composition = CompositionConfigurationPayload(aspectRatio: draft.type == .promo ? "16:9" : "9:16")
        templateID = draft.templateID
        templateVersion = draft.templateVersion
        templateConfiguration = draft.templateConfiguration
    }
}

private struct PromoConfigurationPayload: Encodable {
    let promotionGoal: String
    let productInformation: String
    let callToAction: String
    let targetAudience: String
    let desiredDurationSeconds: Int?
    let durationMode: VideoDurationMode
    let removeFillerAndPauses: Bool

    enum CodingKeys: String, CodingKey {
        case promotionGoal = "promotion_goal"
        case productInformation = "product_information"
        case callToAction = "call_to_action"
        case targetAudience = "target_audience"
        case desiredDurationSeconds = "desired_duration_seconds"
        case durationMode = "duration_mode"
        case removeFillerAndPauses = "remove_filler_and_pauses"
    }

    init(configuration: PromoCreationConfiguration) {
        promotionGoal = configuration.goal.title
        productInformation = configuration.subject.title
        callToAction = switch configuration.goal {
        case .driveDownloads: "Download now"
        case .announce: "Learn more"
        case .explain: "See how it works"
        case .getAttention: "Watch now"
        case .sell: "Shop now"
        }
        targetAudience = "general audience"
        desiredDurationSeconds = configuration.duration.seconds.map(Int.init)
        durationMode = configuration.duration.durationMode
        removeFillerAndPauses = configuration.duration == .fullSource && configuration.removeFillerAndPauses
    }
}

private struct ReelConfigurationPayload: Encodable {
    let socialPlatform: String
    let targetDurationSeconds: Int?
    let durationMode: VideoDurationMode
    let removeFillerAndPauses: Bool
    let style: String
    let hookPreference: String
    let captionStyle: String
    let aspectRatio: String

    enum CodingKeys: String, CodingKey {
        case socialPlatform = "social_platform"
        case targetDurationSeconds = "target_duration_seconds"
        case durationMode = "duration_mode"
        case removeFillerAndPauses = "remove_filler_and_pauses"
        case style
        case hookPreference = "hook_preference"
        case captionStyle = "caption_style"
        case aspectRatio = "aspect_ratio"
    }

    init(configuration: ReelCreationConfiguration) {
        socialPlatform = configuration.platform.apiValue
        targetDurationSeconds = configuration.duration.seconds.map(Int.init)
        durationMode = configuration.duration.durationMode
        removeFillerAndPauses = configuration.duration == .fullSource && configuration.removeFillerAndPauses
        style = configuration.style.apiValue
        hookPreference = configuration.style.hookPreference
        captionStyle = configuration.style.captionStylePreset
        aspectRatio = "9:16"
    }
}

private struct CompositionConfigurationPayload: Encodable {
    let aspectRatio: String

    enum CodingKeys: String, CodingKey {
        case aspectRatio = "aspect_ratio"
    }
}

private struct GenericProjectAnalysis: Encodable {
    let mediaID: String
    let labels: [String]
    let peopleCount: Int
    let qualityScore: Double

    enum CodingKeys: String, CodingKey {
        case mediaID = "media_id"
        case labels
        case peopleCount = "people_count"
        case qualityScore = "quality_score"
    }

    init(mediaID: String, analysis: MediaAnalysis) {
        self.mediaID = mediaID
        labels = analysis.labels
        peopleCount = analysis.peopleCount
        qualityScore = analysis.confidence
    }
}

private struct GenericProjectResponse: Decodable {
    let id: String
    let type: VideoProjectType?
    let title: String
    let context: String
    let mediaIDs: [String]
    let status: String
    let createdAt: Date

    enum CodingKeys: String, CodingKey {
        case id
        case type
        case title
        case context
        case mediaIDs = "media_ids"
        case status
        case createdAt = "created_at"
    }

    var storyFallback: Story {
        Story(
            id: id,
            type: type ?? .story,
            title: title,
            context: MomentContext(rawValue: context) ?? .specialMoments,
            createdAt: createdAt,
            duration: max(8, Double(max(mediaIDs.count, 1)) * 4),
            assetIDs: mediaIDs,
            status: storyStatus,
            videoURL: nil
        )
    }

    private var storyStatus: StoryStatus {
        switch status {
        case "draft", "analyzing", "story_ready":
            .analyzing
        case "rendering":
            .rendering
        case "ready":
            .ready
        case "failed":
            .failed
        default:
            .queued
        }
    }
}

private extension String {
    var nilIfEmpty: String? {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}

private struct PromptTranscriptionResponse: Decodable {
    let transcript: String
}

final class MockStoryService: StoryService, @unchecked Sendable {
    private let persistence: StoryPersistence

    init(persistence: StoryPersistence) {
        self.persistence = persistence
    }

    func cachedStories() async throws -> [Story] {
        try await persistence.loadStories()
    }

    func listStories() async throws -> [Story] {
        try await Task.sleep(for: .milliseconds(180))
        let existing = try await persistence.loadStories()
        if !existing.isEmpty {
            return existing
        }

        let sample = Story(
            id: "preview-story",
            title: "Sunday at home",
            context: .family,
            createdAt: Date(),
            duration: 48,
            assetIDs: [],
            status: .ready,
            videoURL: nil
        )
        try await persistence.saveStories([sample])
        return [sample]
    }

    func story(id: String) async throws -> Story {
        try await Task.sleep(for: .milliseconds(120))
        guard let story = try await listStories().first(where: { $0.id == id }) else {
            throw AppError.invalidResponse
        }
        return story
    }

    func createStory(request: CreateStoryRequest) async throws -> Story {
        try await Task.sleep(for: .milliseconds(240))
        let story = Story(
            id: UUID().uuidString,
            type: .story,
            title: request.context.rawValue,
            context: request.context,
            createdAt: Date(),
            duration: max(12, Double(request.assets.count) * 4),
            assetIDs: request.assets.map(\.id),
            status: .ready,
            videoURL: nil
        )
        let existing = try await persistence.loadStories()
        try await persistence.saveStories([story] + existing)
        return story
    }

    func createProject(request: CreateVideoProjectRequest) async throws -> Story {
        if request.draft.type == .story {
            return try await createStory(
                request: CreateStoryRequest(
                    context: request.draft.context,
                    assets: request.assets,
                    analyses: request.analyses,
                    options: request.draft.storyOptions,
                    templateID: request.draft.templateID,
                    templateVersion: request.draft.templateVersion,
                    templateConfiguration: request.draft.templateConfiguration
                )
            )
        }
        try await Task.sleep(for: .milliseconds(240))
        let story = Story(
            id: UUID().uuidString,
            type: request.draft.type,
            title: request.draft.type == .promo ? "Preparing Promo" : "Preparing Reel",
            context: request.draft.context,
            createdAt: Date(),
            duration: request.draft.type == .promo
                ? request.draft.promo?.duration.seconds ?? 20
                : request.draft.reel?.duration.seconds ?? 15,
            assetIDs: request.mediaIDsByAssetID.isEmpty ? request.assets.map(\.id) : request.assets.compactMap { request.mediaIDsByAssetID[$0.id] },
            status: .analyzing,
            videoURL: nil
        )
        let existing = try await persistence.loadStories()
        try await persistence.saveStories([story] + existing)
        return story
    }

    func videoTemplates(for projectType: VideoProjectType?) async throws -> [VideoTemplate] {
        let templates = VideoTemplate.localFallbackCatalog
        guard let projectType else { return templates }
        return templates.filter { $0.supportedProjectTypes.contains(projectType) }
    }

    func retryProcessing(id: String) async throws -> Story {
        try await mutateStory(id: id) { story in
            story.status = .queued
        }
    }

    func regenerateStory(id: String) async throws -> Story {
        try await mutateStory(id: id) { story in
            story.status = .analyzing
            story.title = "Your moments, revisited"
        }
    }

    func renderAgain(id: String) async throws -> Story {
        try await mutateStory(id: id) { story in
            story.status = .rendering
        }
    }

    func saveStoryEdits(storyID: String, request: StoryEditRequest) async throws -> Story {
        try await mutateStory(id: storyID) { story in
            if !request.removeMediaIDs.isEmpty || !request.removeMomentIDs.isEmpty {
                let removed = Set(request.removeMediaIDs + request.removeMomentIDs)
                story.assetIDs.removeAll { removed.contains($0) }
                story.moments = story.moments?.filter { !removed.contains($0.id) && !removed.contains($0.mediaID) }
            }
            for mediaID in request.addMediaIDs where !story.assetIDs.contains(mediaID) {
                story.assetIDs.append(mediaID)
            }
            if let style = request.style, !style.isEmpty {
                story.description = "FrameMio is making this version feel \(style)."
            }
            if let targetDuration = request.targetDuration {
                story.duration = targetDuration
            }
            story.status = .analyzing
        }
    }

    func deleteStory(id: String) async throws {
        try await Task.sleep(for: .milliseconds(140))
        try await persistence.removeStory(id: id)
    }

    func replaceMoment(storyID: String, assetID: String) async throws -> Story {
        try await mutateStory(id: storyID) { story in
            if story.assetIDs.isEmpty {
                story.assetIDs = [assetID]
            } else {
                story.assetIDs[0] = assetID
            }
            story.assetIDs = deduplicated(story.assetIDs)
            story.status = .queued
        }
    }

    func removeMoment(storyID: String, assetID: String) async throws -> Story {
        try await mutateStory(id: storyID) { story in
            story.assetIDs.removeAll { $0 == assetID }
            story.duration = max(8, Double(max(story.assetIDs.count, 1)) * 4)
            story.status = story.assetIDs.isEmpty ? .failed : .queued
        }
    }

    private func mutateStory(id: String, transform: (inout Story) -> Void) async throws -> Story {
        let existing = try await persistence.loadStories()
        guard let story = existing.first(where: { $0.id == id }) else {
            throw AppError.invalidResponse
        }
        try await Task.sleep(for: .milliseconds(140))
        var updated = story
        transform(&updated)
        try await persistence.saveStories([updated] + existing.filter { $0.id != id })
        return updated
    }

    private func deduplicated(_ assetIDs: [String]) -> [String] {
        var seen = Set<String>()
        return assetIDs.filter { !$0.isEmpty && seen.insert($0).inserted }
    }
}

final class MockPromptTranscriptionService: PromptTranscriptionService, @unchecked Sendable {
    func transcribePromptAudio(_ recording: PromptAudioRecording) async throws -> String {
        try await Task.sleep(nanoseconds: 300_000_000)
        return "Make it warm and emotional, with a slower pace and more focus on smiling faces."
    }

    func livePromptConfiguration() async throws -> PromptLiveTranscriptionConfiguration {
        PromptLiveTranscriptionConfiguration(
            accessToken: "preview-token",
            expiresIn: 60,
            websocketURL: URL(string: "wss://api.deepgram.com/v1/listen")!,
            model: "nova-3",
            language: "en",
            authorizationScheme: "Subprotocol"
        )
    }
}
