# Implement MCP tools

This is the reusable agent workflow for implementing a batch of HyperHive MCP
tools. Follow it when the user points to this file. All files and reports must
be written in English. This file guides the current agent session; it does not
start a background process or scheduled job.

## Absolute rule: NEVER implement deletion tools

**NEVER, NEVER, NEVER implement, register or expose ANY delete-action MCP tool.**

This rule applies before selecting work, during implementation and before
registration. It covers HTTP DELETE and deletion implemented through other
methods, names or indirect calls: remove, purge, wipe, erase, destroy, clear,
reset, batch deletion and generic action/API dispatchers.

- Do not implement deletion even behind confirmation, opt-in, disabled-by-default
  or dry-run settings. Do not create stubs for future deletion tools.
- Inspect handler/helper side effects. If deletion is part of the required
  operation, exclude the operation rather than exposing a partial workaround.
- Never select deletion as a prerequisite, fallback, rollback or MCP cleanup tool.
  Ordinary test-fixture cleanup does not authorize an MCP deletion capability.
- Exclude deletion tools when calculating the user's available tool count.
  "All remaining" means all remaining permitted tools in the chosen area.
- Record deletion endpoints as excluded by user policy, keep them visible in raw
  coverage and award no support credit. Never chase raw 100% by adding deletion.

## 1. Ask for the batch scope

Before implementing anything, ask:

> How many new MCP tools should I implement, and which area should I work on?
> For example: 3 tools in proxy management, 5 in certificates, or all remaining
> tools in access lists.

Read the current [coverage report](UPDATE_STATE.md) to suggest relevant areas
and show how many endpoints are missing. Endpoint counts are not necessarily
tool counts. Ask only for information the user has not already supplied. If the
user already specifies both count and area, proceed without asking again.

- Accept a positive number or **all remaining tools in the selected area**.
- Accept an area, multiple explicit areas or a list of named tools.
- If the requested count exceeds the available capabilities, explain the actual
  remaining queue and use it as the maximum; do not invent tools to meet a quota.
- Resolve ambiguous area names before editing. Do not assume the entire API.
- Wait for the user's scope answer before making implementation changes.
- A previous run's scope is context, not permission for a new batch. An explicit
  request to resume the same batch preserves its saved count and area.

Preparing or editing this workflow does not itself start an implementation batch.

## 2. Prepare the run

1. Read [AGENTS.md](AGENTS.md), [UPDATE_STATE.md](UPDATE_STATE.md) and relevant
   parts of the [detailed plan](tool-implementation-plan.md).
2. Inspect the working tree and preserve unrelated changes. Recheck the selected
   area's routes and registered tools; do not trust historical counts blindly.
3. Select a dependency-aware queue within the user's scope. Prefer reads before
   writes when prerequisites make that useful; do not add unrelated prerequisite
   tools without resolving the scope with the user.
   Remove every deletion capability before presenting the queue or target count.
4. Tell the user the chosen queue and target count, then proceed. This is a
   progress update, not another approval request.
5. Create a uniquely named checkpoint under `mcp/runs/`, such as
   `YYYY-MM-DD-proxies-01.md`. Resume that file when continuing the same batch.
6. Run the relevant baseline checks and record existing failures before editing.

The count means **new tools that pass all required gates in this batch**.
Rechecking existing tools, refactoring helpers, fixing regressions, adding tests
and writing reports do not consume the requested tool count. Track endpoint
coverage separately: one tool is not automatically one newly supported endpoint.

Perform only the shared foundation work needed for this batch. The detailed
plan is a roadmap, not permission to implement every sprint or tool at once.
If the smallest viable foundation change would materially exceed the selected
batch, describe the concrete dependency and resolve that scope before proceeding.

## 3. Repeat for one tool at a time

### A. Review the contract

Read the route registration, middleware, handler, input/output types and relevant
helpers. Record a short contract card in the checkpoint:

- Tool name and mapped HTTP method/path pairs.
- Source references and prerequisites.
- Required/optional inputs, defaults, bounds and omitted versus false/zero values.
- Exact request encoding and success/error response shapes and statuses.
- Permissions, side effects, backend restrictions and update semantics.
- Output schema, tool annotations and any limitations.

Do not infer contracts from endpoint names. If an ambiguity prevents correct
implementation, resolve it or record the tool as blocked.
If the contract includes deletion, exclude the tool immediately under the
absolute rule; do not implement it or ask for an exception to fill the quota.

### B. Write contract tests

Define expected behavior from the API source before implementing the tool.
Verify new tests fail for the intended reason. Cover applicable cases:

- Valid inputs and complete supported request fields.
- Missing, invalid and unknown inputs rejected before backend calls.
- Exact method, path/query encoding, URL prefix, authentication and body.
- Successful output, empty responses and preservation of meaningful fields.
- API permission, missing-resource, conflict and server failures.
- Relevant domain restrictions and partial failures.
- Mutation semantics, including no automatic write retries or fabricated IDs.

