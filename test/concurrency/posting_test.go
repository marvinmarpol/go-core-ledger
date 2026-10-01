//go:build integration

// Package concurrency tests the posting protocol under concurrent load against
// a real Postgres instance. Run with: make test-int
//
// Required: DATABASE_URL env var (defaults to Makefile value if run via make).
package concurrency_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"go-core-ledger/internal/ledger"
	"go-core-ledger/internal/money"
	"go-core-ledger/internal/posting"
	store "go-core-ledger/internal/store/pg"
	"go-core-ledger/internal/uid"
)

const defaultDSN = "postgres://admin:admin123@localhost:5432/core_ledger?sslmode=disable"

func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = defaultDSN
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("postgres unavailable (%v); skipping concurrency tests", err)
	}
	return pool
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// setupAccounts inserts two accounts (src with balance, dst at zero) and
// returns their IDs.
func setupAccounts(t *testing.T, st *store.Store, balance int64, currency string) (srcID, dstID string) {
	t.Helper()
	ctx := context.Background()
	zero := money.MustNew(0, currency)
	bal := money.MustNew(balance, currency)

	src, err := st.InsertAccount(ctx, ledger.Account{
		ID:       uid.New(),
		Currency: currency,
		Balance:  bal, HeldAmount: zero, Floor: zero,
		AllowNegative: false, Version: 1,
	})
	if err != nil {
		t.Fatalf("insert src account: %v", err)
	}

	dst, err := st.InsertAccount(ctx, ledger.Account{
		ID:       uid.New(),
		Currency: currency,
		Balance:  zero, HeldAmount: zero, Floor: zero,
		AllowNegative: true, Version: 1,
	})
	if err != nil {
		t.Fatalf("insert dst account: %v", err)
	}
	return src.ID, dst.ID
}

// TestConcurrentPostings_BalanceConsistency fires N goroutines each posting
// the same amount from src → dst and asserts that the final balances are
// consistent with the number of successful postings.
func TestConcurrentPostings_BalanceConsistency(t *testing.T) {
	pool := openPool(t)
	defer pool.Close()

	st := store.NewStore(pool)
	clock := fixedClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	svc := posting.NewService(pool, clock)

	const (
		goroutines  = 20
		amountEach  = int64(100)
		initialBal  = int64(goroutines * amountEach * 2) // enough for all to succeed
		currency    = "IDR"
	)

	ctx := context.Background()
	srcID, dstID := setupAccounts(t, st, initialBal, currency)

	type outcome struct{ err error }
	results := make([]outcome, goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := posting.Request{
				Entry: ledger.Entry{
					ID:             uid.New(),
					IdempotencyKey: uid.New(), // each goroutine has unique key
					BusinessDate:   clock.t,
					ValueDate:      clock.t,
				},
				Postings: []ledger.Posting{
					{AccountID: srcID, Amount: money.MustNew(amountEach, currency), Direction: ledger.Debit},
					{AccountID: dstID, Amount: money.MustNew(amountEach, currency), Direction: ledger.Credit},
				},
			}
			_, err := svc.Post(ctx, req)
			results[i] = outcome{err: err}
		}(i)
	}
	wg.Wait()

	var successes, failures int
	for _, r := range results {
		if r.err == nil {
			successes++
		} else {
			t.Logf("posting error (expected if floor breached): %v", r.err)
			failures++
		}
	}

	// Verify final src balance = initial - (successes * amountEach)
	src, err := st.GetAccount(ctx, srcID)
	if err != nil {
		t.Fatalf("get src: %v", err)
	}
	dst, err := st.GetAccount(ctx, dstID)
	if err != nil {
		t.Fatalf("get dst: %v", err)
	}

	wantSrc := initialBal - int64(successes)*amountEach
	wantDst := int64(successes) * amountEach

	if src.Balance.Value() != wantSrc {
		t.Errorf("src balance: got %d, want %d (%d successes)", src.Balance.Value(), wantSrc, successes)
	}
	if dst.Balance.Value() != wantDst {
		t.Errorf("dst balance: got %d, want %d (%d successes)", dst.Balance.Value(), wantDst, successes)
	}
	t.Logf("concurrent test: %d successes, %d failures; src=%d dst=%d", successes, failures, src.Balance.Value(), dst.Balance.Value())
}

// TestIdempotentPosting_ReturnsSameResult sends the same idempotency key N times
// concurrently and asserts each caller gets back an identical result.
func TestIdempotentPosting_ReturnsSameResult(t *testing.T) {
	pool := openPool(t)
	defer pool.Close()

	st := store.NewStore(pool)
	clock := fixedClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	svc := posting.NewService(pool, clock)

	const (
		goroutines = 10
		currency   = "IDR"
		amount     = int64(500)
	)

	ctx := context.Background()
	srcID, dstID := setupAccounts(t, st, 100_000, currency)

	sharedKey := uid.New()
	entryID := uid.New()

	type result struct {
		r   posting.Result
		err error
	}
	results := make([]result, goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := posting.Request{
				Entry: ledger.Entry{
					ID:             entryID,
					IdempotencyKey: sharedKey,
					BusinessDate:   clock.t,
					ValueDate:      clock.t,
				},
				Postings: []ledger.Posting{
					{AccountID: srcID, Amount: money.MustNew(amount, currency), Direction: ledger.Debit},
					{AccountID: dstID, Amount: money.MustNew(amount, currency), Direction: ledger.Credit},
				},
			}
			r, err := svc.Post(ctx, req)
			results[i] = result{r: r, err: err}
		}(i)
	}
	wg.Wait()

	// All callers must succeed.
	var firstEntryID string
	for i, r := range results {
		if r.err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, r.err)
			continue
		}
		if firstEntryID == "" {
			firstEntryID = r.r.Entry.ID
		} else if r.r.Entry.ID != firstEntryID {
			t.Errorf("goroutine %d: entry ID %q != first %q (not idempotent)", i, r.r.Entry.ID, firstEntryID)
		}
	}

	// Exactly one journal entry should exist.
	postings, err := store.NewStore(pool).GetPostingsByEntryID(ctx, firstEntryID)
	if err != nil {
		t.Fatalf("get postings: %v", err)
	}
	if len(postings) != 2 {
		t.Errorf("want 2 postings, got %d", len(postings))
	}
}
