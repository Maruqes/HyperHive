# celld fleets: discovery
Date: 2026-10-18 · Goal: agree a small API and implementation plan for independent celld fleets.

## Summary / key decisions
- Requested: create fleets from named servers, edit/delete fleets, provision their storage in the master SILO, and expose bucket counts and deployment/use details.
- Prefer small, fast, readable code and a short plan. Planning only; implementation is not requested yet.
- One server may run multiple fleets. Each fleet has its own process, ports, data directory and bucket.
- Create provisions and starts the fleet; explicit start, stop and restart endpoints are required. Background provisioning was proposed; precise response/status semantics remain to be finalized.
- DELETE removes processes, bucket and application data. Proposed sequencing: keep deletion pending until all assigned agents acknowledge shutdown, then purge storage.

- Deploy applications through the celld CLI; return connection details and instructions, with scoped credentials in a separate authenticated endpoint.

- EDIT includes name and server list. Optional custom ports are desired only if low effort; proposed scope allows port overrides on create or while stopped. Other options must justify their effort.

- Start fresh: remove the existing automatic fleet, its processes, bucket and data. New fleets use explicit membership; no automatic all-machines default. This is a planned reset, not an executed operation.

- Storage information: bucket counts/names, fleet health and CLI deployment details. Object counts and used-byte scans are out of scope.

## Q&A log

### Q1 — Shared servers
- Asked: Can the same server belong to multiple celld fleets?
- Captured: "Yes, servers can run multiple fleets (Recommended)". Independent process, ports, data directory and bucket per fleet.
- Flags: none.

### Q2 — Creation and process controls
- Asked: Should creation start the selected nodes, with background provisioning and status in GET?
- Captured: "Create and start /but should exist andpoint to start stop restart".
- Implication: expose explicit start/stop/restart endpoints; persist desired running/stopped state so automatic recovery respects stop.
- Flags: none.

### Q3 — Deletion policy
- Asked: Retain the bucket by default with explicit purge, or always delete everything?
- Captured: "Always delete processes, bucket and data".
- Proposed safety ordering: persist deleting state; stop and disable every assigned node; wait for offline agents; then delete bucket/data and scoped credentials. Never resurrect a deleting fleet on reconnect.
- Flags: offline-node waiting policy is a recommendation, not yet explicitly confirmed.

### Q4 — Application deployment
- Asked: CLI with connection details, HyperHive application-deploy API, or both?
- Captured: "CLI deployment with connection details (Recommended)".
- Decision: expose bucket/endpoint/region, application URLs and CLI instructions; scoped credentials through a separate authenticated endpoint. No build/upload/deploy pipeline in this change.
- Flags: none.

### Q5 — Editable fields and effort budget
- Asked: Name/server-list edits with add-before-drain membership updates, optional manual ports, or additional settings?
- Captured: "name, server list, ports maybe (only do ports if its easy, if needed lots of effort dont do it) and other options(based always on effort being reseonable)".
- Assessment: ports already need parameterization for shared servers; optional overrides add modest work if restricted to creation/stopped fleets. Validate/reserve a stable HTTP/internal pair and reject conflicts. No live port migration or arbitrary runtime/version configuration in v1.
- Proposed membership behavior: keep existing bucket/data, start and verify new nodes before removing old ones, reject an empty server list, preserve stopped state during edits.
- Flags: additional runtime settings are optional, not committed scope.

### Q6 — Existing automatic fleet
- Asked: Import as default with explicit membership, keep automatic enrollment, or start fresh and remove old data?
- Captured: "Start fresh; remove the existing fleet and its data".
- Decision: planned one-time retirement/purge of old singleton fleet; no imported default fleet and no automatic enrollment. Retain shared SILO storage/container and master administrator configuration for new fleets.
- Sequencing: disable the old automatic coordinator and legacy agent resume; stop all legacy children before deleting their bucket and local data. Track offline members until acknowledged. Implementation/deployment remains outside this planning task.
- Flags: none.

### Q7 — Storage information
- Asked: Bucket counts/names, health and deployment details, or also object counts/used bytes?
- Captured: "Bucket counts/names, health and deployment details (Recommended)".
- Decision: no object/size scans in v1. Define actual SILO bucket count separately from managed fleet count; storage errors must not appear as zero buckets.
- Flags: none.

## Open flags
- Final scope check → user.
- Waiting for offline agents before deletion → proposed sequencing to confirm.
