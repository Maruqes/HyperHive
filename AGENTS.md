# AGENTS.md — HyperHive

Instructions for AI coding agents working in this repository. `CLAUDE.md` files are symlinks to the `AGENTS.md` in the same folder; edit `AGENTS.md`.

## Knowledge base (SilverBullet / "Silverfish")

- Use the `hyperhive-infrastructure-memory` skill. Before working on code, architecture, MCP, infrastructure or decisions, read the relevant SilverBullet entry. Read only what the task needs:
  - code, architecture, decisions → `knowledge/projects/HyperHive/index`
  - MCP → `knowledge/projects/HyperHive/features/mcp/index`
  - hosts and operations → `knowledge/infrastructure/HyperHive/index`
- When a feature is fully done (implemented, tested, affected modules build), update the canonical SilverBullet pages through that skill: the feature page, the domain index, the HTTP/RPC inventories and the source map, as affected. Edit the topic in place rather than appending a log. Keep it small, in English, with no secrets, and state what was verified locally vs. live.
- Skip routine progress. Add an operational-memory entry only for significant operational events or decisions.
- SilverBullet documents source behavior; it does not prove deployment or current tool availability, and it does not authorize operations on hosts.

## System at a glance

| Component | Module | Role |
| --- | --- | --- |
| `master/` | `512SvMan` | Control plane: HTTP API + MCP on `:9595`, SQLite metadata, NPM integration, frontend install, schedulers, websocket, notifications. gRPC registration listener on `:50051`. |
| `slave/` | `slave` | Privileged host agent on every machine (the master host runs one too). gRPC services on `:50052` that drive libvirt, qemu-img, Docker, NFS, Btrfs, SMART, PCI, k3s, celld. |
| `api/` | `github.com/Maruqes/512SvMan/api` | `.proto` contracts in `api/proto/` and their generated Go bindings. Contract of record between master and slave. |
| `logger/` | `github.com/Maruqes/512SvMan/logger` | Shared logger; slaves forward logs to master. |
| `mcp/` | `github.com/Maruqes/HyperHive/mcp` | MCP tool server, embedded in master (`/mcp`) and standalone over stdio. Has its own strict `mcp/AGENTS.md`; read it before touching MCP. |
| `support/` | separate module | HH Support service (own SQLite), deployed by master via Docker. See `support/ARCHITECTURE.md`. |

### Request flows

- **Host operation** (VMs, disks, NFS, Btrfs, Docker, PCI, info…): `master/api` handler → `master/services` → master domain client (`master/virsh`, `master/vm_disk`, …) → gRPC → slave `*Service` handler → slave domain code → host tool.
- **Proxy/certs**: `master/api/npmapi` → `master/npm` → local Nginx Proxy Manager API. Never via a slave.
- **Master-only metadata** (notes, descriptions, schedules): handler → `master/db`.
- **Progress / live updates**: websocket broadcast to browsers. Slaves push messages and notifications back through the master's `ExtraService`.
- **MCP**: MCP tool → shared HTTP client → master REST API. MCP never bypasses handlers.

### State model

State lives in many places: SQLite, libvirt domain XML, filesystem, mounts/exports, firewall/ipset rules, Docker, NPM. No transaction spans them. Every multi-step write must define its order, what a partial failure leaves behind, and its cleanup or rollback. Treat the SQLite row as *intended* config and the host as the *actual* state.

## How to add or change a feature

1. Find the real state authority and path: master-only DB, slave RPC, or NPM.
2. Define inputs, units, validation, outputs and failure semantics. Decide if the call is synchronous or only *starts* async work (return 202 / "started" and report progress, never fake completion).
3. Slave: implement domain logic in the domain package, then expose it in that package's gRPC service.
4. Contract: edit `api/proto/<area>.proto`, regenerate (root `make setup`), never hand-edit `*.pb.go` / `*_grpc.pb.go`. Keep field numbers stable; add fields instead of renumbering.
5. Master: thin client function in the domain package (own timeout), orchestration in `services`, persistence in `db`, route in `api` inside the correct auth group.
6. Add tests for behavior and meaningful failure paths. Build and test every affected module.
7. Update SilverBullet (see above).

## Build, test, run

- Five Go modules on Go 1.27.3, linked by `replace` directives. There is no `go.work`; run commands inside each module.
- Without libvirt dev headers: `go build -tags libvirt_dlopen ./...` and `go test -tags libvirt_dlopen ./...`.
- `make run` / `make first` in `master/` and `slave/` start the binaries **with sudo and change the host** (packages, services, firewall, kernel settings). They are not build checks. Never run them unless the user asks.
- Don't run privileged tests (Btrfs, host tooling) or touch live infrastructure unless asked.

## Coding rules

- Go, idiomatic and matching the surrounding package. Run `gofmt`.
- English only for new code, comments, logs, errors and docs. Some legacy code has Portuguese comments or identifiers (for example `db.Note.Titulo`); keep existing JSON/API field names stable, don't rename them.
- One feature area = one file per layer, with the same name across layers (`api/vm_disk.go`, `services/vm_disk.go`, `db/vm_disk.go`, `vm_disk/`, `slave/vm_disk/`). Add a new file rather than growing an unrelated one.
- Pass `context.Context` through and respect cancellation. Every outbound call (gRPC, HTTP, exec) needs a timeout.
- Wrap errors with context (`fmt.Errorf("grow disk %s: %w", name, err)`). Don't swallow errors; if one is intentionally ignored, log it and say why in a comment.
- Log with `github.com/Maruqes/512SvMan/logger`. Never log secrets, tokens or full request bodies that may contain them.
- No new global mutable state without a mutex. Background loops must be stoppable or clearly process-lifetime.
- Destructive operations (delete, wipe, detach, firewall replace) must check preconditions first and fail closed.
- Never commit secrets, `.env` values, `data.db*`, logs or built binaries.

## Repository hygiene

- `notes/`, `.planning/`, `plan/`, root `*-plan.md`, `test-results/` are dated history, not current behavior. Verify against source before relying on them, and don't treat old PASS reports as fresh.
- Local tests and builds are not deployment verification. Never claim live state without evidence.
