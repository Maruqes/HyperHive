# Plan: sequential MCP tool implementation

Generated: 2026-10-18. Status: proposed workflow; implementation has not started.

Operational entry point: [IMPLEMENT_TOOLS.md](IMPLEMENT_TOOLS.md). Establish the
user's tool count and selected area before running a batch. That batch scope
controls which parts of this roadmap to execute and when to stop.

**NEVER, NEVER, NEVER implement or expose a deletion MCP tool.** Follow the
absolute rule in AGENTS.md and IMPLEMENT_TOOLS.md. Delete actions are excluded
from every implementation sprint, including indirect or differently named actions.

## Objective

Implement the remaining HyperHive MCP capabilities one tool at a time. Finish
and validate each increment before starting the next. Progress must survive
session changes, and coverage must reflect evidence rather than declarations.

No process guarantees bug-free software. Completion means the documented
contract and required checks pass, with limitations and unverified behavior visible.

## Completion and coverage

Keep the existing raw API coverage as the primary metric. Never remove routes
from that denominator to manufacture 100%.

During inventory review, classify every route as an ordinary tool capability,
a streaming/file capability needing an adapter, or a non-tool route such as a
UI asset. Record the reason for every proposed exclusion; do not silently reclassify.
Authentication and routes returning secrets also require explicit contract review.

Report three distinct measures:

- Raw tool coverage: fully supported method/path pairs / all inventoried endpoints.
- Eligible tool coverage: fully supported eligible endpoints / eligible endpoints,
  with the complete eligibility policy and excluded count shown alongside it.
- Inventory disposition: endpoints with a reviewed classification / all endpoints.
  This measures planning completeness, never implementation completeness.

The target is 100% eligible tool coverage and a reviewed disposition for every
route. Deletion endpoints are permanently ineligible under the user's policy;
retain them in raw coverage with no credit. A raw 100% target does not authorize
deletion tools. Explicitly classify UI/static/streaming routes before claiming
any eligible completion result. MCP resources or other
adapters must be reported separately; they do not automatically earn tool credit.
Partial, stale, disabled and blocked work does not count as implemented.

## Sprint 1: establish the loop foundation

Goal: make adding the second tool safe and make later progress reproducible.

### Task 1.1: establish a trustworthy inventory

- Locations: `../master/api/api.go`, feature setup functions, future `catalog/`.
- Separate route assembly from production startup where needed, preserving
  middleware and authentication. Inject dependencies so inventory generation
  cannot initialize NPM, touch production databases or start listeners.
- Prefer walking the assembled Chi router in an isolated command. Keep master
  native dependencies out of the standalone MCP module; document inventory build
  prerequisites separately. Do not install or upgrade dependencies silently.
- Include nested routes, constants, loop-generated routes and aliases. Deduplicate
  only identical method/path pairs. Keep transport/debug scope explicit.
- Export method/path, area, handler and source references. Flag unresolved routes
  and unclassified areas instead of silently dropping them.
- Acceptance: explain every difference from the source-reviewed report; generation
  has no service side effects. Exercise nested, dynamic and duplicate cases.

### Task 1.2: make shared HTTP handling support mutations

- Location: `internal/client/`.
- Extend the existing GET path with shared request handling for reviewed methods,
  JSON bodies, query parameters and escaped path segments.
- Select response handling per endpoint: JSON, empty successful response, text,
  file or bounded stream. Do not treat every 2xx response as JSON.
- Preserve authentication, URL prefixes, cancellation, response limits, redirect
  refusal and safe errors. Do not automatically retry mutations.
- Do not introduce generic call-any-endpoint tools or add business capabilities yet.
- Acceptance: the existing list operation still works and focused tests cover the
  newly required request/response behavior, including empty mutation responses.

### Task 1.3: prepare registration, tests and progress tracking

- Locations: `internal/tools/`, `server_test.go`, `cmd/hyperhive-mcp/main_test.go`;
  proposed `catalog/tool-state.json` and generated coverage artifacts.
- Replace single-tool assumptions with an explicit reviewed expected tool set and
  reusable SDK test cases. Run every tool's cases over HTTP and stdio.
- Keep endpoint mappings beside executable tool registration and export metadata
  from that registry. Avoid a disconnected handwritten coverage mapping.
