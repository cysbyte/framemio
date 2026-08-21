import Combine
import Foundation

@MainActor
final class SettingsViewModel: ObservableObject {
    @Published private(set) var authorization: PhotoLibraryAuthorization = .notDetermined
    @Published private(set) var isLoading = false
    @Published var errorMessage: String?

    private let photoLibrary: PhotoLibraryService
    private var task: Task<Void, Never>?

    init(photoLibrary: PhotoLibraryService) {
        self.photoLibrary = photoLibrary
    }

    func refresh() {
        task?.cancel()
        task = Task { [weak self] in
            guard let self else { return }
            isLoading = true
            defer { isLoading = false }
            authorization = await photoLibrary.authorizationStatus()
        }
    }

    func requestPhotoAccess() {
        task?.cancel()
        task = Task { [weak self] in
            guard let self else { return }
            isLoading = true
            defer { isLoading = false }
            authorization = await photoLibrary.requestAuthorization()
            if authorization == .denied || authorization == .restricted {
                errorMessage = AppError.notAuthorized.localizedDescription
            }
        }
    }

    deinit {
        task?.cancel()
    }
}
