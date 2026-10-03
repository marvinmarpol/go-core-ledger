// Package posting implements the 6-step double-entry posting protocol.
// Every write to the ledger that creates journal entries and postings must
// flow through this package. Do not add a second write path.
package posting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"

	"go-core-ledger/internal/ledger"
	"go-core-ledger/internal/money"
	store "go-core-ledger/internal/store/pg"
	"go-core-ledger/internal/uid"
)

// Request carries everything needed to commit a balanced journal entry.
type Request struct {
	Entry    ledger.Entry
	Postings []ledger.Posting
	// CaptureHoldID, when non-empty, atomically releases this active hold as part
	// of the same transaction. The hold's amount is deducted from held_amount on
	// the hold's account. Exactly one debit posting must match the hold's account
	// and amount.
	CaptureHoldID string
}

// Result holds the committed entry and postings returned after a successful Post.
type Result struct {
	Entry    ledger.Entry
	Postings []ledger.Posting
}

// Poster is the interface for committing journal entries via the 6-step protocol.
// Callers (e.g. the handler) depend on this interface, not the concrete Service.
type Poster interface {
	Post(ctx context.Context, req Request) (Result, error)
}

// Service runs the 6-step posting protocol against a Postgres pool.
type Service struct {
	pool  *pgxpool.Pool
	store *store.Store
	clock ledger.Clock
}

// NewService returns a Service. st and clock must not be nil.
func NewService(pool *pgxpool.Pool, st *store.Store, clock ledger.Clock) *Service {
	return &Service{pool: pool, store: st, clock: clock}
}

// Post commits a balanced journal entry and all its postings in one transaction.
//
// Steps:
//  1. Insert journal entry (duplicate idempotency key → return the stored result).
//  2. Lock account rows with SELECT … FOR UPDATE ordered by account_id.
//  3. Validate entry balance and floor invariants.
//  4. Insert postings; update account balances (and held_amount for captures).
//  5. Insert outbox event "posting_committed".
//  6. Commit.
func (s *Service) Post(ctx context.Context, req Request) (Result, error) {
	if err := ledger.CheckEntryBalances(req.Postings); err != nil {
		return Result{}, fmt.Errorf("validate postings: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	txStore := s.store.WithTx(tx)

	// Step 1
	req.Entry.BookedAt = s.clock.Now()
	entry, err := txStore.InsertJournalEntry(ctx, req.Entry)
	if errors.Is(err, ledger.ErrDuplicateIdempotencyKey) {
		_ = tx.Rollback(ctx)
		return s.fetchExisting(ctx, req.Entry.IdempotencyKey)
	}
	if err != nil {
		return Result{}, fmt.Errorf("step 1: %w", err)
	}

	// Step 2: lock accounts in account_id order
	accounts, err := lockAccounts(ctx, txStore, req.Postings)
	if err != nil {
		return Result{}, fmt.Errorf("step 2: %w", err)
	}

	// Step 2 (cont): lock hold if this is a capture
	var captureHold *ledger.Hold
	if req.CaptureHoldID != "" {
		h, err := txStore.GetHoldForUpdate(ctx, req.CaptureHoldID)
		if err != nil {
			return Result{}, fmt.Errorf("step 2 lock hold: %w", err)
		}
		if h.Status != ledger.HoldActive {
			return Result{}, fmt.Errorf("capture hold %s: status is %s, want active", h.ID, h.Status)
		}
		if err := validateCaptureMatch(h, req.Postings); err != nil {
			return Result{}, fmt.Errorf("capture hold %s: %w", h.ID, err)
		}
		captureHold = &h
	}

	// Step 3: validate invariants
	if err := checkFloors(accounts, req.Postings); err != nil {
		return Result{}, err
	}

	// Step 4a: insert postings
	committed := make([]ledger.Posting, 0, len(req.Postings))
	for _, p := range req.Postings {
		p.ID = uid.New()
		p.EntryID = entry.ID
		stored, err := txStore.InsertPosting(ctx, p)
		if err != nil {
			return Result{}, fmt.Errorf("step 4 insert posting: %w", err)
		}
		committed = append(committed, stored)
	}

	// Step 4b: apply balance changes
	newBalances, err := applyPostings(accounts, req.Postings, captureHold)
	if err != nil {
		return Result{}, fmt.Errorf("step 4 apply balances: %w", err)
	}
	for _, acct := range newBalances {
		if _, err := txStore.UpdateAccountBalance(ctx, acct); err != nil {
			return Result{}, fmt.Errorf("step 4 update balance %s: %w", acct.ID, err)
		}
	}

	// Step 4c: mark hold captured
	if captureHold != nil {
		if _, err := txStore.UpdateHoldStatus(ctx, captureHold.ID, ledger.HoldCaptured); err != nil {
			return Result{}, fmt.Errorf("step 4 capture hold: %w", err)
		}
	}

	// Step 5: outbox event
	payload, err := buildEventPayload(entry, committed)
	if err != nil {
		return Result{}, fmt.Errorf("step 5 build payload: %w", err)
	}
	if _, err := txStore.InsertOutboxEvent(ctx, store.OutboxEventInput{
		ID:            uid.New(),
		AggregateType: "journal_entry",
		AggregateID:   entry.ID,
		EventType:     "posting_committed",
		Payload:       payload,
	}); err != nil {
		return Result{}, fmt.Errorf("step 5 outbox: %w", err)
	}

	// Step 6: commit
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("step 6 commit: %w", err)
	}

	return Result{Entry: entry, Postings: committed}, nil
}

// fetchExisting retrieves the previously committed entry and postings for a
// duplicate idempotency key. Called after the conflicting transaction is rolled back.
func (s *Service) fetchExisting(ctx context.Context, idempotencyKey string) (Result, error) {
	entry, err := s.store.GetJournalEntryByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return Result{}, fmt.Errorf("fetch existing entry: %w", err)
	}
	postings, err := s.store.GetPostingsByEntryID(ctx, entry.ID)
	if err != nil {
		return Result{}, fmt.Errorf("fetch existing postings: %w", err)
	}
	return Result{Entry: entry, Postings: postings}, nil
}

