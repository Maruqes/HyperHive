# HH Support

An independent Go service with SQLite persistence for the shared HH Support dashboard. No VM or agent code runs here. See [the HTTP and sender contract](../docs/designs/hh-support.md).

## Run locally

Requires Go 1.26.2 or later, CGO and a C compiler (the existing `go-sqlite3` driver).

```sh
cd support
export SUPPORT_PROXY_SECRET="$(openssl rand -hex 32)"
export SUPPORT_LISTEN=127.0.0.1:8091
export SUPPORT_DB_PATH=./support.db
go run ./cmd/hh-support
```

Configure master with `SUPPORT_URL=http://127.0.0.1:8091` and the same `SUPPORT_PROXY_SECRET`. Keep credentials out of source control. Missing secrets, invalid listen addresses and database startup failures stop the service. The database parent directory must exist and be writable; SQLite creates the file and WAL sidecars. Keep this directory private to the service account. Back up SQLite with its backup API or stop the service before copying the database and WAL together.

The master owns HH login, Origin validation and one-use WebSocket tickets. It strips `/support` before proxying. Management requires `X-HH-Support-Key`; only local health checks (`GET /healthz`) and independently authenticated sender ingestion (`POST /ingest`) bypass that header. Bind to loopback by default. If master and support are separated, use TLS and network access controls for the service; the shared header must never traverse an untrusted plaintext network. Browsers connect through master, never directly to this listener.

## Managed Docker deployment

Master automatically builds and starts Support on Linux Docker Engine with Compose 2.30 or later using host networking. The default URL is `http://127.0.0.1:8091`; ingestion and browser access remain through the HH proxy. The master executable embeds the Support source, builds a content-tagged local image when missing, and waits for its Go HTTP healthcheck before continuing. First startup needs network access to the Go/Debian base images and Go modules. Deployment failures are logged while HH continues startup. With generated credentials, a failed reconciliation leaves Support proxying disabled for that master boot, even if an old container remains running; retry after repairing the cause.

The Compose project, service and container are named `hyperhive-support`. The service runs as UID/GID `10001:10001`, with a read-only root filesystem, dropped capabilities and a persistent named `hyperhive-support-data` volume at `/data`. Reconciliation reuses compatible containers and never deletes the volume. An unrelated container with the same name is refused.

Master stores a generated shared secret and runtime configuration in `/opt/hyperhive/support` (directory 0700, files 0600); `SUPPORT_STATE_DIR` overrides this location. The secret is reused across restarts. An explicit `SUPPORT_PROXY_SECRET` takes precedence. If persistent credentials are missing while a managed container or data volume exists, restore `proxy-secret` from backup or explicitly configure the shared secret before restarting master. Docker administrators can inspect container environment variables. Keep the state directory and SQLite volume in backups; never delete either to repair startup.

Set `SUPPORT_AUTO_DEPLOY=false` for an independently managed or external Support service, then configure `SUPPORT_URL` and `SUPPORT_PROXY_SECRET` as above. Managed URLs require a loopback IP and explicit HTTP port. Host networking assumes master runs on the Linux Docker host or shares its host network. Existing volumes with incompatible ownership require an administrator to repair that specific volume; the manager does not recursively change permissions or delete data. A failed upgrade preserves the volume but does not automatically roll back database migrations.

## Project structure

The executable is in `cmd/hh-support`. Application code lives under `internal/`, separated into configuration, domain rules, services, SQLite storage, HTTP handlers, security and realtime delivery. Cross-package API regressions live in `tests/integration`; focused tests stay with their packages.

See [architecture and extension guide](ARCHITECTURE.md) for the dependency direction, migration rules and where to add features. Build the executable with `go build -o hh-support ./cmd/hh-support`.

## Send telemetry

Create an App in the HH UI, then use the App ID and one-time secret:

```sh
curl https://HH_HOST/api/support/ingest \
  -H "X-App-ID: $APP_ID" -H "Authorization: Bearer $APP_SECRET" \
  -H 'Content-Type: application/json' \
  --data '{"instance_id":"worker-a","heartbeat":true,"health":{"status":"healthy","message":""},"streams":[{"name":"cpu","type":"number","unit":"%","label":"CPU"}],"samples":[{"stream":"cpu","value":12.5}]}'
```

Omit `instance_id` for direct App reporting. Any discovered instance remains monitored until the App is deleted. Only `heartbeat:true` renews connectivity; sender timestamps never renew it. Explicit health persists across connectivity expiry and restarts. A batch is atomic and credentials are checked before its body is read. Secrets are stored as SHA-256 hashes and only returned on creation/rotation; rotation immediately interrupts old senders.

WAL SQLite uses one connection with a five-second busy timeout. Samples retain receipt order with monotonic IDs; duplicate retries remain separate samples. History queries immediately exclude expired telemetry; background deletion releases the writer after each 1000-row batch. Retention bounds age, not total database size. App deletion permanently removes its history, instances and layout. No aggregation, broker, sender SDK or automation engine is included.

WebSockets register before sending `ready`; clients reload bounded snapshots and history after every reconnect. Frames contain at most 32 samples, queues at most 64 frames; overloaded clients close with 1013. Connections require reauthentication after one hour. Management and ingestion changes become frames only after successful persistence. Retention cleanup emits changes so open dashboards clear expired values.

## Shared widget layouts

`GET/PUT /apps/{id}/layout` reads and saves the App's SQLite-backed layout across browsers, devices and service restarts. Save the current `revision`; a concurrent edit returns 409. Widget kinds include `value`, `line`, `area`, `bar`, `sparkline`, `gauge`, `progress`, `text`, `boolean`, `logs`, `events` and `table`; compatibility is defined in the [HTTP contract](../docs/designs/hh-support.md).

To configure telemetry before its first sample, save a widget with the exact stream name and `stream_type` (`number`, `text`, `boolean`, `log` or `event`), for example `{"id":"cpu","stream":"number_cpu","stream_type":"number","kind":"line","x":0,"y":0,"w":6,"h":3}`. This does not declare a sender stream. The sender must still declare the matching type; a conflicting declaration rejects the entire ingestion batch with 409. `stream_type` remains optional for already declared streams, preserving existing layouts.

## Validate

```sh
go test ./...
go test -race ./...
go vet ./...
```
