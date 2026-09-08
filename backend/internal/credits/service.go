package credits

import (
	"context"
	"fmt"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/store"
)

type Service struct {
	Store  store.Store
	Policy Policy
	Now    func() time.Time
}

func New(serviceStore store.Store, policy Policy, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	if policy.Prices == nil {
		policy = DefaultPolicy()
	}
	return &Service{Store: serviceStore, Policy: policy, Now: now}
}

func (s *Service) GetBalance(ctx context.Context, userID string) (models.CreditBalance, error) {
	if s == nil || s.Store == nil {
		return models.CreditBalance{}, fmt.Errorf("credit service is not configured")
	}
	balance, err := s.Store.GetCreditBalance(ctx, userID)
	if err != nil {
		return models.CreditBalance{}, err
	}
	balance.InitialCreditGrant = s.Policy.InitialGrant
	balance.MonthlySubscriptionCycleCredits = s.Policy.MonthlySubscriptionCycleCredits
	balance.YearlySubscriptionCycleCredits = s.Policy.YearlySubscriptionCycleCredits
	return balance, nil
}

func (s *Service) GetHistory(ctx context.Context, userID string) ([]models.CreditLedgerEntry, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("credit service is not configured")
	}
	return s.Store.ListCreditLedger(ctx, userID)
}

func (s *Service) Quote(ctx context.Context, userID string, action models.CreditAction, stats StoryMediaStats) (models.CreditQuote, error) {
	if s == nil || s.Store == nil {
		return models.CreditQuote{}, fmt.Errorf("credit service is not configured")
	}
	balance, err := s.GetBalance(ctx, userID)
	if err != nil {
		return models.CreditQuote{}, err
	}
	eligible, reason := s.Policy.Validate(action, stats)
	price, ok := s.Policy.Price(action, stats)
	if !ok {
		return models.CreditQuote{}, fmt.Errorf("unknown credit action: %s", action)
	}
	return models.CreditQuote{Action: action, Credits: price, Balance: balance.Available, BalanceAfter: balance.Available - price, Eligible: eligible, Reason: reason}, nil
}

func (s *Service) EnsureInitialGrant(ctx context.Context, userID string) (models.CreditGrant, bool, error) {
	if s == nil || s.Store == nil {
		return models.CreditGrant{}, false, fmt.Errorf("credit service is not configured")
	}
	return s.Store.EnsureCreditGrant(ctx, models.CreditGrant{
		ID:          models.NewID("credit-grant"),
		UserID:      userID,
		Source:      models.CreditGrantSourceInitialGrant,
		Amount:      s.Policy.InitialGrant,
		Remaining:   s.Policy.InitialGrant,
		GrantedAt:   s.Now().UTC(),
		ReferenceID: userID,
	})
}

func (s *Service) EnsureCreditGrant(ctx context.Context, grant models.CreditGrant) (models.CreditGrant, bool, error) {
	if s == nil || s.Store == nil {
		return models.CreditGrant{}, false, fmt.Errorf("credit service is not configured")
	}
	if grant.GrantedAt.IsZero() {
		grant.GrantedAt = s.Now().UTC()
	}
	if grant.ID == "" {
		grant.ID = models.NewID("credit-grant")
	}
	if grant.Remaining <= 0 {
		grant.Remaining = grant.Amount
	}
	return s.Store.EnsureCreditGrant(ctx, grant)
}

func (s *Service) GrantSubscriptionCycle(ctx context.Context, userID, cycleID string, amount int, expiresAt *time.Time, metadata map[string]any) (models.CreditGrant, bool, error) {
	return s.EnsureCreditGrant(ctx, models.CreditGrant{
		UserID:              userID,
		Source:              models.CreditGrantSourceSubscriptionCycle,
		Amount:              amount,
		Remaining:           amount,
		GrantedAt:           s.Now().UTC(),
		ExpiresAt:           expiresAt,
		SubscriptionCycleID: cycleID,
		ReferenceID:         cycleID,
		Metadata:            metadata,
	})
}

func (s *Service) GrantPurchasedCredits(ctx context.Context, userID, purchaseID string, amount int, metadata map[string]any) (models.CreditGrant, bool, error) {
	return s.EnsureCreditGrant(ctx, models.CreditGrant{
		UserID:      userID,
		Source:      models.CreditGrantSourceCreditPurchase,
		Amount:      amount,
		Remaining:   amount,
		GrantedAt:   s.Now().UTC(),
		ReferenceID: purchaseID,
		Metadata:    metadata,
	})
}

func (s *Service) Reserve(ctx context.Context, userID string, action models.CreditAction, referenceType, referenceID string, stats StoryMediaStats, metadata map[string]any) (models.CreditReservation, models.CreditQuote, error) {
	quote, err := s.Quote(ctx, userID, action, stats)
	if err != nil {
		return models.CreditReservation{}, models.CreditQuote{}, err
	}
	if !quote.Eligible {
		if quote.Reason == "" {
			quote.Reason = "action not eligible"
		}
		return models.CreditReservation{}, quote, fmt.Errorf("%s", quote.Reason)
	}
	if quote.Credits <= 0 {
		return models.CreditReservation{}, quote, nil
	}
	reservation, _, err := s.Store.ReserveCredits(ctx, models.CreditReservation{
		ID:            models.NewID("credit-reservation"),
		UserID:        userID,
		Action:        action,
		Amount:        quote.Credits,
		ReferenceType: referenceType,
		ReferenceID:   referenceID,
		State:         models.CreditReservationStateReserved,
		Metadata:      metadata,
		CreatedAt:     s.Now().UTC(),
		UpdatedAt:     s.Now().UTC(),
	})
	return reservation, quote, err
}

func (s *Service) Commit(ctx context.Context, userID, reservationID string) (models.CreditReservation, error) {
	if s == nil || s.Store == nil {
		return models.CreditReservation{}, fmt.Errorf("credit service is not configured")
	}
	return s.Store.CommitCreditReservation(ctx, userID, reservationID)
}

func (s *Service) Release(ctx context.Context, userID, reservationID string) (models.CreditReservation, error) {
	if s == nil || s.Store == nil {
		return models.CreditReservation{}, fmt.Errorf("credit service is not configured")
	}
	return s.Store.ReleaseCreditReservation(ctx, userID, reservationID)
}

func (s *Service) CommitByReference(ctx context.Context, userID string, action models.CreditAction, referenceType, referenceID string) (models.CreditReservation, error) {
	if s == nil || s.Store == nil {
		return models.CreditReservation{}, fmt.Errorf("credit service is not configured")
	}
	reservation, err := s.Store.GetCreditReservationByReference(ctx, userID, action, referenceType, referenceID)
	if err != nil {
		return models.CreditReservation{}, err
	}
	return s.Store.CommitCreditReservation(ctx, userID, reservation.ID)
}

func (s *Service) ReleaseByReference(ctx context.Context, userID string, action models.CreditAction, referenceType, referenceID string) (models.CreditReservation, error) {
	if s == nil || s.Store == nil {
		return models.CreditReservation{}, fmt.Errorf("credit service is not configured")
	}
	reservation, err := s.Store.GetCreditReservationByReference(ctx, userID, action, referenceType, referenceID)
	if err != nil {
		return models.CreditReservation{}, err
	}
	return s.Store.ReleaseCreditReservation(ctx, userID, reservation.ID)
}
