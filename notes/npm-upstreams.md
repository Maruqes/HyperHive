# Reusable NPM upstream pools

HyperHive owns pool definitions and consumer bindings in its SQLite database. NPM continues to own proxy hosts and streams. This feature requires the tested NPM image and the updated master, frontend and MCP server. No production deployment is implied by the local test results.

## Supported targets and configuration

Create separate `http` and `stream` pools. HTTP pools serve proxy hosts and custom locations; each consumer chooses HTTP or HTTPS. Stream pools serve TCP, UDP or both on one listener. Both protocols on a listener share the selected pool. Redirections, certificates, access lists and 404 hosts have no selectors.

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

`context` defaults to `http`. Names are trimmed, lowercased ASCII identifiers, globally unique and editable; immutable IDs generate private `hh_upstream_<id>` targets. `hh_`, `localhost` and `default` are reserved. Members are ordered, unique IP literals with ports 1–65535; IPv6 is rendered with brackets. DNS, scoped addresses, UNIX sockets and active health checks are unsupported.

Supported methods are round_robin, least_conn, random, random_two, hash and hash_consistent, plus HTTP-only ip_hash. Hash methods require `loadbalancer_options.hash_key` from capabilities. Accepted server parameters are weight, max_conns, max_fails, fail_timeout, backup and down. Values, duplicates and method compatibility are checked; arbitrary directives, comments and variables are rejected. At least one usable primary is required. A single member cannot provide failover, and NGINX ignores its max_fails/fail_timeout settings. Shared zones coordinate active connection limits across workers. Pool definition requests are limited to 64 KiB and 128 members.

## Authenticated REST contract

| Method | Path | Contract |
| --- | --- | --- |
| GET | /upstream/list | Definitions, references and apply status |
| GET | /upstream/capabilities | Verified/unknown build support, context methods and hash keys |
| GET | /upstream/{id} | Definition and references; ETag contains the revision |
| POST | /upstream/create | 201 with saved definition after activation; omit ID/revision |
| PUT | /upstream/{id} | Replacement body includes expected `revision`; ID may be omitted or must match |
| DELETE | /upstream/{id} | Requires `If-Match: "<revision>"`; 204 only without references |

Errors are bounded JSON: 400 invalid input, 404 missing identity, 409 name/revision/reference/drift conflict, and 503 unavailable application or recovery. A failed compensation includes `recovery_id`. Recovery status and that identifier are also visible in capabilities and existing pool reads. Definitions retain their last applied generation and error. Capability `unknown` never enables a mutation.

Proxy hosts, custom locations and streams accept `upstream_id`. Omission on create selects direct forwarding. Omission on edit preserves the binding. `null` explicitly detaches and requires a valid direct host/port. A positive ID selects a compatible pool; conflicting direct targets are rejected. Complete frontend/MCP payloads use an empty direct host and zero port in pool mode. Reads return the pool ID and clear private downstream targets. Omitted locations preserve the current list; an explicitly replaced list reconciles its path bindings. Disabled consumers remain references.

HTTPS backend SNI defaults to the public hostname and backend certificate verification is explicitly off, independently of public NPM certificates. When advanced configuration contains `proxy_ssl_`, the adapter defers backend TLS policy to that configuration. Mixed plain/TLS members for one consumer are unsupported. Existing user configuration, metadata, ACLs and WebSocket settings are retained.

## Adapter and persistence

The tested image is `jc21/nginx-proxy-manager@sha256:cd9eba29ca132cb006729f2cb2660126453f84818c2f7d75963ad7b61ef696bd` (OpenResty 1.27.1.2). Native host fields append ports and failed all four pool traffic contexts. The adapter changes only pinned proxy_host.conf, _location.conf, stream.conf and the common proxy.conf include; direct targets retain their forwarding port. NPM startup may append blank lines to the common include; verification normalizes only its trailing LF bytes while keeping directive content pinned. Exact source fingerprints are in `npm-upstreams-compatibility.json`.

Generated files live under `/data/nginx/custom/hyperhive/generations/`, selected by one `current` symlink. Managed includes are appended to http_top.conf and stream.conf while preserving existing content and stream logging. A complete candidate is tested with the container's NGINX before publication; a Unix-socket generation response verifies activation. Failed edits compensate to the previous snapshot; uncertain outcomes remain journaled and block mutations. Confirmed creates are completed during reconciliation instead of deleting the new resource, so MCP creation has no deletion rollback. A failed request can therefore become a confirmed created resource after recovery; inspect state before repeating a create. Startup reconciles the journal before accepting further upstream changes. Pre-write and post-save checks return conflicts for observed external consumer edits. External writers do not share the mutex and can race after a check; stop other administrative writers for coordinated changes.

On master-managed container recreation, an owned `docker-compose.hyperhive-upstreams.yml` adds the adapter mounts without replacing the base compose file. Adapter source files live in `npm-data/hyperhive-upstream-adapter`, outside NPM's recursive `/data/nginx` startup scan. Only the managed common proxy include is mounted writable because NPM's IPv6 preparation rewrites conf.d files; template mounts are read-only. Unsupported images, ambiguous multi-service compose files and conflicting custom template mounts fail closed. Use master-managed startup or the generated compose override when recreating NPM.

## Release and recovery procedure

1. Stop concurrent administrative writes and take consistent backups of HyperHive's SQLite DB (including a SQLite backup rather than a live main-file-only copy), NPM data, certificate data, base/override compose files, adapter assets and generation tree. Keep backups private.
2. Ship the updated master, frontend and standalone MCP binary together. Start the master against the verified image, inspect capabilities, then create an unused pool before binding consumers. First use installs the adapter; subsequent master-managed recreation installs persistent mounts. Do not upgrade NPM independently while pool bindings exist.
3. After a 503, inspect capabilities/recovery_id, the journal and container diagnostics before another mutation. Repair unavailable storage/NPM or invalid custom configuration, then restart the updated master to reconcile. Unknown create outcomes without an observable operation marker remain recovery-required; do not repeat POST or hand-edit journal state without checking the actual NPM resource and completing that operation. Back up state before manual repair.
4. For downgrade, explicitly detach every root/location/stream binding to valid direct endpoints, or disable affected consumers and retain their pool configuration until their references are removed. Remove unused pools only after references are gone. Verify direct traffic, then restore compatible backed-up state or remove the owned adapter mounts/includes and recreate the vanilla container. Never remove pool files while consumers still reference them. Additive DB tables may remain during a binary downgrade.

SQLite, NPM HTTP and NGINX are coordinated with a journal and compensation; they are not one atomic transaction. External writers are detected but do not participate in HyperHive's mutex. This is process/crash recovery, not a guarantee of distributed or power-failure atomicity.

## Local verification

Run `python3 master/internal/testnpm/compatibility.py --output /tmp/hh-upstream-proof` from the repository root. It uses a uniquely named isolated container, disposable volumes and no host listener or production credentials. It exercises exact-image templates, all advertised methods, affinity/distribution, HTTP/custom locations/TCP/UDP, IPv4/IPv6, shared membership, TLS SNI, WebSockets, drift, failure compensation and recreation. Temporary runtime evidence belongs outside the repository; the concise compatibility report records the checked boundary.

Run full master/MCP Go tests, relevant race/vet checks and the frontend Jest, TypeScript and web build checks before release. These checks do not establish production deployment or real ACME issuance.
