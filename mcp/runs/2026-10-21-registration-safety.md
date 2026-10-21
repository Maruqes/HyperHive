# MCP registration and safety rework — 2026-10-21

## Result

Implemented one typed `RegisterTool`, explicit `RegisterAll` and per-area entry
points for Proxies, NGINX, Upstreams, Celld, DNS, Notes, SPA, ISOs and VMs.
One level declaration controls discovery and execution; missing/invalid levels,
duplicate names, missing mappings and HTTP DELETE registration fail startup.

Low retains normal validation. Medium validates domain constraints and target
identifiers, records exact argument digest, redacted arguments and expected impact
privately before dispatch, without a human prompt. High/VeryHigh always require
an explicit per-call form, with bound single-use expiry-protected continuation.
The shared gate includes available session/client identity and rejects replay,
changed arguments, expiry, invalid fields and unavailable/declined/cancelled forms.
Existing VM recovery capture, retention, locking, backup failures and backup-path
returns remain, including Medium writes and transfers.

The user explicitly chose to remove `delete_celld_fleet`; discovery and named-call
negative checks verify it is absent. Current registry is 117 tools, mapped to 117
of the retained 322 API endpoint inventory (36.34%); Celld is 8/17 (47.06%), with
157 missing and 48 excluded overall. No backend capability route changed. The
baseline denominator was retained; this is not a new audit of unrelated APIs.
The full safety table is in [tool safety](../docs/tool-safety.md).

## Local verification

Passed:

- MCP `go test ./...` and `go test -race ./...`.
- MCP `go vet ./...`.
- Master `go test ./api -run 'Test.*MCP' -count=1` and its race variant.
- Master `go vet ./api`.
- Additional final policy race tests after the binding/validation changes and
  focused transport/VM Medium regressions after summary wording cleanup.
- Actual executable stdio plus authenticated HTTP, modern and legacy generations.
- AST check prevents production SDK/decoder registrations bypassing the wrapper.
- Discovery snapshot verifies area/risk/required-action metadata on every tool;
  the documentation inventory was exported from this discovery fixture.
- Pre-dispatch persisted Medium records, privacy, exact IDs above 2^53, storage
  failure, cancellation, bounded capacity, deny/unavailable approval, expired and
  changed-client continuations, replay, altered inputs and no mutation retries.

Initial failures reflected old fixture assumptions about ungated non-VM writes,
Medium picker requirements and Celld deletion. Fixtures were adapted to the new
policy while preserving backend wire, malformed-input and recovery assertions.
No passing tests establish production deployment or human prompt rendering.

## Limits and release gate

Standard MCP client replies do not attest a human click or expose effective chat
mode. The form explicitly requires a human decision in both Approve for me and
Ask for Approval; actual desktop qualification remains pending. Do not treat an
automatically answered form as human approval or claim the SDK fixture qualifies
this release condition. No client permission change or production operation ran.

Medium records check observable inputs and declared impact, not private model
reasoning. Backend state/atomicity limitations remain. Review capacity is local
to a registry; separate processes require separate directories for a strict
aggregate quota. Final audit outcome updates are best effort; retained preflight
alone does not prove execution/success. Recovery records are never auto-deleted.

## Source evidence

Working-tree baseline: `dd63aad991860347d67b1d114d97d8cff810a258` plus this rework.
SHA-256: `963a18b29c8af475443a627a15d724ab57786be742fcdd1ddca3c4b0314e08fe`.
Manifest: `.planning/2026-10-21-mcp-registration-safety/mcp-source-digest.json`.
Existing unrelated README additions and `mcp/TESTING.md` were preserved.

## Registration layout correction after user clarification

The first layout left names and safety levels spread across per-tool/group
helpers. The user clarified that each area's registration function must display
every registration and security level together. Nine `register_<area>.go` files
now contain every direct `RegisterTool(server, "name", definition, safety.Level)`
call. `RegisterVMs` contains all 64 VM calls; `RegisterCelld` contains eight; the
other areas have complete compact lists. Helpers prepare typed schemas/handlers
and recovery callbacks without registering tools or assigning levels. The public
registrar verifies each literal name matches its prepared definition.

After this structural correction, full MCP normal tests, MCP vet and affected
master MCP tests passed. Focused race checks passed for policy packages, registry
source/discovery parity, Medium persistence, protected DNS and representative VM
Medium/High/VeryHigh recovery over the actual HTTP/stdio transports. The full
race run above predates this structural correction. New AST checks prevent
registration outside the nine catalogs and ensure every discovered tool matches
its visible name, area and literal level. All 117 catalog entries also match the
prior live-discovery table. Policy behavior, endpoint coverage and deployment
status are unchanged.

## Final location preference

The user requested the complete area registration functions at the end of the
existing area files instead of separate registration files. All nine functions
were moved accordingly and the separate files removed. The source/discovery
check now asserts end-of-file placement and the same 117 names/areas/levels.
This is a file move with unchanged runtime behavior.
