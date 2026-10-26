# HH Support backend architecture

This module separates business rules, persistence and transport so features can grow without expanding a single executable package.

```text
support/
├── cmd/hh-support/             Executable composition and shutdown
├── internal/
│   ├── config/                 Environment configuration and validation
│   ├── domain/                 Models, validation, health rules and typed errors
│   ├── service/                Application operations and background maintenance
│   ├── storage/sqlite/         Queries, atomic writes and database lifecycle
│   │   └── migrations/         Embedded, numbered SQL schema migrations
│   ├── httpapi/                Authentication, routing and JSON contracts
│   ├── realtime/               WebSocket clients, queues and connection lifecycle
│   └── security/               Random credentials and constant-time comparisons
└── tests/integration/          HTTP, persistence and live-update regressions
```

## Responsibilities and dependencies

`cmd/hh-support` loads configuration, opens storage, constructs the realtime hub/application service/HTTP handler, and owns process lifecycle. Shutdown stops HTTP traffic and maintenance before closing the database. Working-directory-relative database paths retain their previous meaning.

`httpapi` handles HTTP concerns: HH proxy authentication, sender headers, bounded JSON decoding, query parsing, status codes and response encoding. It delegates operations to `service`; handlers contain no SQL. Ingestion authenticates before reading its body and rechecks credentials under the application lock before committing, so rotation cannot authorize a batch using a stale secret.

`service` orchestrates App/tab/layout changes, ingestion validation, heartbeat expiry and retention. It owns synchronization across read/validate/write operations and emits realtime changes only after successful persistence. Business operations can be exercised directly without starting an HTTP server. Feature-related operations live in their own files rather than a central router or utility file.

`domain` contains transport-independent data and rules. Typed domain errors are mapped to HTTP status codes by `httpapi`. Domain code imports neither HTTP handlers nor storage. Keep new business invariants here when they do not require persistence.

`storage/sqlite` contains all SQL and transaction boundaries. It accepts typed domain values and returns typed results; it never writes HTTP responses or publishes socket messages. Application services coordinate its calls. `realtime` owns WebSocket framing, bounded queues, slow-client closure and connection expiry. `security` contains credential primitives shared by the adapters.

The package dependency direction is entry point → adapters/application → domain. Current services use the concrete SQLite and realtime adapters; add a narrow interface when another implementation is actually needed. Do not expose SQL connections through the HTTP layer or add general-purpose repository wrappers.

## Database changes

SQL files are embedded into the executable and applied in numeric sequence. Add a new sequential migration such as `002_add_field.sql`; never edit a migration already shipped. Each migration and its `PRAGMA user_version` update commit in one transaction. Startup refuses a database newer than the executable supports. Existing unversioned databases pass through the idempotent initial migration without losing their data.

Back up the database before deploying schema changes. A failed migration rolls back its own writes; earlier successful migrations remain applied. The migration tests cover upgrades, idempotency, rollback and rejection of newer schemas.

## Adding a feature

Start with its domain types/rules, add persistence operations and any required migration, implement service orchestration, then expose the HTTP contract and realtime changes. Keep tests for pure rules next to the package and extend `tests/integration` for behavior spanning persistence and transport. A sufficiently large new subsystem can become its own internal package while using the same entry point and adapters.

Run `go test -race ./...`, `go vet ./...` and `go build ./cmd/hh-support`. The master integration test builds this command and verifies the external `/api/support` boundary. This refactor changes organization and schema-version bookkeeping; it does not introduce distributed storage or change HTTP payloads.