// lockAccounts fetches and row-locks all accounts referenced in postings,
// in ascending account_id order to guarantee consistent lock acquisition.
func lockAccounts(ctx context.Context, st *store.Store, postings []ledger.Posting) (map[string]ledger.Account, error) {
	ids := uniqueSortedIDs(postings)
	accounts := make(map[string]ledger.Account, len(ids))
	for _, id := range ids {
		acct, err := st.GetAccountForUpdate(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("account %s: %w", id, err)
		}
		accounts[id] = acct
	}
	return accounts, nil
}

// checkFloors validates invariant #5 for every debit leg.
func checkFloors(accounts map[string]ledger.Account, postings []ledger.Posting) error {
	for _, p := range postings {
		if p.Direction != ledger.Debit {
			continue
		}
		if err := ledger.CheckFloor(accounts[p.AccountID], p.Amount); err != nil {
			return fmt.Errorf("floor check: %w", err)
		}
	}
	return nil
}

// applyPostings computes new account states after all posting legs are applied.
// If captureHold is non-nil the hold's account also has its held_amount decremented.
func applyPostings(accounts map[string]ledger.Account, postings []ledger.Posting, captureHold *ledger.Hold) (map[string]ledger.Account, error) {
	updated := make(map[string]ledger.Account, len(accounts))
	for id, a := range accounts {
		updated[id] = a
	}

	for _, p := range postings {
		acct := updated[p.AccountID]
		var (
			newBal money.Amount
			err    error
		)
		switch p.Direction {
		case ledger.Debit:
			newBal, err = acct.Balance.Sub(p.Amount)
		case ledger.Credit:
			newBal, err = acct.Balance.Add(p.Amount)
		default:
			return nil, fmt.Errorf("%w: %d", ledger.ErrInvalidDirection, p.Direction)
		}
		if err != nil {
			return nil, fmt.Errorf("account %s: %w", acct.ID, err)
		}
		acct.Balance = newBal
		updated[p.AccountID] = acct
	}

	if captureHold != nil {
		acct := updated[captureHold.AccountID]
		newHeld, err := acct.HeldAmount.Sub(captureHold.Amount)
		if err != nil {
			return nil, fmt.Errorf("release held_amount %s: %w", acct.ID, err)
		}
		acct.HeldAmount = newHeld
		updated[captureHold.AccountID] = acct
	}

	return updated, nil
}

// validateCaptureMatch ensures that exactly one debit leg in postings matches
// the hold's account and amount, so we can't accidentally capture the wrong hold.
func validateCaptureMatch(hold ledger.Hold, postings []ledger.Posting) error {
	for _, p := range postings {
		if p.Direction == ledger.Debit && p.AccountID == hold.AccountID && p.Amount.Equal(hold.Amount) {
			return nil
		}
	}
	return fmt.Errorf("no debit leg matches hold account=%s amount=%s", hold.AccountID, hold.Amount)
}

// uniqueSortedIDs returns deduplicated account IDs from postings in ascending order.
func uniqueSortedIDs(postings []ledger.Posting) []string {
	seen := make(map[string]struct{}, len(postings))
	for _, p := range postings {
		seen[p.AccountID] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

type eventPayload struct {
	EntryID  string           `json:"entry_id"`
	Postings []postingPayload `json:"postings"`
}

type postingPayload struct {
	ID        string `json:"id"`
	AccountID string `json:"account_id"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Direction string `json:"direction"`
}

func buildEventPayload(entry ledger.Entry, postings []ledger.Posting) (json.RawMessage, error) {
	legs := make([]postingPayload, len(postings))
	for i, p := range postings {
		legs[i] = postingPayload{
			ID:        p.ID,
			AccountID: p.AccountID,
			Amount:    p.Amount.Value(),
			Currency:  p.Amount.Currency(),
			Direction: p.Direction.String(),
		}
	}
	raw, err := json.Marshal(eventPayload{EntryID: entry.ID, Postings: legs})
	if err != nil {
		return nil, fmt.Errorf("marshal event payload: %w", err)
	}
	return raw, nil
}
