// Package property contains property-based invariant tests.
// Each test generates many pseudo-random inputs with a fixed seed and asserts
// the ledger invariants hold for all of them.
package property_test

import (
	"fmt"
	"math/rand"
	"testing"

	"go-core-ledger/internal/ledger"
	"go-core-ledger/internal/money"
)

const seed = 42

// ── Invariant 1: CheckEntryBalances ──────────────────────────────────────────

// TestCheckEntryBalances_BalancedAlwaysPasses generates random balanced entries
// and asserts that CheckEntryBalances returns nil for all of them.
func TestCheckEntryBalances_BalancedAlwaysPasses(t *testing.T) {
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec
	for i := range 500 {
		postings := randomBalancedPostings(rng, 2+rng.Intn(6))
		if err := ledger.CheckEntryBalances(postings); err != nil {
			t.Errorf("case %d: balanced postings failed: %v\npostings: %s", i, err, describePostings(postings))
		}
	}
}

// TestCheckEntryBalances_UnbalancedAlwaysFails generates deliberately unbalanced
// entries and asserts that CheckEntryBalances rejects all of them.
func TestCheckEntryBalances_UnbalancedAlwaysFails(t *testing.T) {
	rng := rand.New(rand.NewSource(seed + 1)) //nolint:gosec
	for i := range 500 {
		postings := randomUnbalancedPostings(rng)
		if err := ledger.CheckEntryBalances(postings); err == nil {
			t.Errorf("case %d: unbalanced postings passed unexpectedly\npostings: %s", i, describePostings(postings))
		}
	}
}

// TestCheckEntryBalances_SinglePostingAlwaysFails asserts that a lone posting
// always fails the balance check, regardless of direction or amount.
func TestCheckEntryBalances_SinglePostingAlwaysFails(t *testing.T) {
	rng := rand.New(rand.NewSource(seed + 2)) //nolint:gosec
	for i := range 200 {
		dir := ledger.Debit
		if rng.Intn(2) == 0 {
			dir = ledger.Credit
		}
		p := randomPosting(rng, fmt.Sprintf("p%d", i), fmt.Sprintf("a%d", i), dir)
		if err := ledger.CheckEntryBalances([]ledger.Posting{p}); err == nil {
			t.Errorf("case %d: single posting passed unexpectedly", i)
		}
	}
}

// ── Invariant 5: CheckFloor ───────────────────────────────────────────────────

// TestCheckFloor_SufficientFundsAlwaysPasses: for any account where
// balance - held - debitAmount >= floor, CheckFloor must return nil.
func TestCheckFloor_SufficientFundsAlwaysPasses(t *testing.T) {
	rng := rand.New(rand.NewSource(seed + 3)) //nolint:gosec
	for i := range 500 {
		balance := int64(rng.Intn(1_000_000) + 1000)
		held := int64(rng.Intn(int(balance / 2)))
		floor := int64(rng.Intn(int(balance/10) + 1))
		avail := balance - held
		if avail <= floor {
			continue // degenerate case — skip
		}
		debit := int64(rng.Intn(int(avail-floor)) + 1)

		acct := ledger.Account{
			ID:            fmt.Sprintf("a%d", i),
			Currency:      "IDR",
			Balance:       money.MustNew(balance, "IDR"),
			HeldAmount:    money.MustNew(held, "IDR"),
			Floor:         money.MustNew(floor, "IDR"),
			AllowNegative: false,
		}
		amt := money.MustNew(debit, "IDR")
		if err := ledger.CheckFloor(acct, amt); err != nil {
			t.Errorf("case %d: sufficient funds rejected: %v (bal=%d held=%d floor=%d debit=%d)",
				i, err, balance, held, floor, debit)
		}
	}
}

// TestCheckFloor_InsufficientFundsAlwaysFails: for any account where
// debitAmount would push available below floor, CheckFloor must return an error.
func TestCheckFloor_InsufficientFundsAlwaysFails(t *testing.T) {
	rng := rand.New(rand.NewSource(seed + 4)) //nolint:gosec
	for i := range 500 {
		balance := int64(rng.Intn(100_000) + 100)
		held := int64(rng.Intn(int(balance)))
		floor := int64(rng.Intn(int(balance)/2 + 1))
		avail := balance - held
		// Ensure debit exceeds avail - floor so it must fail.
		excess := int64(rng.Intn(1000) + 1)
		debit := avail - floor + excess

		acct := ledger.Account{
			ID:            fmt.Sprintf("a%d", i),
			Currency:      "IDR",
			Balance:       money.MustNew(balance, "IDR"),
			HeldAmount:    money.MustNew(held, "IDR"),
			Floor:         money.MustNew(floor, "IDR"),
			AllowNegative: false,
		}
		amt := money.MustNew(debit, "IDR")
		if err := ledger.CheckFloor(acct, amt); err == nil {
			t.Errorf("case %d: insufficient funds passed (bal=%d held=%d floor=%d debit=%d)",
				i, balance, held, floor, debit)
		}
	}
}

