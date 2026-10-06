// Package store provides domain-typed repository adapters over the sqlc-generated Queries.
package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"go-core-ledger/internal/ledger"
	"go-core-ledger/internal/money"
)

// pgUniqueViolation is the PostgreSQL error code for unique_violation.
const pgUniqueViolation = "23505"

// Store wraps the sqlc-generated Queries and exposes domain-typed methods.
// It accepts DBTX so the same struct works with a pool connection or inside
// a pgx.Tx (required by the 6-step posting protocol).
type Store struct {
	q *Queries
}

// NewStore returns a Store backed by db, which may be a *pgxpool.Pool or pgx.Tx.
func NewStore(db DBTX) *Store {
	return &Store{q: New(db)}
}

// WithTx returns a new Store scoped to tx. Use inside the posting transaction.
func (s *Store) WithTx(tx pgx.Tx) *Store {
	return &Store{q: s.q.WithTx(tx)}
}

// ── Accounts ─────────────────────────────────────────────────────────────────

// GetAccount fetches an account by ID.
func (s *Store) GetAccount(ctx context.Context, id string) (ledger.Account, error) {
	uuid, err := parseUUID(id)
	if err != nil {
		return ledger.Account{}, ledger.ErrParseUUID
	}
	row, err := s.q.GetAccount(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledger.Account{}, ledger.ErrAccountNotFound
	}
	if err != nil {
		return ledger.Account{}, fmt.Errorf("get account: %w", err)
	}
	return toAccount(row)
}

// GetAccountByExternalRef fetches an account by its external reference ID.
func (s *Store) GetAccountByExternalRef(ctx context.Context, externalRef string) (ledger.Account, error) {
	row, err := s.q.GetAccountByExternalRef(ctx, pgtype.Text{String: externalRef, Valid: externalRef != ""})
	if errors.Is(err, pgx.ErrNoRows) {
		return ledger.Account{}, ledger.ErrAccountNotFound
	}
	if err != nil {
		return ledger.Account{}, fmt.Errorf("get account by external ref: %w", err)
	}
	return toAccount(row)
}

// GetAccountForUpdate locks the account row (SELECT … FOR UPDATE).
// Must be called inside a transaction with rows ordered by account_id to prevent deadlocks.
func (s *Store) GetAccountForUpdate(ctx context.Context, id string) (ledger.Account, error) {
	uuid, err := parseUUID(id)
	if err != nil {
		return ledger.Account{}, ledger.ErrParseUUID
	}
	row, err := s.q.GetAccountForUpdate(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledger.Account{}, ledger.ErrAccountNotFound
	}
	if err != nil {
		return ledger.Account{}, fmt.Errorf("get account for update: %w", err)
	}
	return toAccount(row)
}

// InsertAccount persists a new account row and returns the stored record.
func (s *Store) InsertAccount(ctx context.Context, a ledger.Account) (ledger.Account, error) {
	uuid, err := parseUUID(a.ID)
	if err != nil {
		return ledger.Account{}, ledger.ErrParseUUID
	}
	row, err := s.q.InsertAccount(ctx, InsertAccountParams{
		ID:            uuid,
		Currency:      a.Currency,
		Balance:       a.Balance.Value(),
		HeldAmount:    a.HeldAmount.Value(),
		Floor:         a.Floor.Value(),
		AllowNegative: a.AllowNegative,
		Version:       a.Version,
		ExternalRef:   textOrNull(a.ExternalRef),
	})
	if err != nil {
		return ledger.Account{}, fmt.Errorf("insert account: %w", err)
	}
	return toAccount(row)
}

