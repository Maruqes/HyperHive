# AGENTS.md — master

Instructions for AI coding agents working in the master module (`512SvMan`). Read the root `AGENTS.md` first for the system model, feature workflow and SilverBullet rules.

## What the binary does

Runs as root (`--exit-after-start` = first-run setup, then exit). Startup order in `main.go`:

1. Load env (`env512`), logging, clean SQLite backups, open SQLite (`db.InitDB`), restore SPA firewall rules.
2. Ensure noVNC, GoAccess, WireGuard; restore VPN peers.
3. Create tables per feature (NFS, VMs/disks/templates, logs, ISOs, info, SMART, backups, Btrfs, Docker, notifications, notes) and start the SQLite backup loop.
4. Install the frontend release and its Nginx container (failure is fatal).
5. Start slave pings and the gRPC registration listener on `:50051` with the `newSlave` callback (Btrfs auto-mount, NFS readiness, delayed workload autostart).
6. Start schedulers: NFS maintenance, automatic VM backups, SMART schedules, host notifications, SPA maintenance.
7. `api.StartApi`: provision NPM, build chi routes, start localhost pprof (`127.0.0.1:6060`) and the websocket heartbeat, then serve HTTP + `/mcp` on `:9595` (blocks).

Many startup steps exit the process on error. Signal handling after `StartApi` never runs during normal serving, so don't rely on graceful shutdown hooks.

## Package map

| Package | Responsibility |
| --- | --- |
| `api/` | HTTP handlers and route setup. Decode, validate, call a service or db, write the response. |
| `api/npmapi/` | HTTP handlers for proxies, streams, redirections, 404 hosts, certificates, access lists and upstreams. |
| `services/` | Business logic: combine DB metadata with slave RPCs, pick the target slave, manage multi-step operations and cleanup. |
| `db/` | SQLite access. One file per area with `Create<Area>Table`, typed structs and context-aware queries. |
| `protocol/` | Slave registry, connections, pings and the registration gRPC server. |
| `virsh/`, `vm_disk/`, `nfs/`, `btrfs/`, `docker/`, `k8s/`, `pci/`, `info/`, `smartdisk/`, `extra/` | Thin gRPC client wrappers: one function per RPC, each with its own `context.WithTimeout`. |
| `npm/` | Client for the local Nginx Proxy Manager API. |
| `websocket/` | Browser websocket registry and `BroadcastMessage`. |
| `nots/` | WebPush and global notifications. |
| `env512/` | Environment configuration. |
| `wireguard/`, `spa/`, `dnsmasq/`, `supportdeploy/`, `internal/` | Feature-specific subsystems and test helpers. |

## Where new code goes

- **New HTTP endpoint**: add a handler in `api/<area>.go` and register it in that file's `setup<Area>API(r chi.Router)`. Call that from the **authenticated** group in `api/api.go`. Public routes are rare and need an explicit reason.
- Handlers stay thin: no SQL, no direct gRPC, no exec. Use `writeJSONWithStatus` and the matching status codes (400 invalid input, 404 missing, 202 accepted async, 500 internal). Map `sql.ErrNoRows` to 404.
- **Business logic** goes in `services/<area>.go` as methods on a `<Area>Service` struct. Return typed result structs with JSON tags, not protobuf types, to the API.
- **Slave call**: add one function per RPC in the domain client package, taking `*grpc.ClientConn` and a request, with a named per-area timeout constant. Long jobs get their own longer bound or a streaming RPC, not the default.
- **Persistence**: new table goes in `db/<area>.go` with `CREATE TABLE IF NOT EXISTS`. Add its `Create…Table` call to `main.go` startup in the right stage. Schema changes must be additive and migrate existing rows (follow the `ensure…Column` pattern). Always use the `ctx` variants (`ExecContext`, `QueryContext`) and close rows.
- **Async work**: return immediately with "started" and report progress with websocket messages or notifications. Store job state if the UI or MCP needs to poll it.
- **Order writes** so a failure leaves a recoverable state, for example host first, then DB, or DB marked pending, then host, then committed. Document the partial-failure result in the handler or service.

## Rules

- The MCP server and its auth bridge live in `api/mcp.go`. Tools themselves live in `../mcp`. Any API change used by MCP must keep the MCP contract tests passing.
- Build check: `go build -tags libvirt_dlopen ./...`; tests: `go test -tags libvirt_dlopen ./...`. Known failing tests: `TestMCPNginxThroughHandlers` and `TestMCPProxyMutationsThroughHandlers` (their SDK clients lack approval elicitation).
- `make run` / `make first` build Tailwind (`npm`) and start master **with sudo**. Don't use them as checks.
- Don't edit `data.db*`, `logs512/`, `static/` build output, `novnc/` or the `512SvMan` binary. Tailwind sources compile to static CSS; change sources, not output.
- Update SilverBullet when a feature is done (root `AGENTS.md`).
