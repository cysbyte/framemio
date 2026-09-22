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
                let restoreSummary = try await restoreActiveSubscriptionCredits()
                let restoredSubscriptions = restoreSummary.restoredSubscriptions
                onRestored()
                if restoredSubscriptions > 0 {
                    statusMessage = "Purchases restored."
                } else if let syncError {
                    statusMessage = nil
                    errorMessage = AppError.from(syncError).localizedDescription
                } else {
                    statusMessage = "No active Pro subscription found."
                }
                settingsDebugLog("restore_complete restored_subscriptions=\(restoredSubscriptions) synced_subscription_credits=\(restoreSummary.syncedSubscriptionCredits)")
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

private extension SettingsViewModel {
    struct RestoreSummary {
        let restoredSubscriptions: Int
        let syncedSubscriptionCredits: Bool
    }

    func restoreActiveSubscriptionCredits() async throws -> RestoreSummary {
        var restoredSubscriptions = 0
        var syncedSubscriptionCredits = false

        for await result in StoreKit.Transaction.currentEntitlements {
            guard let transaction = try? verifiedTransaction(from: result) else {
                settingsDebugLog("restore_current_entitlement_unverified")
                continue
            }
            guard FrameMioProPlanKind(productID: transaction.productID) != nil,
                  transaction.revocationDate == nil,
                  Self.subscriptionTransactionIsActive(transaction) else {
                settingsDebugLog("restore_current_entitlement_ignored product=\(transaction.productID) revoked=\(transaction.revocationDate != nil)")
                continue
            }
            restoredSubscriptions += 1
            syncedSubscriptionCredits = try await syncSubscriptionCycleCredits(for: transaction) || syncedSubscriptionCredits
        }

        return RestoreSummary(
            restoredSubscriptions: restoredSubscriptions,
            syncedSubscriptionCredits: syncedSubscriptionCredits
        )
    }

    func syncSubscriptionCycleCredits(for transaction: StoreKit.Transaction) async throws -> Bool {
        guard let creditService else {
            settingsDebugLog("restore_subscription_credit_sync_skipped reason=no_credit_service product=\(transaction.productID) transaction=\(transaction.id)")
            return false
        }
        guard let plan = FrameMioProPlanKind(productID: transaction.productID) else {
            settingsDebugLog("restore_subscription_credit_sync_ignored reason=non_pro_product product=\(transaction.productID) transaction=\(transaction.id)")
            return false
        }

        let purchaseID = String(transaction.id)
        settingsDebugLog("restore_subscription_credit_sync_start plan=\(plan.rawValue) product=\(transaction.productID) transaction=\(purchaseID) expires=\(transaction.expirationDate?.description ?? "nil")")
        let creditBalance = try await creditService.grantSubscriptionCycleCredits(
            plan: transaction.productID,
            purchaseID: purchaseID,
            expiresAt: transaction.expirationDate
        )
        settingsDebugLog("restore_subscription_credit_sync_success plan=\(plan.rawValue) product=\(transaction.productID) transaction=\(purchaseID) balance=\(creditBalance.available) reserved=\(creditBalance.reserved) subscription=\(creditBalance.subscriptionCredits) subscription_expires=\(creditBalance.subscriptionCycleExpiresAt?.description ?? "nil")")
        return true
    }

    func verifiedTransaction<T>(from result: VerificationResult<T>) throws -> T {
        switch result {
        case let .verified(value):
            return value
        case let .unverified(_, error):
            throw error
        }
    }

    static func subscriptionTransactionIsActive(_ transaction: StoreKit.Transaction, now: Date = Date()) -> Bool {
        guard let expirationDate = transaction.expirationDate else { return true }
        return expirationDate > now
    }
}
