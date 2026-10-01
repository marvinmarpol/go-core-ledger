-- name: InsertHold :one
INSERT INTO holds (id, account_id, amount, currency, expires_at, external_ref)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetHold :one
SELECT * FROM holds WHERE id = $1;

-- name: GetHoldForUpdate :one
SELECT * FROM holds WHERE id = $1 FOR UPDATE;

-- name: GetActiveHoldsByAccountID :many
SELECT * FROM holds WHERE account_id = $1 AND status = 'active' ORDER BY created_at;

-- name: UpdateHoldStatus :one
UPDATE holds
SET status = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- Hold expiry worker: fetch expired active holds for processing.
-- name: GetExpiredHolds :many
SELECT * FROM holds
WHERE status = 'active' AND expires_at <= NOW()
ORDER BY expires_at
LIMIT $1;
