package ledger

import (
	"time"

	"go-core-ledger/internal/money"
)

// HoldStatus represents the lifecycle state of a hold.
type HoldStatus string

// Valid HoldStatus values.
const (
	HoldActive   HoldStatus = "active"
	HoldCaptured HoldStatus = "captured"
	HoldVoided   HoldStatus = "voided"
	HoldExpired  HoldStatus = "expired"
)

// Hold is a reservation of funds against an account's available balance.
// Creating a hold reduces AvailableBalance without touching the ledger balance;
// capturing it posts the debit and releases the reservation.
type Hold struct {
	ID          string
	AccountID   string
	Amount      money.Amount
	Status      HoldStatus
	ExpiresAt   time.Time // zero means no expiry
	ExternalRef string
}
