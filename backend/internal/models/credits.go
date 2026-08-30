package models

import "time"

type CreditAction string

const (
	CreditActionCreateStory     CreditAction = "CREATE_STORY"
	CreditActionRegenerateStory CreditAction = "REGENERATE_STORY"
	CreditActionAddMoment       CreditAction = "ADD_MOMENT"
	CreditActionAIEditStory     CreditAction = "AI_EDIT_STORY"
	CreditActionChangeMusic     CreditAction = "CHANGE_MUSIC"
	CreditActionChangeCaption   CreditAction = "CHANGE_CAPTION"
	CreditActionReorderMoment   CreditAction = "REORDER_MOMENT"
	CreditActionRemoveMoment    CreditAction = "REMOVE_MOMENT"
	CreditActionPreview         CreditAction = "PREVIEW"
)

func NormalizeCreditAction(value string) (CreditAction, bool) {
	switch CreditAction(value) {
	case CreditActionCreateStory,
		CreditActionRegenerateStory,
		CreditActionAddMoment,
		CreditActionAIEditStory,
		CreditActionChangeMusic,
		CreditActionChangeCaption,
		CreditActionReorderMoment,
		CreditActionRemoveMoment,
		CreditActionPreview:
		return CreditAction(value), true
	default:
		return "", false
	}
}

type CreditGrantSource string

const (
	CreditGrantSourceInitialGrant      CreditGrantSource = "INITIAL_GRANT"
	CreditGrantSourceSubscriptionCycle CreditGrantSource = "SUBSCRIPTION_CYCLE"
	CreditGrantSourceCreditPurchase    CreditGrantSource = "CREDIT_PURCHASE"
	CreditGrantSourcePromotionalGrant  CreditGrantSource = "PROMOTIONAL_GRANT"
	CreditGrantSourceRefund            CreditGrantSource = "REFUND"
)

type CreditLedgerEntryType string

const (
	CreditLedgerEntryTypeGrant    CreditLedgerEntryType = "GRANT"
	CreditLedgerEntryTypeConsume  CreditLedgerEntryType = "CONSUME"
	CreditLedgerEntryTypeRelease  CreditLedgerEntryType = "RELEASE"
	CreditLedgerEntryTypeExpire   CreditLedgerEntryType = "EXPIRE"
	CreditLedgerEntryTypeAdjust   CreditLedgerEntryType = "ADJUST"
	CreditLedgerEntryTypeReversal CreditLedgerEntryType = "REVERSAL"
	CreditLedgerEntryTypeTransfer CreditLedgerEntryType = "TRANSFER"
)

type CreditReservationState string

const (
	CreditReservationStateReserved  CreditReservationState = "RESERVED"
	CreditReservationStateConsumed  CreditReservationState = "CONSUMED"
	CreditReservationStateReleased  CreditReservationState = "RELEASED"
	CreditReservationStateCancelled CreditReservationState = "CANCELLED"
)

type CreditBalance struct {
	Available                 int        `json:"available"`
	Reserved                  int        `json:"reserved"`
	SubscriptionCredits       int        `json:"subscription_credits"`
	PurchasedCredits          int        `json:"purchased_credits"`
	SubscriptionReserved      int        `json:"subscription_reserved,omitempty"`
	PurchasedReserved         int        `json:"purchased_reserved,omitempty"`
	SubscriptionCycleID       string     `json:"subscription_cycle_id,omitempty"`
	SubscriptionCycleExpiresAt *time.Time `json:"subscription_cycle_expires_at,omitempty"`
	TotalCredits              int        `json:"total_credits"`
}

type CreditQuote struct {
	Action       CreditAction `json:"action"`
	Credits      int          `json:"credits"`
	Balance      int          `json:"balance"`
	BalanceAfter int          `json:"balanceAfter"`
	Eligible     bool         `json:"eligible"`
	Reason       string       `json:"reason,omitempty"`
}

type CreditGrant struct {
	ID                  string            `json:"id"`
	UserID              string            `json:"user_id"`
	Source              CreditGrantSource `json:"source"`
	Amount              int               `json:"amount"`
	Remaining           int               `json:"remaining"`
	GrantedAt           time.Time         `json:"granted_at"`
	ExpiresAt           *time.Time        `json:"expires_at,omitempty"`
	SubscriptionCycleID string            `json:"subscription_cycle_id,omitempty"`
	ReferenceID         string            `json:"reference_id,omitempty"`
	Metadata            map[string]any    `json:"metadata,omitempty"`
}

type CreditReservationAllocation struct {
	GrantID string            `json:"grant_id,omitempty"`
	Source  CreditGrantSource  `json:"source,omitempty"`
	Amount  int               `json:"amount"`
}

type CreditReservation struct {
	ID            string                        `json:"id"`
	UserID        string                        `json:"user_id"`
	Action        CreditAction                  `json:"action"`
	Amount        int                           `json:"amount"`
	ReferenceType string                        `json:"reference_type"`
	ReferenceID   string                        `json:"reference_id"`
	State         CreditReservationState        `json:"state"`
	Allocations   []CreditReservationAllocation `json:"allocations,omitempty"`
	Metadata      map[string]any                `json:"metadata,omitempty"`
	CreatedAt     time.Time                     `json:"created_at"`
	UpdatedAt     time.Time                     `json:"updated_at"`
	ConsumedAt    *time.Time                    `json:"consumed_at,omitempty"`
	ReleasedAt    *time.Time                    `json:"released_at,omitempty"`
	ExpiresAt     *time.Time                    `json:"expires_at,omitempty"`
}

type CreditLedgerEntry struct {
	ID            string                       `json:"id"`
	UserID        string                       `json:"user_id"`
	Amount        int                          `json:"amount"`
	Type          CreditLedgerEntryType        `json:"type"`
	Action        CreditAction                 `json:"action"`
	Source        CreditGrantSource            `json:"source,omitempty"`
	ReferenceType string                       `json:"reference_type,omitempty"`
	ReferenceID   string                       `json:"reference_id,omitempty"`
	Allocations   []CreditReservationAllocation `json:"allocations,omitempty"`
	CreatedAt     time.Time                    `json:"created_at"`
	Metadata      map[string]any               `json:"metadata,omitempty"`
}
