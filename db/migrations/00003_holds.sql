-- +goose Up
CREATE TYPE hold_status AS ENUM ('active', 'captured', 'voided', 'expired');

CREATE TABLE holds (
    id           TEXT        PRIMARY KEY,
    account_id   TEXT        NOT NULL REFERENCES accounts (id),
    amount       BIGINT      NOT NULL CHECK (amount > 0),
    currency     TEXT        NOT NULL,
    status       hold_status NOT NULL DEFAULT 'active',
    expires_at   TIMESTAMPTZ,
    external_ref TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX holds_account_id_idx        ON holds (account_id);
CREATE INDEX holds_account_id_status_idx ON holds (account_id, status);
CREATE INDEX holds_expires_at_idx        ON holds (expires_at) WHERE status = 'active';

-- +goose Down
DROP TABLE holds;
DROP TYPE hold_status;
