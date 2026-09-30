-- name: GetAccount :one
SELECT * FROM accounts WHERE id = $1;

-- name: GetAccountForUpdate :one
SELECT * FROM accounts WHERE id = $1 FOR UPDATE;

-- name: InsertAccount :one
INSERT INTO accounts (id, currency, balance, held_amount, floor, allow_negative, version, external_ref)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- Optimistic lock via version check; returns updated row or no rows if version mismatch.
-- name: UpdateAccountBalance :one
UPDATE accounts
SET
    balance     = $2,
    held_amount = $3,
    version     = version + 1,
    updated_at  = NOW()
WHERE id = $1 AND version = $4
RETURNING *;

-- name: GetAccountByExternalRef :one
SELECT * FROM accounts WHERE external_ref = $1;