// TestCheckFloor_AllowNegativeAlwaysPasses asserts that allow_negative bypasses
// the floor check for any positive debit amount.
func TestCheckFloor_AllowNegativeAlwaysPasses(t *testing.T) {
	rng := rand.New(rand.NewSource(seed + 5)) //nolint:gosec
	for i := range 200 {
		balance := int64(rng.Intn(1000))
		debit := int64(rng.Intn(10_000_000) + 1)
		acct := ledger.Account{
			ID:            fmt.Sprintf("a%d", i),
			Currency:      "IDR",
			Balance:       money.MustNew(balance, "IDR"),
			HeldAmount:    money.MustNew(0, "IDR"),
			Floor:         money.MustNew(0, "IDR"),
			AllowNegative: true,
		}
		if err := ledger.CheckFloor(acct, money.MustNew(debit, "IDR")); err != nil {
			t.Errorf("case %d: allow_negative account rejected debit %d: %v", i, debit, err)
		}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

var currencies = []string{"IDR", "USD", "EUR"}

func randomPosting(rng *rand.Rand, id, accountID string, dir ledger.Direction) ledger.Posting {
	currency := currencies[rng.Intn(len(currencies))]
	amount := int64(rng.Intn(1_000_000) + 1)
	return ledger.Posting{
		ID:        id,
		AccountID: accountID,
		Amount:    money.MustNew(amount, currency),
		Direction: dir,
	}
}

// randomBalancedPostings generates n/2 debit legs paired with matching credit legs.
func randomBalancedPostings(rng *rand.Rand, n int) []ledger.Posting {
	if n < 2 {
		n = 2
	}
	currency := currencies[rng.Intn(len(currencies))]
	total := int64(rng.Intn(1_000_000) + 1)

	var postings []ledger.Posting
	// Split total into n/2 debit legs summing to total, then matching credits.
	remaining := total
	half := n / 2
	for i := 0; i < half-1; i++ {
		amt := int64(rng.Intn(int(remaining)/half+1) + 1)
		remaining -= amt
		postings = append(postings,
			ledger.Posting{
				ID: fmt.Sprintf("d%d", i), AccountID: fmt.Sprintf("src%d", i),
				Amount: money.MustNew(amt, currency), Direction: ledger.Debit,
			},
			ledger.Posting{
				ID: fmt.Sprintf("c%d", i), AccountID: fmt.Sprintf("dst%d", i),
				Amount: money.MustNew(amt, currency), Direction: ledger.Credit,
			},
		)
	}
	if remaining <= 0 {
		remaining = 1
	}
	postings = append(postings,
		ledger.Posting{
			ID: "dlast", AccountID: "srcLast",
			Amount: money.MustNew(remaining, currency), Direction: ledger.Debit,
		},
		ledger.Posting{
			ID: "clast", AccountID: "dstLast",
			Amount: money.MustNew(remaining, currency), Direction: ledger.Credit,
		},
	)
	return postings
}

// randomUnbalancedPostings generates a two-leg entry where debit ≠ credit.
func randomUnbalancedPostings(rng *rand.Rand) []ledger.Posting {
	currency := currencies[rng.Intn(len(currencies))]
	a := int64(rng.Intn(1_000_000) + 1)
	b := int64(rng.Intn(1_000_000) + 1)
	for b == a {
		b = int64(rng.Intn(1_000_000) + 1)
	}
	return []ledger.Posting{
		{ID: "d1", AccountID: "src", Amount: money.MustNew(a, currency), Direction: ledger.Debit},
		{ID: "c1", AccountID: "dst", Amount: money.MustNew(b, currency), Direction: ledger.Credit},
	}
}

func describePostings(postings []ledger.Posting) string {
	var s string
	for _, p := range postings {
		s += fmt.Sprintf("  %s %s %s\n", p.Direction, p.Amount, p.AccountID)
	}
	return s
}
