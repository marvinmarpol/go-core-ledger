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

CREATE TABLE
    journal_entries (
        id UUID PRIMARY KEY NOT NULL DEFAULT gen_random_uuid (),
        idempotency_key CHARACTER VARYING(64) NOT NULL,
        business_date DATE NOT NULL,
        value_date DATE NOT NULL,
        booked_at TIMESTAMPTZ NOT NULL,
        reverses_id UUID REFERENCES journal_entries (id),
        external_ref CHARACTER VARYING(64),
        metadata JSONB NOT NULL DEFAULT '{}',
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW ()
    );

CREATE UNIQUE INDEX idx_unq_journal_entries_idempotency_key ON journal_entries (idempotency_key);

CREATE INDEX idx_journal_entries_external_ref ON journal_entries (external_ref)
WHERE
    external_ref IS NOT NULL;

CREATE TRIGGER journal_entries_immutable BEFORE
UPDATE
OR DELETE ON journal_entries FOR EACH ROW EXECUTE FUNCTION deny_mutation ();

CREATE TYPE posting_direction AS ENUM ('DEBIT', 'CREDIT');

CREATE TABLE
    postings (
        id UUID PRIMARY KEY NOT NULL DEFAULT gen_random_uuid (),
        entry_id UUID NOT NULL REFERENCES journal_entries (id),
        account_id UUID NOT NULL REFERENCES accounts (id),
        amount BIGINT NOT NULL CHECK (amount > 0),
        direction posting_direction NOT NULL,
        currency CHARACTER(3) NOT NULL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW ()
    );

CREATE INDEX idx_postings_entry_id ON postings (entry_id);

CREATE INDEX idx_postings_account_id ON postings (account_id);

CREATE TRIGGER postings_immutable BEFORE
UPDATE
OR DELETE ON postings FOR EACH ROW EXECUTE FUNCTION deny_mutation ();

-- +goose Down
DROP TRIGGER IF EXISTS postings_immutable ON postings;

DROP TRIGGER IF EXISTS journal_entries_immutable ON journal_entries;

DROP INDEX IF EXISTS idx_unq_journal_entries_idempotency_key;

DROP INDEX IF EXISTS idx_journal_entries_external_ref;

DROP INDEX IF EXISTS idx_postings_entry_id;

DROP INDEX IF EXISTS idx_postings_account_id;

DROP INDEX IF EXISTS idx_postings_account_id;

DROP TABLE postings;

DROP TABLE journal_entries;

DROP TYPE posting_direction;

DROP FUNCTION deny_mutation;
