-- +goose Up
-- Run this once per database to enable the extension
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE
    accounts (
        id UUID PRIMARY KEY NOT NULL DEFAULT gen_random_uuid (),
        currency CHARACTER(3) NOT NULL,
        balance BIGINT NOT NULL DEFAULT 0,
        held_amount BIGINT NOT NULL DEFAULT 0,
        floor BIGINT NOT NULL DEFAULT 0,
        allow_negative BOOLEAN NOT NULL DEFAULT FALSE,
        version BIGINT NOT NULL DEFAULT 0,
        external_ref CHARACTER VARYING(64),
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW (),
        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW ()
    );

-- external reference lookup
CREATE INDEX idx_accounts_external_ref ON accounts (external_ref)
WHERE
    external_ref IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_accounts_external_ref;

DROP TABLE accounts;
