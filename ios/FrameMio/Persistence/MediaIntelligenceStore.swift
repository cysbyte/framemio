import Foundation

actor FileMediaIntelligenceStore: MediaIntelligenceStore {
    private struct Archive: Codable {
        let schemaVersion: Int
        let intelligenceByAssetID: [String: MediaAssetIntelligence]
    }

    private let fileURL: URL
    private let fileManager: FileManager
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder
    private var cachedArchive: Archive?

    init(fileURL: URL? = nil) {
        let fileManager = FileManager.default
        self.fileManager = fileManager
        let baseURL = fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? fileManager.temporaryDirectory
        self.fileURL = fileURL ?? baseURL
            .appendingPathComponent("FrameMio", isDirectory: true)
            .appendingPathComponent("media-intelligence-v1.json", isDirectory: false)

        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        self.encoder = encoder

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        self.decoder = decoder
    }

    func intelligence(for assetID: String) async throws -> MediaAssetIntelligence? {
        try loadArchiveIfNeeded().intelligenceByAssetID[assetID]
    }

    func save(_ intelligence: MediaAssetIntelligence) async throws {
        var values = try loadArchiveIfNeeded().intelligenceByAssetID
        values[intelligence.assetID] = intelligence
        try persist(Archive(schemaVersion: 1, intelligenceByAssetID: values))
    }

    func removeIntelligence(for assetID: String) async throws {
        var values = try loadArchiveIfNeeded().intelligenceByAssetID
        values.removeValue(forKey: assetID)
        try persist(Archive(schemaVersion: 1, intelligenceByAssetID: values))
    }

    func removeAll() async throws {
        cachedArchive = Archive(schemaVersion: 1, intelligenceByAssetID: [:])
        guard fileManager.fileExists(atPath: fileURL.path) else {
            return
        }
        do {
            try fileManager.removeItem(at: fileURL)
        } catch {
            throw AppError.persistenceFailure
        }
    }

    private func loadArchiveIfNeeded() throws -> Archive {
        if let cachedArchive {
            return cachedArchive
        }

        guard fileManager.fileExists(atPath: fileURL.path) else {
            let archive = Archive(schemaVersion: 1, intelligenceByAssetID: [:])
            cachedArchive = archive
            return archive
        }

        do {
            let data = try Data(contentsOf: fileURL, options: .mappedIfSafe)
            let archive = try decoder.decode(Archive.self, from: data)
            cachedArchive = archive
            return archive
        } catch {
            throw AppError.persistenceFailure
        }
    }

    private func persist(_ archive: Archive) throws {
        do {
            try fileManager.createDirectory(
                at: fileURL.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )
            let data = try encoder.encode(archive)
            try data.write(to: fileURL, options: .atomic)
            cachedArchive = archive
        } catch {
            throw AppError.persistenceFailure
        }
    }
}

actor MockMediaIntelligenceStore: MediaIntelligenceStore {
    private var values: [String: MediaAssetIntelligence]

    init(values: [MediaAssetIntelligence] = []) {
        self.values = Dictionary(uniqueKeysWithValues: values.map { ($0.assetID, $0) })
    }

    func intelligence(for assetID: String) async throws -> MediaAssetIntelligence? {
        values[assetID]
    }

    func save(_ intelligence: MediaAssetIntelligence) async throws {
        values[intelligence.assetID] = intelligence
    }

    func removeIntelligence(for assetID: String) async throws {
        values.removeValue(forKey: assetID)
    }

    func removeAll() async throws {
        values.removeAll()
    }
}
