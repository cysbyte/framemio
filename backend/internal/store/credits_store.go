package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	creditAccountKind     = "credit_account"
	creditGrantKind       = "credit_grant"
	creditReservationKind = "credit_reservation"
	creditLedgerKind      = "credit_ledger"
)

type creditGrantRecord struct {
	Grant models.CreditGrant `json:"grant"`
}

type creditReservationRecord struct {
	Reservation models.CreditReservation `json:"reservation"`
}

type creditLedgerRecord struct {
	Entry models.CreditLedgerEntry `json:"entry"`
}

func creditGrantKey(grant models.CreditGrant) string {
	return strings.Join([]string{string(grant.Source), grant.ReferenceID}, ":")
}

func creditReservationKey(reservation models.CreditReservation) string {
	return strings.Join([]string{reservation.ReferenceType, reservation.ReferenceID, string(reservation.Action)}, ":")
}

func creditReservationReferenceKey(action models.CreditAction, referenceType, referenceID string) string {
	return strings.Join([]string{string(action), referenceType, referenceID}, ":")
}

func creditLedgerKey(entry models.CreditLedgerEntry) string {
	return strings.Join([]string{entry.ReferenceType, entry.ReferenceID, string(entry.Type), string(entry.Action)}, ":")
}

func normalizeCreditBalance(balance models.CreditBalance) models.CreditBalance {
	if balance.SubscriptionCredits == 0 && balance.PurchasedCredits == 0 && (balance.Available > 0 || balance.Reserved > 0) {
		balance.PurchasedCredits = balance.Available
		balance.PurchasedReserved = balance.Reserved
	}
	balance.Available = balance.SubscriptionCredits + balance.PurchasedCredits
	balance.Reserved = balance.SubscriptionReserved + balance.PurchasedReserved
	balance.TotalCredits = balance.Available + balance.Reserved
	return balance
}

func legacyCreditBalance(balance models.CreditBalance) models.CreditBalance {
	if balance.SubscriptionCredits == 0 && balance.PurchasedCredits == 0 && (balance.Available > 0 || balance.Reserved > 0) {
		balance.PurchasedCredits = balance.Available
		balance.PurchasedReserved = balance.Reserved
	}
	return normalizeCreditBalance(balance)
}

func persistentCreditSource() models.CreditGrantSource {
	return models.CreditGrantSourceCreditPurchase
}

func creditReservationAllocations(balance models.CreditBalance, amount int) (models.CreditBalance, []models.CreditReservationAllocation, error) {
	if amount < 0 {
		return models.CreditBalance{}, nil, fmt.Errorf("reservation amount must be non-negative")
	}
	balance = legacyCreditBalance(balance)
	if balance.Available < amount {
		return models.CreditBalance{}, nil, ErrInsufficientCredits
	}
	remaining := amount
	allocations := make([]models.CreditReservationAllocation, 0, 2)
	subscriptionAllocation := min(balance.SubscriptionCredits, remaining)
	if subscriptionAllocation > 0 {
		balance.SubscriptionCredits -= subscriptionAllocation
		balance.SubscriptionReserved += subscriptionAllocation
		remaining -= subscriptionAllocation
		allocations = append(allocations, models.CreditReservationAllocation{Source: models.CreditGrantSourceSubscriptionCycle, Amount: subscriptionAllocation})
	}
	if remaining > 0 {
		persistentAllocation := min(balance.PurchasedCredits, remaining)
		if persistentAllocation < remaining {
			return models.CreditBalance{}, nil, ErrInsufficientCredits
		}
		balance.PurchasedCredits -= persistentAllocation
		balance.PurchasedReserved += persistentAllocation
		remaining -= persistentAllocation
		allocations = append(allocations, models.CreditReservationAllocation{Source: persistentCreditSource(), Amount: persistentAllocation})
	}
	return normalizeCreditBalance(balance), allocations, nil
}

