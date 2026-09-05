import Combine
import Foundation
import StoreKit

private func settingsDebugLog(_ message: @autoclosure () -> String) {
    print("[FrameMio][Settings] \(message())")
}

@MainActor
final class SettingsViewModel: ObservableObject {
    @Published private(set) var authorization: PhotoLibraryAuthorization = .notDetermined
    @Published private(set) var isLoading = false
    @Published private(set) var isRestoringPurchases = false
    @Published private(set) var statusMessage: String?
    @Published var errorMessage: String?

    private let photoLibrary: PhotoLibraryService
    private let creditService: CreditService?
    private var task: Task<Void, Never>?
    private var restoreTask: Task<Void, Never>?

    init(photoLibrary: PhotoLibraryService, creditService: CreditService? = nil) {
        self.photoLibrary = photoLibrary
        self.creditService = creditService
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
                settingsDebugLog("restore_start has_credit_service=\(creditService != nil)")
                var syncError: Error?
                do {
                    try await AppStore.sync()
                    settingsDebugLog("restore_appstore_sync_complete")
                } catch is CancellationError {
                    throw CancellationError()
                } catch {
                    syncError = error
                    settingsDebugLog("restore_appstore_sync_failed_continuing error=\(error.localizedDescription)")
                }
                let restoredSubscriptions = 0
                onRestored()
                if restoredSubscriptions > 0 {
                    statusMessage = "Purchases restored."
                } else if let syncError {
                    statusMessage = nil
                    errorMessage = AppError.from(syncError).localizedDescription
                } else {
                    statusMessage = "No active Pro subscription found."
                }
                settingsDebugLog("restore_complete restored_subscriptions=\(restoredSubscriptions)")
            } catch is CancellationError {
                settingsDebugLog("restore_cancelled")
            } catch {
                statusMessage = nil
                errorMessage = AppError.from(error).localizedDescription
                settingsDebugLog("restore_failed error=\(error.localizedDescription) mapped=\(AppError.from(error).localizedDescription)")
            }
        }
    }

    deinit {
        task?.cancel()
        restoreTask?.cancel()
    }
}
