package handler

import (
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"

	"go-core-ledger/internal/ledger"
)

func TestDomainErr_NotFound(t *testing.T) {
	notFound := []error{
		ledger.ErrAccountNotFound,
		ledger.ErrEntryNotFound,
		ledger.ErrHoldNotFound,
	}
	for _, e := range notFound {
		wrapped := fmt.Errorf("some context: %w", e)
		got := domainErr(wrapped)
		var ce *connect.Error
		if !errors.As(got, &ce) {
			t.Fatalf("expected connect.Error for %v, got %T", e, got)
		}
		if ce.Code() != connect.CodeNotFound {
			t.Errorf("%v: got code %v, want NotFound", e, ce.Code())
		}
	}
}

func TestDomainErr_InvalidArgument(t *testing.T) {
	invalid := []error{
		ledger.ErrInsufficientFunds,
		ledger.ErrUnbalancedEntry,
		ledger.ErrEmptyPostings,
		ledger.ErrInvalidDirection,
	}
	for _, e := range invalid {
		got := domainErr(e)
		var ce *connect.Error
		if !errors.As(got, &ce) {
			t.Fatalf("expected connect.Error for %v, got %T", e, got)
		}
		if ce.Code() != connect.CodeInvalidArgument {
			t.Errorf("%v: got code %v, want InvalidArgument", e, ce.Code())
		}
	}
}

func TestDomainErr_AlreadyExists(t *testing.T) {
	got := domainErr(ledger.ErrDuplicateIdempotencyKey)
	var ce *connect.Error
	if !errors.As(got, &ce) {
		t.Fatalf("expected connect.Error, got %T", got)
	}
	if ce.Code() != connect.CodeAlreadyExists {
		t.Errorf("got code %v, want AlreadyExists", ce.Code())
	}
}

func TestDomainErr_VersionConflict(t *testing.T) {
	got := domainErr(ledger.ErrVersionConflict)
	var ce *connect.Error
	if !errors.As(got, &ce) {
		t.Fatalf("expected connect.Error, got %T", got)
	}
	if ce.Code() != connect.CodeAborted {
		t.Errorf("got code %v, want Aborted", ce.Code())
	}
}

func TestDomainErr_Internal(t *testing.T) {
	got := domainErr(errors.New("some unexpected db error"))
	var ce *connect.Error
	if !errors.As(got, &ce) {
		t.Fatalf("expected connect.Error, got %T", got)
	}
	if ce.Code() != connect.CodeInternal {
		t.Errorf("got code %v, want Internal", ce.Code())
	}
}

func TestDomainErr_Nil(t *testing.T) {
	if domainErr(nil) != nil {
		t.Error("domainErr(nil) should return nil")
	}
}

func TestParseDirection(t *testing.T) {
	tests := []struct {
		input   string
		want    ledger.Direction
		wantErr bool
	}{
		{"DEBIT", ledger.Debit, false},
		{"CREDIT", ledger.Credit, false},
		{"debit", 0, true},
		{"", 0, true},
		{"INVALID", 0, true},
	}
	for _, tc := range tests {
		got, err := parseDirection(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseDirection(%q): want error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseDirection(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("parseDirection(%q): got %v, want %v", tc.input, got, tc.want)
			}
		}
	}
}
