-- +goose Up

-- Run this once per database to enable the extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE accounts (
    id             TEXT        PRIMARY KEY,
    currency       TEXT        NOT NULL,
    balance        BIGINT      NOT NULL DEFAULT 0,
    held_amount    BIGINT      NOT NULL DEFAULT 0,
    floor          BIGINT      NOT NULL DEFAULT 0,
    allow_negative BOOLEAN     NOT NULL DEFAULT FALSE,
    version        BIGINT      NOT NULL DEFAULT 0,
    external_ref   TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- external reference lookup
CREATE INDEX accounts_external_ref_idx ON accounts (external_ref) WHERE external_ref IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS accounts_external_ref_idx;
DROP TABLE accounts;
