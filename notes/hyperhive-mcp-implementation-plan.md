# Plan: HyperHive MCP tools and API coverage

Generated: 2026-10-18. Complexity: medium. Status: implementation proposal; no MCP code implemented by this planning task.

Implementation update (2026-10-18): the user narrowed the first increment to
starting the server with exactly one tool, `list_proxies`. That increment now
lives in `mcp/` (see `mcp/README.md`); the broader sprints below remain the
roadmap. Router extraction, generated inventory/coverage, mutations and CI have
not been implemented in this increment.

Follow-up (2026-10-18): the user requested automatic startup on an exposed port
without MCP authentication. HyperHive master now embeds the shared MCP server at
`http://<hyperhive-host>:9595/mcp` using stateless Streamable HTTP, outside API
login middleware. The tool still calls the protected loopback HyperHive API with
server-side credentials (historical behavior, superseded below); missing credentials become invocation errors,
not startup failures. Standalone stdio remains available. This supersedes the
initial stdio-only/remote-transport deferral; multi-user auth is still deferred.
The shared listener port is configured with `API_PORT` in the master's `.env`
(default 9595), including loopback calls and newly generated frontend proxy targets.

Authentication update (2026-10-18): the user replaced the initial public-MCP
and manually configured API-session design. HTTP MCP now requires a fixed
`MCP_TOKEN` Bearer key. REST authentication accepts normal sessions or this key;
the master creates two-minute NPM credentials from its existing local signing key
and active administrator record. No additional login-session environment variable
is used. All code, scripts, documentation and knowledge entries must be English.

## Outcome and scope

Build a small Go MCP server with explicit, typed tools. Start with proxy management and expand by feature. The server calls HyperHive's existing HTTP API, retaining its authentication and business rules. It does not call NGINX Proxy Manager directly or import master services.

Maintain a generated Markdown report and CLI showing endpoint coverage by area and subarea, with exact missing operations, source locations and implementation status. These are the user's selected first-version reporting surfaces. A dashboard can consume the same JSON later.

No full OpenAPI conversion is required. No generic HTTP execution or operation-search tools are required. The first usable release exposes proxy tools; the first infrastructure increment establishes coverage before adding tools. Remote MCP hosting, multi-user auth, and a dashboard are later work.

## Source findings and assumptions

- `master/api/api.go`: `StartApi` currently combines NPM/noVNC initialization, middleware, route registration, listeners and heartbeat startup. It strips an optional `/api` prefix.
- `master/api/npmapi/proxy.go`: eight registered proxy endpoints. Six cover list/create/edit/delete/enable/disable; two configure frontends.
- NGINX/NPM comprises six route groups: proxies (8), certificates (7), access lists (4), streams (6), redirects (6), 404 hosts (6). The observed total is 37; the generator will verify the authoritative total.
- `master/api/isos.go`: three ISO endpoints. Download is asynchronous; the handler reports acceptance, not completion.
- `master/api/virsh.go`: VM operations include lifecycle, settings, migration, disks and backups. Prefixes are not sufficient semantic categories; VM disks/noVNC and host tuning need explicit ownership decisions in taxonomy.
- `master/api/login.go`: existing Authorization-based authentication is reusable. Initial MCP configuration uses an existing token; credentials are not tool inputs.
- `master/npm/proxy.go`: request type includes advanced configuration, locations, TLS flags and string-or-integer access list IDs. Proxy edit currently expects a complete object; its handler indexes the domain list. Some managed proxies reject edit/delete.
- No direct get-proxy-by-ID route was found. A convenience `get_proxy` could filter the list later, but must not earn another endpoint credit.
- Proxy mutation handlers can return empty successful responses. Tools must not invent created IDs or assume every success has JSON.
- No MCP implementation or full OpenAPI document was found in the targeted inspection. Existing unrelated local celld work must be preserved.

Default implementation choices: standalone `mcp/` Go module, official Go MCP SDK, stdio transport, one configured HyperHive base URL and token, all six core proxy tools enabled. Pin a compatible stable SDK release during implementation; this plan does not prescribe an unverified version.

## Architecture and proposed files

```text
AI client -> named MCP tool -> shared HTTP client -> HyperHive API

Actual Chi router -> inventory.json -------+
Registered MCP definitions -> tools.json --+-> coverage engine -> CLI + Markdown
Reviewed endpoint policy -----------------+
Source fingerprints ----------------------+
```

