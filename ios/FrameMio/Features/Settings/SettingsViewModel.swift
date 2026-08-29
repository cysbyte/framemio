import Combine
import Foundation
import StoreKit

@MainActor
final class SettingsViewModel: ObservableObject {
    @Published private(set) var authorization: PhotoLibraryAuthorization = .notDetermined
    @Published private(set) var isLoading = false
    @Published private(set) var isRestoringPurchases = false
    @Published private(set) var statusMessage: String?
    @Published var errorMessage: String?

    private let photoLibrary: PhotoLibraryService
    private var task: Task<Void, Never>?
    private var restoreTask: Task<Void, Never>?

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

    func restorePurchases(onRestored: @escaping @MainActor () -> Void) {
        guard !isRestoringPurchases else { return }
        restoreTask?.cancel()
        restoreTask = Task { [weak self] in
            guard let self else { return }
            isRestoringPurchases = true
            errorMessage = nil
            statusMessage = "Restoring purchases..."
            defer { isRestoringPurchases = false }
            do {
                try await AppStore.sync()
                onRestored()
                statusMessage = "Purchases restored."
            } catch is CancellationError {
            } catch {
                statusMessage = nil
                errorMessage = AppError.from(error).localizedDescription
            }
        }
    }

    deinit {
        task?.cancel()
        restoreTask?.cancel()
    }
}
