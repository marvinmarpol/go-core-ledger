// Package holds manages the reserve/capture/void/expire lifecycle for fund holds.
package holds

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"go-core-ledger/internal/ledger"
	"go-core-ledger/internal/money"
	"go-core-ledger/internal/posting"
	store "go-core-ledger/internal/store/pg"
	"go-core-ledger/internal/uid"
)

// ReserveRequest carries parameters for creating a new hold.
type ReserveRequest struct {
	AccountID   string
	Amount      money.Amount
	ExpiresAt   time.Time // zero means no expiry
	ExternalRef string
}

// CaptureRequest converts an active hold into a posted journal entry.
type CaptureRequest struct {
	HoldID          string
	IdempotencyKey  string
	BusinessDate    time.Time
	ValueDate       time.Time
	ExternalRef     string
	Metadata        map[string]string
	// CreditAccountID receives the offsetting credit leg of the debit.
	CreditAccountID string
}

// HoldManager is the interface for the hold reserve/capture/void/expire lifecycle.
// Callers (e.g. the handler) depend on this interface, not the concrete Service.
type HoldManager interface {
	Reserve(ctx context.Context, req ReserveRequest) (ledger.Hold, error)
	Capture(ctx context.Context, req CaptureRequest) (posting.Result, error)
	Void(ctx context.Context, holdID string) (ledger.Hold, error)
	Expire(ctx context.Context, holdID string) (ledger.Hold, error)
}

// Service manages hold lifecycle: reserve, capture, void, expire.
type Service struct {
	pool    *pgxpool.Pool
	store   *store.Store
	posting posting.Poster
	clock   ledger.Clock
}

// NewService creates a holds Service. All arguments must be non-nil.
func NewService(pool *pgxpool.Pool, st *store.Store, postSvc posting.Poster, clock ledger.Clock) *Service {
	return &Service{pool: pool, store: st, posting: postSvc, clock: clock}
}

// Reserve creates an active hold and increments the account's held_amount,
// reducing available balance without posting a journal entry.
func (s *Service) Reserve(ctx context.Context, req ReserveRequest) (ledger.Hold, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("reserve begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txStore := s.store.WithTx(tx)

	acct, err := txStore.GetAccountForUpdate(ctx, req.AccountID)
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("reserve lock account: %w", err)
	}

	// Check that reserving this amount keeps available balance >= floor.
	if err := ledger.CheckFloor(acct, req.Amount); err != nil {
		return ledger.Hold{}, fmt.Errorf("reserve floor check: %w", err)
	}

	stored, err := txStore.InsertHold(ctx, ledger.Hold{
		ID:          uid.New(),
		AccountID:   req.AccountID,
		Amount:      req.Amount,
		ExpiresAt:   req.ExpiresAt,
		ExternalRef: req.ExternalRef,
	})
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("reserve insert hold: %w", err)
	}

	newHeld, err := acct.HeldAmount.Add(req.Amount)
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("reserve increment held: %w", err)
	}
	acct.HeldAmount = newHeld
	if _, err := txStore.UpdateAccountBalance(ctx, acct); err != nil {
		return ledger.Hold{}, fmt.Errorf("reserve update held_amount: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ledger.Hold{}, fmt.Errorf("reserve commit: %w", err)
	}
	return stored, nil
}

// Capture converts an active hold into a debit posting via the 6-step posting
// protocol. The hold is atomically marked captured inside the same transaction.
// CreditAccountID must receive the offsetting credit leg.
func (s *Service) Capture(ctx context.Context, req CaptureRequest) (posting.Result, error) {
	hold, err := s.store.GetHold(ctx, req.HoldID)
	if err != nil {
		return posting.Result{}, fmt.Errorf("capture read hold: %w", err)
	}
	if hold.Status != ledger.HoldActive {
		return posting.Result{}, fmt.Errorf("capture hold %s: status is %s, want active", hold.ID, hold.Status)
	}

	postReq := posting.Request{
		Entry: ledger.Entry{
			ID:             uid.New(),
			IdempotencyKey: req.IdempotencyKey,
			BusinessDate:   req.BusinessDate,
			ValueDate:      req.ValueDate,
			ExternalRef:    req.ExternalRef,
			Metadata:       req.Metadata,
		},
		Postings: []ledger.Posting{
			{
				AccountID: hold.AccountID,
				Amount:    hold.Amount,
				Direction: ledger.Debit,
			},
			{
				AccountID: req.CreditAccountID,
				Amount:    hold.Amount,
				Direction: ledger.Credit,
			},
		},
		CaptureHoldID: hold.ID,
	}

	result, err := s.posting.Post(ctx, postReq)
	if err != nil {
		return posting.Result{}, fmt.Errorf("capture post: %w", err)
	}
	return result, nil
}

// Void releases an active hold without posting a debit.
// Decrements held_amount; the hold is marked voided.
func (s *Service) Void(ctx context.Context, holdID string) (ledger.Hold, error) {
	return s.releaseHold(ctx, holdID, ledger.HoldVoided)
}

// Expire is identical to Void but sets the hold status to expired.
// Called by the expiry worker for holds whose ExpiresAt has passed.
func (s *Service) Expire(ctx context.Context, holdID string) (ledger.Hold, error) {
	return s.releaseHold(ctx, holdID, ledger.HoldExpired)
}

// releaseHold is the shared transaction for Void and Expire.
func (s *Service) releaseHold(ctx context.Context, holdID string, status ledger.HoldStatus) (ledger.Hold, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("release begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txStore := s.store.WithTx(tx)

	hold, err := txStore.GetHoldForUpdate(ctx, holdID)
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("release lock hold: %w", err)
	}
	if hold.Status != ledger.HoldActive {
		return ledger.Hold{}, fmt.Errorf("release hold %s: status is %s, want active", hold.ID, hold.Status)
	}

	acct, err := txStore.GetAccountForUpdate(ctx, hold.AccountID)
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("release lock account: %w", err)
	}

	newHeld, err := acct.HeldAmount.Sub(hold.Amount)
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("release decrement held: %w", err)
	}
	acct.HeldAmount = newHeld
	if _, err := txStore.UpdateAccountBalance(ctx, acct); err != nil {
		return ledger.Hold{}, fmt.Errorf("release update held_amount: %w", err)
	}

	updated, err := txStore.UpdateHoldStatus(ctx, hold.ID, status)
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("release update hold status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ledger.Hold{}, fmt.Errorf("release commit: %w", err)
	}
	return updated, nil
}