// UpdateAccountBalance applies new balance and held_amount only when the stored
// version matches a.Version (optimistic lock). Returns ErrVersionConflict on mismatch.
func (s *Store) UpdateAccountBalance(ctx context.Context, a ledger.Account) (ledger.Account, error) {
	uuid, err := parseUUID(a.ID)
	if err != nil {
		return ledger.Account{}, ledger.ErrParseUUID
	}
	row, err := s.q.UpdateAccountBalance(ctx, UpdateAccountBalanceParams{
		ID:         uuid,
		Balance:    a.Balance.Value(),
		HeldAmount: a.HeldAmount.Value(),
		Version:    a.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ledger.Account{}, ledger.ErrVersionConflict
	}
	if err != nil {
		return ledger.Account{}, fmt.Errorf("update account balance: %w", err)
	}
	return toAccount(row)
}

// ── Journal entries ───────────────────────────────────────────────────────────

// GetJournalEntry fetches a journal entry by ID.
func (s *Store) GetJournalEntry(ctx context.Context, id string) (ledger.Entry, error) {
	uuid, err := parseUUID(id)
	if err != nil {
		return ledger.Entry{}, ledger.ErrParseUUID
	}
	row, err := s.q.GetJournalEntry(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledger.Entry{}, ledger.ErrEntryNotFound
	}
	if err != nil {
		return ledger.Entry{}, fmt.Errorf("get journal entry: %w", err)
	}
	return toEntry(row)
}

// GetJournalEntryByIdempotencyKey fetches a journal entry by idempotency key.
func (s *Store) GetJournalEntryByIdempotencyKey(ctx context.Context, key string) (ledger.Entry, error) {
	row, err := s.q.GetJournalEntryByIdempotencyKey(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledger.Entry{}, ledger.ErrEntryNotFound
	}
	if err != nil {
		return ledger.Entry{}, fmt.Errorf("get journal entry by idempotency key: %w", err)
	}
	return toEntry(row)
}

// InsertJournalEntry inserts a new journal entry. Returns ErrDuplicateIdempotencyKey
// when a unique constraint on idempotency_key is violated (concurrent retry).
func (s *Store) InsertJournalEntry(ctx context.Context, e ledger.Entry) (ledger.Entry, error) {
	uuid, err := parseUUID(e.ID)
	if err != nil {
		return ledger.Entry{}, ledger.ErrParseUUID
	}
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		return ledger.Entry{}, fmt.Errorf("marshal entry metadata: %w", err)
	}
	row, err := s.q.InsertJournalEntry(ctx, InsertJournalEntryParams{
		ID:             uuid,
		IdempotencyKey: e.IdempotencyKey,
		BusinessDate:   pgtype.Date{Time: e.BusinessDate, Valid: true},
		ValueDate:      pgtype.Date{Time: e.ValueDate, Valid: true},
		BookedAt:       pgtype.Timestamptz{Time: e.BookedAt, Valid: true},
		ReversesID:     uuidOrNull(e.ReversesID),
		ExternalRef:    textOrNull(e.ExternalRef),
		Metadata:       meta,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return ledger.Entry{}, ledger.ErrDuplicateIdempotencyKey
		}
		return ledger.Entry{}, fmt.Errorf("insert journal entry: %w", err)
	}
	return toEntry(row)
}

// ── Postings ──────────────────────────────────────────────────────────────────

// InsertPosting persists a new posting leg and returns the stored record.
func (s *Store) InsertPosting(ctx context.Context, p ledger.Posting) (ledger.Posting, error) {
	id, err := parseUUID(p.ID)
	if err != nil {
		return ledger.Posting{}, ledger.ErrParseUUID
	}
	entryID, err := parseUUID(p.EntryID)
	if err != nil {
		return ledger.Posting{}, ledger.ErrParseUUID
	}
	accountID, err := parseUUID(p.AccountID)
	if err != nil {
		return ledger.Posting{}, ledger.ErrParseUUID
	}
	dir, err := fromDirection(p.Direction)
	if err != nil {
		return ledger.Posting{}, err
	}
	row, err := s.q.InsertPosting(ctx, InsertPostingParams{
		ID:        id,
		EntryID:   entryID,
		AccountID: accountID,
		Amount:    p.Amount.Value(),
		Direction: dir,
		Currency:  p.Amount.Currency(),
	})
	if err != nil {
		return ledger.Posting{}, fmt.Errorf("insert posting: %w", err)
	}
	return toPosting(row)
}

// GetPostingsByAccountID returns all postings for an account, ordered by created_at.
func (s *Store) GetPostingsByAccountID(ctx context.Context, accountID string) ([]ledger.Posting, error) {
	uuid, err := parseUUID(accountID)
	if err != nil {
		return nil, ledger.ErrParseUUID
	}
	rows, err := s.q.GetPostingsByAccountID(ctx, uuid)
	if err != nil {
		return nil, fmt.Errorf("get postings by account: %w", err)
	}
	return mapPostings(rows)
}

// GetPostingsByEntryID returns all postings for a journal entry, ordered by created_at.
func (s *Store) GetPostingsByEntryID(ctx context.Context, entryID string) ([]ledger.Posting, error) {
	uuid, err := parseUUID(entryID)
	if err != nil {
		return nil, ledger.ErrParseUUID
	}
	rows, err := s.q.GetPostingsByEntryID(ctx, uuid)
	if err != nil {
		return nil, fmt.Errorf("get postings by entry: %w", err)
	}
	return mapPostings(rows)
}

// ── Holds ─────────────────────────────────────────────────────────────────────

// InsertHold persists a new hold and returns the stored record.
func (s *Store) InsertHold(ctx context.Context, h ledger.Hold) (ledger.Hold, error) {
	id, err := parseUUID(h.ID)
	if err != nil {
		return ledger.Hold{}, ledger.ErrParseUUID
	}
	accountID, err := parseUUID(h.AccountID)
	if err != nil {
		return ledger.Hold{}, ledger.ErrParseUUID
	}
	row, err := s.q.InsertHold(ctx, InsertHoldParams{
		ID:          id,
		AccountID:   accountID,
		Amount:      h.Amount.Value(),
		Currency:    h.Amount.Currency(),
		ExpiresAt:   timeOrNull(h.ExpiresAt),
		ExternalRef: textOrNull(h.ExternalRef),
	})
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("insert hold: %w", err)
	}
	return toHold(row)
}

