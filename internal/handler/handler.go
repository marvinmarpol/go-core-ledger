// Package handler implements the Connect RPC handlers for the LedgerService.
package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "go-core-ledger/api/gen/ledger/v1"
	"go-core-ledger/api/gen/ledger/v1/ledgerv1connect"
	"go-core-ledger/internal/holds"
	"go-core-ledger/internal/ledger"
	"go-core-ledger/internal/money"
	"go-core-ledger/internal/posting"
	"go-core-ledger/internal/uid"
)

// AccountService covers account and entry read/write operations used by the handler.
// *store.Store satisfies this interface; define it here so the handler does not
// import the storage package directly.
type AccountService interface {
	InsertAccount(ctx context.Context, acct ledger.Account) (ledger.Account, error)
	GetAccount(ctx context.Context, id string) (ledger.Account, error)
	GetJournalEntry(ctx context.Context, id string) (ledger.Entry, error)
	GetPostingsByEntryID(ctx context.Context, entryID string) ([]ledger.Posting, error)
	GetPostingsByAccountID(ctx context.Context, accountID string) ([]ledger.Posting, error)
}

// Handler implements ledgerv1connect.LedgerServiceHandler.
type Handler struct {
	ledgerv1connect.UnimplementedLedgerServiceHandler
	accounts AccountService
	posting  posting.Poster
	holds    holds.HoldManager
	clock    ledger.Clock
}

// New creates a Handler wired to the provided services.
func New(
	accounts AccountService,
	postSvc posting.Poster,
	holdsSvc holds.HoldManager,
	clock ledger.Clock,
) *Handler {
	return &Handler{
		accounts: accounts,
		posting:  postSvc,
		holds:    holdsSvc,
		clock:    clock,
	}
}

// ── Accounts ──────────────────────────────────────────────────────────────────

// CreateAccount creates a new ledger account.
func (h *Handler) CreateAccount(
	ctx context.Context,
	req *connect.Request[v1.CreateAccountRequest],
) (*connect.Response[v1.CreateAccountResponse], error) {
	msg := req.Msg

	if msg.GetCurrency() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("currency is required"))
	}

	zeroAmt, err := money.New(0, msg.GetCurrency())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid currency: %w", err))
	}

	floor := zeroAmt
	if f := msg.GetFloor(); f != nil {
		if f.GetCurrency() != "" && f.GetCurrency() != msg.GetCurrency() {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("floor currency must match account currency"))
		}
		floor, err = money.New(f.GetValue(), msg.GetCurrency())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid floor: %w", err))
		}
	}

	acct := ledger.Account{
		ID:            uid.New(),
		Currency:      msg.GetCurrency(),
		Balance:       zeroAmt,
		HeldAmount:    zeroAmt,
		Floor:         floor,
		AllowNegative: msg.GetAllowNegative(),
		Version:       1,
		ExternalRef:   msg.GetExternalRef(),
	}

	stored, err := h.accounts.InsertAccount(ctx, acct)
	if err != nil {
		return nil, domainErr(err)
	}
	return connect.NewResponse(&v1.CreateAccountResponse{
		Account: toProtoAccount(stored),
	}), nil
}

// GetAccount fetches an account by ID.
func (h *Handler) GetAccount(
	ctx context.Context,
	req *connect.Request[v1.GetAccountRequest],
) (*connect.Response[v1.GetAccountResponse], error) {
	if req.Msg.GetId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("id is required"))
	}
	acct, err := h.accounts.GetAccount(ctx, req.Msg.GetId())
	if err != nil {
		return nil, domainErr(err)
	}
	return connect.NewResponse(&v1.GetAccountResponse{
		Account: toProtoAccount(acct),
	}), nil
}

// ── Postings ──────────────────────────────────────────────────────────────────

// PostEntry commits a balanced journal entry and its postings.
func (h *Handler) PostEntry(
	ctx context.Context,
	req *connect.Request[v1.PostEntryRequest],
) (*connect.Response[v1.PostEntryResponse], error) {
	msg := req.Msg
	if msg.GetIdempotencyKey() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("idempotency_key is required"))
	}
	if len(msg.GetPostings()) < 2 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("at least two posting legs are required"))
	}

	postings, err := fromProtoLegs(msg.GetPostings())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	bizDate := tsToTime(msg.GetBusinessDate())
	valueDate := tsToTime(msg.GetValueDate())

	result, err := h.posting.Post(ctx, posting.Request{
		Entry: ledger.Entry{
			ID:             uid.New(),
			IdempotencyKey: msg.GetIdempotencyKey(),
			BusinessDate:   bizDate,
			ValueDate:      valueDate,
			ExternalRef:    msg.GetExternalRef(),
			Metadata:       msg.GetMetadata(),
		},
		Postings: postings,
	})
	if err != nil {
		return nil, domainErr(err)
	}
	return connect.NewResponse(&v1.PostEntryResponse{
		Entry:    toProtoEntry(result.Entry),
		Postings: toProtoPostings(result.Postings),
	}), nil
}

