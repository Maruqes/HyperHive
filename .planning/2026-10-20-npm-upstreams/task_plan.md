# Task Plan: NPM UpStreams implementation planning

## Goal
Deliver a source-grounded implementation plan, without implementing the feature.

## Status
Requirements, source research, external documentation review and plan authoring complete.
Final plan: notes/npm-upstreams-plan.md.

## Confirmed requirements
- HTTP/HTTPS and TCP/UDP together in the first release.
- Preserve name, loadbalancer_type and server_arr with ip, port, validated settings_string.

## Decisions
- Plan stable pool IDs, separate HTTP/stream contexts and build-verified capabilities.
- Exact-image adapter proof precedes implementation.
- Implementation remains future work; this task only authors the plan.

## Validation
Reviewed saved plan against confirmed scope and inspected paths; no application tests or live changes.

## Implementation run — 2026-10-20
Goal: implement notes/npm-upstreams-plan.md, preserving the compatibility gate.

### Phase 1: Exact-image compatibility
**Status:** complete
Create isolated NPM 2.13.5 fixture, capture templates/build and prove HTTP, locations, TCP and UDP traffic.

### Phase 2: Persistent CRUD and application
**Status:** complete
Implement validation, storage, journal, adapter and authenticated API after compatibility succeeds.

### Phase 3: Consumer integration
**Status:** complete
Bind proxy hosts, locations and streams with omission/null and drift handling.

### Phase 4: Frontend and MCP
**Status:** complete
Implement management/selectors and named eligible MCP tools.

### Phase 5: Release verification and knowledge
**Status:** complete
Run acceptance/recovery tests, document release and update canonical SilverBullet.

## Next Step
Implementation and local acceptance complete. Review notes/npm-upstreams.md before any deployment. No production changes or Git commits were performed.

## Errors Encountered
- Frontend parent is not a Git repository; inspect its HyperHive child when needed.
- Serena symbol overview has not returned promptly; use bounded source inspection for fixture work.

### Implementation findings and errors
- NPM requires mounted /data and /etc/letsencrypt; fixture now uses anonymous volumes and removes them.
- Native fields fail pool traffic; port-omitting pinned adapter passes all four contexts.
- Candidate nginx.conf must reside under /etc/nginx because NGINX resolves relative includes from its configuration directory, even with -p. Corrected and verified.
- Existing proxy contract tests exposed unsafe domain-based management checks and omitted advanced configuration loss; reconciled by ID and merging supplied fields.
- Existing mock proxy fixture did not persist its created record; corrected fixture to reflect actual NPM before update.
- MCP HTTP configuredAPI needed new forwarding methods, and tool registry expectations increase from 105 to 110.
- SDK default output coercion rounds large arbitrary response integers; explicit raw JSON result encoding preserves wire values.
- A few shell commands used a parent-relative path from module cwd and failed before edits; rerun with correct workspace paths.

- Real staged ENOSPC, invalid external hook and rejected reload tests pass; rollback retains working traffic.
- Complete master and MCP normal suites pass; affected master race suites pass.
- Exact-image traffic now verifies each advertised method, affinity, held-connection least_conn, weighted routing, HTTP/locations/TCP/UDP and both IP families.
- Recreation exposed NPM startup rewriting every conf.d file and scanning /data/nginx recursively. Adapter source assets moved to /data/hyperhive-upstream-adapter; only the managed common proxy include needs a writable mount. Recreation acceptance passes.
- K11 retrieval exception: initial broad feature-path listing exceeded the ordinary approximate retrieval budget; narrowed to the existing upstream topic and bounded source work afterwards.

- Final rerun: NPM startup trailing blank lines are accepted only for the common include; directive edits remain rejected. Exact-image runtime and lifecycle pass.
- Frontend Expo test dependencies/configuration repaired; all 113 tests, TypeScript and web export pass.
- Canonical SilverBullet upstream topic, proxy navigation, MCP coverage and project entry updated and reread.
