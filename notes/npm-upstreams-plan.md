# Plan: NPM UpStreams

Generated: 2026-10-20. Complexity: high. Status: proposed implementation plan; no feature implementation or deployment performed.

## Outcome and scope

Create reusable, persistent upstream pools in HyperHive's NPM area. Users can select a pool in HTTP/HTTPS proxy hosts, custom proxy locations, and TCP/UDP streams. The user confirmed that HTTP/HTTPS and TCP/UDP ship together, and that `settings_string` accepts validated NGINX server parameters. Redirections, certificates, access lists and 404 hosts do not independently forward traffic, so they do not need pool selectors.

HyperHive owns pool definitions and references in its existing SQLite database. NPM continues to own proxy/stream resources. Generated configuration is derived state. Do not modify NPM's database schema or require an upstream NPM fork. Preserve existing direct-target payloads and manually authored configuration.

## Inspected foundation and compatibility gate

- Backend: `master/npm/{npm,proxy,streams}.go`, `master/api/npmapi/{proxy,streams}.go`, `master/api/api.go`, `master/db/db.go`.
- NPM bootstrap pins `jc21/nginx-proxy-manager:2.13.5`, mounts `npm-data` at `/data`, and already creates custom configuration for stream logging. Existing compose files are preserved, so the actual image can differ from the source default.
- Frontend: `/home/marques/projects/HyperHiveFront/HyperHive/{app,services,types}/{proxy,streams}.*`; actual stream type filename is `types/stream.ts`.
- MCP: `mcp/internal/client/nginx.go` and the existing proxy client definitions (locate their symbols before editing), `mcp/internal/tools/{nginx,proxy_mutations}.go`, and `mcp/AGENTS.md`.
- An existing untracked `master/api/npmapi/proxy_config_test.go` and another task's planning files were present. Preserve and reconcile their contracts before implementation; do not overwrite them.

NPM's [proxy template](https://raw.githubusercontent.com/NginxProxyManager/nginx-proxy-manager/v2.13.5/backend/templates/proxy_host.conf), [location template](https://raw.githubusercontent.com/NginxProxyManager/nginx-proxy-manager/v2.13.5/backend/templates/_location.conf), [proxy include](https://raw.githubusercontent.com/NginxProxyManager/nginx-proxy-manager/v2.13.5/docker/rootfs/etc/nginx/conf.d/include/proxy.conf) and [stream template](https://raw.githubusercontent.com/NginxProxyManager/nginx-proxy-manager/v2.13.5/backend/templates/stream.conf) append a forwarding port. Inference: setting a pool name as the host alone is not a proven integration. Sprint 1 must test this against the exact image. Prefer native NPM fields only if real traffic proves correct pool selection; otherwise ship narrowly scoped, version-pinned template overlays that omit the port for managed pool targets and preserve direct targets. Do not add duplicate `proxy_pass` directives or overwrite whole generated host files after every NPM save.

## API and model

Example creation body, retaining the requested field names:

```json
{
  "name": "app_pool",
  "context": "http",
  "loadbalancer_type": "least_conn",
  "server_arr": [
    {"ip": "10.0.0.11", "port": 8080, "settings_string": "max_fails=3 fail_timeout=10s"},
    {"ip": "10.0.0.12", "port": 8080, "settings_string": "weight=2 max_fails=3 fail_timeout=10s"}
  ]
}
```

`context` is `http` or `stream`, defaulting to `http` for the three-field request. Stream pools can serve TCP and UDP; a stream enabling both protocols uses the same selected pool. Different TCP/UDP backends require separate stream resources. An HTTP pool cannot be selected by a stream. HTTP versus HTTPS is a consumer setting, allowing a pool to be reused with an explicitly selected transport; mixed TLS/plain members in a single consumer are unsupported.

