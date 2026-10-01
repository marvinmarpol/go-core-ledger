package posting

import (
	"errors"
	"testing"

	"go-core-ledger/internal/ledger"
	"go-core-ledger/internal/money"
)

// ── applyPostings ─────────────────────────────────────────────────────────────

func mustAmount(v int64, c string) money.Amount { return money.MustNew(v, c) }

func acct(id string, bal, held int64, cur string) ledger.Account {
	return ledger.Account{
		ID:         id,
		Currency:   cur,
		Balance:    mustAmount(bal, cur),
		HeldAmount: mustAmount(held, cur),
		Floor:      mustAmount(0, cur),
		Version:    1,
	}
}

func TestApplyPostings_BasicDebitCredit(t *testing.T) {
	accounts := map[string]ledger.Account{
		"src": acct("src", 10000, 0, "IDR"),
		"dst": acct("dst", 0, 0, "IDR"),
	}
	postings := []ledger.Posting{
		{AccountID: "src", Amount: mustAmount(3000, "IDR"), Direction: ledger.Debit},
		{AccountID: "dst", Amount: mustAmount(3000, "IDR"), Direction: ledger.Credit},
	}

	updated, err := applyPostings(accounts, postings, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := updated["src"].Balance.Value(); got != 7000 {
		t.Errorf("src balance: got %d, want 7000", got)
	}
	if got := updated["dst"].Balance.Value(); got != 3000 {
		t.Errorf("dst balance: got %d, want 3000", got)
	}
}

func TestApplyPostings_CaptureReleasesHeld(t *testing.T) {
	accounts := map[string]ledger.Account{
		"src": acct("src", 10000, 5000, "IDR"),
		"dst": acct("dst", 0, 0, "IDR"),
	}
	postings := []ledger.Posting{
		{AccountID: "src", Amount: mustAmount(5000, "IDR"), Direction: ledger.Debit},
		{AccountID: "dst", Amount: mustAmount(5000, "IDR"), Direction: ledger.Credit},
	}
	hold := &ledger.Hold{
		ID:        "hold-1",
		AccountID: "src",
		Amount:    mustAmount(5000, "IDR"),
	}

	updated, err := applyPostings(accounts, postings, hold)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := updated["src"].Balance.Value(); got != 5000 {
		t.Errorf("src balance: got %d, want 5000", got)
	}
	if got := updated["src"].HeldAmount.Value(); got != 0 {
		t.Errorf("src held_amount: got %d, want 0", got)
	}
}

func TestApplyPostings_InvalidDirection(t *testing.T) {
	accounts := map[string]ledger.Account{"a": acct("a", 1000, 0, "IDR")}
	postings := []ledger.Posting{
		{AccountID: "a", Amount: mustAmount(100, "IDR"), Direction: ledger.Direction(99)},
	}
	_, err := applyPostings(accounts, postings, nil)
	if !errors.Is(err, ledger.ErrInvalidDirection) {
		t.Errorf("want ErrInvalidDirection, got %v", err)
	}
}

// ── checkFloors ───────────────────────────────────────────────────────────────

func TestCheckFloors_PassesOnSufficientFunds(t *testing.T) {
	accounts := map[string]ledger.Account{
		"src": acct("src", 10000, 0, "IDR"),
	}
	postings := []ledger.Posting{
		{AccountID: "src", Amount: mustAmount(10000, "IDR"), Direction: ledger.Debit},
	}
	if err := checkFloors(accounts, postings); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckFloors_FailsOnInsufficientFunds(t *testing.T) {
	accounts := map[string]ledger.Account{
		"src": acct("src", 1000, 0, "IDR"),
	}
	postings := []ledger.Posting{
		{AccountID: "src", Amount: mustAmount(1001, "IDR"), Direction: ledger.Debit},
	}
	if err := checkFloors(accounts, postings); !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Errorf("want ErrInsufficientFunds, got %v", err)
	}
}

func TestCheckFloors_SkipsCredits(t *testing.T) {
	// Credit legs never consume available balance so they always pass floor.
	accounts := map[string]ledger.Account{
		"dst": acct("dst", 0, 0, "IDR"),
	}
	postings := []ledger.Posting{
		{AccountID: "dst", Amount: mustAmount(99999, "IDR"), Direction: ledger.Credit},
	}
	if err := checkFloors(accounts, postings); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── uniqueSortedIDs ───────────────────────────────────────────────────────────

func TestUniqueSortedIDs(t *testing.T) {
	postings := []ledger.Posting{
		{AccountID: "zzz"},
		{AccountID: "aaa"},
		{AccountID: "mmm"},
		{AccountID: "aaa"}, // duplicate
	}
	ids := uniqueSortedIDs(postings)
	want := []string{"aaa", "mmm", "zzz"}
	if len(ids) != len(want) {
		t.Fatalf("len: got %d, want %d", len(ids), len(want))
	}
	for i, v := range want {
		if ids[i] != v {
			t.Errorf("ids[%d]: got %q, want %q", i, ids[i], v)
		}
	}
}

// ── validateCaptureMatch ──────────────────────────────────────────────────────

func TestValidateCaptureMatch_Matches(t *testing.T) {
	hold := ledger.Hold{AccountID: "src", Amount: mustAmount(5000, "IDR")}
	postings := []ledger.Posting{
		{AccountID: "src", Amount: mustAmount(5000, "IDR"), Direction: ledger.Debit},
		{AccountID: "dst", Amount: mustAmount(5000, "IDR"), Direction: ledger.Credit},
	}
	if err := validateCaptureMatch(hold, postings); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateCaptureMatch_NoMatch(t *testing.T) {
	hold := ledger.Hold{AccountID: "src", Amount: mustAmount(5000, "IDR")}
	postings := []ledger.Posting{
		{AccountID: "other", Amount: mustAmount(5000, "IDR"), Direction: ledger.Debit},
		{AccountID: "dst", Amount: mustAmount(5000, "IDR"), Direction: ledger.Credit},
	}
	if err := validateCaptureMatch(hold, postings); err == nil {
		t.Error("expected error, got nil")
	}
}
