import Foundation
import XCTest
@testable import FrameMio

final class VideoProjectCreationTests: XCTestCase {
    func testCreateOptionsExposeRequestedModes() {
        XCTAssertEqual(VideoProjectType.story.title, "Story")
        XCTAssertEqual(VideoProjectType.story.description, "For trips, memories and everyday moments")
        XCTAssertEqual(VideoProjectType.promo.title, "Promo")
        XCTAssertEqual(VideoProjectType.promo.description, "For products, apps and businesses")
        XCTAssertEqual(VideoProjectType.reel.title, "Reel")
        XCTAssertEqual(VideoProjectType.reel.description, "For short-form social content")
    }

    func testDraftFactoriesCreateEachProjectType() {
        let story = VideoProjectDraft.story(context: .trip)
        let promo = VideoProjectDraft.promo(
            configuration: PromoCreationConfiguration(
                subject: .app,
                goal: .driveDownloads,
                duration: .thirty,
                prompt: "Create a premium 20-second advertisement for my app."
            )
        )
        let reel = VideoProjectDraft.reel(
            configuration: ReelCreationConfiguration(
                platform: .tiktok,
                duration: .fifteen,
                style: .dynamic,
                prompt: "Make this fast and punchy."
            )
        )

        XCTAssertEqual(story.type, .story)
        XCTAssertEqual(story.context, .trip)
        XCTAssertEqual(promo.type, .promo)
        XCTAssertEqual(promo.promo?.subject, .app)
        XCTAssertEqual(promo.promo?.goal, .driveDownloads)
        XCTAssertEqual(promo.prompt, "Create a premium 20-second advertisement for my app.")
        XCTAssertEqual(reel.type, .reel)
        XCTAssertEqual(reel.reel?.platform, .tiktok)
        XCTAssertEqual(reel.reel?.style, .dynamic)
        XCTAssertEqual(reel.prompt, "Make this fast and punchy.")
    }

    func testOldStoryWithoutTypeDefaultsToStoryProjectType() throws {
        let data = Data("""
        {
          "id": "legacy-story",
          "title": "Kyoto Trip",
          "context": "Trip",
          "createdAt": "2026-08-26T07:00:00Z",
          "duration": 24,
          "assetIDs": ["photo-1"],
          "status": "ready"
        }
        """.utf8)

        let story = try FrameMioJSONCoding.apiDecoder().decode(Story.self, from: data)

        XCTAssertNil(story.type)
        XCTAssertEqual(story.projectType, .story)
    }

    func testProjectTypeDecodesForCardsAndLists() throws {
        let data = Data("""
        {
          "id": "promo-1",
          "type": "promo",
          "title": "FrameMio Launch",
          "context": "Special moments",
          "createdAt": "2026-08-26T07:00:00Z",
          "updated_at": "2026-08-27T09:30:00Z",
          "duration": 30,
          "assetIDs": ["media-1"],
          "status": "analyzing"
        }
        """.utf8)

        let story = try FrameMioJSONCoding.apiDecoder().decode(Story.self, from: data)

        XCTAssertEqual(story.type, .promo)
        XCTAssertEqual(story.projectType.title, "Promo")
        XCTAssertEqual(story.lastModifiedAt, ISO8601DateFormatter().date(from: "2026-08-27T09:30:00Z"))
    }

    func testGenericProjectContextFallsBackToSpecialMoments() throws {
        let data = Data("""
        {
          "id": "promo-1",
          "type": "promo",
          "title": "App Promo",
          "context": "Promo · App · Drive downloads",
          "created_at": "2026-09-18T14:38:38Z",
          "media_ids": ["media-1"],
          "status": "analyzing"
        }
        """.utf8)

        let story = try FrameMioJSONCoding.apiDecoder().decode(Story.self, from: data)

        XCTAssertEqual(story.projectType, .promo)
        XCTAssertEqual(story.context, .specialMoments)
        XCTAssertEqual(story.assetIDs, ["media-1"])
    }

