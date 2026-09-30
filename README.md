# go-core-ledger

A double-entry core ledger service written in Go.

**Correctness over delivery speed.** A bug here creates or destroys money.

---

## What this service does

- Maintains accounts and their balances
- Records journal entries and immutable postings
- Enforces double-entry balance invariants on every write
- Manages fund holds (reserve / capture / void / expire)
- Publishes ledger events via transactional outbox

**Out of scope:** interest calculation, fee schedules, loan amortization — those belong in the product engine, which sends posting instructions here.

---

## Invariants

Every change must preserve all five:

1. Every journal entry balances — `sum(debits) == sum(credits)` per currency.
2. An account's balance equals the sum of its postings.
3. Postings and journal entries are immutable. Corrections are new reversing entries (`reverses_id`). `UPDATE`/`DELETE` on those tables is rejected at the database level.
4. A retried request with the same idempotency key produces exactly one journal entry.
5. Available balance (`balance − held`) never breaches the account's floor unless `allow_negative` is set.

---

## Stack

| Concern | Choice |
|---|---|
| Language | Go 1.26 |
| HTTP router | [chi v5](https://github.com/go-chi/chi) |
| Database | PostgreSQL via [pgx/v5](https://github.com/jackc/pgx) |
| Query generation | [sqlc](https://sqlc.dev) |
| Migrations | [goose v3](https://github.com/pressly/goose) |
| Dependency injection | [Wire](https://github.com/google/wire) |
| Linting | golangci-lint v2 |

No ORM. Queries are written in SQL (`db/queries/`) and generated into `internal/store/pg/`.

---

## Project layout

```
cmd/
  ledger-api/         API server entrypoint
  ledger-worker/      Outbox relay, hold expiry, verification jobs (planned)

internal/
  money/              Amount type — int64 minor units + ISO 4217 currency code
  ledger/             Domain: Account, Entry, Posting, Direction, Clock, invariant checks
  posting/            Posting protocol — 6-step transaction orchestration (planned)
  holds/              Reserve / capture / void / expire (planned)
  outbox/             Transactional outbox relay (planned)
  store/pg/           sqlc-generated queries + repository adapters
  app/                Application wiring and config

db/
  migrations/         Goose migration files
  queries/            sqlc input query files

api/
  proto/              gRPC/Connect API definitions (planned)

test/
  property/           Property-based invariant tests (planned)
  concurrency/        Parallel posting tests against real Postgres (planned)
```

---

## Local setup

### Prerequisites

- Go 1.26+
- PostgreSQL 14+
- `golangci-lint` v2 and `wire` (installed via `make setup`)

### 1. Install dev tools

```bash
make setup
```

### 2. Configure the database

Copy and edit the environment variables (or export them directly):

```bash
export LEDGER_DB_HOST=localhost
export LEDGER_DB_PORT=5432
export LEDGER_DB_USER=admin
export LEDGER_DB_PASSWORD=admin123
export LEDGER_DB_NAME=core_ledger
```

Alternatively, create a `.env` file at the project root — the service reads it automatically.

### 3. Run migrations

```bash
make migrate-up
```

### 4. Run the server

```bash
make run
```

---

## Development commands

| Command | Description |
|---|---|
| `make test` | Unit tests with coverage report |
| `make test-race` | Unit tests with race detector |
| `make test-int` | Integration tests (requires local Postgres) |
| `make lint` | golangci-lint |
| `make sqlc` | Regenerate `internal/store/pg/` from `db/queries/` |
| `make migrate-up` | Apply all pending migrations |
| `make wire` | Regenerate `wire_gen.go` |
| `make build` | Compile to `dist/myapp` |

Run `make lint test-race` before every PR.  
Run `make test-int` for anything touching `internal/posting`, `internal/holds`, or `db/`.

---

## Money representation

All monetary values use `internal/money.Amount` — an immutable `int64` (minor units) plus an ISO 4217 currency code. Never use `float32` or `float64` for money anywhere in this codebase, including tests and logs.

```go
// IDR 15,000
a := money.MustNew(15000, "IDR")

// USD 12.50
b := money.MustNew(1250, "USD")
```

---

## Posting protocol

Every debit/credit goes through `internal/posting` inside a single database transaction, in this exact order:

1. Insert journal entry with idempotency key (conflict → return stored result).
2. Lock balance rows with `SELECT … FOR UPDATE ORDER BY account_id`.
3. Validate invariants and account floors.
4. Insert postings, update balances, bump account version.
5. Insert outbox event.
6. Commit.

Do not bypass this path or add a second write path.

---

## Security and compliance

- Indonesian bank (OJK/BI). All data and infrastructure stay onshore.
- Audit trail is a regulatory requirement — postings and journal entries are immutable at the database level.
- No secrets in code or config files; read from environment or secret manager.
- Do not log full account numbers, NIK, or PII — use masked forms.
- Test fixtures use synthetic data only. Never use real customer data, NIK, or account numbers.