func reservationAllocationsForBalance(balance models.CreditBalance, reservation models.CreditReservation) []models.CreditReservationAllocation {
	if len(reservation.Allocations) > 0 {
		return reservation.Allocations
	}
	balance = legacyCreditBalance(balance)
	remaining := reservation.Amount
	allocations := make([]models.CreditReservationAllocation, 0, 2)
	purchasedAllocation := min(balance.PurchasedReserved, remaining)
	if purchasedAllocation > 0 {
		allocations = append(allocations, models.CreditReservationAllocation{Source: models.CreditGrantSourceCreditPurchase, Amount: purchasedAllocation})
		remaining -= purchasedAllocation
	}
	if remaining > 0 {
		allocations = append(allocations, models.CreditReservationAllocation{Source: models.CreditGrantSourceSubscriptionCycle, Amount: remaining})
	}
	return allocations
}

func releaseCreditReservationAllocations(balance models.CreditBalance, allocations []models.CreditReservationAllocation) models.CreditBalance {
	for _, allocation := range allocations {
		switch allocation.Source {
		case models.CreditGrantSourceSubscriptionCycle:
			balance.SubscriptionReserved -= allocation.Amount
			balance.SubscriptionCredits += allocation.Amount
		default:
			balance.PurchasedReserved -= allocation.Amount
			balance.PurchasedCredits += allocation.Amount
		}
	}
	return normalizeCreditBalance(balance)
}

func commitCreditReservationAllocations(balance models.CreditBalance, allocations []models.CreditReservationAllocation) models.CreditBalance {
	for _, allocation := range allocations {
		switch allocation.Source {
		case models.CreditGrantSourceSubscriptionCycle:
			balance.SubscriptionReserved -= allocation.Amount
		default:
			balance.PurchasedReserved -= allocation.Amount
		}
	}
	return normalizeCreditBalance(balance)
}

func (s *MemoryStore) ensureCreditMaps() {
	if s.creditAccounts == nil {
		s.creditAccounts = make(map[string]models.CreditBalance)
	}
	if s.creditGrants == nil {
		s.creditGrants = make(map[string]models.CreditGrant)
	}
	if s.creditReservations == nil {
		s.creditReservations = make(map[string]models.CreditReservation)
	}
	if s.creditLedger == nil {
		s.creditLedger = make(map[string][]models.CreditLedgerEntry)
	}
	if s.creditGrantByReference == nil {
		s.creditGrantByReference = make(map[string]string)
	}
	if s.creditReservationByReference == nil {
		s.creditReservationByReference = make(map[string]string)
	}
}

func (s *MemoryStore) GetCreditBalance(_ context.Context, userID string) (models.CreditBalance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.creditAccounts == nil {
		return models.CreditBalance{}, nil
	}
	return normalizeCreditBalance(s.creditAccounts[userID]), nil
}

func (s *MemoryStore) EnsureCreditGrant(_ context.Context, grant models.CreditGrant) (models.CreditGrant, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureCreditMaps()
	if grant.Amount < 0 {
		return models.CreditGrant{}, false, fmt.Errorf("grant amount must be non-negative")
	}
	key := creditGrantKey(grant)
	if grantID, ok := s.creditGrantByReference[key]; ok {
		return s.creditGrants[grantID], true, nil
	}
	if grant.ID == "" {
		grant.ID = models.NewID("credit-grant")
	}
	if grant.Remaining <= 0 {
		grant.Remaining = grant.Amount
	}
	if grant.GrantedAt.IsZero() {
		grant.GrantedAt = time.Now().UTC()
	}
	balance := normalizeCreditBalance(s.creditAccounts[grant.UserID])
	switch grant.Source {
	case models.CreditGrantSourceSubscriptionCycle:
		balance.SubscriptionCredits = grant.Remaining
		balance.SubscriptionReserved = 0
		balance.SubscriptionCycleID = grant.SubscriptionCycleID
		balance.SubscriptionCycleExpiresAt = grant.ExpiresAt
	default:
		balance.PurchasedCredits += grant.Remaining
	}
	s.creditAccounts[grant.UserID] = normalizeCreditBalance(balance)
	s.creditGrants[grant.ID] = grant
	s.creditGrantByReference[key] = grant.ID
	s.creditLedger[grant.UserID] = append(s.creditLedger[grant.UserID], models.CreditLedgerEntry{
		ID:            grant.ID + ":grant",
		UserID:        grant.UserID,
		Amount:        grant.Amount,
		Type:          models.CreditLedgerEntryTypeGrant,
		Action:        models.CreditActionCreateStory,
		Source:        grant.Source,
		ReferenceType: string(grant.Source),
		ReferenceID:   grant.ReferenceID,
		CreatedAt:     grant.GrantedAt,
		Metadata:      grant.Metadata,
	})
	return grant, false, nil
}

