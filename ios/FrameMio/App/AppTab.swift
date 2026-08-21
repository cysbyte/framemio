import Foundation

enum AppTab: Hashable {
    case home
    case moments
    case story
    case profile

    var title: String {
        switch self {
        case .home:
            "Home"
        case .moments:
            "Moments"
        case .story:
            "Stories"
        case .profile:
            "You"
        }
    }

    var icon: String {
        switch self {
        case .home:
            "house"
        case .moments:
            "photo.on.rectangle.angled"
        case .story:
            "rectangle.stack.badge.play"
        case .profile:
            "person.crop.circle"
        }
    }
}
