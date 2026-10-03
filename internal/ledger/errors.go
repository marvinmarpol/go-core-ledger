// Package ledger defines the core domain types and invariants for the double-entry ledger.
// It has no dependencies on storage, transport, or external services.
package ledger

import "errors"

// Errors returned by ledger invariant checks and store adapters.
var (
	ErrInsufficientFunds       = errors.New("insufficient funds: available balance would breach floor")
	ErrUnbalancedEntry         = errors.New("journal entry does not balance: debits must equal credits per currency")
	ErrEmptyPostings           = errors.New("journal entry must have at least two postings")
	ErrInvalidDirection        = errors.New("invalid posting direction")
	ErrAccountNotFound         = errors.New("account not found")
	ErrEntryNotFound           = errors.New("journal entry not found")
	ErrHoldNotFound            = errors.New("hold not found")
	ErrDuplicateIdempotencyKey = errors.New("journal entry with this idempotency key already exists")
	ErrVersionConflict         = errors.New("account version conflict: concurrent modification detected")
)