    func testStoryLastModifiedFallsBackToCreatedAt() throws {
        let data = Data("""
        {
          "id": "story-1",
          "type": "story",
          "title": "Kyoto Trip",
          "context": "Trip",
          "createdAt": "2026-08-26T07:00:00Z",
          "duration": 30,
          "assetIDs": ["media-1"],
          "status": "ready"
        }
        """.utf8)

        let story = try FrameMioJSONCoding.apiDecoder().decode(Story.self, from: data)

        XCTAssertEqual(story.lastModifiedAt, story.createdAt)
    }

    func testEditorTerminologyMapsProjectTypes() {
        XCTAssertEqual(VideoProjectTerminology.forType(.story).plural, "Moments")
        XCTAssertEqual(VideoProjectTerminology.forType(.promo).plural, "Scenes")
        XCTAssertEqual(VideoProjectTerminology.forType(.reel).plural, "Clips")
    }

    func testEditorConfigurationControlsDifferByProjectType() {
        let story = VideoProjectEditorConfiguration.forType(.story)
        let promo = VideoProjectEditorConfiguration.forType(.promo)
        let reel = VideoProjectEditorConfiguration.forType(.reel)

        XCTAssertEqual(story.primaryAIButtonTitle, "Edit with AI")
        XCTAssertFalse(story.showsBrandControls)
        XCTAssertTrue(promo.showsBrandControls)
        XCTAssertTrue(promo.showsCTAControls)
        XCTAssertTrue(promo.aiSuggestions.contains("Make the CTA more prominent."))
        XCTAssertFalse(reel.showsBrandControls)
        XCTAssertTrue(reel.showsSceneDurationControls)
        XCTAssertTrue(reel.aiSuggestions.contains("Make this more engaging."))
        XCTAssertTrue(reel.aiSuggestions.contains("Strengthen the hook."))
        XCTAssertTrue(reel.aiSuggestions.contains("Make this 15 seconds."))
        XCTAssertTrue(reel.aiSuggestions.contains("Remove boring parts."))
        XCTAssertTrue(reel.aiSuggestions.contains("Make the captions more readable."))
        XCTAssertTrue(reel.aiSuggestions.contains("Make this more cinematic."))
    }

    func testPromoEditorDraftComposesModeAwareInstruction() throws {
        var draft = VideoProjectEditorDraft()
        draft.instruction = "Make the opening more attention grabbing."
        draft.brandName = "FrameMio"
        draft.brandColors = "Black and electric blue"
        draft.ctaText = "Download now"
        draft.ctaStyle = "Prominent end card"
        draft.setSceneEdit(
            PromoSceneEditDraft(
                purpose: .hook,
                durationSeconds: 5,
                headline: "Launch faster",
                supportingText: "AI-assisted promo videos",
                ctaText: "Try it free",
                aiAction: "Make this scene more persuasive."
            ),
            for: "media-1"
        )

        let request = draft.request(for: .forType(.promo))
        let instruction = try XCTUnwrap(request.instruction)

        XCTAssertTrue(instruction.contains("Project type: Promo."))
        XCTAssertTrue(instruction.contains("AI instruction: Make the opening more attention grabbing."))
        XCTAssertTrue(instruction.contains("Brand name: FrameMio"))
        XCTAssertTrue(instruction.contains("CTA text: Download now"))
        XCTAssertTrue(instruction.contains("CTA position/style: Prominent end card"))
        XCTAssertTrue(instruction.contains("Promo scene edits:"))
        XCTAssertTrue(instruction.contains("asset media-1; purpose hook; duration 5s"))
        XCTAssertTrue(instruction.contains("headline Launch faster"))
    }

    func testPromoSceneOrderingUsesSharedEditorDraft() {
        var draft = VideoProjectEditorDraft()
        let first = Self.presentation(id: "m1", assetID: "media-1")
        let second = Self.presentation(id: "m2", assetID: "media-2")
        let third = Self.presentation(id: "m3", assetID: "media-3")

        draft.moveMoment(from: 2, to: 0, currentMoments: [first, second, third])

        XCTAssertEqual(draft.orderedMediaIDs, ["media-3", "media-1", "media-2"])
        XCTAssertEqual(draft.ordered([first, second, third]).map(\.assetID), ["media-3", "media-1", "media-2"])
    }

