import Foundation

actor MockMediaSelectionService: MediaSelectionService {
    private let assets: [MediaAsset]
    private let albums: [MediaAlbum]
    private let albumAssetIDs: [String: Set<String>]

    init(
        assets: [MediaAsset] = [],
        albums: [MediaAlbum] = [],
        albumAssetIDs: [String: Set<String>] = [:]
    ) {
        self.assets = assets
        self.albums = albums
        self.albumAssetIDs = albumAssetIDs
    }

    func fetchAssets(
        in scope: MediaSelectionScope,
        filter: PhotoLibraryMediaFilter
    ) async throws -> [MediaAsset] {
        try Task.checkCancellation()
        let scopedAssets: [MediaAsset]
        switch scope.kind {
        case .all, .context:
            scopedAssets = assets
        case .timePeriod:
            scopedAssets = assets.filter { asset in
                guard let creationDate = asset.creationDate else { return false }
                if let startDate = scope.startDate, creationDate < startDate { return false }
                if let endDate = scope.endDate, creationDate > endDate { return false }
                return true
            }
        case .album:
            let identifiers = scope.albumLocalIdentifier.flatMap { albumAssetIDs[$0] } ?? []
            scopedAssets = assets.filter { identifiers.contains($0.id) }
        }

        switch filter {
        case .all:
            return scopedAssets
        case .photos:
            return scopedAssets.filter { !$0.isVideo }
        case .videos:
            return scopedAssets.filter(\.isVideo)
        }
    }

    func fetchAlbums() async throws -> [MediaAlbum] {
        try Task.checkCancellation()
        return albums
    }
}

actor MockOriginalMediaSource: OriginalMediaSource {
    struct Fixture: Sendable {
        let data: Data
        let fileName: String
        let contentType: String

        init(
            data: Data,
            fileName: String = "media.bin",
            contentType: String = "application/octet-stream"
        ) {
            self.data = data
            self.fileName = fileName
            self.contentType = contentType
        }
    }

    private let fixtures: [String: Fixture]
    private let directory: URL
    private(set) var materializationCount = 0
    private(set) var removedURLs: [URL] = []

    init(fixtures: [String: Fixture] = [:], directory: URL? = nil) {
        self.fixtures = fixtures
        self.directory = directory ?? FileManager.default.temporaryDirectory
            .appendingPathComponent("FrameMio-MockUploads-\(UUID().uuidString)", isDirectory: true)
    }

    func materializeOriginalMedia(
        for asset: MediaAsset,
        allowsNetworkAccess: Bool,
        progress: (@Sendable (Double) -> Void)?
    ) async throws -> OriginalMediaFile {
        try Task.checkCancellation()
        let fixture = fixtures[asset.id] ?? Fixture(
            data: Data("FrameMio \(asset.id)".utf8),
            fileName: asset.isVideo ? "\(asset.id).mov" : "\(asset.id).jpg",
            contentType: asset.isVideo ? "video/quicktime" : "image/jpeg"
        )
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let url = directory.appendingPathComponent("\(UUID().uuidString)-\(fixture.fileName)")
        try fixture.data.write(to: url, options: .atomic)
        materializationCount += 1
        progress?(1)
        _ = allowsNetworkAccess
        return OriginalMediaFile(
            url: url,
            fileName: fixture.fileName,
            contentType: fixture.contentType,
            fileSizeBytes: Int64(fixture.data.count)
        )
    }

    func removeMaterializedMedia(at url: URL) async {
        removedURLs.append(url)
        try? FileManager.default.removeItem(at: url)
    }
}

struct MockDirectUploadRequest: Hashable, Sendable {
    let presignedURL: URL
    let fileSizeBytes: Int64
    let usesBackgroundSession: Bool
}

actor MockDirectObjectStorageClient: DirectObjectStorageClient {
    private var failuresRemainingByPartNumber: [Int: Int]
    private var cancellationRequestNumbers: Set<Int>
    private let uploadDelay: Duration?
    private var activeUploads = 0
    private(set) var maximumActiveUploads = 0
    private(set) var requests: [MockDirectUploadRequest] = []

    init(
        failuresRemainingByPartNumber: [Int: Int] = [:],
        cancellationRequestNumbers: Set<Int> = [],
        uploadDelay: Duration? = nil
    ) {
        self.failuresRemainingByPartNumber = failuresRemainingByPartNumber
        self.cancellationRequestNumbers = cancellationRequestNumbers
        self.uploadDelay = uploadDelay
    }

    func upload(
        fileURL: URL,
        to presignedURL: URL,
        headers: [String: String],
        usesBackgroundSession: Bool,
        progress: @escaping @Sendable (Int64, Int64) -> Void
    ) async throws -> DirectObjectUploadResult {
        try Task.checkCancellation()
        let fileSize = Int64(
            try fileURL.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0
        )
        let requestNumber = requests.count + 1
        requests.append(
            MockDirectUploadRequest(
                presignedURL: presignedURL,
                fileSizeBytes: fileSize,
                usesBackgroundSession: usesBackgroundSession
            )
        )
        activeUploads += 1
        maximumActiveUploads = max(maximumActiveUploads, activeUploads)
        defer { activeUploads -= 1 }

        if let uploadDelay {
            try await Task.sleep(for: uploadDelay)
        }
        if cancellationRequestNumbers.remove(requestNumber) != nil {
            throw CancellationError()
        }

        let partNumber = Int(presignedURL.lastPathComponent) ?? 1
        if let failures = failuresRemainingByPartNumber[partNumber], failures > 0 {
            failuresRemainingByPartNumber[partNumber] = failures - 1
            throw URLError(.networkConnectionLost)
        }

        progress(fileSize, fileSize)
        _ = headers
        return DirectObjectUploadResult(statusCode: 200, eTag: "etag-\(partNumber)")
    }
}
