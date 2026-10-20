# MCP development guidelines

## Absolute rule: no deletion tools

**NEVER, NEVER, NEVER implement, register or expose an MCP tool that deletes
data or resources.** This applies to every area and every transport.

- Never expose an HTTP DELETE endpoint through MCP.
- Also reject deletion through POST/PUT/PATCH, aliases, generic action parameters,
  batch operations or helper calls. Names such as remove, purge, wipe, erase,
  destroy, clear and reset do not create an exception when the action deletes data.
- Do not add deletion behind a confirmation flag, opt-in switch, disabled
  registration, dry-run option or generic API tool. Do not scaffold a future deletion tool.
- Review actual effects, not just names and HTTP methods. Skip any operation
  whose required behavior includes deletion; never weaken its contract to count it.
- Exclude deletion capabilities from implementation queues and eligible coverage.
  Keep them visible as policy exclusions in the raw API denominator with no credit.
- Requests for "all tools", "the rest" or "100% coverage" never override this rule.

## Explicitly authorized VM exceptions

The user explicitly authorized these seven VM capabilities on 2026-10-20:
creation, setting autostart/live flags (including false), automatic backup schedule
creation/update/enable (including retention deletion), and SSH public-key injection.
Implement their named typed contracts with the existing approval and recovery rules.
This does not authorize standalone deletion, VM import/restore/migration, disk
creation or any other previously excluded capability. SSH injection requires a
shut-off guest and a privately persisted primary-disk artifact before dispatch.

## Scope and architecture

- Write all code, scripts and documentation in English.
- Implement the user's requested increment; add named, typed tools progressively.
- Keep this folder a standalone Go module using a pinned official MCP Go SDK.
- Share tool registration between embedded HTTP and standalone stdio.
- Call the HyperHive HTTP API through the shared client. Do not bypass its
  authentication or business rules with direct database, NPM or master-service calls.
- Keep tools grouped by feature under `internal/tools` and HTTP logic under
  `internal/client`. Master-specific authentication bridges belong to master.

## Configuration and behavior

- Keep API URLs and credentials outside tool arguments. Never commit secrets or
  include them in logs, errors or reports.
- Preserve authentication boundaries and fail closed when credentials are unavailable.
- Keep the shared API listener, loopback URL and generated proxy targets consistent.
- Reserve standalone stdout for MCP traffic; send diagnostics to stderr.
- Use timeouts, honor cancellation and avoid automatic retries of mutations.
- Handle JSON and plain-text API failures without leaking sensitive response bodies.
- Match API contracts exactly. Preserve omitted versus explicit false/zero values
  in updates, and never invent response fields or IDs.

## VM risk, approval and recovery

- Give every VM, VM disk/template and host-tuning tool an explicit risk level:
  low, medium, high or very high. Publish the level in discovery and its contract.
- Medium and higher operations require an explicit per-invocation user choice
  through the MCP client's approval picker. A tool argument, prior blanket
  permission or assistant-authored confirmation is not approval. Fail closed
  when the client cannot provide the picker or the user declines/cancels.
- Bind approval to the exact operation and arguments; reject replayed, expired,
  forged or altered approval continuations. Never automatically retry writes.
- Before changing existing VM XML or settings, save actual old domain XML and
  the relevant old settings, including the prior CPU pinning when changing it.
  Save corresponding prior state for host tuning, disks, templates and schedules.
- Persist the recovery record privately on the MCP server before the write:
  private directories/files, unique retained records and no secrets in logs.
  Backup read, validation or persistence failure must block the mutation.
- Return the backup location on success and on subsequent backend failure.
  Do not promise disk snapshots, automatic rollback, live-state completeness or
  atomicity against external changes when the API does not provide them.
- Verify approvals, denied/unavailable picker, backup failures and retained
  pre-change state through both MCP transports using disposable fixtures.

## Verification checks

- Test tool discovery and invocation through an SDK client.
- Use mock HTTP servers to verify method, path, authentication, body, response
  mapping and meaningful failure cases. Do not require production mutations.
- Run `go test ./...` from this module after behavior changes. Use race checks,
  vet and focused master tests when relevant to the change.

## Documentation and coverage

- Keep this file general: no current tool lists, dated findings or fixed counts.
- Keep `README.md` short: purpose, coverage, connection and essential development steps.
- Keep connection examples in `CONNECTING.md`.
- For implementation batches, follow `IMPLEMENT_TOOLS.md`: establish the user's
  tool count and selected area before starting, then validate one tool at a time.
- Follow `UPDATE_STATE.md` to review current source, refresh its report and update
  the README after API or tool changes. Do not implement new tools merely to raise coverage.
- Compute coverage as fully supported unique HTTP method/path pairs divided by
  all endpoints in the stated scope. Count endpoints, not tool names.
- Count support only for registered, enabled tools with reviewed contracts and
  passing relevant tests. Internal helper calls earn no additional coverage.
- Distinguish implemented, missing, partial, stale, disabled and excluded endpoints.
  Only implemented endpoints count as fully supported; keep exclusions visible.
- Show counts and percentages for the total and each area. Sum endpoint counts
  for parent areas; never average percentages. Use N/A for empty areas and never
  round incomplete coverage to 100%.
- Record source evidence, limitations and missing endpoints. Recheck nested routes,
  aliases and dynamic registrations; never reuse historical denominators blindly.
- Describe source inspection honestly. Do not claim a generator, CLI, live check
  or deployment exists unless it was actually implemented or performed.