func (s *MemoryStore) ReserveCredits(_ context.Context, reservation models.CreditReservation) (models.CreditReservation, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureCreditMaps()
	key := creditReservationReferenceKey(reservation.Action, reservation.ReferenceType, reservation.ReferenceID)
	if existingID, ok := s.creditReservationByReference[key]; ok {
		return s.creditReservations[existingID], true, nil
	}
	if reservation.ID == "" {
		reservation.ID = models.NewID("credit-reservation")
	}
	balance := normalizeCreditBalance(s.creditAccounts[reservation.UserID])
	if balance.Available < reservation.Amount {
		return models.CreditReservation{}, false, ErrInsufficientCredits
	}
	updatedBalance, allocations, err := creditReservationAllocations(balance, reservation.Amount)
	if err != nil {
		return models.CreditReservation{}, false, err
	}
	balance = updatedBalance
	s.creditAccounts[reservation.UserID] = balance
	now := time.Now().UTC()
	reservation.State = models.CreditReservationStateReserved
	reservation.Allocations = allocations
	if reservation.CreatedAt.IsZero() {
		reservation.CreatedAt = now
	}
	reservation.UpdatedAt = now
	s.creditReservations[reservation.ID] = reservation
	s.creditReservationByReference[key] = reservation.ID
	return reservation, false, nil
}

func (s *MemoryStore) GetCreditReservation(_ context.Context, userID, reservationID string) (models.CreditReservation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	reservation, ok := s.creditReservations[reservationID]
	if !ok || reservation.UserID != userID {
		return models.CreditReservation{}, ErrNotFound
	}
	return reservation, nil
}

func (s *MemoryStore) GetCreditReservationByReference(_ context.Context, userID string, action models.CreditAction, referenceType, referenceID string) (models.CreditReservation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.creditReservationByReference == nil {
		return models.CreditReservation{}, ErrNotFound
	}
	reservationID, ok := s.creditReservationByReference[creditReservationReferenceKey(action, referenceType, referenceID)]
	if !ok {
		return models.CreditReservation{}, ErrNotFound
	}
	reservation, ok := s.creditReservations[reservationID]
	if !ok || reservation.UserID != userID {
		return models.CreditReservation{}, ErrNotFound
	}
	return reservation, nil
}

func (s *MemoryStore) CommitCreditReservation(_ context.Context, userID, reservationID string) (models.CreditReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureCreditMaps()
	reservation, ok := s.creditReservations[reservationID]
	if !ok || reservation.UserID != userID {
		return models.CreditReservation{}, ErrNotFound
	}
	switch reservation.State {
	case models.CreditReservationStateConsumed:
		return reservation, nil
	case models.CreditReservationStateReleased, models.CreditReservationStateCancelled:
		return models.CreditReservation{}, ErrConflict
	}
	balance := normalizeCreditBalance(s.creditAccounts[userID])
	if balance.Reserved < reservation.Amount {
		return models.CreditReservation{}, ErrConflict
	}
	balance = commitCreditReservationAllocations(balance, reservationAllocationsForBalance(balance, reservation))
	s.creditAccounts[userID] = balance
	now := time.Now().UTC()
	reservation.State = models.CreditReservationStateConsumed
	reservation.ConsumedAt = &now
	reservation.UpdatedAt = now
	s.creditReservations[reservationID] = reservation
	s.creditLedger[userID] = append(s.creditLedger[userID], models.CreditLedgerEntry{
		ID:            reservationID + ":consume",
		UserID:        userID,
		Amount:        -reservation.Amount,
		Type:          models.CreditLedgerEntryTypeConsume,
		Action:        reservation.Action,
		Allocations:   append([]models.CreditReservationAllocation(nil), reservation.Allocations...),
		ReferenceType: reservation.ReferenceType,
		ReferenceID:   reservation.ReferenceID,
		CreatedAt:     now,
		Metadata:      reservation.Metadata,
	})
	return reservation, nil
}