```text
master/api/router.go                    # Router construction without service startup
master/cmd/api-inventory/main.go         # Walk the same router used in production
mcp/go.mod
mcp/cmd/hyperhive-mcp/main.go            # serve, coverage, describe commands
mcp/internal/client/                    # HTTP configuration, auth, error handling
mcp/internal/tools/registry.go          # Single source of tool definitions + mappings
mcp/internal/tools/proxies.go           # First six tools
mcp/internal/apicoverage/               # Inventory types, taxonomy, drift, reports
mcp/catalog/policy.json                 # Reviewed areas, exclusions, aliases
mcp/catalog/inventory.json              # Generated route/source inventory
mcp/catalog/tools.json                  # Generated from actual tool definitions
mcp/catalog/coverage.json               # Generated machine-readable coverage
mcp/docs/coverage.md                    # Generated overview + missing by area
mcp/docs/areas/nginx-proxies.md          # Maintained feature contract + source map
mcp/README.md                          # Setup, commands and expansion recipe
mcp/AGENTS.md                          # Short, focused agent workflow
scripts/mcp-report.sh                   # Root convenience wrapper
.github/workflows/mcp.yml               # New focused validation workflow
```

`inventory.json` is generated from the actual API routes. `policy.json` supplies semantic classifications; it is not a second handwritten endpoint list. Tool-to-endpoint mappings live alongside the executable definitions, and `tools.json` is their export, not a manually maintained success list.

## Coverage contract

### What the percentage means

An endpoint is one unique HTTP method plus canonical registered route, for example `PUT /proxy/edit`. Different methods on the same path are separate endpoints. The optional external `/api` prefix does not double the count. Preserve case, path parameters and meaningful trailing slash behavior; normalize only verified equivalences.

Primary headline:

```text
API coverage = fully supported current endpoints / all endpoints in that area * 100
```

An endpoint is fully supported only when a registered, enabled MCP tool exposes its intended API contract, has a declared mapping and passing contract-test evidence, and its reviewed source fingerprint is current. Registration and schemas alone do not prove implementation quality; this is a conservative engineering metric, not proof of every possible behavior.

Rules:

1. Count distinct endpoint keys, not tools. Convenience tools and multiple tools for one route earn one credit total. Hidden requests such as auth or list-before-update do not earn coverage unless the corresponding capability is exposed to the user.
2. A workflow tool may cover multiple endpoints only with explicit per-endpoint mappings and evidence for each capability. Calling an endpoint incidentally is insufficient.
3. A narrow tool supporting only some request options is `partial`. Keep it visible, but do not count it as full coverage. Record unsupported fields/behaviors. Full endpoint coverage still does not claim exhaustive test coverage.
4. `missing`, `partial`, `stale`, `disabled` and `excluded` remain visible in the primary denominator. Do not improve the headline by hiding unsupported endpoints.
5. If useful, show an additional eligible-scope percentage excluding reviewed non-MCP routes. Label it separately and always print the full counts. An excluded console or static asset remains visible in the inventory with a reason.
6. Zero endpoints means `N/A`, never 100%. Print counts and one decimal; never round an incomplete area up to 100%.
7. A known coverage loss is reported accurately, not frozen at the previous percentage. Ordinary missing endpoints do not fail CI; invalid, stale or unclassified metadata does.
8. Each endpoint belongs to one leaf area. Parent percentages use summed endpoint counts, never the average of child percentages. Tags may cross-reference areas without creating denominator duplication.

States: `missing` (no tool), `partial` (limited contract), `implemented` (full and current), `stale` (mapped contract needs review), `disabled` (not advertised in the evaluated profile), `excluded` (reviewed omission with reason). A merely planned mapping stays `missing`. Preserve underlying mappings when stale or disabled so reports explain what changed.

Report the profile (initially `default`), source revision, source-content digest including uncommitted changes, registry digest, taxonomy version and test-evidence status. A committed report is a snapshot; the CLI must check freshness and return an actionable error or regenerate rather than silently presenting obsolete coverage. Generation should be deterministic for unchanged inputs.

### Areas

