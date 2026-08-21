import Foundation

enum MomentContext: String, CaseIterable, Codable, Hashable, Identifiable, Sendable {
    case today = "Today"
    case weekend = "Weekend"
    case trip = "Trip"
    case birthday = "Birthday"
    case friends = "Friends"
    case family = "Family"
    case pets = "Pets"
    case specialMoments = "Special moments"

    var id: String { rawValue }

    var pickerTitle: String {
        self == .specialMoments ? "Select Photos" : rawValue
    }

    var icon: String {
        switch self {
        case .today:
            "sun.max"
        case .weekend:
            "calendar"
        case .trip:
            "airplane"
        case .birthday:
            "birthday.cake"
        case .friends:
            "person.2"
        case .family:
            "house"
        case .pets:
            "pawprint"
        case .specialMoments:
            "heart"
        }
    }
}
