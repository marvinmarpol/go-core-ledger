-- +goose Up
CREATE TABLE
    outbox_events (
        id UUID PRIMARY KEY NOT NULL DEFAULT gen_random_uuid (),
        aggregate_type CHARACTER VARYING(64) NOT NULL,
        aggregate_id CHARACTER VARYING(64) NOT NULL,
        event_type CHARACTER VARYING(64) NOT NULL,
        payload JSONB NOT NULL,
        published_at TIMESTAMPTZ,
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW ()
    );

-- Relay worker query: unpublished events in insertion order
CREATE INDEX idx_outbox_events_unpublished ON outbox_events (created_at)
WHERE
    published_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_outbox_events_unpublished;

DROP TABLE outbox_events;
