-- name: InsertJournalEntry :one
INSERT INTO journal_entries (id, idempotency_key, business_date, value_date, booked_at, reverses_id, external_ref, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetJournalEntryByIdempotencyKey :one
SELECT * FROM journal_entries WHERE idempotency_key = $1;

-- name: GetJournalEntry :one
SELECT * FROM journal_entries WHERE id = $1;
