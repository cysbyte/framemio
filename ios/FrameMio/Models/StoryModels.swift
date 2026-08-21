import Foundation

enum StoryStatus: String, Codable, Hashable, Sendable {
    case queued
    case analyzing
    case rendering
    case ready
    case failed
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
}

struct CreateStoryRequest: Codable, Hashable, Sendable {
    let context: MomentContext
    let assets: [MediaAsset]
    let analyses: [MediaAnalysis]
}