func (s *MemoryStore) ReleaseCreditReservation(_ context.Context, userID, reservationID string) (models.CreditReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureCreditMaps()
	reservation, ok := s.creditReservations[reservationID]
	if !ok || reservation.UserID != userID {
		return models.CreditReservation{}, ErrNotFound
	}
	switch reservation.State {
	case models.CreditReservationStateReleased:
		return reservation, nil
	case models.CreditReservationStateConsumed:
		return models.CreditReservation{}, ErrConflict
	}
	balance := normalizeCreditBalance(s.creditAccounts[userID])
	if balance.Reserved < reservation.Amount {
		return models.CreditReservation{}, ErrConflict
	}
	balance = releaseCreditReservationAllocations(balance, reservationAllocationsForBalance(balance, reservation))
	s.creditAccounts[userID] = balance
	now := time.Now().UTC()
	reservation.State = models.CreditReservationStateReleased
	reservation.ReleasedAt = &now
	reservation.UpdatedAt = now
	s.creditReservations[reservationID] = reservation
	s.creditLedger[userID] = append(s.creditLedger[userID], models.CreditLedgerEntry{
		ID:            reservationID + ":release",
		UserID:        userID,
		Amount:        reservation.Amount,
		Type:          models.CreditLedgerEntryTypeRelease,
		Action:        reservation.Action,
		Allocations:   append([]models.CreditReservationAllocation(nil), reservation.Allocations...),
		ReferenceType: reservation.ReferenceType,
		ReferenceID:   reservation.ReferenceID,
		CreatedAt:     now,
		Metadata:      reservation.Metadata,
	})
	return reservation, nil
}

func (s *MemoryStore) ListCreditLedger(_ context.Context, userID string) ([]models.CreditLedgerEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := append([]models.CreditLedgerEntry(nil), s.creditLedger[userID]...)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})
	return entries, nil
}

func (s *PostgresStore) GetCreditBalance(ctx context.Context, userID string) (models.CreditBalance, error) {
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind=$1 AND user_id=$2 AND id=$2`, creditAccountKind, userID).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.CreditBalance{}, nil
	}
	if err != nil {
		return models.CreditBalance{}, fmt.Errorf("get credit balance: %w", err)
	}
	var balance models.CreditBalance
	if err := json.Unmarshal(data, &balance); err != nil {
		return models.CreditBalance{}, fmt.Errorf("decode credit balance: %w", err)
	}
	return normalizeCreditBalance(balance), nil
}

func (s *PostgresStore) EnsureCreditGrant(ctx context.Context, grant models.CreditGrant) (models.CreditGrant, bool, error) {
	if grant.Amount < 0 {
		return models.CreditGrant{}, false, fmt.Errorf("grant amount must be non-negative")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.CreditGrant{}, false, err
	}
	defer tx.Rollback(ctx)
	grantKey := creditGrantKey(grant)
	if grant.ID == "" {
		grant.ID = models.NewID("credit-grant")
	}
	if grant.Remaining <= 0 {
		grant.Remaining = grant.Amount
	}
	if grant.GrantedAt.IsZero() {
		grant.GrantedAt = time.Now().UTC()
	}
	data, err := json.Marshal(grant)
	if err != nil {
		return models.CreditGrant{}, false, err
	}
	grantResult, err := tx.Exec(ctx, `INSERT INTO framemio_domain_records (kind,id,user_id,logical_key,data,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$6) ON CONFLICT DO NOTHING`, creditGrantKind, grant.ID, grant.UserID, grantKey, jsonbParam(data), grant.GrantedAt)
	if err != nil {
		return models.CreditGrant{}, false, err
	}
	if grantResult.RowsAffected() == 0 {
		var existingData []byte
		if err := tx.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind=$1 AND user_id=$2 AND logical_key=$3 FOR UPDATE`, creditGrantKind, grant.UserID, grantKey).Scan(&existingData); err != nil {
			return models.CreditGrant{}, false, err
		}
		var existing models.CreditGrant
		if err := json.Unmarshal(existingData, &existing); err != nil {
			return models.CreditGrant{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return models.CreditGrant{}, false, err
		}
		return existing, true, nil
	}
	balance, err := loadCreditBalanceTx(ctx, tx, grant.UserID)
	if err != nil {
		return models.CreditGrant{}, false, err
	}
	balance = normalizeCreditBalance(balance)
	switch grant.Source {
	case models.CreditGrantSourceSubscriptionCycle:
		balance.SubscriptionCredits = grant.Remaining
		balance.SubscriptionReserved = 0
		balance.SubscriptionCycleID = grant.SubscriptionCycleID
		balance.SubscriptionCycleExpiresAt = grant.ExpiresAt
	default:
		balance.PurchasedCredits += grant.Remaining
	}
	if err := saveCreditBalanceTx(ctx, tx, grant.UserID, balance); err != nil {
		return models.CreditGrant{}, false, err
	}
	ledger := models.CreditLedgerEntry{
		ID:            grant.ID + ":grant",
		UserID:        grant.UserID,
		Amount:        grant.Amount,
		Type:          models.CreditLedgerEntryTypeGrant,
		Action:        models.CreditActionCreateStory,
		Source:        grant.Source,
		ReferenceType: string(grant.Source),
		ReferenceID:   grant.ReferenceID,
		CreatedAt:     grant.GrantedAt,
		Metadata:      grant.Metadata,
	}
	if err := insertCreditLedgerTx(ctx, tx, ledger); err != nil {
		return models.CreditGrant{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return models.CreditGrant{}, false, err
	}
	return grant, false, nil
}

