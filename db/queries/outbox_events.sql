-- name: InsertOutboxEvent :one
INSERT INTO outbox_events (id, aggregate_type, aggregate_id, event_type, payload)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- Relay worker: fetch next batch of unpublished events in insertion order.
-- name: GetUnpublishedOutboxEvents :many
SELECT * FROM outbox_events
WHERE published_at IS NULL
ORDER BY created_at
LIMIT $1;

-- name: MarkOutboxEventPublished :one
UPDATE outbox_events
SET published_at = NOW()
WHERE id = $1
RETURNING *;
