-- +goose Up
CREATE TYPE hold_status AS ENUM ('active', 'captured', 'voided', 'expired');

CREATE TABLE
    holds (
        id UUID PRIMARY KEY NOT NULL DEFAULT gen_random_uuid (),
        account_id UUID NOT NULL REFERENCES accounts (id),
        amount BIGINT NOT NULL CHECK (amount > 0),
        currency CHARACTER(3) NOT NULL,
        status hold_status NOT NULL DEFAULT 'active',
        expires_at TIMESTAMPTZ,
        external_ref CHARACTER VARYING(64),
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW (),
        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW ()
    );

CREATE INDEX idx_holds_account_id ON holds (account_id);

CREATE INDEX idx_holds_account_id_status ON holds (account_id, status);

CREATE INDEX idx_holds_expires_at ON holds (expires_at)
WHERE
    status = 'active';

-- +goose Down
DROP INDEX IF EXISTS idx_holds_account_id;

DROP INDEX IF EXISTS idx_holds_account_id_status;

DROP INDEX IF EXISTS idx_holds_expires_at;

DROP TABLE holds;

DROP TYPE hold_status;