func (s *PostgresStore) ReserveCredits(ctx context.Context, reservation models.CreditReservation) (models.CreditReservation, bool, error) {
	if reservation.Amount < 0 {
		return models.CreditReservation{}, false, fmt.Errorf("reservation amount must be non-negative")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.CreditReservation{}, false, err
	}
	defer tx.Rollback(ctx)
	key := creditReservationReferenceKey(reservation.Action, reservation.ReferenceType, reservation.ReferenceID)
	var existingData []byte
	err = tx.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind=$1 AND user_id=$2 AND logical_key=$3 FOR UPDATE`, creditReservationKind, reservation.UserID, key).Scan(&existingData)
	if err == nil {
		var existing models.CreditReservation
		if err := json.Unmarshal(existingData, &existing); err != nil {
			return models.CreditReservation{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return models.CreditReservation{}, false, err
		}
		return existing, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return models.CreditReservation{}, false, err
	}
	balance, err := loadCreditBalanceTx(ctx, tx, reservation.UserID)
	if err != nil {
		return models.CreditReservation{}, false, err
	}
	balance = normalizeCreditBalance(balance)
	if balance.Available < reservation.Amount {
		return models.CreditReservation{}, false, ErrInsufficientCredits
	}
	updatedBalance, allocations, err := creditReservationAllocations(balance, reservation.Amount)
	if err != nil {
		return models.CreditReservation{}, false, err
	}
	balance = updatedBalance
	if err := saveCreditBalanceTx(ctx, tx, reservation.UserID, balance); err != nil {
		return models.CreditReservation{}, false, err
	}
	if reservation.ID == "" {
		reservation.ID = models.NewID("credit-reservation")
	}
	now := time.Now().UTC()
	reservation.State = models.CreditReservationStateReserved
	reservation.Allocations = allocations
	if reservation.CreatedAt.IsZero() {
		reservation.CreatedAt = now
	}
	reservation.UpdatedAt = now
	data, err := json.Marshal(reservation)
	if err != nil {
		return models.CreditReservation{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO framemio_domain_records (kind,id,user_id,logical_key,data,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, creditReservationKind, reservation.ID, reservation.UserID, key, jsonbParam(data), reservation.CreatedAt, reservation.UpdatedAt); err != nil {
		return models.CreditReservation{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return models.CreditReservation{}, false, err
	}
	return reservation, false, nil
}

func (s *PostgresStore) GetCreditReservation(ctx context.Context, userID, reservationID string) (models.CreditReservation, error) {
	var reservation models.CreditReservation
	if err := s.get(ctx, creditReservationKind, userID, reservationID, &reservation); err != nil {
		return models.CreditReservation{}, err
	}
	return reservation, nil
}

func (s *PostgresStore) GetCreditReservationByReference(ctx context.Context, userID string, action models.CreditAction, referenceType, referenceID string) (models.CreditReservation, error) {
	var reservation models.CreditReservation
	if err := s.getByLogical(ctx, creditReservationKind, userID, creditReservationReferenceKey(action, referenceType, referenceID), &reservation); err != nil {
		return models.CreditReservation{}, err
	}
	return reservation, nil
}

func (s *PostgresStore) CommitCreditReservation(ctx context.Context, userID, reservationID string) (models.CreditReservation, error) {
	return updateCreditReservationTx(ctx, s.pool, userID, reservationID, func(reservation models.CreditReservation, balance models.CreditBalance) (models.CreditReservation, models.CreditBalance, models.CreditLedgerEntry, error) {
		switch reservation.State {
		case models.CreditReservationStateConsumed:
			return reservation, balance, models.CreditLedgerEntry{}, nil
		case models.CreditReservationStateReleased, models.CreditReservationStateCancelled:
			return models.CreditReservation{}, balance, models.CreditLedgerEntry{}, ErrConflict
		}
		if balance.Reserved < reservation.Amount {
			return models.CreditReservation{}, balance, models.CreditLedgerEntry{}, ErrConflict
		}
		balance = commitCreditReservationAllocations(balance, reservationAllocationsForBalance(balance, reservation))
		now := time.Now().UTC()
		reservation.State = models.CreditReservationStateConsumed
		reservation.ConsumedAt = &now
		reservation.UpdatedAt = now
		ledger := models.CreditLedgerEntry{ID: reservation.ID + ":consume", UserID: reservation.UserID, Amount: -reservation.Amount, Type: models.CreditLedgerEntryTypeConsume, Action: reservation.Action, Allocations: append([]models.CreditReservationAllocation(nil), reservation.Allocations...), ReferenceType: reservation.ReferenceType, ReferenceID: reservation.ReferenceID, CreatedAt: now, Metadata: reservation.Metadata}
		return reservation, balance, ledger, nil
	})
}

func (s *PostgresStore) ReleaseCreditReservation(ctx context.Context, userID, reservationID string) (models.CreditReservation, error) {
	return updateCreditReservationTx(ctx, s.pool, userID, reservationID, func(reservation models.CreditReservation, balance models.CreditBalance) (models.CreditReservation, models.CreditBalance, models.CreditLedgerEntry, error) {
		switch reservation.State {
		case models.CreditReservationStateReleased:
			return reservation, balance, models.CreditLedgerEntry{}, nil
		case models.CreditReservationStateConsumed:
			return models.CreditReservation{}, balance, models.CreditLedgerEntry{}, ErrConflict
		}
		if balance.Reserved < reservation.Amount {
			return models.CreditReservation{}, balance, models.CreditLedgerEntry{}, ErrConflict
		}
		balance = releaseCreditReservationAllocations(balance, reservationAllocationsForBalance(balance, reservation))
		now := time.Now().UTC()
		reservation.State = models.CreditReservationStateReleased
		reservation.ReleasedAt = &now
		reservation.UpdatedAt = now
		ledger := models.CreditLedgerEntry{ID: reservation.ID + ":release", UserID: reservation.UserID, Amount: reservation.Amount, Type: models.CreditLedgerEntryTypeRelease, Action: reservation.Action, Allocations: append([]models.CreditReservationAllocation(nil), reservation.Allocations...), ReferenceType: reservation.ReferenceType, ReferenceID: reservation.ReferenceID, CreatedAt: now, Metadata: reservation.Metadata}
		return reservation, balance, ledger, nil
	})
}

func (s *PostgresStore) ListCreditLedger(ctx context.Context, userID string) ([]models.CreditLedgerEntry, error) {
	rows, err := s.pool.Query(ctx, `SELECT data FROM framemio_domain_records WHERE kind=$1 AND user_id=$2 ORDER BY created_at`, creditLedgerKind, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]models.CreditLedgerEntry, 0)
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var entry models.CreditLedgerEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func loadCreditBalanceTx(ctx context.Context, tx pgx.Tx, userID string) (models.CreditBalance, error) {
	var data []byte
	err := tx.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind=$1 AND user_id=$2 AND id=$2 FOR UPDATE`, creditAccountKind, userID).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.CreditBalance{}, nil
	}
	if err != nil {
		return models.CreditBalance{}, err
	}
	var balance models.CreditBalance
	if err := json.Unmarshal(data, &balance); err != nil {
		return models.CreditBalance{}, err
	}
	return balance, nil
}

