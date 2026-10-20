# Proxy custom NGINX configuration plan

Date: 2026-10-20

## Goal
Support exact custom NGINX snippets on proxy hosts and custom locations through backend and existing MCP tools, preserving compatibility and managed-host restrictions.

## Phases
1. Discovery — complete: host advanced_config already passes through; location fields are missing; edits check management by first domain and can erase omitted host config.
2. Backend — complete: add optional location advanced_config; preserve omitted host advanced_config on edit, allow explicit empty string, reject null; validate edit ID/domains and check managed target by ID. Existing full replacement semantics for other fields remain.
3. MCP — complete: expose optional location advanced_config and clarify host/server-context semantics in discovery and contracts; retain required host configuration and existing tools/routes.
4. Validation — complete: exact multiline directives through create/update/list; omitted versus empty host config, managed renamed targets, invalid input, upstream failures; full MCP and affected backend tests.
5. Knowledge — complete: update canonical SilverBullet proxy and MCP contracts with evidence and deployment boundary.

## Acceptance and rollback
- Snippets retain newlines, quotes, Unicode and NGINX variables; no local directive rewriting or claimed NGINX syntax validation.
- Missing optional location config remains compatible; explicitly empty config is serialized.
- Existing managed frontend setup remains protected; custom config is for ordinary hosts.
- No live infrastructure mutation or deployment is part of local implementation. Revert the affected source changes to roll back; no database migration is required.

## Next Step
Complete. Deliver the local implementation, plan, validation results and SilverBullet page paths.

## Errors Encountered
- Initial upstream schema URL returned 404; use current official component schema path.
- SilverBullet cannot replace a nonexistent section; added the new section by appending to the existing page.
- Optional pointer inference exposed a nullable MCP field; explicitly constrained the discovery schema to string.
- Initial MCP documentation filename was wrong; actual path is mcp/docs/areas/nginx-proxies.md.

## Final validation
- Full MCP suite: 505 passed in 5 packages.
- Focused MCP create/update race checks: passed, including HTTP and real stdio.
- Backend npmapi/npm normal and race suites: 80 passed in 2 packages.
- Backend and MCP vet passed; git diff --check passed.
- SilverBullet canonical proxy behavior and MCP contract section reread and verified; existing parent routes and cross-links verified. No new pages or infrastructure inventory changes were needed.
- No deployment or live NPM syntax/reload check performed.
