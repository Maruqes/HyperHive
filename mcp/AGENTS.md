# HyperHive MCP development guide

## Current state

This folder contains a reusable Go MCP server and standalone stdio command with exactly one tool,
`list_proxies`, calling the authenticated HyperHive `GET /proxy/list` endpoint.
See `README.md` for configuration, output and verification. The official SDK is
pinned in `go.mod`; the module does not import master services. HyperHive master
embeds `NewHTTPHandler` at unauthenticated `/mcp` on its existing exposed :9595
listener, outside API login middleware. The user explicitly requested automatic
HTTP startup without MCP authentication. Backend calls still use the existing
authenticated API with server-side `HYPERHIVE_API_TOKEN`. Missing credentials
produce tool errors, without blocking master startup or MCP discovery.
`API_PORT` in the master's environment/.env changes this shared API/MCP port;
unset/empty defaults to 9595. Keep the listener, loopback URL and generated
frontend proxy targets consistent. Existing proxy configuration is not rewritten.

API inventory, coverage commands/reports, mutations and CI described below
remain future work. The user explicitly narrowed the first increment to one
tool. Do not create the entire scaffold merely because this guide describes it;
implement the increment requested by the user. No generated coverage claim is
available yet.

The detailed proposal is in
[`../notes/hyperhive-mcp-implementation-plan.md`](../notes/hyperhive-mcp-implementation-plan.md).
Use that plan for background and acceptance criteria, and follow the user's
latest decisions when scope changes.

## Product direction

Build a small MCP server that lets an AI use HyperHive through explicit, named,
typed tools. Add capabilities progressively rather than exposing hundreds of
endpoints at once.

The execution path should be:

```text
AI client -> named MCP tool -> shared HTTP client -> HyperHive API
```

- Call the existing HyperHive HTTP API so authentication and business rules
  remain authoritative. Do not bypass it by calling NGINX Proxy Manager,
  databases, or master services directly.
- Keep a standalone Go module in this folder and official pinned Go MCP SDK.
  Both embedded HTTP and standalone stdio must share the same tool registration.
- Share configuration, authentication, HTTP handling and error translation.
  Organize tools by feature so each addition stays small.
- A complete OpenAPI specification is not a prerequisite. Generic
  search/describe/call-operation tools are not the intended primary interface.

## First capabilities and development order

1. Establish a generated API inventory and baseline coverage report.
2. Implement the shared HTTP client and MCP startup with `list_proxies`.
3. Add `create_proxy`, `update_proxy`, `delete_proxy`, `enable_proxy` and
   `disable_proxy`, with meaningful contract tests for each.
4. Automate report freshness, API change detection and validation in CI.
5. Expand one feature at a time: frontend setup, certificates, access lists,
   streams, redirects and 404 hosts; then ISOs and VM capabilities as requested.

Each increment must be independently usable and verifiable. Keep scope small;
do not implement later areas just to raise a percentage.

Initial source references, to recheck before implementation:

| Concern | Existing source |
| --- | --- |
| Route registration and startup | `../master/api/api.go` |
| Authentication | `../master/api/login.go` |
| Proxy endpoints | `../master/api/npmapi/proxy.go` |
| Proxy request types | `../master/npm/proxy.go` |
| Other NGINX features | `../master/api/npmapi/` |
| ISOs | `../master/api/isos.go` |
| VM operations | `../master/api/virsh.go` |

## Coverage must remain visible

The user wants a generated Markdown report plus a CLI showing coverage for every
API area, with an exact list of what is missing. A dashboard is optional future
work. Percentages must be computed, never manually maintained.

```text
coverage = fully supported current endpoints / total endpoints in the area * 100
```

- An endpoint is a unique HTTP method plus registered route. Count endpoints,
  not tool names. Multiple tools wrapping one endpoint earn one credit.
- Count an endpoint as fully supported only when its capability is exposed by
  a registered, enabled tool, its contract is supported and tested, and its
  reviewed API metadata is current. Incidental internal requests do not earn
  additional credit.
- Distinguish `implemented`, `missing`, `partial`, `stale`, `disabled` and
  `excluded`. Show limitations and reasons. Partial support must not count as
  full coverage.
- Keep all endpoints visible in the primary denominator, including intentional
  exclusions. If an eligible-only percentage is useful, label it separately.