- Persist per-tool state: endpoint mapping, contract references, pending/in-progress/
  verified/blocked state, last completed step, test evidence and next action.
- Keep evidence tied to source content and registry digests. A previous passing
  run becomes insufficient when its relevant contract or implementation changes.
- Acceptance: list_proxies passes the shared harness; an unregistered tool,
  dangling endpoint mapping or stale contract cannot earn coverage.

### Task 1.4: automate report freshness

- Locations: proposed inventory/coverage command, `UPDATE_STATE.md`, `README.md`.
- Generate totals and missing lists from inventory plus reviewed registry evidence.
  Mark contract changes stale, new routes missing and deleted mappings invalid.
- Preserve full endpoint visibility and area totals. Generate the report block in
  UPDATE_STATE.md without overwriting its reusable refresh instructions.
- Update the short README summary from the same result. Provide refresh and check
  modes; check mode fails for stale artifacts or invalid mappings, not for honestly
  reported missing tools. Never invent command names in docs before they exist.
- Acceptance: changing a route or contract makes freshness checks fail; regeneration
  updates both documents consistently and a second run produces no content change.

Sprint verification: the existing tool remains usable over both transports,
report refresh is repeatable, and no new business tool is counted yet.

## Sprint 2: apply the loop to each tool

One iteration owns one named tool and its explicit endpoint mapping. Supporting
helpers may change when necessary, but unrelated capabilities wait for their turn.

### Step 1: resume and choose

Read AGENTS.md, the current report and the saved checkpoint. Inspect working-tree
changes; preserve unrelated work. Resume an unfinished tool before choosing a new
one. Prefer stale supported contracts first, then the next dependency-ready item.
Record the chosen tool and endpoint before editing implementation.

Gate: the scope, prerequisite tools and current failure/baseline state are known.

### Step 2: write the contract card

Read the actual route, middleware, handler, types and relevant helpers. Record:

- Tool name, purpose, method/path, area and source references.
- All supported inputs: required, optional, defaults, nullability, bounds and
  omitted-versus-explicit-zero/false behavior. Explain intentional limitations.
- Request headers, query/path/body encoding and API response shapes/statuses.
- Authentication/permission requirements, side effects and backend restrictions.
- Output schema, examples, errors, idempotency and read-only/destructive annotations.
- Special behavior: downloads, streaming, async jobs, partial success or timeouts.

Gate: every API capability in the mapped endpoint is supported or honestly marked
partial. No guesses about required fields, update semantics or created IDs remain.

### Step 3: define expected tests before implementation

Build cases from the API contract and observable backend behavior. Do not derive
expected requests from the new tool implementation itself. Add failing tests for
new behavior and verify they fail for the intended reason.

| Concern | Required evidence when applicable |
| --- | --- |
| Discovery | Expected name, description, schemas and annotations over both transports |
| Inputs | Required/missing, wrong type, unknown fields, bounds, omitted/zero/false |
| HTTP request | Exact method, escaped path/query, prefix, Bearer header and JSON body |
| Valid success | All supported fields survive; output and text fallback match the contract |
| Empty success | Actual API status/body accepted; no invented ID or returned resource |
| Backend failure | Applicable auth, permission, missing-object, conflict and server errors |
| Invalid response | Malformed/wrong-shape JSON, empty JSON response and response size bounds |
| Mutation behavior | One intended write, no automatic retry, correct update semantics |
| Cancellation | Shared transport handles cancellation/deadlines; special long jobs have explicit tests |
| Domain rules | Managed objects, resource prerequisites and partial backend failures |

Use shared tests for shared transport behavior instead of duplicating every
transport edge case for every tool. Add tool-specific regressions when relevant.

Gate: the tests describe independently reviewed behavior and cover the tool's
meaningful failure modes. Non-applicable cases have an explicit reason.

### Step 4: implement the smallest complete slice

Add the client operation, typed MCP inputs/outputs, validation, registration and
endpoint mapping. Share registration across transports. Keep credentials in
configuration. Match existing API behavior; do not repair unrelated API defects
or silently expand the endpoint contract as part of a tool addition.

Gate: focused tests pass and the operation supports the reviewed contract fully.

### Step 5: prove the complete invocation path

Invoke the tool through SDK clients over authenticated Streamable HTTP and the
actual stdio executable, backed by a disposable HTTP fixture. Verify discovery,
schema rejection before backend calls, the outgoing request, results and errors.

