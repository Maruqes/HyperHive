# Plan: Unified MCP registration and safety policy

Generated: 2026-10-21. Complexity: high. Status: proposed implementation; planning complete.

## Outcome and confirmed policy

Every exposed HyperHive MCP tool uses one typed `RegisterTool` function, declares exactly one safety level, and belongs to an explicit feature registration function. A reviewer can inspect one area and see its complete tool list and safety classifications.

The user confirmed both decisions on 2026-10-21:

| Level | Required execution behavior |
| --- | --- |
| `safety.Low` | Execute after ordinary authentication, schema and domain validation; no HH safety prompt. |
| `safety.Medium` | Require a recorded pre-execution check of target, exact arguments and expected impact; no human approval. |
| `safety.High` | Require explicit human approval for this invocation in both “Approve for me” and “Ask for Approval”. |
| `safety.VeryHigh` | Same mandatory human approval, with clear disruption, data-loss, scope and recovery limitations. |

Client restrictions may still block or prompt for Low/Medium calls; HH cannot override them. Risk declarations are mandatory, with no implicit Low default. Missing or invalid classifications fail registration. Existing VM recovery requirements remain independent of the approval threshold.

Scope: registration and safety for all currently exposed areas, shared HTTP/stdio behavior, associated policy/docs and tests. Preserve names, schemas, exact JSON numbers, output shapes, API authentication, cancellation, timeout behavior and no automatic mutation retries. No new feature tools, production mutations, client permission changes or deployment are included in this planning task.

## Evidence and prerequisites

Source baseline: `dd63aad991860347d67b1d114d97d8cff810a258`, inspected working tree. Existing unrelated changes: `mcp/README.md` modified and `mcp/TESTING.md` untracked; preserve them.

- `mcp/server.go:newServer` explicitly registers DNS, Notes, SPA, ISOs and VMs, but `tools.Register` in `internal/tools/proxies.go` also nests Celld, NGINX and optional upstream registration.
- `internal/tools/arguments.go:addExactTool` already preserves exact input numbers while retaining SDK schema handling. Reuse it inside the registrar.
- `internal/tools/vms.go:vmTool` emits risk metadata, while VM write helpers separately pass the risk again. Remove that duplication.
- `internal/vmsafety/approval.go:Gate.Require` currently approves every non-Low VM invocation, with legacy elicitation and modern input-request continuations. Medium behavior must change deliberately.
- `internal/tools/vms_writes.go:addVMWrite` orders validation, approval, lock, snapshot, durable private backup and mutation. Preserve its recovery guarantees when centralizing policy.
- `mcp/go.mod` pins official MCP Go SDK v1.8.0. Plan against that installed version; no SDK upgrade is required by this refactor.
- SilverBullet evidence: `knowledge/projects/HyperHive/features/mcp/vm-approval-and-recovery`, especially “Client form not shown — 2026-10-20”. Stored client observations are historical, not proof of current compatibility.

External contracts reviewed on 2026-10-21:

