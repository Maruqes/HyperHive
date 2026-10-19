# MCP NGINX continuation

## Goal
Continue NGINX MCP implementation in English and update SilverBullet afterward, as explicitly requested.

## Next Step
Completed: all 21 permitted NGINX tools, final local validation, coverage documentation and verified SilverBullet updates. Await a new user-selected area.

## Phases
- Source discovery and baseline: complete.
- Establish selected tool count: complete (all 21 remaining permitted tools).
- Sequential implementation and transport validation: complete (21/21).
- Coverage/documentation refresh and final checks: complete.
- Update and verify SilverBullet: complete (four pages saved and reread).

## Decisions
- Preserve all uncommitted Celld implementation and tests.
- Proxy management is complete; NGINX has 24 reported missing non-DELETE endpoints across certificates, access lists, streams, redirects and 404 hosts, subject to effect review.
- Manual certificate creation invokes rollbackCreatedCertificate and must be excluded under the current no-deletion rule. No new deletion exception was requested.
- No deployment or production mutations; local disposable fixtures only.
- Follow mcp/AGENTS.md and IMPLEMENT_TOOLS.md: wait for selected count before implementation.

## Errors
None in this batch.