Use an immutable pool ID and internal name such as `hh_upstream_42`, while `name` remains editable. Enforce a globally unique conservative ASCII name, normalized consistently for uniqueness; reject reserved names. Store ordered members, canonical parsed settings, revision and last applied generation/error. Persist bindings by consumer type, NPM ID and optional location path. Return the original API shape with canonicalized `settings_string`, `id`, `revision`, apply status and reference summaries. No raw internal target names should leak into the editable user model.

Proposed authenticated routes, following current action-based routing:

| Method | Route | Result |
| --- | --- | --- |
| GET | `/upstream/list` | Pool summaries and apply status |
| GET | `/upstream/{id}` | Definition and references |
| GET | `/upstream/capabilities` | Verified methods/options by context and image |
| POST | `/upstream/create` | 201 with created resource after successful apply |
| PUT | `/upstream/{id}` | Replace definition using expected revision |
| DELETE | `/upstream/{id}` | 204 only when no references exist |

Use 400 for malformed/unsupported inputs, 404 for missing IDs, 409 for duplicate names, revision conflicts, drift or referenced deletion, and 503 for unavailable/failed application. Return bounded structured errors and a recovery identifier when compensation fails. Register static paths ahead of `/{id}`. Decide and document body/revision-header format in Sprint 2.

Add optional `upstream_id` to proxy hosts, locations and streams. For existing clients, omission on create means a direct target; omission on edit preserves an existing pool binding, `null` explicitly detaches and requires valid direct fields, and an ID selects a compatible pool. Reject conflicting direct fields plus a selected pool rather than silently ignoring them. Location omission preserves the existing location contract; replacing locations explicitly reconciles their bindings. Reads reconstruct these fields from binding state. Keep legacy direct-target requests and managed frontend proxy restrictions intact.

## Balancers and parameter validation

