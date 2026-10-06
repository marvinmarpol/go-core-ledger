# go-core-ledger

A double-entry core ledger service written in Go.

**Correctness over delivery speed.** A bug here creates or destroys money.

---

## How it works

This service is a bank's **ledger** — the permanent record book of every movement of money. It does not compute interest, schedule fees, or make lending decisions. Those belong to the product engine, which sends posting instructions here. This service records what happened and keeps balances accurate.

### Accounts and balances

An account is a position that holds a balance in a single currency (e.g. IDR). A balance is never edited directly — it is always the running sum of every movement recorded against that account. Two accounts are special: the floor, which is the minimum balance the system will allow, and the held amount, which is money set aside but not yet moved.

### Double-entry bookkeeping

Every transaction has two sides. When IDR 500,000 leaves one account, it must arrive somewhere else — another account, a fee pool, or a clearing account. The system refuses to record a transaction unless every debit is matched by an equal credit. This is the oldest error-detection mechanism in finance and is the reason a ledger cannot "lose" money.

### Journal entries and postings

A **journal entry** is the record that a transaction happened — it carries a timestamp, a business date, and an idempotency key. Its **postings** are the individual debit and credit lines that make up the transaction. Once recorded, neither the entry nor its postings can ever be changed or deleted; corrections are new reversing entries. This immutability is a regulatory requirement under OJK/BI.

### Holds

A **hold** is a reservation. It says "this money is spoken for, but hasn't moved yet." A common example is a merchant pre-authorisation on a card: the available balance falls immediately, but the actual debit only posts at settlement (capture). If settlement never happens, the hold is voided and the reservation is released. The system guarantees that placing a hold, capturing it, and posting the debit are all atomic — there is no window where money appears twice or disappears.

### Idempotency

If a network failure causes the same payment request to arrive twice, the system records it exactly once and returns the same result both times. The caller can retry without risk of double-posting.

### Event relay (outbox)

Whenever a transaction commits, the system records an event in the same database write — inside the same atomic commit. A background worker reads those pending events and forwards them to downstream systems (notifications, reconciliation, analytics). This design guarantees that an event is published if and only if the transaction was actually committed — notifications cannot fire for transactions that rolled back, and transactions cannot commit silently without notifying downstream systems.

---

## What this service does

- Maintains accounts and their balances
- Records journal entries and immutable postings
- Enforces double-entry balance invariants on every write
- Manages fund holds (reserve / capture / void / expire)
- Publishes ledger events via transactional outbox
- Runs nightly balance reconciliation against posting history

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
| API protocol | [Connect](https://connectrpc.com) (gRPC + gRPC-Web + HTTP/JSON) |
| Linting | golangci-lint v2 |

No ORM. Queries are written in SQL (`db/queries/`) and generated into `internal/store/pg/`.

---

## Project layout

```
cmd/
  ledger-api/         API server entrypoint
  ledger-worker/      Outbox relay, hold expiry, verification jobs

internal/
  money/              Amount type — int64 minor units + ISO 4217 currency code
  ledger/             Domain: Account, Entry, Posting, Direction, Clock, invariant checks
  posting/            Posting protocol — 6-step transaction orchestration
  holds/              Reserve / capture / void / expire
  outbox/             Transactional outbox relay
  worker/             Worker process wiring and background job loops
  store/pg/           sqlc-generated queries + repository adapters
  uid/                UUID v4 generator
  app/                API server wiring and config

db/
  migrations/         Goose migration files
  queries/            sqlc input query files

api/
  proto/              Connect/gRPC API definition (source of truth)
  gen/                Generated Go code — DO NOT EDIT

test/
  property/           Property-based invariant tests
  concurrency/        Parallel posting tests against real Postgres
```

---

## Local setup

### Prerequisites

- Go 1.26+
- PostgreSQL 14+
- `golangci-lint` v2, `buf`, `protoc-gen-go`, `protoc-gen-connect-go` (installed via `make setup`)

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

### 4. Run the API server

```bash
make run
```

### 5. Run the worker (optional)

The worker runs three background jobs: outbox relay, hold expiry, and nightly balance verification.

```bash
make run-worker
```

Worker-specific env vars (all optional, with defaults):

| Env var | Default | Purpose |
|---|---|---|
| `WORKER_RELAY_INTERVAL` | `5s` | How often to flush outbox events |
| `WORKER_RELAY_BATCH` | `100` | Events per relay tick |
| `WORKER_EXPIRY_INTERVAL` | `30s` | How often to expire stale holds |
| `WORKER_EXPIRY_BATCH` | `50` | Holds processed per expiry tick |
| `WORKER_VERIFY_INTERVAL` | `24h` | How often to run balance reconciliation |

---

## Development commands

| Command | Description |
|---|---|
| `make test` | Unit tests with coverage report |
| `make test-race` | Unit tests with race detector |
| `make test-int` | Integration tests (requires local Postgres) |
| `make lint` | golangci-lint |
| `make sqlc` | Regenerate `internal/store/pg/` from `db/queries/` |
| `make proto` | Regenerate `api/gen/` from `api/proto/` |
| `make migrate-up` | Apply all pending migrations |
| `make build` | Compile API server to `dist/myapp` |
| `make build-worker` | Compile worker to `dist/ledger-worker` |
| `make run-worker` | Build and run the worker process |

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