Initial taxonomy includes `nginx` with `proxies`, `certificates`, `access-lists`, `streams`, `redirects`, `404-hosts`; `vms` with lifecycle/configuration/migration/backups; `isos`; and other existing API groups discovered from registration. Explicitly resolve VM disks, host tuning, noVNC, guest API and shared streaming routes. Include auth, static/browser support and streaming routes in named areas with their applicability stated.

Unknown routes land in `unclassified`, appear in the report, and fail classification validation. Unknown routes must never disappear because an area is not on the MCP roadmap yet. Use longest-specific-path matching plus explicit overrides; validate that every route has exactly one leaf owner.

Illustrative report after six fully supported proxy tools, assuming the observed inventory is confirmed:

| Area | Implemented / total | Coverage | Details |
| --- | ---: | ---: | --- |
| NGINX | 6 / 37 | 16.2% | Child breakdown and missing list |
| NGINX / Proxies | 6 / 8 | 75.0% | Two frontend setup operations missing |
| NGINX / Certificates | 0 / 7 | 0.0% | Seven endpoints missing |
| ISOs | 0 / 3 | 0.0% | List, download and delete missing |

These are target examples, not current implemented coverage. VMs receive actual counts from the generator; the user's 87% was an example.

### CLI interface (proposed; not available yet)

```bash
./scripts/mcp-report.sh refresh
./scripts/mcp-report.sh check
./scripts/mcp-report.sh coverage
./scripts/mcp-report.sh coverage --area nginx.proxies --missing
./scripts/mcp-report.sh coverage --area vms --missing --format json
./scripts/mcp-report.sh describe 'PUT /proxy/edit'
./scripts/mcp-report.sh diff --base origin/main
```

`refresh` rebuilds the API inventory, exports tool definitions and renders JSON/Markdown. `check` verifies current sources, tool registration, classifications, fingerprints and committed generated files without writing. `coverage` displays a current snapshot; `describe` includes route, handler, source references, schema references, tool mapping, limitations and verification. `diff` compares to the requested ref and reports new/removed/changed operations and coverage changes. Missing refs produce a clear error; local coverage does not require a Git remote or network.

## Inventory and change detection without whole-codebase AI reads

### Authoritative route enumeration

Extract a pure router builder from `StartApi`, preserving middleware order and public/protected route boundaries. Production startup and the inventory command both use it. Use Chi `Walk` on this router; do not execute handlers, initialize NPM/noVNC, connect to services, open listeners or invoke `StartApi`. Audit all registration helpers first. Account for package init side effects (the guest API currently starts a cleanup goroutine) and move any inventory-relevant service startup to the normal startup path if necessary.

Include all route registration styles, nested routers, mounts and aliases. Explicitly represent wildcard/all-method handlers and distinguish WebSocket/SSE/static routes; do not pretend they are ordinary JSON operations or multiply a wildcard into fictitious functional capabilities. Conditional registrations need a named inventory profile and condition metadata so default coverage does not imply every configuration is supported.

Resolve handler symbols and repository-relative source files with Go runtime/source metadata, supplemented by a small AST index where necessary for anonymous handlers and request types. Persist symbol names and source hashes; line numbers are navigation hints and may move. Never infer API route completeness from regex counts alone.

The inventory command imports the master API and therefore needs the master's Go toolchain/native build prerequisites, including libvirt development libraries. Document these and install them in the inventory CI job. The independent MCP module must build without those dependencies.

### Detecting contract drift

Fingerprint canonical Go syntax for mapped handlers and declared request/response types, ignoring whitespace/comments/line-number movement. Include tool schemas and mappings. A changed route signature, handler or tracked type flags the corresponding coverage entry as `stale` until reviewed and its evidence updated. Removed routes leave dangling mappings that fail validation. New routes automatically appear as missing, with classification required.

For the first feature, explicitly declare dependencies such as `npm.Proxy`, `npm.Location`, `npm.StringOrInt` and relevant downstream helpers. Track type/helper references conservatively; unresolved references must be reported, not assumed unchanged. Do not attempt a universal Go semantic analyzer in v1. Changes to relevant API/helper files outside the tracked symbols trigger feature-level review, and changes to shared middleware/auth invalidate affected areas conservatively. This catches broader drift at the cost of occasional unnecessary reviews. It cannot prove behavioral equivalence.

The program can cheaply scan source metadata; the AI reads only the resulting change list and relevant functions/types. Compute report input freshness from all route-source inputs plus watched contracts and registry files, so a cached inventory cannot hide a new endpoint.