Reuse shared transport tests for generic limits, cancellation, timeouts,
redirect refusal and safe errors. Add operation-specific tests where needed.
Fixtures must express independently reviewed expectations, not copy the new
implementation's assumptions.

### C. Implement and exercise MCP

Add the smallest complete client operation, typed tool, validation and endpoint
mapping. Share registration across HTTP and stdio. Invoke the tool through SDK
clients over **both authenticated HTTP and the actual stdio executable**, using
disposable backend fixtures. Check discovery, schemas, results and error behavior.

Where practical, also test the actual HyperHive handler with disposable downstream
dependencies. Report fixture, handler and real-backend validation separately.
Do not contact production or deploy as part of this local implementation workflow.

### D. Pass the acceptance gate

Review the implementation against the contract card. Run focused tests first,
then from `mcp/`:

```sh
go test ./...
go test -race ./...
go vet ./...
```

Run relevant master checks if routing, authentication or master behavior changed.
Record unavailable required checks as blockers. Fix failures and rerun affected
checks; do not weaken assertions or remove cases to advance the queue.

A tool becomes verified only when its reviewed contract is fully supported,
registration is enabled, both MCP transports are exercised and all required
checks pass. If only part of its endpoint contract is supported, report partial
coverage rather than awarding full endpoint credit.
Before finishing, inspect the tool registry and changes for deletion names,
DELETE endpoint mappings and indirect deletion paths. No deletion capability may
be added, even if its tests pass.

### E. Save progress and refresh documentation

1. Record the tool's outcome, contract/source evidence, checks and limitations.
2. Follow [UPDATE_STATE.md](UPDATE_STATE.md) to refresh the full report and README
   from the current source. Use existing coverage tooling when available; otherwise
   perform and label a source-reviewed calculation. Do not invent available commands.
3. Verify supported/missing totals, per-area percentages and tool mappings agree.
4. Increment the completed-tool count only for a newly verified tool.
5. Send a short progress update: completed/target, tool, validation and coverage delta.
6. Continue immediately to the next scoped tool while the batch has work remaining.

Do not request permission between successful iterations. Do not add extra tools
after reaching the agreed count or completing the selected area's queue.

## 4. Handle blockers and interruptions

- Save incomplete work with its actual status and next action; do not count it.
- Investigate failures with materially different approaches. After three failed
  approaches to the same blocker, stop repeating it and explain the evidence.
- Stop the batch if the blocked tool requires a user decision or prevents the
  remaining queue from being validated. Ask one concise, concrete question.
- Independent queued tools may continue after a blocker is checkpointed. Keep
  the blocker visible; never substitute work outside the selected area or exceed
  the count. Report fewer completed tools if the remaining scope cannot meet it.
- Honor a user pause or stop promptly and leave a recoverable checkpoint.
- On resume, inspect actual code and revalidate changed evidence before trusting
  a saved verified status. Do not restart completed tools or reset the batch count.
- Do not create another chat, schedule future runs, commit, push or deploy unless
  the user separately requests it. Keep each increment reviewable locally.

## 5. Finish the batch

Stop when the agreed number of new tools is verified, the selected queue is
exhausted, the user stops the work, or a required blocker prevents continuation.
Refresh final documentation and provide:

- Requested area/count and verified/target count.
- Tools completed, partial work and blockers.
- Before/after overall and selected-area endpoint counts and percentages.
- Checks passed, checks unavailable and limits of backend validation.
- Checkpoint path and the next pending tool.

Keep raw API coverage visible. Label eligible-only coverage separately if used;
explain exclusions and never silently shrink the denominator to claim 100%.
The batch is not complete merely because code was written or tests were added.

## Checkpoint template

```markdown
# MCP implementation batch

## Scope
- User request:
- Selected area(s):
- Target new tools:
- Completed new tools: 0
- Baseline coverage:
- Baseline checks/failures:

## Queue
| Tool | Endpoint mapping | State | Required evidence / next action |
| --- | --- | --- | --- |
| example_tool | METHOD /path | pending | Review contract |

## Active contract card
- Tool and endpoint mapping:
- Source references/content evidence:
- Inputs and request encoding:
- Outputs and API errors:
- Permissions, side effects and restrictions:
- Validation cases and limitations:

## Iteration results
- Tool:
- Outcome:
- Checks and validation level:
- Coverage before/after:
- Report and README refreshed:

## Resume
- Current tool and last completed step:
- Blocker, if any:
- Next action:
```

## Example invocation

```text
Follow mcp/IMPLEMENT_TOOLS.md.
```

The agent first asks for count and area. To supply both upfront:

```text
Follow mcp/IMPLEMENT_TOOLS.md. Implement 3 new tools in proxy management.
```

To resume an interrupted batch:

```text
Follow mcp/IMPLEMENT_TOOLS.md and resume the batch recorded in
mcp/runs/<checkpoint-file>.md using its original scope and remaining count.
```
