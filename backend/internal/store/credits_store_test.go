package store

import (
	"context"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
)

func TestMemoryStoreReserveConsumesSubscriptionCreditsFirst(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	store.ensureCreditMaps()
	store.creditAccounts["user-1"] = models.CreditBalance{SubscriptionCredits: 20, PurchasedCredits: 500}

	reservation, created, err := store.ReserveCredits(context.Background(), models.CreditReservation{
		UserID:        "user-1",
		Action:        models.CreditActionCreateStory,
		Amount:        40,
		ReferenceType: "story",
		ReferenceID:   "story-1",
	})
	if err != nil {
		t.Fatalf("reserve credits: %v", err)
	}
	if created {
		t.Fatal("expected first reservation to be created")
	}
	if got, want := len(reservation.Allocations), 2; got != want {
		t.Fatalf("allocation count = %d, want %d", got, want)
	}
	if reservation.Allocations[0].Source != models.CreditGrantSourceSubscriptionCycle || reservation.Allocations[0].Amount != 20 {
		t.Fatalf("first allocation = %+v, want subscription 20", reservation.Allocations[0])
	}
	if reservation.Allocations[1].Source != models.CreditGrantSourceCreditPurchase || reservation.Allocations[1].Amount != 20 {
		t.Fatalf("second allocation = %+v, want purchased 20", reservation.Allocations[1])
	}

	balance, err := store.GetCreditBalance(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balance.SubscriptionCredits != 0 || balance.PurchasedCredits != 480 || balance.Reserved != 40 {
		t.Fatalf("unexpected post-reserve balance: %+v", balance)
	}

	if _, err := store.CommitCreditReservation(context.Background(), "user-1", reservation.ID); err != nil {
		t.Fatalf("commit reservation: %v", err)
	}
	balance, err = store.GetCreditBalance(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("get balance after commit: %v", err)
	}
	if balance.SubscriptionCredits != 0 || balance.PurchasedCredits != 480 || balance.Reserved != 0 {
		t.Fatalf("unexpected post-commit balance: %+v", balance)
	}
}

func TestMemoryStoreGrantSeparatesSubscriptionAndPurchasedCredits(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	store.ensureCreditMaps()
	store.creditAccounts["user-1"] = models.CreditBalance{PurchasedCredits: 500}

	_, created, err := store.EnsureCreditGrant(context.Background(), models.CreditGrant{
		ID:                  "grant-cycle-1",
		UserID:              "user-1",
		Source:              models.CreditGrantSourceSubscriptionCycle,
		Amount:              1_200,
		Remaining:           1_200,
		GrantedAt:           time.Now().UTC(),
		SubscriptionCycleID: "cycle-1",
		ReferenceID:         "cycle-1",
	})
	if err != nil {
		t.Fatalf("grant subscription credits: %v", err)
	}
	if created {
		t.Fatal("expected new grant to be created")
	}

	balance, err := store.GetCreditBalance(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balance.SubscriptionCredits != 1_200 || balance.PurchasedCredits != 500 || balance.TotalCredits != 1_700 {
		t.Fatalf("unexpected balance after subscription grant: %+v", balance)
	}

	_, _, err = store.EnsureCreditGrant(context.Background(), models.CreditGrant{
		ID:          "grant-purchase-1",
		UserID:      "user-1",
		Source:      models.CreditGrantSourceCreditPurchase,
		Amount:      250,
		Remaining:   250,
		GrantedAt:   time.Now().UTC(),
		ReferenceID: "purchase-1",
	})
	if err != nil {
		t.Fatalf("grant purchased credits: %v", err)
	}

	balance, err = store.GetCreditBalance(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("get balance after purchase: %v", err)
	}
	if balance.SubscriptionCredits != 1_200 || balance.PurchasedCredits != 750 || balance.TotalCredits != 1_950 {
		t.Fatalf("unexpected balance after purchase grant: %+v", balance)
	}
}

func TestMemoryStoreLegacyBalanceMigratesToPurchasedCredits(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	store.ensureCreditMaps()
	store.creditAccounts["user-legacy"] = models.CreditBalance{Available: 100, Reserved: 20}

	balance, err := store.GetCreditBalance(context.Background(), "user-legacy")
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balance.SubscriptionCredits != 0 || balance.PurchasedCredits != 100 || balance.PurchasedReserved != 20 || balance.TotalCredits != 100 {
		t.Fatalf("unexpected legacy migration balance: %+v", balance)
	}
}