### Focused future-agent workflow

1. Read `mcp/README.md`, generated coverage summary and `mcp/AGENTS.md`.
2. Run `check`; refresh inventory if source inputs changed. Do not rely on report modification time alone.
3. Ask CLI for the target area's missing/stale endpoints and `describe` the selected operation.
4. Read that area's short contract page and only the linked handlers, DTOs and helpers. Expand inspection if a dependency is unclear.
5. Add the named typed tool, mapping and meaningful HTTP contract tests. Use the registration helper so metadata and execution cannot drift apart.
6. Regenerate reports and run focused tests/checks. Review the resulting coverage diff.

Each area page records source files/symbols, auth and request conventions, request/response caveats, example calls, limitations and exact validation commands. Maintain only pages for areas being implemented; the generated route inventory provides the initial index for all others.

## Sprint 1: Generate a trustworthy baseline

Goal: one command produces actual endpoint totals and an actionable all-missing MCP report. No MCP service is needed yet.

### 1.1 Separate route assembly from startup
- Files: `master/api/api.go`, new `master/api/router.go`, router registration tests.
- Work: introduce the side-effect-audited router builder and make `StartApi` reuse it. Preserve prefix handling, middleware order and all setup calls. Keep service initialization in startup.
- Dependencies: none.
- Acceptance/validation: production-equivalent route signatures and middleware/public-auth boundaries are unchanged; building/walking the router makes no external requests or listeners. Verify actual router construction, not a manually copied test route list.

### 1.2 Export routes with source references
- Files: `master/cmd/api-inventory/main.go`, inventory serialization helpers; `mcp/catalog/inventory.json`.
- Work: walk the built router; generate stable endpoint keys, methods, paths, transport markers, handler/source references and source digest. Detect ambiguous or unresolved registrations.
- Dependencies: 1.1.
- Acceptance/validation: repeated exports are identical; confirm eight proxy and three ISO routes against registration; verify nested guest routes, wildcard routes and same-path different-method cases. No production API calls.

### 1.3 Classify endpoints and render baseline coverage
- Files: `mcp/go.mod`, `mcp/internal/apicoverage/`, `mcp/catalog/policy.json`, `mcp/cmd/hyperhive-mcp/main.go`, `scripts/mcp-report.sh`, generated catalog/report files.
- Work: implement taxonomy, raw and optional eligible coverage, states, missing lists and CLI refresh/check/coverage/describe. Begin with an empty tool registry.
- Dependencies: 1.2.
- Acceptance/validation: every route is accounted for once; all areas show accurate zero supported counts; excluded routes have reasons; unknown classifications fail; stale artifacts fail check. Fixture-test weighted totals, zero denominators, duplicate mappings and rounding below 100%.

Demo: run refresh, see every API area, and list exactly what is missing under `nginx.proxies` and `isos`.

## Sprint 2: Connect one real MCP tool

Goal: a local MCP client lists and calls `list_proxies` through the existing HyperHive API.

### 2.1 Add the shared API client
- Files: `mcp/internal/client/` and tests; `mcp/README.md` configuration section.
- Work: configure `HYPERHIVE_API_URL` and `MCP_TOKEN`, define whether the configured base includes `/api`, and attach auth headers centrally. Support context cancellation, bounded timeouts, empty success bodies and JSON/plain-text errors. Reject cross-origin redirects that could forward credentials. Never print credentials.
- Dependencies: 1.3 module structure.
- Acceptance/validation: `httptest` verifies base-path composition, Authorization, method/body forwarding, cancellation, 401/403, plain-text failures and empty responses. Do not retry mutations automatically.

### 2.2 Add stdio MCP registration and list tool
- Files: `mcp/internal/tools/registry.go`, `mcp/internal/tools/proxies.go`, `mcp/cmd/hyperhive-mcp/main.go`.
- Work: pin the official SDK, implement `serve`, and define `list_proxies` with description, typed schema, appropriate annotations, endpoint mapping and contract evidence. Register/export metadata from the same definitions. Keep stdout reserved for protocol traffic; logs use stderr.
- Dependencies: 2.1.
- Acceptance/validation: SDK client initialize/list-tools/call-tool smoke test; exactly the expected tool is advertised; it calls `GET /proxy/list`; errors remain MCP tool errors without fabricated results. Manifest export matches actual advertised tools.