func saveCreditBalanceTx(ctx context.Context, tx pgx.Tx, userID string, balance models.CreditBalance) error {
	data, err := json.Marshal(balance)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO framemio_domain_records (kind,id,user_id,logical_key,data,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,now(),now()) ON CONFLICT (kind,id) DO UPDATE SET data=EXCLUDED.data, updated_at=EXCLUDED.updated_at`, creditAccountKind, userID, userID, userID, jsonbParam(data))
	return err
}

func insertCreditLedgerTx(ctx context.Context, tx pgx.Tx, entry models.CreditLedgerEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO framemio_domain_records (kind,id,user_id,logical_key,data,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$6) ON CONFLICT DO NOTHING`, creditLedgerKind, entry.ID, entry.UserID, creditLedgerKey(entry), jsonbParam(data), entry.CreatedAt)
	return err
}

func loadByLogicalTx(ctx context.Context, tx pgx.Tx, kind, userID, logicalKey string, destination any) error {
	var data []byte
	err := tx.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind=$1 AND user_id=$2 AND logical_key=$3`, kind, userID, logicalKey).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, destination)
}

func updateCreditReservationTx(ctx context.Context, pool *pgxpool.Pool, userID, reservationID string, mutate func(models.CreditReservation, models.CreditBalance) (models.CreditReservation, models.CreditBalance, models.CreditLedgerEntry, error)) (models.CreditReservation, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return models.CreditReservation{}, err
	}
	defer tx.Rollback(ctx)
	reservation, err := loadCreditReservationTx(ctx, tx, userID, reservationID)
	if err != nil {
		return models.CreditReservation{}, err
	}
	balance, err := loadCreditBalanceTx(ctx, tx, userID)
	if err != nil {
		return models.CreditReservation{}, err
	}
	updatedReservation, updatedBalance, ledger, err := mutate(reservation, balance)
	if err != nil {
		return models.CreditReservation{}, err
	}
	if err := saveCreditBalanceTx(ctx, tx, userID, updatedBalance); err != nil {
		return models.CreditReservation{}, err
	}
	if ledger.ID != "" {
		if err := insertCreditLedgerTx(ctx, tx, ledger); err != nil {
			return models.CreditReservation{}, err
		}
	}
	if err := saveCreditReservationTx(ctx, tx, updatedReservation); err != nil {
		return models.CreditReservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return models.CreditReservation{}, err
	}
	return updatedReservation, nil
}

func loadCreditReservationTx(ctx context.Context, tx pgx.Tx, userID, reservationID string) (models.CreditReservation, error) {
	var data []byte
	err := tx.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind=$1 AND user_id=$2 AND id=$3 FOR UPDATE`, creditReservationKind, userID, reservationID).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.CreditReservation{}, ErrNotFound
	}
	if err != nil {
		return models.CreditReservation{}, err
	}
	var reservation models.CreditReservation
	if err := json.Unmarshal(data, &reservation); err != nil {
		return models.CreditReservation{}, err
	}
	return reservation, nil
}

func saveCreditReservationTx(ctx context.Context, tx pgx.Tx, reservation models.CreditReservation) error {
	data, err := json.Marshal(reservation)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO framemio_domain_records (kind,id,user_id,logical_key,data,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (kind,id) DO UPDATE SET data=EXCLUDED.data, updated_at=EXCLUDED.updated_at`, creditReservationKind, reservation.ID, reservation.UserID, creditReservationReferenceKey(reservation.Action, reservation.ReferenceType, reservation.ReferenceID), jsonbParam(data), reservation.CreatedAt, reservation.UpdatedAt)
	return err
}
