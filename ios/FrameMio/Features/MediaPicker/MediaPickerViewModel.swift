import Combine
import Foundation
import SwiftUI
import UIKit

struct MediaAssetSection: Identifiable {
    let id: String
    let title: String
    let assets: [MediaAsset]
}

@MainActor
final class MediaPickerViewModel: ObservableObject {
    @Published private(set) var assets: [MediaAsset] = []
    @Published private(set) var thumbnails: [String: UIImage] = [:]
    @Published private(set) var isLoading = false
    @Published private(set) var hasCompletedInitialLoad = false
    @Published private(set) var selectedIDs: Set<String> = []
    @Published private(set) var authorization: PhotoLibraryAuthorization = .notDetermined
    @Published private(set) var filter: PhotoLibraryMediaFilter = .all
    @Published var errorMessage: String?

    let context: MomentContext
    private let photoLibrary: PhotoLibraryService
    private let selectionService: MediaSelectionService
    private var allAssets: [MediaAsset] = []
    private var loadTask: Task<Void, Never>?
    private var loadSequence = 0
    private var thumbnailTasks: [String: Task<Void, Never>] = [:]
    private var cachedAssets: [MediaAsset] = []

    init(
        context: MomentContext,
        photoLibrary: PhotoLibraryService,
        selectionService: MediaSelectionService
    ) {
        self.context = context
        self.photoLibrary = photoLibrary
        self.selectionService = selectionService
    }

    var selectedAssets: [MediaAsset] {
        allAssets.filter { selectedIDs.contains($0.id) }
    }

    var selectedCount: Int {
        selectedIDs.count
    }

    var totalAssetCount: Int {
        allAssets.count
    }

    var canContinue: Bool {
        !selectedIDs.isEmpty
    }

    var sections: [MediaAssetSection] {
        makeSections(from: assets)
    }

    var isAccessBlocked: Bool {
        authorization == .denied || authorization == .restricted
    }

    var accessTitle: String {
        authorization == .restricted ? "Photo access is restricted" : "Photo access is off"
    }

    var accessMessage: String {
        authorization == .restricted
            ? "This iPhone does not currently allow FrameMio to read your library."
            : "Allow FrameMio to see your photos and videos so it can find the moments worth keeping."
    }

    func load() {
        load(scope: .context(context))
    }

    func load(scope: MediaSelectionScope) {
        loadTask?.cancel()
        cancelThumbnailTasks()
        loadSequence += 1
        let currentLoadSequence = loadSequence

        loadTask = Task { [weak self] in
            guard let self else { return }
            isLoading = true
            defer {
                if currentLoadSequence == loadSequence {
                    isLoading = false
                    hasCompletedInitialLoad = true
                }
            }

            do {
                var status = await photoLibrary.authorizationStatus()
                if status == .notDetermined {
                    status = await photoLibrary.requestAuthorization()
                }
                guard currentLoadSequence == loadSequence else { return }
                authorization = status

                guard status == .authorized || status == .limited else {
                    allAssets = []
                    assets = []
                    return
                }

                let fetchedAssets = try await selectionService.fetchAssets(in: scope, filter: .all)
                try Task.checkCancellation()
                guard currentLoadSequence == loadSequence else { return }
                allAssets = fetchedAssets
                let availableIDs = Set(fetchedAssets.map(\.id))
                selectedIDs = selectedIDs.intersection(availableIDs)
                updateDisplayedAssets()
                refreshCache()
                errorMessage = nil
            } catch is CancellationError {
            } catch {
                guard currentLoadSequence == loadSequence else { return }
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    func load(datePeriod startDate: Date, endDate: Date) {
        load(scope: .timePeriod(startDate: startDate, endDate: endDate))
    }

    func load(albumLocalIdentifier: String) {
        load(scope: .album(localIdentifier: albumLocalIdentifier))
    }

    func albums() async throws -> [MediaAlbum] {
        try await selectionService.fetchAlbums()
    }

    func setFilter(_ filter: PhotoLibraryMediaFilter) {
        guard self.filter != filter else { return }
        cancelThumbnailTasks()
        self.filter = filter
        updateDisplayedAssets()
        refreshCache()
        FrameMioHaptics.secondaryAction()
    }

    func toggleSelection(for asset: MediaAsset) {
        if selectedIDs.contains(asset.id) {
            selectedIDs.remove(asset.id)
        } else {
            selectedIDs.insert(asset.id)
        }
        FrameMioHaptics.selection()
    }

    func image(for asset: MediaAsset) -> Image? {
        guard let image = thumbnails[asset.id] else { return nil }
        return Image(uiImage: image)
    }

    func loadThumbnail(for asset: MediaAsset, targetSize: CGSize) {
        guard thumbnails[asset.id] == nil, thumbnailTasks[asset.id] == nil else { return }

        let assetID = asset.id
        thumbnailTasks[assetID] = Task { [weak self, photoLibrary] in
            do {
                let data = try await photoLibrary.requestThumbnail(for: asset, targetSize: targetSize)
                try Task.checkCancellation()
                if let image = UIImage(data: data) {
                    self?.thumbnails[assetID] = image
                }
            } catch is CancellationError {
            } catch {
            }
            self?.thumbnailTasks[assetID] = nil
        }
    }

    func cancelThumbnail(for asset: MediaAsset) {
        thumbnailTasks[asset.id]?.cancel()
        thumbnailTasks[asset.id] = nil
    }

    private func updateDisplayedAssets() {
        switch filter {
        case .all:
            assets = allAssets
        case .photos:
            assets = allAssets.filter { !$0.isVideo }
        case .videos:
            assets = allAssets.filter(\.isVideo)
        }
    }

    private func refreshCache() {
        if !cachedAssets.isEmpty {
            photoLibrary.stopCachingThumbnails(
                for: cachedAssets,
                targetSize: FrameMioMetrics.mediaThumbnailRequestSize
            )
        }

        cachedAssets = Array(assets.prefix(FrameMioMetrics.mediaCacheWindow))
        photoLibrary.startCachingThumbnails(
            for: cachedAssets,
            targetSize: FrameMioMetrics.mediaThumbnailRequestSize
        )
    }

    private func cancelThumbnailTasks() {
        thumbnailTasks.values.forEach { $0.cancel() }
        thumbnailTasks.removeAll()
    }

    private func makeSections(from assets: [MediaAsset]) -> [MediaAssetSection] {
        var groupedAssets: [String: [MediaAsset]] = [:]
        var orderedKeys: [String] = []

        for asset in assets {
            let key = sectionKey(for: asset.creationDate)
            if groupedAssets[key] == nil {
                orderedKeys.append(key)
            }
            groupedAssets[key, default: []].append(asset)
        }

        return orderedKeys.map { key in
            MediaAssetSection(
                id: key,
                title: sectionTitle(for: key),
                assets: groupedAssets[key] ?? []
            )
        }
    }

    private func sectionKey(for date: Date?) -> String {
        guard let date else { return "undated" }
        return String(Calendar.current.startOfDay(for: date).timeIntervalSince1970)
    }

    private func sectionTitle(for key: String) -> String {
        guard let timestamp = TimeInterval(key), key != "undated" else { return "Undated" }
        let date = Date(timeIntervalSince1970: timestamp)
        let calendar = Calendar.current

        if calendar.isDateInToday(date) { return "Today" }
        if calendar.isDateInYesterday(date) { return "Yesterday" }
        return date.formatted(.dateTime.weekday(.wide).month(.abbreviated).day())
    }

    deinit {
        loadTask?.cancel()
        thumbnailTasks.values.forEach { $0.cancel() }
    }
}
