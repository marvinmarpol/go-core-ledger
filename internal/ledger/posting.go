package ledger

import "go-core-ledger/internal/money"

// Posting is one leg of a journal entry.
// Amount is always positive; Direction encodes whether it debits or credits the account.
type Posting struct {
	ID        string
	EntryID   string
	AccountID string
	Amount    money.Amount
	Direction Direction
}