### 2.3 Publish the first coverage increment
- Files: generated `tools.json`, `coverage.json`, `coverage.md`; `mcp/docs/areas/nginx-proxies.md`, `mcp/README.md`.
- Work: document setup and first operation contract; regenerate coverage from executable registration and passing evidence.
- Dependencies: 2.2.
- Acceptance/validation: one supported proxy endpoint (1/8 = 12.5%) if the full list contract is exposed; new report remains reproducible. Verify using mocks by default; optional manual API smoke testing uses an explicitly selected environment.

Demo: invoke `list_proxies` from an MCP client and see the report move from zero to one supported endpoint.

## Sprint 3: Finish ordinary proxy management

Goal: expose six core proxy capabilities and report the remaining two setup endpoints explicitly.

### 3.1 Add create and update tools
- Files: `mcp/internal/tools/proxies.go`, proxy DTOs/tests, feature contract page.
- Work: add `create_proxy` -> `POST /proxy/create`, `update_proxy` -> `PUT /proxy/edit`. Expose the actual supported payload fields, including nested locations and TLS/access-list settings. Validate nonempty domain lists, port ranges and required update IDs. Either document complete replacement semantics or implement tested read/merge/write with explicit omitted-vs-false/zero handling; default to clearly documented complete inputs for v1.
- Dependencies: Sprint 2.
- Acceptance/validation: exact method/path/JSON contracts, explicit false/zero values, nested options, empty successful responses and managed-proxy 403 behavior. A limited initial field set is marked partial, not full.

### 3.2 Add delete, enable and disable tools
- Files: `mcp/internal/tools/proxies.go`, tests, area page.
- Work: add `delete_proxy` -> `DELETE /proxy/delete`, `enable_proxy` -> `POST /proxy/enable`, `disable_proxy` -> `POST /proxy/disable`, using ID-bearing JSON bodies. Annotate read/write/destructive behavior accurately; annotations do not enforce authorization.
- Dependencies: Sprint 2; independent of 3.1 once shared DTO/client work is ready.
- Acceptance/validation: contract tests verify ID bodies including DELETE, backend failures, and managed-proxy restrictions. Server-side checks remain authoritative.

### 3.3 Verify complete core workflow and report
- Files: proxy integration tests and generated reports.
- Work: run an MCP-client workflow against a mock HyperHive API: list, create, edit, disable, enable, delete. Confirm registry/schema/test evidence and report mappings together.
- Dependencies: 3.1, 3.2.
- Acceptance/validation: if six full contracts pass, proxies report 6/8 = 75.0%; NGINX is 6/37 = 16.2% if inventory is unchanged. Missing list names `POST /proxy/setupFrontEnd` and `POST /proxy/setupFrontEndSecure`. No separate get tool or incidental list call inflates totals.

Demo: six named tools work, and the coverage report explains exactly why proxies are not yet 100%.

## Sprint 4: Keep coverage current and make expansion cheap

Goal: API/tool changes update reports and surface drift automatically; the next contributor can select work without scanning the project.

### 4.1 Add fingerprints, diff and review states
- Files: `mcp/internal/apicoverage/`, policy/registry metadata, CLI diff command and test fixtures.
- Work: implement source/type/helper fingerprints and conservative shared-file invalidation, reviewed digests, changed/removed/new operation diffs, and build/test-evidence association. A reviewed digest is updated only alongside relevant contract review/tests.
- Dependencies: Sprints 1–3.
- Acceptance/validation: fixture changes to a handler, nested DTO, shared helper or auth file identify affected tools; whitespace changes do not; an added route is missing, removed route breaks mappings, and untouched sources remain current. Non-Git working-tree changes also invalidate freshness.

### 4.2 Add CI freshness and tool consistency checks
- Files: new `.github/workflows/mcp.yml`, `scripts/mcp-report.sh`, Makefile convenience targets if appropriate.
- Work: run independent MCP tests, install master inventory prerequisites in a separate job/step, regenerate artifacts and fail on dirty generated outputs or validation errors. Trigger on API/tool/contract dependencies and relevant helpers; start broad enough to catch drift. Publish the coverage Markdown summary and change list.
- Dependencies: 4.1.
- Acceptance/validation: new endpoints may be missing without requiring immediate implementation, but must be classified; stale implemented contracts, orphan mappings and mismatched advertised tools fail. A normal unrelated change does not claim false API coverage. Optional minimum-coverage gates come later.