// GetEntry fetches a journal entry and its postings by entry ID.
func (h *Handler) GetEntry(
	ctx context.Context,
	req *connect.Request[v1.GetEntryRequest],
) (*connect.Response[v1.GetEntryResponse], error) {
	if req.Msg.GetId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("id is required"))
	}
	entry, err := h.accounts.GetJournalEntry(ctx, req.Msg.GetId())
	if err != nil {
		return nil, domainErr(err)
	}
	postings, err := h.accounts.GetPostingsByEntryID(ctx, entry.ID)
	if err != nil {
		return nil, domainErr(err)
	}
	return connect.NewResponse(&v1.GetEntryResponse{
		Entry:    toProtoEntry(entry),
		Postings: toProtoPostings(postings),
	}), nil
}

// GetPostingsByAccount returns all postings for an account.
func (h *Handler) GetPostingsByAccount(
	ctx context.Context,
	req *connect.Request[v1.GetPostingsByAccountRequest],
) (*connect.Response[v1.GetPostingsByAccountResponse], error) {
	if req.Msg.GetAccountId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("account_id is required"))
	}
	ps, err := h.accounts.GetPostingsByAccountID(ctx, req.Msg.GetAccountId())
	if err != nil {
		return nil, domainErr(err)
	}
	return connect.NewResponse(&v1.GetPostingsByAccountResponse{
		Postings: toProtoPostings(ps),
	}), nil
}

// ── Holds ─────────────────────────────────────────────────────────────────────

// ReserveHold creates an active hold against an account's available balance.
func (h *Handler) ReserveHold(
	ctx context.Context,
	req *connect.Request[v1.ReserveHoldRequest],
) (*connect.Response[v1.ReserveHoldResponse], error) {
	msg := req.Msg
	if msg.GetAccountId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("account_id is required"))
	}
	amt, err := fromProtoMoney(msg.GetAmount())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	hold, err := h.holds.Reserve(ctx, holds.ReserveRequest{
		AccountID:   msg.GetAccountId(),
		Amount:      amt,
		ExpiresAt:   tsToTime(msg.GetExpiresAt()),
		ExternalRef: msg.GetExternalRef(),
	})
	if err != nil {
		return nil, domainErr(err)
	}
	return connect.NewResponse(&v1.ReserveHoldResponse{
		Hold: toProtoHold(hold),
	}), nil
}

// CaptureHold atomically converts an active hold into a posted journal entry.
func (h *Handler) CaptureHold(
	ctx context.Context,
	req *connect.Request[v1.CaptureHoldRequest],
) (*connect.Response[v1.CaptureHoldResponse], error) {
	msg := req.Msg
	if msg.GetHoldId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("hold_id is required"))
	}
	if msg.GetIdempotencyKey() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("idempotency_key is required"))
	}
	if msg.GetCreditAccountId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("credit_account_id is required"))
	}

	result, err := h.holds.Capture(ctx, holds.CaptureRequest{
		HoldID:          msg.GetHoldId(),
		IdempotencyKey:  msg.GetIdempotencyKey(),
		BusinessDate:    tsToTime(msg.GetBusinessDate()),
		ValueDate:       tsToTime(msg.GetValueDate()),
		ExternalRef:     msg.GetExternalRef(),
		Metadata:        msg.GetMetadata(),
		CreditAccountID: msg.GetCreditAccountId(),
	})
	if err != nil {
		return nil, domainErr(err)
	}
	return connect.NewResponse(&v1.CaptureHoldResponse{
		Entry:    toProtoEntry(result.Entry),
		Postings: toProtoPostings(result.Postings),
	}), nil
}

// VoidHold releases an active hold without posting a debit.
func (h *Handler) VoidHold(
	ctx context.Context,
	req *connect.Request[v1.VoidHoldRequest],
) (*connect.Response[v1.VoidHoldResponse], error) {
	if req.Msg.GetHoldId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("hold_id is required"))
	}
	hold, err := h.holds.Void(ctx, req.Msg.GetHoldId())
	if err != nil {
		return nil, domainErr(err)
	}
	return connect.NewResponse(&v1.VoidHoldResponse{
		Hold: toProtoHold(hold),
	}), nil
}

// ── Domain → proto mapping ────────────────────────────────────────────────────

func toProtoMoney(a money.Amount) *v1.Money {
	return &v1.Money{Value: a.Value(), Currency: a.Currency()}
}