When practical, also exercise the real HyperHive handler with disposable/mocked
downstream dependencies. Fixtures prove the client contract, not the complete NPM,
libvirt or infrastructure behavior. Label these validation levels separately.

Never validate a delete, reboot, disk wipe or similar operation against production.
A sandbox backend test needs a defined disposable environment and cleanup; it is
separate from permission to implement the tool locally. Missing sandbox evidence
must remain visible and must never be described as a successful live test.

Gate: both MCP transports exercise the new tool; existing tools still behave as expected.

### Step 6: review and run the acceptance checks

Review the diff against the contract card: omitted fields, unexpected defaults,
false/zero handling, secret exposure, unsafe retries, error mapping and accidental
API behavior changes. Verify every mapped field and endpoint has evidence.

From `mcp/`, run the focused cases first, then the acceptance checks once:

```sh
go test ./...
go test -race ./...
go vet ./...
```

Run focused master integration tests when registration, routing, authentication
or handler behavior changed. If required native prerequisites are unavailable,
record the exact blocked check; do not downgrade it to a pass. Fix failures and
rerun the affected checks. Broaden testing only when changes justify it.

Gate: all required checks pass. A required failure or unavailable check prevents
verified status. Optional sandbox checks are reported separately.

### Step 7: publish the checkpoint and coverage

Mark the tool verified only after the prior gates pass. Store the contract/source
digest, checks and outcome. Regenerate the full state report and README together.
Confirm totals reconcile and explain the exact coverage delta. Review the final
patch as one tool increment; commit only if committing is part of the authorized workflow.

Checkpoint example:

```text
Tool: create_proxy
Endpoint: POST /proxy/create
State: verified
Validation: client contract + HTTP MCP + stdio MCP + required repository checks
Live validation: not performed
Coverage delta: +1 unique endpoint
Next: update_proxy
```

Gate: code, evidence, saved state and both reports agree. Then begin the next tool
without asking for routine local implementation approval again.

### Step 8: handle a blocked iteration

If the source contract is ambiguous, required validation cannot run or an API bug
prevents correct support, save the evidence and exact next action. Do not count
partial work or replace failed assertions with weaker checks to advance coverage.
After three unsuccessful materially different approaches, stop repeating the
attempt and surface the blocker. Another independent tool may proceed only after
this tool is explicitly checkpointed as blocked; it remains unfinished in totals.
Ask the user only for decisions or access that are actually necessary.

## Sprint 3 and later: expand by area

Complete proxy management first, one tool per iteration:

1. Revalidate list_proxies in the shared harness.
2. create_proxy: preserve the supported Proxy request contract; accept HTTP 200
   with an empty body and do not fabricate a created ID.
3. update_proxy: review required domain lookup and managed-proxy restrictions;
   define full replacement versus merge behavior before exposing the tool.
4. enable_proxy, then disable_proxy: separate iterations with their actual ID
   payloads and distinct managed-proxy rules. Never implement delete_proxy.
5. setup_frontend, then setup_frontend_secure: distinct iterations covering
   generated configuration and possible partial completion of multi-write work.

These cover seven permitted proxy endpoints if each full contract is supported.
The delete endpoint remains excluded: full eligible proxy coverage is 7/7,
while raw proxy coverage is at most 7/8 (87.50%) under this policy.
The order is a starting queue, not permission to skip prerequisite review.

Then process certificates, access lists, streams, redirects and 404 hosts;
follow with ordinary JSON capabilities in the other API areas. Within each area,
prefer reads, creates, updates and non-deletion actions in dependency order. Plan
file transfers, long jobs and streaming adapters as explicit contract types.

Sprint verification: all tools in the area pass the same gates, previous areas
remain green, missing/stale lists reconcile and the next area is clearly queued.

## Final acceptance and rollback

Completion requires no unresolved eligible endpoint, all required checks passing,
a fresh inventory, consistent reports and reviewed non-tool dispositions. The
summary must show raw and eligible percentages separately plus any live-validation
limits. A blocked or stale eligible endpoint keeps the goal incomplete.

For a failed increment, remove or disable only that increment's registration and
mapping, preserve prior working tools, and regenerate coverage. Avoid blanket
resets that could discard unrelated work. Do not deploy or change live servers as
part of this local implementation loop.
