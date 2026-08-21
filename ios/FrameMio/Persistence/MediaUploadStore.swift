import Foundation

actor FileMediaUploadStore: MediaUploadStore {
    private struct Archive: Codable {
        let schemaVersion: Int
        let uploads: [String: MediaUpload]
    }

    private let fileURL: URL
    private let fileManager: FileManager
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder
    private var archive: Archive?

    init(fileURL: URL? = nil) {
        let fileManager = FileManager.default
        self.fileManager = fileManager
        let baseURL = fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? fileManager.temporaryDirectory
        self.fileURL = fileURL ?? baseURL
            .appendingPathComponent("FrameMio", isDirectory: true)
            .appendingPathComponent("media-uploads-v1.json", isDirectory: false)

        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        self.encoder = encoder
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        self.decoder = decoder
    }

    func upload(for id: String) async throws -> MediaUpload? {
        try loadArchive().uploads[id]
    }

    func save(_ upload: MediaUpload) async throws {
        var uploads = try loadArchive().uploads
        uploads[upload.id] = upload
        try persist(Archive(schemaVersion: 1, uploads: uploads))
    }

    func remove(uploadID: String) async throws {
        var uploads = try loadArchive().uploads
        uploads.removeValue(forKey: uploadID)
        try persist(Archive(schemaVersion: 1, uploads: uploads))
    }

    func allUploads() async throws -> [MediaUpload] {
        try loadArchive().uploads.values.sorted { $0.updatedAt > $1.updatedAt }
    }

    private func loadArchive() throws -> Archive {
        if let archive {
            return archive
        }
        guard fileManager.fileExists(atPath: fileURL.path) else {
            let empty = Archive(schemaVersion: 1, uploads: [:])
            archive = empty
            return empty
        }
        do {
            let data = try Data(contentsOf: fileURL, options: .mappedIfSafe)
            let loaded = try decoder.decode(Archive.self, from: data)
            archive = loaded
            return loaded
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
            try encoder.encode(archive).write(to: fileURL, options: .atomic)
            self.archive = archive
        } catch {
            throw AppError.persistenceFailure
        }
    }
}

actor MockMediaUploadStore: MediaUploadStore {
    private var uploads: [String: MediaUpload]

    init(uploads: [MediaUpload] = []) {
        self.uploads = Dictionary(uniqueKeysWithValues: uploads.map { ($0.id, $0) })
    }

    func upload(for id: String) async throws -> MediaUpload? {
        uploads[id]
    }

    func save(_ upload: MediaUpload) async throws {
        uploads[upload.id] = upload
    }

    func remove(uploadID: String) async throws {
        uploads.removeValue(forKey: uploadID)
    }

    func allUploads() async throws -> [MediaUpload] {
        uploads.values.sorted { $0.updatedAt > $1.updatedAt }
    }
}
