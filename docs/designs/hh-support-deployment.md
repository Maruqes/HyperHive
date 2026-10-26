# HH Support managed deployment

Date: 2026-10-26. Scope: local implementation contract for the frontend/deployment follow-up in `.planning/2026-10-26-hh-support/frontend-deployment-plan.md`. Source-reviewed against Support configuration/startup, `support/go.mod`, master startup and Support proxy; no production deployment is asserted.

## Agreed boundary

Embed the actual Support build context in a root package of the `hyperhive/support` module and import that package from master. Keep service internals private. The embedded context includes Dockerfile, go.mod/go.sum, command, all internal Go sources and SQL migrations. Extract to a protected temporary directory, build a local image tagged with a deterministic source hash, remove the temporary context and reconcile only the managed Compose project. No registry publication or guessed published image. Documentation and tests exercise the standalone master build dependency, not only `go test` inside Support.

Defaults: Compose project, service and container `hyperhive-support`, image `hyperhive-support:<source-hash>`, host state directory `/opt/hyperhive/support`, named volume `hyperhive-support-data`. Generated files belong to the master account, directory 0700 and secret-bearing files 0600. Labels identify HyperHive and Compose ownership; the image tag identifies source content. Before reconciling, inspect any existing container with the chosen identity; refuse a conflicting unmanaged container rather than replace it. No global Docker pruning, volume deletion or orphan removal.

## Network and configuration

Use Linux Docker `network_mode: host`, with no Compose `ports` mappings. Set service `SUPPORT_LISTEN=127.0.0.1:8091`, `SUPPORT_DB_PATH=/data/support.db`; master targets `http://127.0.0.1:8091`. HTTP ingestion and browser traffic still go through master `/api/support`; host networking does not mean exposing Support on all interfaces. Native Docker Engine on Linux is the supported target. A master running inside an unrelated bridged container cannot reach host loopback under this contract; document that limitation rather than invent a host gateway.

Default managed deployment on. `SUPPORT_AUTO_DEPLOY=false` explicitly opts out and preserves the existing external `SUPPORT_URL` and `SUPPORT_PROXY_SECRET` behavior. An explicitly external URL must not silently be overwritten by managed startup: require the documented opt-out or reject the conflicting managed configuration with an actionable error. Validate URL/listen compatibility before deployment; reject non-loopback managed URLs. Use existing `SUPPORT_ALLOWED_ORIGINS` unchanged for browser WebSocket access.

## Credentials and storage

If `SUPPORT_PROXY_SECRET` is supplied, use that explicit value consistently for both master and container. Otherwise reuse the protected persistent generated secret, or generate at least 32 random bytes only on first managed initialization. Write the generated value atomically before Docker reconciliation. Never regenerate on ordinary restart/build failure; an existing managed deployment with a missing persisted secret requires explicit recovery, not silent credential replacement. Never put secrets in image layers, build arguments, image tags or logs. Generated Compose/env files can contain the runtime value only with 0600 permissions. Docker administrators can inspect container environment by design.

Use one stable named volume mounted at `/data`; changing source hash must not change volume identity. SQLite WAL/SHM files share that directory. The runtime runs under a fixed numeric nonroot UID/GID and `/data` is owned by that identity in the image before Docker initializes a fresh volume. Read-only root filesystem, dropped capabilities and no-new-privileges are compatible if `/data` remains writable. If an existing volume has incompatible ownership, fail with a clear repair instruction; do not chmod 777, delete data or recursively chown arbitrary host paths. Retain data on master/container restart and service image replacement.

## Image and readiness

`github.com/mattn/go-sqlite3` requires CGO. Builder must provide a C compiler and `CGO_ENABLED=1`; choose compatible builder/runtime libc, for example Debian-to-Debian, instead of a CGO binary copied into scratch or incompatible Alpine. Match the declared Support Go version. Build for the host architecture unless cross-compilation explicitly supplies an appropriate C toolchain.

Provide a Go healthcheck command in the same executable: HTTP GET loopback `/healthz`, short timeout, exit nonzero for connection error/non-200. It loads environment configuration but does not open SQLite or run migrations. Health endpoint already pings SQLite. Docker healthcheck start period allows migrations; `compose up -d --wait --wait-timeout 60` waits for healthy service, not merely a running container. The whole deployment, including downloads/build, has a 15-minute context. Missing Docker daemon or Compose v2/`--wait` support returns a deployment error. Restart policy `unless-stopped` keeps Support alive independently of master.

## Master lifecycle and failure behavior

Initialize after environment loading and before `api.StartApi`, so the proxy captures the same URL and resolved shared secret used by the service. Set the resolved environment before constructing the proxy. Keep source extraction/build/reconciliation in `master/supportdeploy`; coordinator owns only the main startup call and log handling. Run with a bounded context; no shell interpolation of environment values. A failure is logged once with an actionable cause and HH continues startup. Existing external configuration must remain usable on deployment failure; support routes return existing 503 behavior when no service is available. Do not stop or remove Support on master exit. A subsequent master boot retries reconciliation; do not add a background deployment scheduler.

## Frontend acceptance and checks

Logs navigation expands to System logs (`/logs`) and HH Support (`/support`), preserving existing URLs and shared HH layout. A visible unavailable state must explain that managed deployment failed or external service configuration is missing; it must not present an endless spinner. Backend configuration/bootstrap endpoints, if needed for setup UX, must remain HH-authenticated and must never return the shared proxy secret. Frontend only receives per-App ingest credentials through existing one-time flows.

Focused checks: deterministic source hash and embedded migrations; protected/reused secret; external opt-out; unmanaged project collision; command failures/timeouts; generated host-network Compose with persistent volume; same credential forwarded by master. Build and run the actual image against disposable named data, ingest once, recreate the container and verify retained records plus healthy nonroot startup. Verify frontend build/types, navigation and browser workflows. No production host changes are part of this implementation.

## Risks and remaining evidence

No design blocker found. First startup requires Docker build access to the selected Go/runtime images and module downloads; later source changes rebuild the local image. Port conflicts and damaged/full volumes correctly fail readiness. Host root/Docker administrators can read the stored trust secret. Local image/run validation is required before claiming the nonroot volume behavior or libc compatibility works.