func toProtoAccount(a ledger.Account) *v1.Account {
	return &v1.Account{
		Id:            a.ID,
		Currency:      a.Currency,
		Balance:       toProtoMoney(a.Balance),
		HeldAmount:    toProtoMoney(a.HeldAmount),
		Floor:         toProtoMoney(a.Floor),
		AllowNegative: a.AllowNegative,
		Version:       a.Version,
		ExternalRef:   a.ExternalRef,
	}
}

func toProtoEntry(e ledger.Entry) *v1.JournalEntry {
	return &v1.JournalEntry{
		Id:             e.ID,
		IdempotencyKey: e.IdempotencyKey,
		BusinessDate:   timestamppb.New(e.BusinessDate),
		ValueDate:      timestamppb.New(e.ValueDate),
		BookedAt:       timestamppb.New(e.BookedAt),
		ReversesId:     e.ReversesID,
		ExternalRef:    e.ExternalRef,
		Metadata:       e.Metadata,
	}
}

func toProtoPosting(p ledger.Posting) *v1.Posting {
	return &v1.Posting{
		Id:        p.ID,
		EntryId:   p.EntryID,
		AccountId: p.AccountID,
		Amount:    toProtoMoney(p.Amount),
		Direction: p.Direction.String(),
	}
}

func toProtoPostings(ps []ledger.Posting) []*v1.Posting {
	out := make([]*v1.Posting, len(ps))
	for i, p := range ps {
		out[i] = toProtoPosting(p)
	}
	return out
}

func toProtoHold(h ledger.Hold) *v1.Hold {
	ph := &v1.Hold{
		Id:          h.ID,
		AccountId:   h.AccountID,
		Amount:      toProtoMoney(h.Amount),
		Status:      string(h.Status),
		ExternalRef: h.ExternalRef,
	}
	if !h.ExpiresAt.IsZero() {
		ph.ExpiresAt = timestamppb.New(h.ExpiresAt)
	}
	return ph
}

// ── Proto → domain mapping ────────────────────────────────────────────────────

func fromProtoMoney(m *v1.Money) (money.Amount, error) {
	if m == nil {
		return money.Amount{}, fmt.Errorf("amount is required")
	}
	if m.GetCurrency() == "" {
		return money.Amount{}, fmt.Errorf("amount.currency is required")
	}
	if m.GetValue() <= 0 {
		return money.Amount{}, fmt.Errorf("amount.value must be positive")
	}
	amt, err := money.New(m.GetValue(), m.GetCurrency())
	if err != nil {
		return money.Amount{}, fmt.Errorf("invalid amount: %w", err)
	}
	return amt, nil
}

func fromProtoLegs(legs []*v1.PostingLeg) ([]ledger.Posting, error) {
	out := make([]ledger.Posting, len(legs))
	for i, leg := range legs {
		amt, err := fromProtoMoney(leg.GetAmount())
		if err != nil {
			return nil, fmt.Errorf("posting[%d]: %w", i, err)
		}
		dir, err := parseDirection(leg.GetDirection())
		if err != nil {
			return nil, fmt.Errorf("posting[%d]: %w", i, err)
		}
		out[i] = ledger.Posting{
			AccountID: leg.GetAccountId(),
			Amount:    amt,
			Direction: dir,
		}
	}
	return out, nil
}

func parseDirection(s string) (ledger.Direction, error) {
	switch s {
	case "DEBIT":
		return ledger.Debit, nil
	case "CREDIT":
		return ledger.Credit, nil
	default:
		return 0, fmt.Errorf("direction must be DEBIT or CREDIT, got %q", s)
	}
}

// tsToTime converts a protobuf Timestamp to time.Time; zero-value for nil.
func tsToTime(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

// ── Error mapping ─────────────────────────────────────────────────────────────

// domainErr translates domain sentinel errors into Connect status codes.
func domainErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ledger.ErrAccountNotFound),
		errors.Is(err, ledger.ErrEntryNotFound),
		errors.Is(err, ledger.ErrHoldNotFound):
		return connect.NewError(connect.CodeNotFound, err)

	case errors.Is(err, ledger.ErrInsufficientFunds),
		errors.Is(err, ledger.ErrUnbalancedEntry),
		errors.Is(err, ledger.ErrEmptyPostings),
		errors.Is(err, ledger.ErrInvalidDirection):
		return connect.NewError(connect.CodeInvalidArgument, err)

	case errors.Is(err, ledger.ErrDuplicateIdempotencyKey):
		// Already committed — caller should read the existing result.
		return connect.NewError(connect.CodeAlreadyExists, err)

	case errors.Is(err, ledger.ErrVersionConflict):
		return connect.NewError(connect.CodeAborted, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