- Give each endpoint one leaf area. Compute parent totals from endpoint counts,
  not by averaging percentages. Keep new areas visible even before any tools
  exist for them; unclassified routes must be flagged.
- Report counts alongside percentages. Zero endpoints means `N/A`; never
  round an incomplete area up to 100%.
- NGINX coverage includes more than proxies: certificates, access lists,
  streams, redirects and 404 hosts also count. Proxy completion must not imply
  all of NGINX is complete.
- For each missing or stale operation, show its method/path, source
  file/function, mapped tool if any, status and remaining work.

The initial inspection on 2026-10-18 found eight proxy routes, 37 NGINX routes
overall and three ISO routes. These are historical observations, not fixed
denominators. Regenerate the inventory from current source. Six fully supported
proxy operations would mean 6/8 proxy coverage, not 100%.

## Avoid rereading the whole codebase

Build deterministic tooling that produces a reusable API index and change list.
Programs may scan the source as needed; future AI sessions should use the
generated index to narrow their reads.

Proposed artifacts, to create only as their implementation becomes necessary:

- `catalog/inventory.json`: generated routes, area ownership, handler/type
  references and source fingerprints.
- Tool definitions with endpoint mappings beside executable registration;
  export their metadata rather than maintaining a separate handwritten list.
- `catalog/coverage.json` and `docs/coverage.md`: generated status, percentages
  and missing lists.
- `docs/areas/<area>.md`: short maintained contracts for implemented areas,
  including source pointers, request/response conventions and caveats.
- CLI commands for refresh, freshness checking, area coverage, missing
  endpoints, operation details and API changes.

The preferred route inventory walks the same assembled Chi router used by
HyperHive. Separate router construction from service startup first; preserve
middleware order and authentication boundaries. Inventory generation must not
call production startup, execute handlers, initialize external services or open
listeners. Audit package initialization and native build prerequisites. The MCP
binary should remain independent of the master's native dependencies.

Track changes to routes, mapped handlers, request/response types and relevant
helpers. Use conservative feature-level invalidation for changes outside the
tracked symbols and shared auth/middleware changes. Source hashes are review
triggers, not proof of behavioral equivalence. Do not build a universal Go
semantic analyzer as a prerequisite to the first tools.

Generated artifacts must identify the source revision/content digest and tool
registry used. Check actual source content, including uncommitted changes;
report age alone is insufficient. New endpoints become missing, changed
contracts become stale, and removed endpoints invalidate dangling mappings.
CI should catch stale reports, invalid mappings and unclassified routes while
allowing honestly reported missing capabilities.

Once this tooling exists, use this workflow:

1. Read this guide and the current coverage summary; check artifact freshness.
2. Select the requested area's missing or stale operation.
3. Read its feature notes and linked handlers, types and helpers. Expand the
   inspection when dependencies are unclear.
4. Implement the named tool, mapping and contract tests together.
5. Regenerate reports, run focused checks and review the coverage change.

Before the tooling exists, inspect the targeted route group and its contracts
directly. Do not claim generated commands or reports are available yet.

## Implementation and verification conventions

- Configure the HyperHive API base URL and token outside tool arguments; keep
  credentials out of source, reports and logs. Start with an existing token.
- Keep stdout reserved for MCP protocol traffic; use stderr for diagnostics.
- Handle empty successful responses and both JSON and plain-text errors.
  Preserve useful backend status information without leaking secrets.
- Set timeouts, honor cancellation and avoid automatic retries of mutations.
- Define update semantics explicitly. Validate required fields and preserve the
  distinction between omitted values and explicit false/zero values if merging.
- Recheck proxy contracts: edit expects a domain list, managed proxies can
  reject edits/deletes, and successful mutations may not return created IDs.
  Never invent response fields the API does not return.
- Verify HTTP method, path, headers, body and error handling with mock servers.
  Verify MCP initialization, tool enumeration and invocation through an SDK
  client as well as testing individual handlers.
- Keep tests focused on meaningful contracts and behavior. Coverage reporting
  is endpoint support coverage, not code test coverage.
- Do not require production mutations to validate ordinary development work.
  Follow applicable repository and infrastructure instructions if a later task
  includes live infrastructure or deployment work.

Completion of an increment means working tools, accurate current coverage,
documented limitations and enough source context for the next focused change.
