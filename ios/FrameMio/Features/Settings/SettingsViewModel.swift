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
                let restoredSubscriptions = try await restoreSubscriptionCredits()
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

    private func verifiedTransaction<T>(from result: VerificationResult<T>) throws -> T {
        switch result {
        case let .verified(value):
            return value
        case let .unverified(_, error):
            throw error
        }
    }

    private func restoreSubscriptionCredits() async throws -> Int {
        guard let creditService else {
            settingsDebugLog("restore_entitlements_skipped reason=no_credit_service")
            return 0
        }
        var restoredTransactionIDs = Set<String>()
        var restoredCount = 0
        settingsDebugLog("restore_current_entitlements_start")
        for await result in Transaction.currentEntitlements {
            if try await restoreSubscriptionCredit(from: result, creditService: creditService, restoredTransactionIDs: &restoredTransactionIDs) {
                restoredCount += 1
            }
        }
        settingsDebugLog("restore_current_entitlements_complete restored=\(restoredCount)")
        if restoredCount == 0 {
            settingsDebugLog("restore_latest_transactions_start")
            for productID in FrameMioProCatalog.productIDs {
                guard let result = await Transaction.latest(for: productID) else {
                    settingsDebugLog("restore_latest_transaction_missing product=\(productID)")
                    continue
                }
                if try await restoreSubscriptionCredit(from: result, creditService: creditService, restoredTransactionIDs: &restoredTransactionIDs) {
                    restoredCount += 1
                }
            }
            settingsDebugLog("restore_latest_transactions_complete restored=\(restoredCount)")
        }
        return restoredCount
    }

    private func restoreSubscriptionCredit(
        from result: VerificationResult<Transaction>,
        creditService: CreditService,
        restoredTransactionIDs: inout Set<String>
    ) async throws -> Bool {
        let transaction = try verifiedTransaction(from: result)
        settingsDebugLog("restore_transaction_seen product=\(transaction.productID) id=\(transaction.id) revoked=\(transaction.revocationDate != nil) expires=\(transaction.expirationDate?.description ?? "nil")")
        guard let plan = FrameMioProPlanKind(productID: transaction.productID) else {
            settingsDebugLog("restore_transaction_ignored reason=unknown_product product=\(transaction.productID)")
            return false
        }
        guard transaction.revocationDate == nil else {
            settingsDebugLog("restore_transaction_ignored reason=revoked product=\(transaction.productID) id=\(transaction.id)")
            return false
        }
        let purchaseID = String(transaction.id)
        guard restoredTransactionIDs.insert(purchaseID).inserted else {
            settingsDebugLog("restore_transaction_ignored reason=duplicate product=\(transaction.productID) id=\(purchaseID)")
            return false
        }
        settingsDebugLog("restore_grant_start plan=\(plan.rawValue) purchase_id=\(purchaseID)")
        try await creditService.grantSubscriptionCycle(
            plan: plan.rawValue,
            purchaseID: purchaseID,
            expiresAt: transaction.expirationDate
        )
        settingsDebugLog("restore_grant_complete plan=\(plan.rawValue) purchase_id=\(purchaseID)")
        return true
    }

    deinit {
        task?.cancel()
        restoreTask?.cancel()
    }
}