    func testReelEditorDraftComposesHookAndAudioInstruction() throws {
        var draft = VideoProjectEditorDraft()
        draft.instruction = "Make the first 2 seconds stronger."
        draft.sceneTimingInstruction = "Remove slow sections."
        draft.audioDirection = "Use upbeat audio."

        let request = draft.request(for: .forType(.reel))
        let instruction = try XCTUnwrap(request.instruction)

        XCTAssertTrue(instruction.contains("Project type: Reel."))
        XCTAssertTrue(instruction.contains("AI instruction: Make the first 2 seconds stronger."))
        XCTAssertTrue(instruction.contains("Clip editing: Remove slow sections."))
        XCTAssertTrue(instruction.contains("Audio: Use upbeat audio."))
    }

    func testStoryCreationUsesExistingStoryAPI() async throws {
        let apiClient = RecordingAPIClient(responses: [
            Self.storyResponse(id: "story-1", type: .story, title: "Trip", mediaIDs: ["asset-1"])
        ])
        let service = RemoteStoryService(apiClient: apiClient, persistence: MockStoryPersistence())

        let story = try await service.createProject(
            request: CreateVideoProjectRequest(
                draft: .story(context: .trip),
                assets: [Self.asset(id: "asset-1")],
                analyses: [Self.analysis(assetID: "asset-1")]
            )
        )

        let requests = await apiClient.recordedRequests()
        XCTAssertEqual(story.projectType, VideoProjectType.story)
        XCTAssertEqual(requests.map { $0.path }, ["/v1/stories"])
        XCTAssertEqual(requests.first?.method, APIRequest.Method.post)
    }

    func testPromoCreationUsesGenericProjectAPIAndAnalysis() async throws {
        let apiClient = RecordingAPIClient(responses: [
            Self.genericProjectResponse(id: "promo-1", type: .promo, title: "Promo Draft", mediaIDs: ["media-1"]),
            Data("{}".utf8)
        ])
        let service = RemoteStoryService(apiClient: apiClient, persistence: MockStoryPersistence())

        let story = try await service.createProject(
            request: CreateVideoProjectRequest(
                draft: .promo(
                    configuration: PromoCreationConfiguration(
                        subject: .app,
                        goal: .driveDownloads,
                        duration: .thirty,
                        prompt: "Create a premium 20-second advertisement for my app."
                    )
                ),
                assets: [Self.asset(id: "asset-1")],
                analyses: [Self.analysis(assetID: "asset-1")],
                mediaIDsByAssetID: ["asset-1": "media-1"]
            )
        )

        let requests = await apiClient.recordedRequests()
        let body: [String: Any] = try XCTUnwrap(requests.first?.jsonBody)
        let configuration = try XCTUnwrap(body["configuration"] as? [String: Any])
        let promo = try XCTUnwrap(configuration["promo"] as? [String: Any])

        XCTAssertEqual(story.projectType, VideoProjectType.promo)
        XCTAssertEqual(requests.map { $0.path }, ["/projects", "/projects/promo-1/analyze"])
        XCTAssertEqual(body["type"] as? String, "promo")
        XCTAssertEqual(body["media_ids"] as? [String], ["media-1"])
        XCTAssertEqual(configuration["ai_instructions"] as? String, "Create a premium 20-second advertisement for my app.")
        XCTAssertEqual(promo["promotion_goal"] as? String, "Drive downloads")
        XCTAssertEqual(promo["desired_duration_seconds"] as? Int, 30)
    }