- [MCP elicitation](https://modelcontextprotocol.io/specification/2026-07-28/client/elicitation): clients control presentation; form responses are not cryptographic proof of a human click.
- [MCP annotations](https://blog.modelcontextprotocol.io/posts/2026-03-16-tool-annotations/): hints describe tools; enforcement must occur in the execution path.
- [Official Codex configuration](https://learn.chatgpt.com/docs/config-file/config-reference): MCP elicitation permission, tool approval modes and reviewer selection are separate controls. The documented `approvals_reviewer` can use `user` or `auto_review`; do not infer effective behavior from UI labels.

## Proposed architecture

Illustrative API, finalized during Sprint 1; these types do not yet exist:

```go
func RegisterTool[In, Out any](
    registry *Registry,
    definition Definition[In],
    handler mcp.ToolHandlerFor[In, Out],
    level safety.Level,
) error
```

`Definition[In]` contains name, description, endpoint, accurate effect annotations, schemas, typed validation, a redacted target/impact summary and an optional typed preflight callback. Recovery hooks remain feature-specific. A shared `Registry` owns the MCP server, approval gate, review-record store and registration inventory. Explicit generics remain a package-level function because Go methods cannot introduce their own type parameters.

One risk argument supplies discovery metadata, human-readable descriptions, audit classification and runtime behavior. Preserve existing `hyperhive/risk` wire values, including `very high`, unless a separately documented compatibility change is justified. Add area and required-action metadata consistently. Never infer risk or read-only behavior solely from HTTP verbs: certificate downloads use POST and expose private keys.

`RegisterAll` calls `RegisterVMs`, `RegisterCelld`, `RegisterDNS`, `RegisterISOs`, `RegisterNotes`, `RegisterSPA`, `RegisterProxies`, `RegisterNginx` and `RegisterUpstreams` explicitly. Each area has a narrow API interface and a visible registration list; specialized schema, transport and recovery helpers may remain below it. Eliminate silent optional area discovery by type assertion in the production composition path; intentional test subsets must be explicit.

Execution pipeline:

1. SDK/schema checks, exact decoding, domain validation and safe summary construction.
2. Medium: prepare and validate a review bound to the exact invocation. High/VeryHigh: issue a human approval request and suspend before execution.
3. After approval, acquire appropriate execution locking without holding locks during human interaction. Revalidate current target/preconditions. Reissue approval if the reviewed operation or material impact changes.
4. Persist the required Medium review before backend dispatch. Perform and persist existing VM recovery capture for every operation that currently requires it, regardless of level.
5. Call the existing typed API adapter once, preserve partial-failure semantics and record a sanitized outcome.

For Medium, the durable review contains tool, level, timestamp, target, argument digest, redacted argument summary, expected impact and validated preconditions. A typed server preflight enforces checks; agent-facing discovery directs the agent to assess the same information. Neither metadata nor a record proves the model's internal reasoning. No new `confirmed` or approval Boolean tool arguments. Record persistence or required preflight failure blocks execution. Private bounded records exclude credentials, raw XML, certificate bytes and arbitrary backend bodies. Configure record location/retention explicitly; keep audit records separate from retained VM recovery artifacts.

## Sprint 1: Establish the registry and classification contract

Goal: a runnable registry foundation and a reviewed classification inventory before behavior migration.

### 1.1 Inventory registrations and policy exceptions
- Locations: `mcp/internal/tools/*.go`, `mcp/server.go`, existing discovery tests, `mcp/AGENTS.md`.
- Produce `mcp/docs/tool-safety.md`: every actual registered name, area, endpoint, level, rationale, preflight and recovery requirement. Snapshot names/contracts through both transports.
- Classify effects and sensitivity individually. Ordinary reads are Low candidates; bounded note edits/ISO initiation are Medium candidates; DNS reloads, routing/security changes, credential export and disruptive fleet/host actions require explicit High/VeryHigh review. Preserve existing VM levels initially.
- Resolve the existing `delete_celld_fleet` discrepancy: source explicitly labels an earlier user authorization, while current `mcp/AGENTS.md` bans deletion and lists only VM exceptions. Record this as a migration prerequisite, using authoritative prior authorization if available; otherwise obtain a specific decision before migrating that registration. Do not silently remove it, grant a new exception or add other deletion tools.
- Dependencies: none. Validation: inventory reconciles with actual discovery and identifies all classification gaps and existing exceptions.

### 1.2 Add shared safety and registration types
- Proposed locations: `mcp/internal/safety/{level,policy}.go`, `mcp/internal/tools/registry.go`, `mcp/internal/tools/arguments.go`.
- Extract shared risk/approval concepts from `vmsafety`, leaving VM recovery code independent. A short-lived internal compatibility alias is acceptable during migration.
- Add typed `RegisterTool`, definition validation and duplicate-name detection. Reuse exact decoding and existing schema/output construction. Return startup errors through the server constructors; authentication wrapping must remain unchanged.
- Dependencies: 1.1. Validation: fixtures demonstrate explicit classification, exact integer preservation, invalid-definition rejection and discovery/runtime metadata consistency.

Demo: a fixture registry starts and exposes a representative typed tool with its declared level. Existing production registrations still run while migration is incomplete.

## Sprint 2: Implement the shared execution policy

Goal: all four levels work through a representative tool over both transports.

### 2.1 Generalize invocation approval
- Locations: proposed `internal/safety/approval.go`, existing `internal/vmsafety/approval.go` and approval tests.
- Preserve single-use, expiry, bounded pending state, canonical exact-argument binding and cancellation; use area-neutral messages/continuation identifiers. Invalidate pending legacy approvals on process replacement instead of accepting stale tokens.
- Bind continuation state to the authenticated client/session identity available in each transport; document the shared-token identity limitation. Reject altered tool/arguments, invalid response shape, replay, expiry and cross-session continuation.
- Request an explicit approve/cancel selection with no approving default. A missing/declined/cancelled/unsupported prompt blocks the operation without writes.
- Dependencies: 1.2. Validation: transport tests cover every failure and prove zero mutation calls before approval.

### 2.2 Add Medium preflight and private review records
- Proposed locations: `internal/safety/{review,store}.go`, typed callbacks in area definitions.
- Implement the target/argument/impact checks and recording contract above. Preserve cancellation and exact input identity through preflight and dispatch. Use operation-specific read checks where available; never perform a hidden mutation as a check.
- Dependencies: 1.2. Validation: valid Medium executes without elicitation; target mismatch, invalid impact/preconditions, cancellation and disk/persistence errors execute nothing. Redaction tests check sensitive inputs and errors.

### 2.3 Qualify the human approval boundary
- Locations: shared approval tests, `mcp/CONNECTING.md`, disposable client fixtures.
- Test High/VeryHigh in actual supported Codex “Ask for Approval” and “Approve for me” modes, including reviewer configuration. Verify the human sees a prompt and the backend remains untouched until their decision. Mock approval acceptance alone is insufficient evidence.
- HH cannot inspect or enforce arbitrary client UI mode through standard MCP. If a mode automatically answers approval forms without a human, do not claim compliance. Require a trusted client integration with verifiable human decision provenance, or an authenticated HH-owned approval surface binding a short-lived single-use decision to exact arguments. Keep that path blocked until the chosen boundary is validated. Never accept agent-authored `human=true` metadata as provenance.
- Dependencies: 2.1. Validation: document observed client/version/mode behavior. This is a release gate for the user's human-only requirement; it may require a client-side integration beyond the HH repository.

Demo: Low runs; Medium records its checks; High/VeryHigh suspend for human approval and deny safely when unavailable. A fixture client demonstrates every branch; real-client qualification is reported separately.

## Sprint 3: Migrate every feature area

Goal: every production tool uses the registrar and every area is independently inspectable.

### 3.1 Migrate VM registrations and recovery integration
- Locations: `internal/tools/vms*.go`, `internal/vmsafety/backup.go`, VM tests.
- Replace duplicate risk arguments and direct gate calls with shared registration policy. Preserve old state/XML, disk artifacts, locking, backup paths on backend errors and no automatic rollback claims. Medium VM actions now use the review path while retaining required recovery capture.
- Dependencies: Sprint 2 core and 1.1 classifications. Validation: VM approval/recovery/transfer regressions; missing backup prevents writes at every applicable level; approval occurs exactly once.

### 3.2 Split the nested area composition
- Locations: `internal/tools/{proxies,proxy_mutations,celld,nginx,upstreams}.go`, `mcp/server.go`.
- Introduce explicit area entry points and narrow interfaces. Move every individual registration under its appropriate area; replace nested registration in `Register`. Preserve deliberate supported-area selection in tests.
- Dependencies: 1.2. Validation: server starts with the same permitted tool names and no duplicates/missing areas; HTTP/stdio discovery matches.

### 3.3 Migrate non-VM definitions and handlers
- Locations: `internal/tools/{celld,dns,isos,notes,spa,proxies,proxy_mutations,nginx,upstreams}.go`.
- Use `RegisterTool` and the reviewed per-tool classifications. Keep validators ahead of safety decisions and state checks; preserve typed handlers, outputs, exact numbers, optional inputs and queued/partial outcomes. Review credentials and binary exports explicitly.
- Dependencies: 2.1–2.2, 3.2; deletion registration also depends on 1.1 policy reconciliation.
- Validation: existing feature contracts pass with safety-aware fixtures; each area's review checks and approval/no-approval decisions are exercised.

### 3.4 Remove bypasses and reconcile guidance
- Locations: `internal/tools/arguments.go`, superseded builders/helpers, `mcp/AGENTS.md`, `mcp/CONNECTING.md`, area docs.
- Permit raw SDK registration only inside the shared registrar/decoding implementation. Add an AST-based check to prevent future bypasses. Keep handler-specific adapters when they preserve contracts; remove duplicate safety decisions.
- Update the old Medium-and-higher approval rule to the user's confirmed threshold; document the canonical table and link it from area docs. Preserve deletion restrictions and authorized recovery requirements.
- Dependencies: 3.1–3.3. Validation: scan all registration paths and discovery metadata; no tool can omit a level or escape the policy wrapper.

Demo: `RegisterAll` is the complete composition list; each area shows every tool and one explicit risk argument. Tool discovery and API contracts match the baseline except intended safety metadata and approval behavior.

## Sprint 4: Verify, document and prepare release

Goal: a locally verified change with an honest client-compatibility report.

### 4.1 End-to-end regression matrix
- Locations: `mcp/*_test.go`, `internal/safety/*_test.go`, `internal/tools/*_test.go`, affected master MCP tests.
- Exercise Low/Medium/High/VeryHigh across authenticated HTTP and the actual stdio executable; cover modern input continuations and supported legacy elicitation/session flows.
- Include concurrency, denial, replay, altered IDs above 2^53, wrong session, expiry, cancellation before/after approval, review/backup failure, backend partial failure and no mutation retries. Verify safe error text and unchanged output shapes.
- Dependencies: Sprint 3. Validation commands from `mcp`: `go test ./...`, `go test -race ./...`, `go vet ./...`; run affected master/API tests when shared-server wiring changes.

### 4.2 Refresh documentation and release evidence
- Locations: `mcp/docs/tool-safety.md`, `mcp/docs/areas/*.md`, `mcp/CONNECTING.md`, `mcp/README.md`, `mcp/UPDATE_STATE.md`, test guide and SilverBullet canonical MCP pages.
- Generate/verify the readable area safety inventory from actual registrations. Refresh coverage using existing reporting rules; this refactor earns no new endpoint coverage. Preserve unrelated documentation edits.
- Record fixtures versus real-client checks versus deployment separately. Deployment is a later authorized operation, using a disposable qualification environment first.
- Dependencies: 4.1 and successful 2.3 human-boundary qualification. Validation: no undefined levels, missing tools, bypasses, unsupported client claims or stale Medium policy statements in the changed documentation.

Demo: reviewer can audit each area, run both transports, inspect redacted Medium review records and observe a real human prompt for High/VeryHigh in both requested chat modes.

## Risks, release gates and rollback

- Human provenance is the largest integration risk: standard elicitation responses alone do not establish who approved. A mode that auto-answers must remain unsupported/blocked until a trusted human path is available.
- A recorded Medium check enforces observable preconditions, not private model reasoning. Describe this distinction precisely.
- Risk follows effects: sensitive reads may be High, and ordinary write endpoints are not all equally risky. Review actual fleet, DNS, proxy, retention and host effects.
- Review/audit storage needs bounded disk use and redaction; retained VM recovery files keep their existing retention guarantees.
- Approval waiting must not hold execution locks. Preflight is repeated after human delay; externally changing resources still have backend atomicity limits.
- No full test suite was run for this planning-only task. Implementation acceptance requires Sprint 4 checks and real-client release qualification.
- Roll back migration commits by area while preserving stored recovery/review records. If reverting would restore ungated non-VM writes, disable affected tools or retain the shared enforcement until fixed. Do not silently return to a weaker production policy. Restart invalidates outstanding approval continuations.

## Definition of done

Every registered tool has one visible level and uses the common execution wrapper. All areas have explicit registration functions. Medium records validated target/argument/impact checks without requiring human approval. High/VeryHigh cannot execute without the qualified human decision path in either requested chat mode. Existing VM recovery, API contracts and transport parity pass the stated regressions. Client limitations and deployment state are reported without unsupported guarantees.
