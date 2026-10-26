# HH Support implementation contract

Date: 2026-10-26. Source: `plan/HH-Support/01-api-frontend-grill.md`, `master/api/api.go`, `master/api/login.go`, frontend `services/api-client.ts`, `services/websocket-client.ts` and `package.json`. SilverBullet project index was consulted; this is a source-reviewed implementation proposal, not deployed behavior.

## Options and decision

1. A separate Go module and SQLite database behind HH's authenticated reverse proxy: independent lifecycle and storage, with a small explicit trust boundary. Recommended; satisfies the requested service separation and reuses Go/SQLite/WebSocket conventions.
2. A separate service validating HH/NPM sessions itself: fewer master endpoints, but duplicates login validation, exposes upstream credentials and couples the service to NPM. Rejected.

Use `support/` in the backend repository, with its own `go.mod`, executable, README and tests. The executable is `cmd/hh-support`; `internal/` separates domain rules, application services, SQLite storage/migrations, HTTP, realtime, configuration and security. See [backend architecture](../../support/ARCHITECTURE.md). Do not modify HyperHive-Agent. No broker, automation engine, sender SDK, infrastructure inference, per-user dashboards or automatic metric aggregation.

## Routing, authentication and deployment

External routes are `/api/support` and descendants. Existing master middleware strips `/api`; register `/support` and `/support/*` before the general authenticated group. Strip exactly `/support` before forwarding, so service routes are `/apps`, `/tabs`, `/ingest`, `/ws`, etc. Match segment boundaries; `/apiary` must not become `/ary`. Preserve queries and WebSocket upgrades using the standard reverse proxy. Existing NPM `/api` routing can continue to reach master unchanged.

Master configuration: `SUPPORT_URL=http://127.0.0.1:8091` and `SUPPORT_PROXY_SECRET`; service: `SUPPORT_LISTEN=127.0.0.1:8091`, `SUPPORT_DB_PATH=./support.db`, same required `SUPPORT_PROXY_SECRET`. Validate the configured upstream URL. Missing service configuration returns 503 only for support routes. Document TLS requirements for a non-loopback service; do not expose service management directly to untrusted networks.

Master authenticates management with existing HH middleware. Always delete any incoming `X-HH-Support-Key`, then inject the configured secret upstream. Service requires this secret for every route except local `GET /healthz` and independently authenticated `POST /ingest`. Compare secrets in constant time. Do not forward HH session cookies/Authorization to service management. For ingestion preserve `Authorization: Bearer <App secret>` and `X-App-ID: <App ID>`; HH login is not required. Only exact POST ingest is public. App secrets are random 32-byte URL-safe tokens, stored only as SHA-256 hashes; return plaintext once on creation and rotation, never in lists or errors.

Browser sockets must not put a reusable HH token in URLs. Master implements authenticated `POST /support/ws-ticket`, returns `{ "ticket": string }`; random ticket is held in master memory, single-use, expires after 30 seconds, capped at 1024 pending tickets. `GET /support/ws?ticket=...` atomically consumes ticket, validates browser Origin against the request host or explicitly permitted HH frontend origins, strips ticket query, and proxies with the trust header. Ticket endpoints belong to master, not support. Do not log tickets or credentials. Native clients use the same flow. Bound socket lifetime to one hour before reauthentication.

## JSON types

All IDs are strings; timestamps are RFC3339 UTC strings. All responses are JSON except empty 204 and WebSocket frames. Errors: `{ "error": string }`. Unknown IDs return 404, validation 400, authentication 401, nonempty-tab/layout revision conflicts 409, oversized requests 413, unavailable upstream 503. A single JSON value is allowed per body.

```ts
type Connectivity = "unknown" | "online" | "offline";
type Health = "unknown" | "healthy" | "degraded" | "unhealthy";
type StreamType = "number" | "text" | "boolean" | "log" | "event";
type Severity = "debug" | "info" | "warn" | "error" | "fatal";
type Tab = { id: string; name: string };
type Instance = { instance_id: string; last_heartbeat_at: string | null;
  connectivity: Connectivity; health: Health; health_message: string };
type App = { id: string; name: string; tab_id: string; tags: string[];
  heartbeat_timeout_seconds: number; retention_days: number;
  connectivity: Connectivity; health: Health; health_message: string;
  last_heartbeat_at: string | null; instances: Instance[] };
type Stream = { name: string; type: StreamType; unit: string; label: string };
type LogValue = { message: string; severity: Severity };
type EventValue = { name: string; fields: Record<string, unknown> };
type Sample = { id: number; app_id: string; stream: string;
  instance_id: string | null; timestamp: string; received_at: string;
  value: number | string | boolean | LogValue | EventValue };
type WidgetKind = "value" | "line" | "gauge" | "text" | "boolean" | "logs" | "events";
type Widget = { id: string; stream: string; kind: WidgetKind;
  x: number; y: number; w: number; h: number;
  min?: number; max?: number; window_minutes?: number; severity?: Severity[] };
type Layout = { revision: number; widgets: Widget[] };
type AppDetail = { app: App; streams: Stream[]; layout: Layout };
```