    func testReelCreationUsesGenericProjectAPIAndAnalysis() async throws {
        let apiClient = RecordingAPIClient(responses: [
            Self.genericProjectResponse(id: "reel-1", type: .reel, title: "Instagram Reel", mediaIDs: ["media-1"]),
            Data("{}".utf8)
        ])
        let service = RemoteStoryService(apiClient: apiClient, persistence: MockStoryPersistence())

        let story = try await service.createProject(
            request: CreateVideoProjectRequest(
                draft: .reel(
                    configuration: ReelCreationConfiguration(
                        platform: .instagram,
                        duration: .fifteen,
                        style: .dynamic,
                        prompt: "Make this fast and bright."
                    )
                ),
                assets: [Self.asset(id: "asset-1")],
                analyses: [Self.analysis(assetID: "asset-1")],
                mediaIDsByAssetID: ["asset-1": "media-1"]
            )
        )

        let requests = await apiClient.recordedRequests()
        let body: [String: Any] = try XCTUnwrap(requests.first?.jsonBody)
        let configuration = try XCTUnwrap(body["configuration"] as? [String: Any])
        let reel = try XCTUnwrap(configuration["reel"] as? [String: Any])

        XCTAssertEqual(story.projectType, VideoProjectType.reel)
        XCTAssertEqual(requests.map { $0.path }, ["/projects", "/projects/reel-1/analyze"])
        XCTAssertEqual(body["type"] as? String, "reel")
        XCTAssertEqual(configuration["ai_instructions"] as? String, "Make this fast and bright.")
        XCTAssertEqual(reel["social_platform"] as? String, "instagram")
        XCTAssertEqual(reel["target_duration_seconds"] as? Int, 15)
        XCTAssertEqual(reel["style"] as? String, "dynamic")
        XCTAssertEqual(reel["hook_preference"] as? String, "strong opening hook")
        XCTAssertEqual(reel["caption_style"] as? String, "bold_social")
        XCTAssertEqual(reel["aspect_ratio"] as? String, "9:16")
    }

    private static func asset(id: String) -> MediaAsset {
        MediaAsset(
            id: id,
            localIdentifier: id,
            kind: .image,
            creationDate: nil,
            duration: 0,
            pixelWidth: 1920,
            pixelHeight: 1080
        )
    }

    private static func analysis(assetID: String) -> MediaAnalysis {
        MediaAnalysis(assetID: assetID, labels: ["product"], peopleCount: 0, confidence: 0.82)
    }

    private static func presentation(id: String, assetID: String) -> StoryMomentPresentation {
        StoryMomentPresentation(
            id: id,
            assetID: assetID,
            title: assetID,
            subtitle: "Photo",
            description: nil,
            kind: .photo,
            duration: 3
        )
    }

    private static func storyResponse(id: String, type: VideoProjectType, title: String, mediaIDs: [String]) -> Data {
        Data("""
        {
          "id": "\(id)",
          "type": "\(type.rawValue)",
          "title": "\(title)",
          "context": "Trip",
          "createdAt": "2026-08-26T07:00:00Z",
          "duration": 24,
          "assetIDs": \(jsonArray(mediaIDs)),
          "status": "analyzing"
        }
        """.utf8)
    }

    private static func genericProjectResponse(id: String, type: VideoProjectType, title: String, mediaIDs: [String]) -> Data {
        Data("""
        {
          "id": "\(id)",
          "type": "\(type.rawValue)",
          "title": "\(title)",
          "context": "Special moments",
          "created_at": "2026-08-26T07:00:00Z",
          "media_ids": \(jsonArray(mediaIDs)),
          "status": "analyzing"
        }
        """.utf8)
    }

    private static func jsonArray(_ values: [String]) -> String {
        let encodedValues = values.map { "\"\($0)\"" }
        return "[\(encodedValues.joined(separator: ","))]"
    }
}

private actor RecordingAPIClient: APIClient {
    struct RecordedRequest: Sendable {
        let method: APIRequest.Method
        let path: String
        let body: Data?

        var jsonBody: [String: Any]? {
            guard let body else { return nil }
            return try? JSONSerialization.jsonObject(with: body) as? [String: Any]
        }
    }

    private var requests: [RecordedRequest] = []
    private var responses: [Data]

    init(responses: [Data]) {
        self.responses = responses
    }

    func data(for request: APIRequest) async throws -> Data {
        requests.append(RecordedRequest(method: request.method, path: request.path, body: request.body))
        guard !responses.isEmpty else {
            return Data()
        }
        return responses.removeFirst()
    }

    func recordedRequests() -> [RecordedRequest] {
        requests
    }
}
