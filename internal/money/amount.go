// Package money defines the amount type for all monetary values in this service.
package money

import (
	"errors"
	"fmt"
	"math"
)

// Errors returned by Amount operations.
var (
	ErrCurrencyMismatch = errors.New("currency mismatch")
	ErrOverflow         = errors.New("amount overflow")
	ErrInvalidCurrency  = errors.New("invalid currency")
)

// Amount is an immutable monetary value: int64 minor units + ISO 4217 currency code.
type Amount struct {
	value    int64
	currency string
}

// New returns an Amount. Returns ErrInvalidCurrency if currency is empty.
func New(value int64, currency string) (Amount, error) {
	if value := activeCurrency[currency]; !value {
		return Amount{}, ErrInvalidCurrency
	}
	return Amount{value: value, currency: currency}, nil
}

// MustNew returns an Amount and panics if currency is empty. Use in package-level vars and tests.
func MustNew(value int64, currency string) Amount {
	a, err := New(value, currency)
	if err != nil {
		panic(fmt.Sprintf("money.MustNew: %v", err))
	}
	return a
}

// Zero returns an Amount with value 0 for the given currency.
func Zero(currency string) (Amount, error) {
	return New(0, currency)
}

// Value returns the int64 minor-unit value.
func (a Amount) Value() int64 { return a.value }

// Currency returns the ISO 4217 currency code.
func (a Amount) Currency() string { return a.currency }

// IsZero reports whether the value is 0.
func (a Amount) IsZero() bool { return a.value == 0 }

// IsPositive reports whether the value is > 0.
func (a Amount) IsPositive() bool { return a.value > 0 }

// IsNegative reports whether the value is < 0.
func (a Amount) IsNegative() bool { return a.value < 0 }

// Equal reports whether a and b have the same value and currency.
func (a Amount) Equal(b Amount) bool {
	return a.currency == b.currency && a.value == b.value
}

// SameCurrency reports whether a and b share the same currency code.
func (a Amount) SameCurrency(b Amount) bool {
	return a.currency == b.currency
}

// Add returns a + b. Returns ErrCurrencyMismatch or ErrOverflow on failure.
func (a Amount) Add(b Amount) (Amount, error) {
	if a.currency != b.currency {
		return Amount{}, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, a.currency, b.currency)
	}
	if addOverflows(a.value, b.value) {
		return Amount{}, fmt.Errorf("%w: %s %d + %d", ErrOverflow, a.currency, a.value, b.value)
	}
	return Amount{value: a.value + b.value, currency: a.currency}, nil
}

// Sub returns a - b. Returns ErrCurrencyMismatch or ErrOverflow on failure.
func (a Amount) Sub(b Amount) (Amount, error) {
	if a.currency != b.currency {
		return Amount{}, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, a.currency, b.currency)
	}
	if subOverflows(a.value, b.value) {
		return Amount{}, fmt.Errorf("%w: %s %d - %d", ErrOverflow, a.currency, a.value, b.value)
	}
	return Amount{value: a.value - b.value, currency: a.currency}, nil
}

// Neg returns -a. Returns ErrOverflow if a.Value() == math.MinInt64.
func (a Amount) Neg() (Amount, error) {
	if a.value == math.MinInt64 {
		return Amount{}, fmt.Errorf("%w: cannot negate MinInt64", ErrOverflow)
	}
	return Amount{value: -a.value, currency: a.currency}, nil
}

// Abs returns |a|. Returns ErrOverflow if a.Value() == math.MinInt64.
func (a Amount) Abs() (Amount, error) {
	if a.value >= 0 {
		return a, nil
	}
	return a.Neg()
}

// Less reports whether a < b. Returns ErrCurrencyMismatch if currencies differ.
func (a Amount) Less(b Amount) (bool, error) {
	if a.currency != b.currency {
		return false, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, a.currency, b.currency)
	}
	return a.value < b.value, nil
}

// LessOrEqual reports whether a <= b. Returns ErrCurrencyMismatch if currencies differ.
func (a Amount) LessOrEqual(b Amount) (bool, error) {
	if a.currency != b.currency {
		return false, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, a.currency, b.currency)
	}
	return a.value <= b.value, nil
}

// String returns a human-readable representation, e.g. "IDR 100000". for logging only.
func (a Amount) String() string {
	return fmt.Sprintf("%s %d", a.currency, a.value)
}

// addOverflows reports whether a + b would overflow int64.
func addOverflows(a, b int64) bool {
	if b > 0 && a > math.MaxInt64-b {
		return true
	}
	if b < 0 && a < math.MinInt64-b {
		return true
	}
	return false
}

// subOverflows reports whether a - b would overflow int64.
func subOverflows(a, b int64) bool {
	if b < 0 && a > math.MaxInt64+b {
		return true
	}
	if b > 0 && a < math.MinInt64+b {
		return true
	}
	return false
}
