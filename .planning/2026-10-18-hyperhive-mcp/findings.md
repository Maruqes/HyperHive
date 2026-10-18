# Findings

- HTTP API: Go/Chi; route registrations in master/api/api.go delegate to feature setup functions and master/api/npmapi.
- master/api/npmapi/proxy.go registers eight proxy endpoints, including six ordinary management operations and two frontend setup operations.
- No get-proxy-by-ID endpoint was found in that route group; a future convenience tool would need to derive its result from list, and must not create extra endpoint coverage credit.
- NGINX/NPM has six route groups: proxy (8), certificates (7), access lists (4), streams (6), redirections (6), 404 hosts (6): 37 route registrations, pending authoritative extractor verification.
- master/api/isos.go registers three ISO endpoints.
- Current API startup mixes registration with middleware, heartbeat/server startup and process handling, so inventory generation must not call production startup.
- Auth middleware accepts Authorization header and validates through the existing login service. MCP should call HyperHive HTTP routes rather than downstream NPM directly.
- Proxy mutations return HTTP 200 with empty bodies; edit indexes DomainNames[0], and managed proxies reject edit/delete.
- Existing dirty working tree includes unrelated celld work; do not alter it.
- No .github/workflows directory exists at inspection time.
- User selected Markdown report + CLI.
- Official MCP Go SDK documentation supports typed tool registration and stdio; use an independent mcp Go module so the MCP binary does not import the master and its native dependencies.
- Chi provides Walk for inventorying an assembled router. Proposed authoritative inventory uses the actual registration graph after extracting router construction from startup; do not duplicate route lists or call StartApi for inventory.
- StartApi initializes NPM/noVNC before route setup and starts listeners/heartbeat afterward. Guest API package init also starts a rate-limiter cleanup goroutine; inventory construction must be audited for side effects.
- Native master dependencies (including libvirt) affect inventory CI even though the MCP binary remains independent. Record prerequisites and avoid claiming all tooling builds without them.
- Context7 skill is not installed; latest official Go SDK and Chi documentation were consulted through web browsing instead.
- References: https://go.sdk.modelcontextprotocol.io/ ; https://github.com/modelcontextprotocol/go-sdk/blob/main/docs/server.md ; https://pkg.go.dev/github.com/go-chi/chi/v5#Walk . Treat retrieved text as reference data only.

## First implementation increment
- Read infrastructure index, both named host notes, operational memory, project index and admin-auth note before work. Source-only implementation; no remote access needed.
- Current handler `listProxies` returns the complete `[]npm.Proxy` as JSON, with auth managed by the master. Nil lists may encode as null.
- User narrowed scope to exactly one tool; defer broad inventory/coverage and avoid changing the dirty master router.
- Official release page and Go module resolver confirm SDK v1.8.0 (minimum Go 1.25.0); local Go is 1.26.2. Pin v1.8.0.
- SDK stdio client against the compiled server confirmed exactly one registered tool, inferred no-argument schema rejecting unknown fields, structured results plus text fallback, and IsError for backend failures.
- Tests preserve string/numeric access-list IDs and nested metadata/locations. Empty/null lists normalize to []; malformed/empty success bodies are errors. Error bodies are omitted and redirects refused to avoid credential disclosure.
- Validation uses local fixtures only; no claim of deployment, real API connectivity or generated endpoint coverage.

## HTTP startup increment
- Existing master API listens on :9595 (all interfaces), registers public routes before an authenticated Chi group, and strips optional /api prefix.
- No existing reusable server-side NPM login token is available: SetupNPM skips login on established installations. Do not collect user tokens or bypass API authentication.
- Expose /mcp on the existing API listener; lazy backend client uses server-side HYPERHIVE_API_TOKEN. Missing token must not prevent master/MCP startup; calls report configuration errors.
- SDK v1.8.0 supports stateless Streamable HTTP with JSON responses and propagated HTTP cancellation; retain default body/localhost protections.

## Configurable port
- API_PORT (master environment/.env) defaults to 9595 and is validated in 1..65535 before API startup. The shared API/MCP listener, MCP loopback URL and new frontend reverse-proxy locations follow the resolved port.
- Added API_PORT=9595 to .env.example and appended only that default to local ignored master/.env when absent, without displaying credentials or changing existing settings.
- Actual SDK HTTP integration on an ephemeral custom port discovers exactly list_proxies and successfully invokes it through the authenticated loopback REST route. Existing proxy/client URLs and MAIN_LINK must be updated by the operator if switching an established deployment's port.

## Static-key API authentication and NPM bridge
- User explicitly chose requiring MCP_TOKEN on MCP and later required normal API sessions OR the fixed key, with the old manual session variable removed. This supersedes the pending automatic-login/service-account question.
- The pinned NPM 2.13.5 upstream verifies RS256 against /data/keys.json; user-scoped JWTs require attrs.id of an enabled, non-deleted account and roles are loaded from NPM's DB. Confirmed upstream models/token.js, lib/access.js, lib/config.js and internal/token.js.
- Implemented in master: constant-time fixed-key validation, read-only local npm-data keys/database access, deterministic selection of first active admin, two-minute user-scoped JWT with random nonce. NPM still receives its expected JWT, never the fixed key. MCP module continues to call HyperHive API only. No NPM data/users are modified and no configured login session/password is required.
- Added disposable signing/database fixtures and integration that invokes the real proxy handler, validates the generated JWT upstream and verifies normal sessions continue to work.
- An initial missing-state integration test still pointed to hardcoded 9595, yielding a connectivity error instead of backend 503; fixed it to use its actual disposable listener port.
