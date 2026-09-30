package money_test

import (
	"errors"
	"math"
	"testing"

	"go-core-ledger/internal/money"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		value    int64
		currency string
		wantErr  error
	}{
		{"valid positive", 1500, "IDR", nil},
		{"valid zero", 0, "IDR", nil},
		{"valid negative", -500, "USD", nil},
		{"empty currency", 100, "", money.ErrInvalidCurrency},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, err := money.New(tc.value, tc.currency)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("New(%d, %q) error = %v, want %v", tc.value, tc.currency, err, tc.wantErr)
			}
			if err == nil {
				if a.Value() != tc.value {
					t.Errorf("Value() = %d, want %d", a.Value(), tc.value)
				}
				if a.Currency() != tc.currency {
					t.Errorf("Currency() = %q, want %q", a.Currency(), tc.currency)
				}
			}
		})
	}
}

func TestMustNewPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty currency")
		}
	}()
	money.MustNew(100, "")
}

func TestAdd(t *testing.T) {
	tests := []struct {
		name    string
		a, b    money.Amount
		want    int64
		wantErr error
	}{
		{"positive + positive", money.MustNew(100, "IDR"), money.MustNew(200, "IDR"), 300, nil},
		{"positive + negative", money.MustNew(300, "IDR"), money.MustNew(-100, "IDR"), 200, nil},
		{"zero + zero", money.MustNew(0, "IDR"), money.MustNew(0, "IDR"), 0, nil},
		{
			"currency mismatch",
			money.MustNew(100, "IDR"), money.MustNew(100, "USD"),
			0, money.ErrCurrencyMismatch,
		},
		{
			"positive overflow",
			money.MustNew(math.MaxInt64, "IDR"), money.MustNew(1, "IDR"),
			0, money.ErrOverflow,
		},
		{
			"negative overflow",
			money.MustNew(math.MinInt64, "IDR"), money.MustNew(-1, "IDR"),
			0, money.ErrOverflow,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.a.Add(tc.b)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Add() error = %v, want %v", err, tc.wantErr)
			}
			if err == nil && got.Value() != tc.want {
				t.Errorf("Add() = %d, want %d", got.Value(), tc.want)
			}
		})
	}
}

func TestSub(t *testing.T) {
	tests := []struct {
		name    string
		a, b    money.Amount
		want    int64
		wantErr error
	}{
		{"positive - positive", money.MustNew(300, "IDR"), money.MustNew(100, "IDR"), 200, nil},
		{"zero - positive", money.MustNew(0, "IDR"), money.MustNew(100, "IDR"), -100, nil},
		{
			"currency mismatch",
			money.MustNew(100, "IDR"), money.MustNew(100, "USD"),
			0, money.ErrCurrencyMismatch,
		},
		{
			"positive overflow",
			money.MustNew(math.MaxInt64, "IDR"), money.MustNew(-1, "IDR"),
			0, money.ErrOverflow,
		},
		{
			"negative overflow",
			money.MustNew(math.MinInt64, "IDR"), money.MustNew(1, "IDR"),
			0, money.ErrOverflow,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.a.Sub(tc.b)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Sub() error = %v, want %v", err, tc.wantErr)
			}
			if err == nil && got.Value() != tc.want {
				t.Errorf("Sub() = %d, want %d", got.Value(), tc.want)
			}
		})
	}
}

func TestNeg(t *testing.T) {
	a := money.MustNew(500, "IDR")
	neg, err := a.Neg()
	if err != nil {
		t.Fatalf("Neg() unexpected error: %v", err)
	}
	if neg.Value() != -500 {
		t.Errorf("Neg() = %d, want -500", neg.Value())
	}

	_, err = money.MustNew(math.MinInt64, "IDR").Neg()
	if !errors.Is(err, money.ErrOverflow) {
		t.Errorf("Neg(MinInt64) error = %v, want ErrOverflow", err)
	}
}

func TestAbs(t *testing.T) {
	pos := money.MustNew(500, "IDR")
	neg := money.MustNew(-500, "IDR")

	absPos, err := pos.Abs()
	if err != nil || absPos.Value() != 500 {
		t.Errorf("Abs(500) = %v, %v", absPos, err)
	}
	absNeg, err := neg.Abs()
	if err != nil || absNeg.Value() != 500 {
		t.Errorf("Abs(-500) = %v, %v", absNeg, err)
	}
}

func TestEqual(t *testing.T) {
	a := money.MustNew(100, "IDR")
	b := money.MustNew(100, "IDR")
	c := money.MustNew(200, "IDR")
	d := money.MustNew(100, "USD")

	if !a.Equal(b) {
		t.Error("Equal(same value, same currency) = false, want true")
	}
	if a.Equal(c) {
		t.Error("Equal(different value) = true, want false")
	}
	if a.Equal(d) {
		t.Error("Equal(different currency) = true, want false")
	}
}

func TestLess(t *testing.T) {
	a := money.MustNew(100, "IDR")
	b := money.MustNew(200, "IDR")

	less, err := a.Less(b)
	if err != nil || !less {
		t.Errorf("Less(100, 200) = %v, %v", less, err)
	}
	less, err = b.Less(a)
	if err != nil || less {
		t.Errorf("Less(200, 100) = %v, %v", less, err)
	}

	_, err = a.Less(money.MustNew(100, "USD"))
	if !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Errorf("Less(mismatch) error = %v, want ErrCurrencyMismatch", err)
	}
}

func TestString(t *testing.T) {
	a := money.MustNew(100000, "IDR")
	if a.String() != "IDR 100000" {
		t.Errorf("String() = %q, want %q", a.String(), "IDR 100000")
	}
}

func TestIsZeroPositiveNegative(t *testing.T) {
	zero := money.MustNew(0, "IDR")
	pos := money.MustNew(1, "IDR")
	neg := money.MustNew(-1, "IDR")

	if !zero.IsZero() || zero.IsPositive() || zero.IsNegative() {
		t.Error("zero flags wrong")
	}
	if pos.IsZero() || !pos.IsPositive() || pos.IsNegative() {
		t.Error("positive flags wrong")
	}
	if neg.IsZero() || neg.IsPositive() || !neg.IsNegative() {
		t.Error("negative flags wrong")
	}
}