### 4.3 Document the focused contribution recipe
- Files: `mcp/AGENTS.md`, `mcp/README.md`, first area page.
- Work: specify the six-step focused workflow above, named tool conventions, how to add taxonomy/mappings/tests, and report commands. Include explicit guidance to inspect linked dependencies when uncertain.
- Dependencies: 4.1 and 4.2.
- Acceptance/validation: dry-run choosing a missing ISO endpoint using only the index, area/operation description and linked source; no whole-codebase AI read is necessary.

Demo: add a temporary route in a test fixture and observe missing coverage, then change a mapped DTO and observe a stale tool with exact source pointers. Restore fixtures; check passes.

## Expansion order

1. Six proxy management tools establish the pattern.
2. Add the two frontend setup tools if desired to reach full proxy endpoint coverage; these perform broader workflows and need their own typed contracts.
3. Certificates and access lists, then streams/redirects/404 hosts, progressively complete NGINX. Proxy 100% does not mean NGINX 100%.
4. ISOs are a small next area (three operations). Represent download as accepted/in progress unless a real completion signal is available.
5. VMs start with list/get/start/shutdown/restart, then settings, storage, migration and backups. Each addition updates the same report. Stream/console/file operations require a deliberate MCP representation and may remain explicitly excluded or partial.

Expansion is a roadmap, not part of the first release. An operational MCP tool exposing the coverage report can be added later if useful; the agreed v1 interface is Markdown + CLI.

## Verification and completion criteria

- Inventory tests cover router assembly fidelity, nested routes, aliases, wildcards and source resolution without external service activity.
- Coverage fixtures cover aggregation, every state, exclusions, profiles, duplicate/unknown mappings, partial parameters, orphan routes, freshness and source drift.
- HTTP tests use fake servers for the real wire contract, including empty 200 bodies, DELETE JSON, error bodies and complete update semantics.
- MCP integration tests use the official SDK client to initialize, enumerate and invoke advertised tools. Do not only test Go handlers directly.
- First-release completion: six working proxy tools, correct per-area coverage with exact missing lists, a current source index, reproducible reports, and a documented way to extend the next area. Sprint 4 completes the automatic maintenance workflow.
- No production mutation is needed for these checks. Live deployment and infrastructure documentation are separate work.

## Risks and resolved edge cases

- Runtime walking is chosen over regex extraction so nested registration is authoritative. The small router-construction refactor needs behavior-preservation checks and native CI dependencies.
- Source fingerprints are conservative review triggers, not a complete behavioral model. Explicit helpers plus feature/shared-file fallbacks avoid pretending a handler-only hash captures every dependency.
- Markdown can become stale. Input digests, refresh/check commands and CI make that visible; raw report viewing remains a snapshot of its recorded revision.
- A manifest can exaggerate support. Derive it from executable registration and require mapped contract evidence; still review whether a tool exposes the complete endpoint contract.
- Tool subsets must not reset existing fields during updates. Require complete documented input in v1 or explicitly test merge semantics before offering patch-like behavior.
- Tokens and backend responses can contain sensitive values. Keep secrets outside metadata/reports and sanitize error/log output without discarding useful status information.
- A local stdio process uses the operator's configured identity. If remote MCP is added, design per-user authorization and token handling as its own phase; do not silently share one administrator token.
- Keep HTTP-to-MCP adapter conventions stable. Avoid broad API refactors or OpenAPI adoption as prerequisites for the first six tools.

## Rollback

The MCP binary and generated reports are additive; stop/remove the local MCP configuration to disable access. Keep the router-construction extraction isolated so it can be reverted separately if required, and verify the original startup path. Do not couple MCP rollout to server deployment or database migrations.

## References

- [Official Go MCP SDK](https://go.sdk.modelcontextprotocol.io/): typed tool handlers, client/server support and transports.
- [Go MCP SDK server guide](https://github.com/modelcontextprotocol/go-sdk/blob/main/docs/server.md): tool registration, schemas and results.
- [Chi Walk API](https://pkg.go.dev/github.com/go-chi/chi/v5#Walk): enumeration of method/route registrations from an assembled router.

Official documentation checked on 2026-10-18. Context7 is not available in this session; direct official documentation was used.
