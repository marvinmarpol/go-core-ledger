package ledger

import (
	"fmt"

	"go-core-ledger/internal/money"
)

// CheckEntryBalances verifies invariant #1: sum(debits) == sum(credits) per currency.
// Returns ErrEmptyPostings if len(postings) < 2, ErrUnbalancedEntry on mismatch.
func CheckEntryBalances(postings []Posting) error {
	if len(postings) < 2 {
		return ErrEmptyPostings
	}

	debits := map[string]money.Amount{}
	credits := map[string]money.Amount{}

	for _, p := range postings {
		if !p.Direction.IsValid() {
			return fmt.Errorf("%w: posting %s", ErrInvalidDirection, p.ID)
		}
		currency := p.Amount.Currency()
		switch p.Direction {
		case Debit:
			if existing, ok := debits[currency]; ok {
				sum, err := existing.Add(p.Amount)
				if err != nil {
					return fmt.Errorf("debit sum: %w", err)
				}
				debits[currency] = sum
			} else {
				debits[currency] = p.Amount
			}
		case Credit:
			if existing, ok := credits[currency]; ok {
				sum, err := existing.Add(p.Amount)
				if err != nil {
					return fmt.Errorf("credit sum: %w", err)
				}
				credits[currency] = sum
			} else {
				credits[currency] = p.Amount
			}
		}
	}

	for currency, debitTotal := range debits {
		creditTotal, ok := credits[currency]
		if !ok {
			return fmt.Errorf("%w: currency %s has debits but no credits", ErrUnbalancedEntry, currency)
		}
		if !debitTotal.Equal(creditTotal) {
			return fmt.Errorf("%w: currency %s debits=%s credits=%s", ErrUnbalancedEntry, currency, debitTotal, creditTotal)
		}
	}
	for currency := range credits {
		if _, ok := debits[currency]; !ok {
			return fmt.Errorf("%w: currency %s has credits but no debits", ErrUnbalancedEntry, currency)
		}
	}

	return nil
}

// CheckFloor verifies invariant #5: after debiting debitAmount the account's available
// balance stays >= account.Floor. Returns nil when account.AllowNegative is true.
func CheckFloor(account Account, debitAmount money.Amount) error {
	if account.AllowNegative {
		return nil
	}
	ok, err := account.CanDebit(debitAmount)
	if err != nil {
		return fmt.Errorf("check floor: %w", err)
	}
	if !ok {
		return fmt.Errorf("%w: account %s", ErrInsufficientFunds, account.ID)
	}
	return nil
}
