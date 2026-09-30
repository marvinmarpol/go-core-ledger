// Package ledger defines the core domain types and invariants for the double-entry ledger.
// It has no dependencies on storage, transport, or external services.
package ledger

import "errors"

// Sentinel errors returned by ledger invariant checks.
var (
	ErrInsufficientFunds = errors.New("insufficient funds: available balance would breach floor")
	ErrUnbalancedEntry   = errors.New("journal entry does not balance: debits must equal credits per currency")
	ErrEmptyPostings     = errors.New("journal entry must have at least two postings")
	ErrInvalidDirection  = errors.New("invalid posting direction")
	ErrAccountNotFound   = errors.New("account not found")
)
