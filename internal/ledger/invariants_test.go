package ledger_test

import (
	"errors"
	"testing"

	"go-core-ledger/internal/ledger"
	"go-core-ledger/internal/money"
)

func posting(id, accountID string, value int64, currency string, dir ledger.Direction) ledger.Posting {
	return ledger.Posting{
		ID:        id,
		AccountID: accountID,
		Amount:    money.MustNew(value, currency),
		Direction: dir,
	}
}

func TestCheckEntryBalances(t *testing.T) {
	tests := []struct {
		name     string
		postings []ledger.Posting
		wantErr  error
	}{
		{
			name: "balanced IDR entry",
			postings: []ledger.Posting{
				posting("p1", "a1", 1000, "IDR", ledger.Debit),
				posting("p2", "a2", 1000, "IDR", ledger.Credit),
			},
		},
		{
			name: "unbalanced IDR entry",
			postings: []ledger.Posting{
				posting("p1", "a1", 1000, "IDR", ledger.Debit),
				posting("p2", "a2", 900, "IDR", ledger.Credit),
			},
			wantErr: ledger.ErrUnbalancedEntry,
		},
		{
			name:     "single posting",
			postings: []ledger.Posting{posting("p1", "a1", 1000, "IDR", ledger.Debit)},
			wantErr:  ledger.ErrEmptyPostings,
		},
		{
			name:     "no postings",
			postings: []ledger.Posting{},
			wantErr:  ledger.ErrEmptyPostings,
		},
		{
			name: "multi-currency balanced",
			postings: []ledger.Posting{
				posting("p1", "a1", 500, "IDR", ledger.Debit),
				posting("p2", "a2", 500, "IDR", ledger.Credit),
				posting("p3", "a3", 100, "USD", ledger.Debit),
				posting("p4", "a4", 100, "USD", ledger.Credit),
			},
		},
		{
			name: "multi-currency one leg off",
			postings: []ledger.Posting{
				posting("p1", "a1", 500, "IDR", ledger.Debit),
				posting("p2", "a2", 500, "IDR", ledger.Credit),
				posting("p3", "a3", 100, "USD", ledger.Debit),
				posting("p4", "a4", 99, "USD", ledger.Credit),
			},
			wantErr: ledger.ErrUnbalancedEntry,
		},
		{
			name: "debit with no matching credit currency",
			postings: []ledger.Posting{
				posting("p1", "a1", 500, "IDR", ledger.Debit),
				posting("p2", "a2", 500, "USD", ledger.Credit),
			},
			wantErr: ledger.ErrUnbalancedEntry,
		},
		{
			name: "four-leg balanced",
			postings: []ledger.Posting{
				posting("p1", "a1", 300, "IDR", ledger.Debit),
				posting("p2", "a2", 700, "IDR", ledger.Debit),
				posting("p3", "a3", 1000, "IDR", ledger.Credit),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ledger.CheckEntryBalances(tc.postings)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("CheckEntryBalances() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func account(id string, balance, held, floor int64, currency string, allowNeg bool) ledger.Account {
	return ledger.Account{
		ID:            id,
		Currency:      currency,
		Balance:       money.MustNew(balance, currency),
		HeldAmount:    money.MustNew(held, currency),
		Floor:         money.MustNew(floor, currency),
		AllowNegative: allowNeg,
	}
}

func TestCheckFloor(t *testing.T) {
	tests := []struct {
		name        string
		acct        ledger.Account
		debitAmount money.Amount
		wantErr     error
	}{
		{
			name:        "exact floor hit passes",
			acct:        account("a1", 1000, 0, 0, "IDR", false),
			debitAmount: money.MustNew(1000, "IDR"),
		},
		{
			name:        "one unit below floor fails",
			acct:        account("a1", 1000, 0, 0, "IDR", false),
			debitAmount: money.MustNew(1001, "IDR"),
			wantErr:     ledger.ErrInsufficientFunds,
		},
		{
			name:        "allow negative always passes",
			acct:        account("a1", 0, 0, 0, "IDR", true),
			debitAmount: money.MustNew(9999999, "IDR"),
		},
		{
			name:        "hold reduces available — fails earlier",
			acct:        account("a1", 1000, 500, 0, "IDR", false),
			debitAmount: money.MustNew(600, "IDR"),
			wantErr:     ledger.ErrInsufficientFunds,
		},
		{
			name:        "hold reduces available — still passes",
			acct:        account("a1", 1000, 200, 0, "IDR", false),
			debitAmount: money.MustNew(800, "IDR"),
		},
		{
			name:        "non-zero floor respected",
			acct:        account("a1", 1000, 0, 100, "IDR", false),
			debitAmount: money.MustNew(901, "IDR"),
			wantErr:     ledger.ErrInsufficientFunds,
		},
		{
			name:        "non-zero floor — debit lands exactly on floor",
			acct:        account("a1", 1000, 0, 100, "IDR", false),
			debitAmount: money.MustNew(900, "IDR"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ledger.CheckFloor(tc.acct, tc.debitAmount)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("CheckFloor() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