// GetHold fetches a hold by ID without locking.
func (s *Store) GetHold(ctx context.Context, id string) (ledger.Hold, error) {
	uuid, err := parseUUID(id)
	if err != nil {
		return ledger.Hold{}, ledger.ErrParseUUID
	}
	row, err := s.q.GetHold(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledger.Hold{}, ledger.ErrHoldNotFound
	}
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("get hold: %w", err)
	}
	return toHold(row)
}

// GetHoldForUpdate locks the hold row (SELECT … FOR UPDATE). Must be called inside a transaction.
func (s *Store) GetHoldForUpdate(ctx context.Context, id string) (ledger.Hold, error) {
	uuid, err := parseUUID(id)
	if err != nil {
		return ledger.Hold{}, ledger.ErrParseUUID
	}
	row, err := s.q.GetHoldForUpdate(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledger.Hold{}, ledger.ErrHoldNotFound
	}
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("get hold for update: %w", err)
	}
	return toHold(row)
}

// GetActiveHoldsByAccountID returns all active holds for an account, ordered by created_at.
func (s *Store) GetActiveHoldsByAccountID(ctx context.Context, accountID string) ([]ledger.Hold, error) {
	uuid, err := parseUUID(accountID)
	if err != nil {
		return nil, ledger.ErrParseUUID
	}
	rows, err := s.q.GetActiveHoldsByAccountID(ctx, uuid)
	if err != nil {
		return nil, fmt.Errorf("get active holds: %w", err)
	}
	return mapHolds(rows)
}

// GetExpiredHolds returns up to limit active holds whose expiry has passed. Used by the expiry worker.
func (s *Store) GetExpiredHolds(ctx context.Context, limit int32) ([]ledger.Hold, error) {
	rows, err := s.q.GetExpiredHolds(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("get expired holds: %w", err)
	}
	return mapHolds(rows)
}

// UpdateHoldStatus transitions a hold to the given status.
func (s *Store) UpdateHoldStatus(ctx context.Context, id string, status ledger.HoldStatus) (ledger.Hold, error) {
	row, err := s.q.UpdateHoldStatus(ctx, UpdateHoldStatusParams{
		ID:     uuidOrNull(id),
		Status: HoldStatus(status),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ledger.Hold{}, ledger.ErrHoldNotFound
	}
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("update hold status: %w", err)
	}
	return toHold(row)
}

// ── Outbox events ─────────────────────────────────────────────────────────────

// OutboxEventInput carries parameters for inserting a new outbox event.
// ID must be a UUID v4 string from uid.New().
type OutboxEventInput struct {
	ID            string
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       json.RawMessage
}

// InsertOutboxEvent inserts a new unpublished outbox event.
func (s *Store) InsertOutboxEvent(ctx context.Context, in OutboxEventInput) (OutboxEvent, error) {
	id, err := parseUUID(in.ID)
	if err != nil {
		return OutboxEvent{}, fmt.Errorf("insert outbox event: %w", err)
	}
	row, err := s.q.InsertOutboxEvent(ctx, InsertOutboxEventParams{
		ID:            id,
		AggregateType: in.AggregateType,
		AggregateID:   in.AggregateID,
		EventType:     in.EventType,
		Payload:       in.Payload,
	})
	if err != nil {
		return OutboxEvent{}, fmt.Errorf("insert outbox event: %w", err)
	}
	return row, nil
}

// GetUnpublishedOutboxEvents returns up to limit undelivered events in insertion order. Used by the relay worker.
func (s *Store) GetUnpublishedOutboxEvents(ctx context.Context, limit int32) ([]OutboxEvent, error) {
	rows, err := s.q.GetUnpublishedOutboxEvents(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("get unpublished outbox events: %w", err)
	}
	return rows, nil
}

// MarkOutboxEventPublished stamps published_at = NOW() on the given event.
// id must be a UUID v4 string matching an outbox_events row.
func (s *Store) MarkOutboxEventPublished(ctx context.Context, id string) (OutboxEvent, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return OutboxEvent{}, fmt.Errorf("mark outbox event published: %w", err)
	}
	row, err := s.q.MarkOutboxEventPublished(ctx, uid)
	if err != nil {
		return OutboxEvent{}, fmt.Errorf("mark outbox event published: %w", err)
	}
	return row, nil
}