Expose a registry of supported methods and required options, filtered by context and the tested NGINX/OpenResty build. Initial candidates: `round_robin` (no directive), `least_conn`, `random`, `random_two`/least-connection selection, `hash`, `hash_consistent`, and HTTP-only `ip_hash`. Hash methods require a separate validated `loadbalancer_options.hash_key`; never accept an arbitrary directive in `loadbalancer_type`. Add `least_time` and other build-specific methods only after capability tests establish support; latest documentation does not prove availability in this image. See official [HTTP upstream](https://nginx.org/en/docs/http/ngx_http_upstream_module.html) and [stream upstream](https://nginx.org/en/docs/stream/ngx_stream_upstream_module.html) references.

Parse `settings_string` into allowlisted tokens, then serialize canonically. Baseline candidates are `weight`, `max_conns`, `max_fails`, `fail_timeout`, `backup`, `down`. Validate ranges, durations, duplicates and method compatibility; for example, reject backup members with hash/random methods and HTTP ip_hash. Reject semicolons, braces, comments, newlines, variables and unknown tokens. Only allow validated IP literals in `ip` initially, including correctly bracketed IPv6 output; DNS/UNIX targets need a future explicit address contract. Require at least one usable primary, reject duplicate endpoints, and bound body size, member count and string lengths. Treat failures as passive health behavior; active health probes are outside this release. A one-member pool needs an explanatory health warning.

The capabilities response must distinguish verified, unsupported and unknown options. Missing runtime access cannot silently enable methods. Hash-key input has its own restricted variable/expression grammar and context allowlist. Record shared-zone requirements and whether connection limits apply across or per worker.

## Configuration ownership and failure behavior

Generate separate HTTP and stream pool files beneath `/data/nginx/custom/hyperhive/`. Install one managed include in `http_top.conf` and one in `stream.conf`, preserving existing content, especially stream logging. [NPM documents these context-specific hooks](https://nginxproxymanager.com/advanced-config/). Consumers reference internal names without adding a member port. HTTPS consumers need explicit upstream SNI/verification behavior; keep public certificates and backend TLS settings separate.

Use a single mutation coordinator across pools and affected proxy/stream writes. Persist an operation journal and candidate generation, stage deterministic files, validate the complete candidate with the exact container NGINX, atomically publish owned files, reload gracefully, verify activation, then mark applied. A SQLite transaction cannot make NPM HTTP, files and NGINX atomic. Preserve last-good definitions, bindings and NPM payloads, compensate on failure, and expose recovery-required state if rollback fails. NPM API calls may reload themselves: design and test an order that installs pools before binding consumers, unbinds consumers before removing pools, and prevents intermediate missing targets. On startup, reconcile journaled state before accepting mutations. Detect external NPM edits and refuse conflicting updates rather than overwriting them.

## Sprint 1 — executable compatibility proof

1. **Exact-image fixture.** Add disposable integration fixtures under `master/internal/testnpm/` and a compatibility report under `notes/`. Inspect image identity, `nginx -V`, schemas, templates and custom include order. No production access. **Dependency:** none. **Validation:** fixture starts and captures build evidence.
2. **Pool targeting proof.** Exercise root proxy, custom location, TCP and UDP forwarding to two labeled backends using native fields. If any context fails, build a minimal adapter prototype covering its templates/includes. Use reserved managed markers only after proving NPM accepts and preserves them; use a namespaced host marker plus persisted bindings if metadata is unsuitable. **Dependency:** 1. **Validation:** generated config and actual traffic reach both members, including non-default ports and IPv6.
3. **Adapter upgrade contract.** Document exact overlay files, mount paths, fingerprints, existing-compose migration and supported image digest. Abort unsupported template installation and retain last-good state. **Dependency:** 2. **Validation:** regeneration, container restart and a simulated image mismatch preserve direct hosts and fail safely.

Demo gate: all four protocols work before selecting the final adapter. This sprint must not end with HTTP working and streams deferred.

## Sprint 2 — persistent pool CRUD and safe application

1. **Models and validation.** New `master/npm/upstream.go` and focused validator tests. Implement method registry and settings parser. **Dependency:** Sprint 1. **Acceptance:** valid canonical models; deterministic errors for invalid combinations. **Validation:** table tests and parser fuzz cases.
2. **Storage and migrations.** New `master/db/npm_upstreams.go`, using existing database initialization conventions in `master/main.go`. Store definitions, members, revisions, bindings, generations and journal. **Dependency:** 1. **Acceptance:** restart persistence and reference-safe deletion. **Validation:** migration/reopen, uniqueness, stale revisions and transaction tests.
3. **Renderer and apply coordinator.** New `master/npm/upstream_config.go` and `upstream_apply.go`; bootstrap integration in `master/npm/npm.go`. **Dependency:** 1–2. **Acceptance:** preserve custom files, serialized changes, complete validation, reload and recovery status. **Validation:** injected write/test/reload/commit failures and crash recovery.
4. **HTTP endpoints.** New `master/api/npmapi/upstreams.go`, route registration in `master/api/api.go`. Reuse existing admin authentication. **Dependency:** 3. **Acceptance:** CRUD/capabilities with documented status semantics. **Validation:** actual-handler fixtures, auth denial and structured-error tests.

Demo gate: create, edit, list and delete unused HTTP/stream pools; invalid input leaves active configuration unchanged; restart retains definitions.

## Sprint 3 — reusable targets in all consumers

1. **Proxy and location bindings.** Extend `master/npm/proxy.go` and `master/api/npmapi/proxy.go`; separate client DTOs from downstream NPM payloads. **Dependency:** Sprint 2. **Acceptance:** select/change/detach pools and preserve user advanced configuration, paths, TLS, ACLs, headers, caching and WebSockets. **Validation:** existing proxy contract tests plus omission/null/conflict cases and traffic fixtures.
2. **Stream bindings.** Extend `master/npm/streams.go` and `master/api/npmapi/streams.go`, preserving metadata and resource descriptions. **Dependency:** Sprint 2. **Acceptance:** TCP, UDP and dual-protocol listeners use compatible pools; certificate behavior remains intact. **Validation:** forwarding, failover and replacement tests.
3. **Reference lifecycle and drift.** Coordinate create/update/delete/enable of consumers and startup reconciliation. Include disabled consumers as references. **Dependency:** 1–2. **Acceptance:** shared pool edits update every consumer, rename keeps IDs stable, referenced deletion returns 409, managed proxy policy remains enforced. **Validation:** concurrent pool deletion/bind, external NPM change and partial failure fixtures.

Demo gate: two proxies plus TCP/UDP resources share appropriate pools, edit membership once, observe changed routing everywhere, detach safely.

## Sprint 4 — frontend and MCP

1. **Pool management UI.** Add `types/upstream.ts`, `services/upstreams.ts`, `app/upstreams.tsx` under `/home/marques/projects/HyperHiveFront/HyperHive`; connect existing navigation in `app/_layout.tsx` as appropriate. **Dependency:** Sprint 2 API. **Acceptance:** list/create/edit/delete, member rows, capability-driven methods, field errors, references and apply state. **Validation:** UI/type checks and keyboard/touch walkthrough.
2. **Consumer selectors.** Update `app/proxy.tsx`, `app/streams.tsx` and their services/types. **Dependency:** Sprint 3 and UI task 1. **Acceptance:** direct/pool mode, compatible dropdowns, location support and safe detach with explicit direct fields. **Validation:** round-trip existing and pooled resources; inaccessible pool capability shows an actionable state.
3. **Named MCP tools.** Extend shared client and registry with list/get/create/update upstream tools, and update proxy/stream tools for optional bindings and capabilities. **Dependency:** Sprints 2–3. Follow `mcp/AGENTS.md`; expose no upstream deletion tool or deletion aliases. **Acceptance:** explicit schemas, context validation, preserved omitted fields, no mutation retries. **Validation:** SDK discovery/invocation over HTTP and stdio, mock method/path/auth assertions; refresh coverage and tool contracts.

Demo gate: configure and reuse pools through the UI and eligible MCP tools for HTTP and streams.

## Sprint 5 — release verification and recovery

1. **End-to-end acceptance suite.** Add disposable exact-image tests for every advertised method, IP family and protocol, configuration regeneration and restart. **Dependency:** Sprints 1–4. **Validation:** distribution assertions use adequate samples; affinity uses controlled client addresses; least-connection tests hold connections open; UDP tests account for session behavior. Verify weighted routing, primary loss/recovery, all backends unavailable, custom locations/paths, TLS/SNI and WebSockets. Never replay non-idempotent HTTP requests automatically to demonstrate failover.
2. **Failure/concurrency suite.** Test disk full, unavailable NPM, invalid external custom config, reload failure, crash between each journal stage, stale revision, drift and rollback failure. **Dependency:** Sprint 3. **Acceptance:** truthful applied/recovery status and retained last-good traffic; no false successful response.
3. **Release/rollback guide.** Document backup of HyperHive DB, NPM data, compose and owned files; compatible image identity; ordered enablement; diagnostics and downgrade procedure. **Dependency:** 1–2. **Validation:** rehearse install and rollback on disposable data. Keep database migrations additive. Before removing adapter/includes, restore direct consumer targets or disable affected consumers; never leave references to removed pools.

Run focused Go tests first, then required `go test ./...` in affected modules, relevant race checks, frontend configured type/lint/tests and the integration suite. This planning task ran no behavior tests and verified no live service.

## Delivery criteria and dependencies

Sprint 1 → Sprint 2 → Sprint 3 → final integration. Frontend pool UI can follow Sprint 2 while consumer UI/MCP wait for Sprint 3 contracts. Each numbered task is a separate reviewable commit; adapter proof may require multiple small commits. HTTP/HTTPS and TCP/UDP remain one release acceptance gate. Existing clients continue direct forwarding unchanged. Invalid or unsupported configurations cannot replace working routing. Reuse is by stable ID with visible dependencies and safe deletion. The adapter remains the highest-risk dependency and must be proven before production implementation proceeds.
