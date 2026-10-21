# Celld MCP contracts

Eight named Celld tools share the authenticated HyperHive HTTP client and the same
registration over Streamable HTTP and executable stdio. API URLs and MCP_TOKEN
are configuration, never arguments. Every result is `{ "result": <API object> }`
with an identical JSON text fallback. Object fields and integer values are
preserved; a client's own JSON decoder may still convert numbers to floating point.
The SDK's automatic object-output conversion is bypassed only for validated Celld
results to avoid rounding nanosecond timestamps. Input schema validation remains
in the SDK, with additional domain checks before HTTP dispatch.

| Tool | Arguments | HTTP mapping |
| --- | --- | --- |
| `list_celld_fleets` | `{}` | `GET /celld/fleets` |
| `get_celld_health` | `{}` | `GET /celld/health` |
| `create_celld_fleet` | `name`, `servers`, optional `ports` | `POST /celld/fleets` |
| `start_celld_fleet` | `id` | `POST /celld/fleets/{id}/start` |
| `stop_celld_fleet` | `id` | `POST /celld/fleets/{id}/stop` |
| `get_celld_credentials` | `id` | `GET /celld/fleets/{id}/credentials` |
| `get_celld_logs` | `id` and optional filters below | `GET /celld/fleets/{id}/logs` |
| `get_celld_events` | `id` and optional filters below | `GET /celld/fleets/{id}/events` |

## Inputs and responses

IDs contain 1–64 letters, digits, underscores or hyphens, starting with a letter
or digit. Use IDs from the list/create results. Unknown fields, null arguments,
invalid IDs and invalid local input types/bounds fail before backend access.
Optional fields must be omitted rather than supplied as null.

List preserves the full fleet list, storage health, nullable bucket counts and
bucket lists, reset_pending and optional errors. Health preserves storage,
machine and fleet diagnostics. An unhealthy response uses HTTP 503 with
`healthy: false`; it is a successful diagnostic tool result, not an MCP error.
A generic 503 error body or an inconsistent health/status combination is rejected.

Create requires a unique name of 1–128 bytes without surrounding whitespace and a
nonempty array of unique inventory server names. Omit ports for automatic
allocation, or supply `{ "http": 8080, "internal": 8081 }`: both must be distinct
integers in 1–65535. HyperHive decides inventory validity, name uniqueness, port
reservations and reset/busy conflicts. No extra inventory call or tool is added.

Create, start and stop return the real JSON fleet state acknowledged by
HTTP 202. This is queued work, not completed provisioning, shutdown or removal.
Start sets desired_state=running; stop sets desired_state=stopped and preserves
configuration/data. List can be called later to inspect the resulting state.

Celld fleet deletion is excluded from MCP at the user's request (2026-10-21).
It remains a backend REST capability. All exposed Celld writes and credential
reads require High approval under the [shared safety policy](../tool-safety.md).

Credentials explicitly returns aws_access_key_id and aws_secret_access_key.
These are sensitive, fleet-scoped credentials; the tool description identifies
this disclosure. No other tool fetches them. A deleting fleet returns HTTP 409.
Credentials, API tokens and backend error bodies are not logged by this module.

## Logs and events

Both tools accept these optional fields and preserve supplied zero/false values:

| Field | Meaning and bounds |
| --- | --- |
| `from`, `to` | Signed 64-bit Unix milliseconds; zero uses API defaults. Default is the last hour, or the cursor's end time; the API clamps history to seven-day retention and validates the window/future bound. |
| `limit` | 0 means default 200; maximum 500 for JSON or 2000 for download. Negative values are rejected. |
| `cursor` | Opaque continuation, at most 1024 bytes; do not alter it. |
| `node` | Exact match, at most 128 bytes. |
| `workload` | Exact match, at most 256 bytes. |
| `source` | Exact match, at most 32 bytes. |
| `severity` | Uppercased by HyperHive, at most 16 bytes after uppercasing. |
| `text` | Text search, at most 256 bytes. |
| `download` | Omit/false for JSON; true for bounded NDJSON export. |

JSON pages preserve records, from/to, next_cursor, truncated, incomplete and error.
Partial collection and unavailable history can be valid HTTP 200 responses:
inspect incomplete/error instead of assuming an empty records array is complete.
Keep the same filters/time window when using next_cursor.

Downloads parse the API's NDJSON into `result.records`, expose truncated from
X-Observability-Truncated, and include next_cursor when X-Next-Cursor is present.
An empty successful download returns an empty records array. Exports do not
provide from/to or incomplete/error metadata: the tool does not invent it.
Continue exports using next_cursor, keeping download=true and the same filters.
No live stream, trace, metrics or observability snapshot tool was added.

## Transport and verification

All requests use the shared 30-second timeout, cancellation, 16 MiB response
bound and redirect refusal. Mutations are never automatically retried. HTTP
400/401/403/404/409/500/503 errors expose status without sensitive backend bodies;
health's reviewed diagnostic 503 is the sole special status contract.

Fixtures independently check all nine tools over authenticated HTTP and the real
stdio executable: discovery/schema/annotations, wire method/path/query/body and
Bearer token, complete results and text fallback, malformed responses, invalid
inputs before dispatch, safe errors and no retries. Download tests include empty
and multiline exports and continuation/truncation headers. Large numeric values
are checked for exact text preservation.

The master integration test uses real MCP routing/authentication, actual Celld
handlers, disposable signing/admin state and a disposable fleet state file.
It verifies automatic ports, server/name/busy conflicts, queued start/stop,
credential retrieval for retained fleets and partial/empty history/export behavior.
No reconciler, agent, process, storage bucket or production server is started or
modified. These checks prove API acceptance, not eventual reconciliation.

Sources: `master/api/celld.go`, `master/api/celld_observability.go`,
`master/services/celld.go`, `master/services/celld_fleets.go`,
`master/services/celld_reconcile.go`, `master/services/celld_observability_store.go`,
`slave/celld/fleets.go`; implementations in `mcp/internal/client/celld*.go` and
`mcp/internal/tools/celld.go`; fixture evidence in `mcp/celld_test.go` and
`master/api/celld_mcp_test.go`.