// OutboxEventIDString formats the pgtype.UUID of an OutboxEvent into a UUID v4 string.
func OutboxEventIDString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// parseUUID converts a UUID v4 string (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx) to pgtype.UUID.
func parseUUID(s string) (pgtype.UUID, error) {
	s = strings.ReplaceAll(s, "-", "")
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return pgtype.UUID{}, fmt.Errorf("invalid UUID %q", s)
	}
	var arr [16]byte
	copy(arr[:], b)
	return pgtype.UUID{Bytes: arr, Valid: true}, nil
}

// ── Mapping helpers ───────────────────────────────────────────────────────────

func toAccount(row Account) (ledger.Account, error) {
	bal, err := money.New(row.Balance, row.Currency)
	if err != nil {
		return ledger.Account{}, fmt.Errorf("account balance: %w", err)
	}
	held, err := money.New(row.HeldAmount, row.Currency)
	if err != nil {
		return ledger.Account{}, fmt.Errorf("account held_amount: %w", err)
	}
	floor, err := money.New(row.Floor, row.Currency)
	if err != nil {
		return ledger.Account{}, fmt.Errorf("account floor: %w", err)
	}
	return ledger.Account{
		ID:            row.ID.String(),
		Currency:      row.Currency,
		Balance:       bal,
		HeldAmount:    held,
		Floor:         floor,
		AllowNegative: row.AllowNegative,
		Version:       row.Version,
		ExternalRef:   nullTextToString(row.ExternalRef),
	}, nil
}

func toEntry(row JournalEntry) (ledger.Entry, error) {
	var meta map[string]string
	if len(row.Metadata) > 0 {
		if err := json.Unmarshal(row.Metadata, &meta); err != nil {
			return ledger.Entry{}, fmt.Errorf("unmarshal entry metadata: %w", err)
		}
	}
	return ledger.Entry{
		ID:             row.ID.String(),
		IdempotencyKey: row.IdempotencyKey,
		BusinessDate:   row.BusinessDate.Time,
		ValueDate:      row.ValueDate.Time,
		BookedAt:       row.BookedAt.Time,
		ReversesID:     nullUUIDToString(row.ReversesID),
		ExternalRef:    nullTextToString(row.ExternalRef),
		Metadata:       meta,
	}, nil
}

func toPosting(row Posting) (ledger.Posting, error) {
	amt, err := money.New(row.Amount, row.Currency)
	if err != nil {
		return ledger.Posting{}, fmt.Errorf("posting amount: %w", err)
	}
	dir, err := toDirection(row.Direction)
	if err != nil {
		return ledger.Posting{}, err
	}
	return ledger.Posting{
		ID:        nullUUIDToString(row.ID),
		EntryID:   nullUUIDToString(row.EntryID),
		AccountID: nullUUIDToString(row.AccountID),
		Amount:    amt,
		Direction: dir,
	}, nil
}

func toHold(row Hold) (ledger.Hold, error) {
	amt, err := money.New(row.Amount, row.Currency)
	if err != nil {
		return ledger.Hold{}, fmt.Errorf("hold amount: %w", err)
	}
	var expiresAt time.Time
	if row.ExpiresAt.Valid {
		expiresAt = row.ExpiresAt.Time
	}
	return ledger.Hold{
		ID:          nullUUIDToString(row.ID),
		AccountID:   nullUUIDToString(row.AccountID),
		Amount:      amt,
		Status:      ledger.HoldStatus(row.Status),
		ExpiresAt:   expiresAt,
		ExternalRef: nullTextToString(row.ExternalRef),
	}, nil
}

func mapPostings(rows []Posting) ([]ledger.Posting, error) {
	out := make([]ledger.Posting, 0, len(rows))
	for _, row := range rows {
		p, err := toPosting(row)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func mapHolds(rows []Hold) ([]ledger.Hold, error) {
	out := make([]ledger.Hold, 0, len(rows))
	for _, row := range rows {
		h, err := toHold(row)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

func toDirection(d PostingDirection) (ledger.Direction, error) {
	switch d {
	case PostingDirectionDEBIT:
		return ledger.Debit, nil
	case PostingDirectionCREDIT:
		return ledger.Credit, nil
	default:
		return 0, fmt.Errorf("%w: %q", ledger.ErrInvalidDirection, d)
	}
}

func fromDirection(d ledger.Direction) (PostingDirection, error) {
	switch d {
	case ledger.Debit:
		return PostingDirectionDEBIT, nil
	case ledger.Credit:
		return PostingDirectionCREDIT, nil
	default:
		return "", fmt.Errorf("%w: %d", ledger.ErrInvalidDirection, d)
	}
}

func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func uuidOrNull(s string) pgtype.UUID {
	uuid, err := parseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return uuid
}

func nullTextToString(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

func nullUUIDToString(t pgtype.UUID) string {
	if !t.Valid {
		return ""
	}
	return t.String()
}

func timeOrNull(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}