Metadata unit/label defaults are empty strings; stream label display falls back to name. App mutations replace all editable settings. No implicit coercion of telemetry values. Numbers must be finite.

## Endpoints shared by frontend and service

Paths below are service paths; frontend `apiRequest` uses `/support` plus path, as its configured base already includes `/api`.

| Method/path | Request | Success response |
| --- | --- | --- |
| GET `/tabs` | none | `Tab[]` |
| POST `/tabs` | `{name}` | 201 `Tab` |
| PUT `/tabs/{id}` | `{name}` | `Tab` |
| DELETE `/tabs/{id}` | none | 204; 409 if Apps remain |
| GET `/apps` | optional `tab_id`, `tag` query filters | `App[]` |
| POST `/apps` | `{name,tab_id,tags?,heartbeat_timeout_seconds?,retention_days?}` | 201 `{app:App,secret:string}` |
| GET `/apps/{id}` | none | `AppDetail` |
| PUT `/apps/{id}` | `{name,tab_id,tags,heartbeat_timeout_seconds,retention_days}` | `App` |
| DELETE `/apps/{id}` | none | 204; explicitly confirmed UI deletion removes its history/layout/instances |
| POST `/apps/{id}/rotate-secret` | none | `{secret:string}`; previous secret immediately invalid |
| GET `/apps/{id}/layout` | none | `Layout` |
| PUT `/apps/{id}/layout` | `Layout` with current revision | `Layout` with incremented revision; stale edit 409 |
| GET `/apps/{id}/streams` | none | `Stream[]` |
| GET `/apps/{id}/samples` | required `stream`; optional `before`, `after`, `limit`, `severity`, `since` | `{samples:Sample[],next_before:number|null}` |
| POST `/ingest` | contract below | `{accepted:number}` after commit |
| GET `/ws` | trusted upgrade | push frames below |
| GET `/healthz` | none | `{status:"ok"}` after DB ping |

Create a default `General` tab on first startup. Tags belong to App settings: edit/remove labels there; derive filter options from existing App tags, so there is no independent tag registry. Trim names/tags; App/tab names 1–120 chars, maximum 32 tags of 1–64 chars, deduplicate tags exactly. Timeout 10–86400 seconds, default 120. Retention 1–365 days, default 7. Tab/App identity persists across rename.

History defaults: latest 200 samples, maximum 1000. Query IDs are exclusive integer cursors. `before` and `after` are mutually exclusive. Return chronological ascending samples; latest/before select the newest matching bounded page then reverse it. `after` selects earliest matching page in ascending order. `next_before` is oldest returned ID only when more older matching data exists, otherwise null. `since` accepts RFC3339 received-time lower bound; comma-separated severity applies only to log streams and rejects invalid values. Latest widgets use the highest received sample ID; line charts use bounded history, display received order and disclose truncation/load-more. No implicit averaging/downsampling.

## Sender ingestion

```json
{
  "instance_id": "worker-a",
  "heartbeat": true,
  "health": {"status":"healthy","message":""},
  "streams": [{"name":"cpu","type":"number","unit":"%","label":"CPU"}],
  "samples": [{"stream":"cpu","timestamp":"2026-10-26T15:00:00Z","value":12.5}]
}
```

All top-level fields are optional, but at least heartbeat true, health, stream definitions or samples must be present. `instance_id` omitted means direct App data; reject empty provided IDs, cap at 128 chars. Any valid authenticated request with an instance ID discovers that instance; only heartbeat true updates its liveness. First discovery without heartbeat is offline. A newly created App without senders is unknown. Direct App reporting is monitored only after the first direct request; its heartbeat participates alongside all discovered instances. App offline if any monitored participant is offline; online if all are online; unknown if none exist. Never create an unnamed instance record. No retirement, hidden aging-out or implicit instance removal.

Sender timestamp defaults to server receipt and is informational: it never changes heartbeat expiry, retention or latest-value ordering. A heartbeat replay is another accepted receipt; delivery is at-least-once with no deduplication promise. Samples append in receipt order using a monotonic SQLite ID. Health status permits healthy/degraded/unhealthy only; reports persist until explicitly replaced, display unknown before first report, and aggregate unhealthy > degraded > unknown > healthy across monitored participants. Connectivity expiry leaves reported health intact and visible separately. Restart recomputes connectivity from persisted server heartbeat timestamps; a service absence counts toward timeout. A one-second service sweep publishes expiry changes, never frontend polling.

