# HH Support integration

HH Support is an independent Go service with its own SQLite database. The existing HH frontend uses it through the master API. API/storage setup and ingestion examples are in [the service README](../support/README.md); the wire contract is in [the implementation design](designs/hh-support.md). Backend packages, dependencies and migration rules are documented in [the architecture guide](../support/ARCHITECTURE.md). From `support/`, run or build `./cmd/hh-support`.

## Start and route

1. Start the updated HH master on the Linux Docker host with Docker Engine and Compose 2.30 or later (`up --wait`, raw environment files) available. It automatically builds the embedded Support source and reconciles the `hyperhive-support` service before starting the API. First boot needs Internet access for base images and Go modules; unchanged sources reuse the local image.
2. Defaults are `SUPPORT_URL=http://127.0.0.1:8091` and `SUPPORT_STATE_DIR=/opt/hyperhive/support`. Master persists a generated shared secret in that private state directory (0700, files 0600). An explicit `SUPPORT_PROXY_SECRET` overrides it. Keep the secret server-side and back up this directory. Set `SUPPORT_AUTO_DEPLOY=false` to manage Support separately; then set the same secret (at least 32 characters) in master and Support. Restart master after configuration changes.
3. Existing reverse-proxy routing of `/api` to master also carries `/api/support`. Master reserves `/support` after stripping `/api` and strips `/support` before forwarding. Keep WebSocket upgrades enabled on external proxies. The static frontend container does not own API routing.
4. Build the frontend from its `HyperHiveFront/HyperHive` checkout, configure its API base as usual, and open **Logs → HH Support** in HH navigation. System logs remain at `/logs`; Support remains at `/support`.

Support allows all browser origins by default: unset/empty `SUPPORT_ALLOWED_ORIGINS` and `SUPPORT_ALLOWED_ORIGINS=*` both disable Origin restrictions. HH authentication and single-use WebSocket tickets remain required. To restrict access by browser origin, configure comma-separated exact origins such as `https://hh.example.com`; same-host sockets and clients without Origin remain accepted. Allowing all origins removes the browser-origin defense, so prefer an explicit allowlist for sensitive deployments. Use HTTPS externally and authenticated TLS/private networking between hosts. The default Support listener is loopback; keep its port private.

Expo development at `http://localhost:8081` works against a separate master without extra Origin configuration. If using an explicit allowlist, include the page's exact origin, including scheme and port, without a trailing slash; `127.0.0.1` and `localhost` are different origins. Restart master after configuration changes. The authenticated ticket endpoint reports missing Origin permission before the browser attempts a WebSocket, while REST management remains available. A socket path starting `/support/ws` is correct when the saved API base has no `/api` prefix; master accepts both forms.

## Authentication

Management operations use existing HH login permissions. Master authenticates them, removes HH session credentials and injects `X-HH-Support-Key`. Support validates this shared secret. Missing configuration or an unreachable service affects only Support routes and produces 503.

Only exact `POST /api/support/ingest` bypasses HH login; Support validates `X-App-ID` and `Authorization: Bearer <App secret>`. App creation and rotation show the secret once. Rotation invalidates old sender credentials immediately.

Live frontend connections request authenticated `POST /api/support/ws-ticket`. Its ticket establishes one WebSocket connection at `/api/support/ws?ticket=...` within 30 seconds. Master atomically consumes it and checks browser Origin. HH request logs omit Support query strings. Configure external access logs to omit query strings for this endpoint too.

## Operational boundaries

Support data and shared layouts persist in SQLite in the named Docker volume `hyperhive-support-data`, mounted at `/data`. Back it up independently using a SQLite-consistent backup. The container uses host networking but listens only on loopback: public access still goes through the master. Master must run on the Docker host or share its host network, not an unrelated bridged network. Runtime UID/GID is `10001:10001`, with read-only root filesystem, dropped capabilities and no-new-privileges.

Master waits up to 60 seconds for container health after building (the overall deployment is bounded to 15 minutes). Failures are logged and HH continues startup; generated proxy credentials are only enabled after readiness. A failed build does not replace the existing container. Reconciliation does not delete data or stop Support when master exits; Docker uses `unless-stopped`. Conflicting unmanaged containers are refused. If a managed container or data volume exists but its persisted secret is missing, restore the secret or explicitly configure it; do not delete the volume to repair startup. Source updates may migrate SQLite; automatic database downgrade/rollback is not provided.

App deletion permanently deletes history, instances, streams and layout. A discovered instance stays monitored for the App lifetime; there is no retirement workflow. Connectivity and explicit health are separate.

Retention bounds sample age, not total disk use. History/live buffers are bounded; high ingestion can still fill disk within retention. Ingestion is at-least-once and retries can duplicate samples. Reconnect reloads retained snapshots/history; it does not promise replay of every live frame.

Automations and VM-agent implementation remain deferred. This adds automatic deployment on master startup, not a Git-branch push/CI deployment pipeline. No production host rollout is implied by local validation.
