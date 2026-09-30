-- name: InsertPosting :one
INSERT INTO postings (id, entry_id, account_id, amount, direction, currency)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetPostingsByEntryID :many
SELECT * FROM postings WHERE entry_id = $1 ORDER BY created_at;

-- name: GetPostingsByAccountID :many
SELECT * FROM postings WHERE account_id = $1 ORDER BY created_at;
