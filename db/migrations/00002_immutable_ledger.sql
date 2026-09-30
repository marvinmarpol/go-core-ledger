-- +goose Up

-- Rejects any UPDATE or DELETE on the immutable ledger tables.
-- Corrections must be entered as new reversing journal entries.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION deny_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'mutations on % are forbidden; use a reversing journal entry (reverses_id)', TG_TABLE_NAME;
END;
$$;
-- +goose StatementEnd

CREATE TABLE journal_entries (
    id              TEXT        PRIMARY KEY,
    idempotency_key TEXT        NOT NULL,
    business_date   DATE        NOT NULL,
    value_date      DATE        NOT NULL,
    booked_at       TIMESTAMPTZ NOT NULL,
    reverses_id     TEXT        REFERENCES journal_entries (id),
    external_ref    TEXT,
    metadata        JSONB       NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX journal_entries_idempotency_key_idx ON journal_entries (idempotency_key);
CREATE INDEX journal_entries_external_ref_idx ON journal_entries (external_ref) WHERE external_ref IS NOT NULL;

CREATE TRIGGER journal_entries_immutable
    BEFORE UPDATE OR DELETE ON journal_entries
    FOR EACH ROW EXECUTE FUNCTION deny_mutation();


CREATE TYPE posting_direction AS ENUM ('DEBIT', 'CREDIT');

CREATE TABLE postings (
    id         TEXT             PRIMARY KEY,
    entry_id   TEXT             NOT NULL REFERENCES journal_entries (id),
    account_id TEXT             NOT NULL REFERENCES accounts (id),
    amount     BIGINT           NOT NULL CHECK (amount > 0),
    direction  posting_direction NOT NULL,
    currency   TEXT             NOT NULL,
    created_at TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

CREATE INDEX postings_entry_id_idx   ON postings (entry_id);
CREATE INDEX postings_account_id_idx ON postings (account_id);

CREATE TRIGGER postings_immutable
    BEFORE UPDATE OR DELETE ON postings
    FOR EACH ROW EXECUTE FUNCTION deny_mutation();

-- +goose Down
DROP TRIGGER IF EXISTS postings_immutable ON postings;
DROP TRIGGER IF EXISTS journal_entries_immutable ON journal_entries;
DROP TABLE postings;
DROP TYPE posting_direction;
DROP TABLE journal_entries;
DROP FUNCTION deny_mutation;
