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

The supported release is NPM 2.16.0 with OpenResty 1.31.1.1. The desired tag is defined once by `image` in `master/npm/npm.go`. The exact Docker image config ID and registry manifest digest are recorded separately in `npm-upstreams-compatibility.json`; a config ID must not be used as a `repository@digest` registry reference. Native host fields append ports and failed all four pool traffic contexts. The adapter changes only pinned proxy_host.conf, _location.conf, stream.conf and the common proxy.conf include; direct targets retain their forwarding port. NPM startup may append blank lines to the common include; verification normalizes only its trailing LF bytes while keeping directive content pinned. Exact source fingerprints are in `npm-upstreams-compatibility.json`.

Generated files live under `/data/nginx/custom/hyperhive/generations/`, selected by one `current` symlink. Managed includes are appended to http_top.conf and stream.conf while preserving existing content and stream logging. A complete candidate is tested with the container's NGINX before publication; a Unix-socket generation response verifies activation. Failed edits compensate to the previous snapshot; uncertain outcomes remain journaled and block mutations. Confirmed creates are completed during reconciliation instead of deleting the new resource, so MCP creation has no deletion rollback. A failed request can therefore become a confirmed created resource after recovery; inspect state before repeating a create. Startup reconciles the journal before accepting further upstream changes. Pre-write and post-save checks return conflicts for observed external consumer edits. External writers do not share the mutex and can race after a check; stop other administrative writers for coordinated changes.

On master-managed container recreation, an owned `docker-compose.hyperhive-upstreams.yml` adds the adapter mounts without replacing the base compose file. Adapter source files live in `npm-data/hyperhive-upstream-adapter`, outside NPM's recursive `/data/nginx` startup scan. Only the managed common proxy include is mounted writable because NPM's IPv6 preparation rewrites conf.d files; template mounts are read-only. Unsupported images, ambiguous multi-service compose files and conflicting custom template mounts fail closed. Use master-managed startup or the generated compose override when recreating NPM.

## Upgrade from the previous adapter

The latest adapter does not support the old NPM image. The approved migration deletes all existing upstream pools and proxy hosts/streams that reference them, including proxy hosts with only a pooled custom location. It keeps direct-target resources, users, certificates, access lists, redirections and unrelated configuration.

From the repository root, after obtaining this source update:

```bash
sudo pm2 stop hyperhive-master
go -C master build
sudo python3 scripts/update_npm.py --reset-upstreams
```

Only after the script succeeds and traffic is checked:

```bash
sudo pm2 start hyperhive-master
```

An old master binary is optional; the updater does not require or reject a saved binary based on its adapter version. The script checks the rebuilt master's read-only `--npm-upgrade-manifest` against the source image pin, validates the pulled image's exact identity, and rejects unknown overrides, conflicting template mounts, external databases and nested data mounts.

Before any destructive work it stops NPM and creates a private backup in the NPM working directory's `npm-update-backups/`: both data directories, a consistent HyperHive SQLite backup, base/managed override Compose files and an optional saved master binary when master/512SvMan.npm-previous exists. It retains the running image under a local rollback tag. With `--reset-upstreams` it also deliberately discards pending pool journals: the entire old pool subsystem and its pooled consumers are being removed, not reconciled.

The script soft-deletes pooled resources in the stopped NPM SQLite database, removes their generated configuration, clears HyperHive pool/binding/journal records, removes owned adapter files/includes, updates only the base image line and recreates NPM without the old override. Unrelated hook content is preserved. The new adapter is installed when new pools are created with the updated master. NGINX syntax and API readiness are checked; certificate issuance/renewal still needs operator testing.

If anything fails after the complete backup, keep the master stopped and run the exact printed rollback command:

```bash
sudo python3 scripts/update_npm.py --rollback /absolute/path/to/npm-update-backups/TIMESTAMP
```

Rollback restores the previous image/configuration, NPM data, HyperHive SQLite state and the saved master binary when available. Restore the older Git revision and rebuild the master after NPM rollback if that binary is unavailable or incompatible. Do this before restarting PM2. It preserves the newer data directories and SQLite files separately instead of merging or deleting them. Restart PM2 only after rollback succeeds. This restores the whole backed-up HyperHive database, including unrelated metadata changes made since the backup; keep administrative writers stopped throughout testing and rollback. Do not prune the retained old image or delete the private backup before accepting the upgrade.

SQLite, NPM HTTP and NGINX are coordinated with a journal and compensation; they are not one atomic transaction. External writers are detected but do not participate in HyperHive's mutex. This is process/crash recovery, not a guarantee of distributed or power-failure atomicity.

## Local verification

Run `python3 master/internal/testnpm/compatibility.py --output /tmp/hh-upstream-proof` from the repository root. It uses a uniquely named isolated container, disposable volumes and no host listener or production credentials. It exercises exact-image templates, all advertised methods, affinity/distribution, HTTP/custom locations/TCP/UDP, IPv4/IPv6, shared membership, TLS SNI, WebSockets, drift, failure compensation and recreation. Temporary runtime evidence belongs outside the repository; the concise compatibility report records the checked boundary.

Run full master/MCP Go tests, relevant race/vet checks and the frontend Jest, TypeScript and web build checks before release. These checks do not establish production deployment or real ACME issuance.
