# HyperHive MCP implementation planning

## Goal
Produce a source-grounded implementation plan for explicit MCP tools, starting with proxy management, and automated per-area endpoint coverage with missing-operation details. Original planning completed; the user subsequently authorized the single-tool implementation increment below.

## Phases
- Research API registration, proxy contracts and MCP SDK: complete.
- Define coverage accounting, drift detection and focused agent workflow: complete.
- Save phased implementation plan and validate it against source: complete.

## Decisions
- User chose generated Markdown report plus CLI for the first version.
- Expose named, typed tools progressively; do not use generic search/describe/call as the primary interface.
- Preserve unrelated local changes and other task planning directories.

## Next Step
The requested HTTP MCP server and configurable port are complete. Connect at http://<host>:<API_PORT>/mcp (default 9595), with exactly list_proxies. Inventory/coverage and mutations remain deferred.

## Errors Encountered
- A glob for master/api/*API* matched no files; subsequent searches use actual directories and filenames.
- .github/workflows does not exist; plan CI integration as a new workflow after inspecting repository conventions.

## First implementation increment — 2026-10-18
User requested starting the server with exactly one tool: list proxy hosts. This narrows the initial increment: defer router extraction, inventory/coverage CLI and mutations.

- Verify current list/auth contracts and pin official SDK: complete.
- Implement standalone stdio server, shared HTTP client and `list_proxies`: complete.
- Verify HTTP and MCP contracts; document setup and limitations: complete.

Implemented the single-tool server against `GET /proxy/list`.

### Implementation progress
- Verify current list/auth contracts and pin official SDK: complete.
- Implement standalone stdio server, shared HTTP client and `list_proxies`: complete.
- Verify HTTP and MCP contracts; document setup and limitations: complete.

### Implementation errors
- First test run rejected all invalid configurations except an empty `Bearer ` token, because trimming left `Bearer`; now rejected explicitly.

### Next Step
Single-tool increment complete. Tests, race checks, vet, build and whitespace review passed. No servers accessed or deployed; broader roadmap is deferred.

## HTTP startup increment — 2026-10-18
User now requires master startup to expose unauthenticated MCP over a port.
- Integrate reusable stateless Streamable HTTP handler at /mcp on existing :9595, outside API auth: complete.
- Keep exactly list_proxies; protect backend API using a server-side configured token, with lazy actionable errors if absent.
- Verify HTTP protocol, auth boundary and master build/tests; update canonical notes/docs: complete.
HTTP startup increment complete; port configuration follow-up below is also complete.

## Configurable shared port — 2026-10-18
User confirmed the final outcome: connectable HTTP MCP with exactly list_proxies and a .env-configurable port defaulting to 9595.
- Add API_PORT validation and use it for the listener and MCP loopback URL: complete.
- Add configuration to .env.example and local .env without reading/exposing credentials: complete.
- Verify actual HTTP MCP invocation on a nondefault port; finish docs/notes: complete.
Next action: user can rebuild/restart master with API_PORT and backend HYPERHIVE_API_TOKEN configured, then connect via Streamable HTTP. Source work and local verification complete; no production deploy performed.