Stream declarations upsert label/unit; type is immutable (409 on changed type). Samples must reference a declared stream from storage or the same batch. Reject the entire batch before any mutation if invalid; commit definitions, discovery, heartbeat, health and samples together. Validate exact values for all five types; no logs automatically change health.

Limits: 1 MiB request, 1000 samples and 100 stream declarations per batch, 256 streams and 1024 instances per App. Stream names 1–128 chars. Text/log messages maximum 16 KiB UTF-8; health message 1024 chars. Event names 1–120 chars and fields JSON object at most 16 KiB. Reject unknown top-level/sample fields to expose sender mistakes. Use bounded reads, SQL parameters, authentication before parsing and HTTP timeouts. Do not add a global ingestion rate policy without evidence; disk usage remains governed by configured retention and deployment capacity.

## WebSockets and reconciliation

One authenticated support socket per mounted support page. Push all support changes; client needs no subscription protocol for this few-App version.

```ts
type Frame =
  | { type: "ready" }
  | { type: "changed"; app_id?: string }
  | { type: "samples"; app_id: string; samples: Sample[] }
  | { type: "state"; app: App };
```

Send ready only after client registration. Client waits for ready, fetches tabs/App summaries/selected detail/latest history, and buffers live frames until initial fetch ends. Merge history/live samples by persisted sample ID. Metadata/layout mutations emit changed; ingestion emits samples plus state when needed; expiry emits state. Frontend reloads bounded metadata/detail on changed, coalescing bursts; this is event-driven, no interval polling. Reconnect obtains a new ticket, repeats snapshots/history loading and clears stale cache before reconciliation. Retained history remains browsable after gaps; no durable WS replay protocol is promised.

Per-client outbound queue holds at most 64 frames; overflow closes that socket with 1013 so the client reconnects and reloads retained state. Bound each frame, batch sample frames, 10-second write deadline, ping every 30 seconds and pong/read deadline 60 seconds. Never hold a DB transaction while writing a socket. Discard incoming application messages, cap read size, and stop goroutines when clients close.

## Storage and cleanup

SQLite WAL and busy timeout; one writer/connection is adequate initially. Tables: tabs, apps, instances, streams, samples and app layout (layout JSON can live on apps). Add foreign keys with cascading App deletion, indexes on `(app_id,stream,id)` and `(app_id,received_at,id)`. Keep last heartbeat and health in persistent App/instance rows; retention deletes only telemetry. Cleanup every minute in batches of at most 1000 rows until caught up, releasing the DB between batches. Filter expired records from history immediately, so cleanup lag never returns expired data; widgets show no data once the newest retained sample expires. Read startup errors loudly and fail service startup on missing trust secret, invalid config or inaccessible database.

## Frontend and validation scope

Add Support navigation in existing shared sidebar/layout, Expo route `app/support.tsx`, typed `services/support-client.ts`, support-specific socket management and focused UI components. Reuse `apiRequest`, installed SVG/chart/UI primitives and existing theme; add no dependencies for this proof of concept. App list uses tabs/tag filtering and separate connectivity/health indicators. App detail shows instances, settings, one-time credential copy, discovered streams and a shared dashboard editor.

Widget compatibility: value supports number/text/boolean; line/gauge number; text text; boolean boolean; logs log; events event. Gauge default 0–100, require finite min < max. Line window default 60 minutes and bounded history. Grid uses 12 columns, integer x/y/w/h, w/h >=1, x>=0, y>=0, x+w<=12, max 50 widgets; validate compatibility and unique IDs on save. Pointer drag/resize plus accessible move/resize controls; save whole layout with optimistic revision, show conflict and reload rather than overwrite silently. Missing streams and expired values show explicit no-data states.

Tests must cover all five ingestion types; atomic rejection; secret redaction/rotation; direct and instance connectivity/health and restart; stream type conflicts; limits; history cursor boundaries/filtering/retention; layout revision and compatibility; WS origin/ticket replay/spoofed trust header/slow clients; master prefix precedence and upgrades; frontend service parsing, sample merge, widget compatibility and typecheck/build. Final source review checks root backend and nested frontend documentation/config/build hooks. Do not run or change live infrastructure.

Risk: deleting an App permanently removes history; rotating credentials interrupts senders until updated. A discovered instance remains monitored for the App's lifetime by design. Received-time telemetry and at-least-once ingestion deliberately retain retry duplicates. SQLite and WS queues bound request memory, not the total size of a high-ingestion database.
