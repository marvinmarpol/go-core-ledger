package ledger

import (
	"fmt"

	"go-core-ledger/internal/money"
)

// Account is the ledger account entity. Balance is the sum of all posted amounts.
type Account struct {
	ID            string
	Currency      string
	Balance       money.Amount
	HeldAmount    money.Amount
	Floor         money.Amount
	AllowNegative bool
	Version       int64
	ExternalRef   string // external ID kept for traceability
}

// AvailableBalance returns Balance − HeldAmount.
func (a Account) AvailableBalance() (money.Amount, error) {
	avail, err := a.Balance.Sub(a.HeldAmount)
	if err != nil {
		return money.Amount{}, fmt.Errorf("available balance: %w", err)
	}
	return avail, nil
}

// CanDebit reports whether debiting amount would keep available balance >= Floor.
// Always returns true when AllowNegative is set.
func (a Account) CanDebit(amount money.Amount) (bool, error) {
	if a.AllowNegative {
		return true, nil
	}
	avail, err := a.AvailableBalance()
	if err != nil {
		return false, err
	}
	afterDebit, err := avail.Sub(amount)
	if err != nil {
		return false, fmt.Errorf("can debit: %w", err)
	}
	ok, err := a.Floor.LessOrEqual(afterDebit)
	if err != nil {
		return false, fmt.Errorf("can debit: %w", err)
	}
	return ok, nil
}
