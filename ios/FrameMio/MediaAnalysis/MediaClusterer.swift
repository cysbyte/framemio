import Foundation

struct MediaClusterer: MediaClustering {
    struct Thresholds: Hashable, Sendable {
        let maximumHashDistance: Int
        let maximumTimeInterval: TimeInterval
        let maximumLocationDistanceMeters: Double

        static let nearDuplicates = Thresholds(
            maximumHashDistance: 6,
            maximumTimeInterval: 120,
            maximumLocationDistanceMeters: 250
        )
    }

    private let thresholds: Thresholds

    init(thresholds: Thresholds = .nearDuplicates) {
        self.thresholds = thresholds
    }

    func clusters(from intelligence: [MediaAssetIntelligence]) async throws -> [MediaCluster] {
        try Task.checkCancellation()
        let photos = intelligence.compactMap { item -> ClusterCandidate? in
            guard let metadata = item.photoMetadata else {
                return nil
            }
            return ClusterCandidate(metadata: metadata)
        }

        var disjointSet = DisjointSet(count: photos.count)
        let sortedIndices = photos.indices.sorted {
            (photos[$0].creationDate ?? .distantPast) < (photos[$1].creationDate ?? .distantPast)
        }

        for sortedPosition in sortedIndices.indices {
            try Task.checkCancellation()
            let lhsIndex = sortedIndices[sortedPosition]
            guard let lhsHash = photos[lhsIndex].perceptualHash else {
                continue
            }

            for nextPosition in sortedIndices.index(after: sortedPosition)..<sortedIndices.endIndex {
                let rhsIndex = sortedIndices[nextPosition]
                if exceedsTimeWindow(photos[lhsIndex], photos[rhsIndex]) {
                    break
                }
                guard let rhsHash = photos[rhsIndex].perceptualHash,
                      lhsHash.hammingDistance(to: rhsHash) <= thresholds.maximumHashDistance,
                      locationsAreCompatible(photos[lhsIndex].location, photos[rhsIndex].location) else {
                    continue
                }
                disjointSet.union(lhsIndex, rhsIndex)
            }
        }

        var groupedIndices: [Int: [Int]] = [:]
        for index in photos.indices {
            groupedIndices[disjointSet.root(of: index), default: []].append(index)
        }

        return groupedIndices.values
            .map { makeCluster(indices: $0, candidates: photos) }
            .sorted {
                ($0.startDate ?? .distantPast) > ($1.startDate ?? .distantPast)
            }
    }

    private func exceedsTimeWindow(_ lhs: ClusterCandidate, _ rhs: ClusterCandidate) -> Bool {
        guard let lhsDate = lhs.creationDate, let rhsDate = rhs.creationDate else {
            return false
        }
        return rhsDate.timeIntervalSince(lhsDate) > thresholds.maximumTimeInterval
    }

    private func locationsAreCompatible(_ lhs: GeoCoordinate?, _ rhs: GeoCoordinate?) -> Bool {
        guard let lhs, let rhs else {
            return true
        }
        return Self.distanceInMeters(from: lhs, to: rhs) <= thresholds.maximumLocationDistanceMeters
    }

    private func makeCluster(indices: [Int], candidates: [ClusterCandidate]) -> MediaCluster {
        let members = indices.map { candidates[$0] }
        let representative = members.max {
            ($0.qualityScore ?? 0) < ($1.qualityScore ?? 0)
        } ?? members[0]
        let dates = members.compactMap(\.creationDate).sorted()
        let distances = members.compactMap { member -> Double? in
            guard let lhs = representative.perceptualHash,
                  let rhs = member.perceptualHash else {
                return nil
            }
            return Double(lhs.hammingDistance(to: rhs))
        }
        let confidence: Double
        if members.count < 2 || distances.isEmpty {
            confidence = 0
        } else {
            confidence = max(0, 1 - distances.average / 64)
        }

        return MediaCluster(
            id: "near-duplicate-\(representative.assetID)",
            assetIDs: members.map(\.assetID),
            representativeAssetID: representative.assetID,
            startDate: dates.first,
            endDate: dates.last,
            location: representative.location,
            similarityConfidence: confidence
        )
    }

    private static func distanceInMeters(from lhs: GeoCoordinate, to rhs: GeoCoordinate) -> Double {
        let earthRadius = 6_371_000.0
        let latitudeDelta = (rhs.latitude - lhs.latitude) * .pi / 180
        let longitudeDelta = (rhs.longitude - lhs.longitude) * .pi / 180
        let lhsLatitude = lhs.latitude * .pi / 180
        let rhsLatitude = rhs.latitude * .pi / 180
        let value = sin(latitudeDelta / 2) * sin(latitudeDelta / 2)
            + cos(lhsLatitude) * cos(rhsLatitude)
            * sin(longitudeDelta / 2) * sin(longitudeDelta / 2)
        return earthRadius * 2 * atan2(sqrt(value), sqrt(1 - value))
    }
}

private struct ClusterCandidate {
    let assetID: String
    let creationDate: Date?
    let location: GeoCoordinate?
    let perceptualHash: PerceptualHash?
    let qualityScore: Double?

    init(metadata: PhotoMetadata) {
        self.assetID = metadata.assetID
        self.creationDate = metadata.creationDate
        self.location = metadata.location
        self.perceptualHash = metadata.perceptualHash
        self.qualityScore = metadata.quality?.overallScore
    }
}

private struct DisjointSet {
    private var parents: [Int]
    private var ranks: [Int]

    init(count: Int) {
        self.parents = Array(0..<count)
        self.ranks = Array(repeating: 0, count: count)
    }

    mutating func root(of index: Int) -> Int {
        if parents[index] != index {
            parents[index] = root(of: parents[index])
        }
        return parents[index]
    }

    mutating func union(_ lhs: Int, _ rhs: Int) {
        let lhsRoot = root(of: lhs)
        let rhsRoot = root(of: rhs)
        guard lhsRoot != rhsRoot else {
            return
        }

        if ranks[lhsRoot] < ranks[rhsRoot] {
            parents[lhsRoot] = rhsRoot
        } else if ranks[lhsRoot] > ranks[rhsRoot] {
            parents[rhsRoot] = lhsRoot
        } else {
            parents[rhsRoot] = lhsRoot
            ranks[lhsRoot] += 1
        }
    }
}

private extension Collection where Element == Double {
    var average: Double {
        isEmpty ? 0 : reduce(0, +) / Double(count)
    }
}
