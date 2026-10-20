# Upstream tools

Scope authorized by notes/npm-upstreams-plan.md: list/get/capabilities/create/update; deletion excluded. Existing proxy/location/stream tools gain optional bindings. No production access.

Contracts: GET /upstream/list returns an array; GET /upstream/{id} returns definition/references; GET /upstream/capabilities distinguishes unknown/verified build support; POST /upstream/create returns 201 only after activation; PUT /upstream/{id} uses required expected revision. Consumer upstream_id is positive ID, omitted on edit to preserve, or null to detach with valid direct fields. Selected pools use empty direct host and zero port in complete MCP consumer payloads. HTTP/stream context validation remains authoritative in master. No mutation retries or deletion tools.

Validation pending both-transport SDK tests, master handler tests and full module checks.

Policy review: pooled consumer creates never invoke deletion compensation. A confirmed NPM create whose response or local commit failed remains journaled and startup completes its binding after verifying the operation marker and active state. Pool create commit ambiguity is likewise completed. Unknown outcomes block writes; POST is never retried. Explicit REST/UI deletion remains outside MCP.
