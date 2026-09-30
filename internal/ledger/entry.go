package ledger

import "time"

// Entry is an immutable journal entry. Corrections are new entries with ReversesID set.
type Entry struct {
	ID             string
	IdempotencyKey string
	BusinessDate   time.Time
	ValueDate      time.Time
	BookedAt       time.Time
	ReversesID     string            // non-empty for reversal/correction entries
	ExternalRef    string            // external transaction ID kept for traceability
	Metadata       map[string]string // arbitrary product-engine context
}
