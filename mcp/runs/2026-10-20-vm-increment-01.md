# VM MCP implementation increment — 2026-10-20

## Scope and authorization

The user explicitly requested VM creation, autostart/live flag changes, automatic
backup schedule creation/editing/enabling, and SSH key injection. Seven named
tools are implemented. This is a narrow exception for the requested behaviors,
including false flag record removal and automatic retention deletion; all other
VM exclusions remain. No production mutations or deployment were performed.
Existing unrelated master/npm working changes were preserved.

## Reviewed contracts

| Tool | Endpoint | Risk | Contract and prior-state evidence |
| --- | --- | --- | --- |
| create_vm | POST /virsh/createvm | high | All 14 API fields preserved, including false/empty/zero selectors. Template defaults supported. Records actual target inventory, destination NFS and selected ISO/template; duplicate names fail closed. HTTP 201 text gives acceptance, no ID. |
| set_vm_autostart | POST /virsh/autostart/{vm_name} | medium | Required auto_start boolean; retains actual autoStart plus secure domain. False removes the flag entry. |
| set_vm_live | POST /virsh/vmlive/{vm_name} | high | Required enable boolean; retains actual isLive plus secure domain. Changes master flag, not running state. |
| create_vm_automatic_backup | POST /virsh/autobak | very high | Complete schedule; positive IDs/counts; HH:MM server time, >=45-minute window, midnight supported, equal times mean 24 hours. Enables immediately; future retention deletion explicit. Records inventory, target VM and NFS. |
| update_vm_automatic_backup | PUT /virsh/autobak/{id} | very high | Full replacement and unconditional enable; additionally retains exact old schedule. |
| enable_vm_automatic_backup | PATCH /virsh/autopak/enable/{id} | very high | Existing positive ID; already-enabled schedule rejected. Exact old schedule plus VM/NFS retained. |
| add_vm_ssh_key | POST /virsh/add_ssh_key/{vm_name} | very high | Single supported OpenSSH public key; requires shut-off guest. Private complete primary disk-file export and fsynced path/size/SHA-256 manifest precede injection. Rechecks exact guest/domain identity before one write. |

Sources inspected: master/api/virsh.go; master/services/virsh.go and
virshBackups.go; master/db/virshBackups.go; slave/virsh/ssh_key.go and
custom_cpu.go; existing MCP client, typed registrations, approval, snapshot and
artifact storage helpers. Master's SSH customization worker uses a legacy
background RPC; cancellation does not prove it stopped.

All tools share HTTP/stdio registration, exact integer decoding, argument-bound
runtime approval, serialized snapshot/save/write, private retained recovery,
safe errors and no automatic retries. Creation/SSH JSON operations use a
15-minute timeout; export retains its six-hour bound and earlier deadlines.
SSH public keys use golang.org/x/crypto/ssh v0.55.0, matching master's version.

## Verification

- Baseline: full MCP normal suite passed before edits.
- Six new configuration tools first failed SDK calls with unknown-tool errors.
- All new writes exercised through authenticated SDK HTTP and actual executable
  stdio, modern and legacy protocol generations. Includes both boolean values,
  live/template creation defaults, exact wire body/method/path/auth, missing and
  unknown arguments, declined/canceled approval, failed snapshots, retained
  recovery records, safe backend failures and one mutation per invocation.
- Snapshot guards cover missing/null/invalid previous flags, duplicate creation,
  absent ISO/NFS/schedules, invalid retention metadata and already-enabled backup.
- SSH tests cover a complete >16 MiB export, exact bytes/hash/privacy and manifest
  before mutation, canceled/declined/unavailable picker, record save failure,
  running guest, mismatched primary disk, snapshot/export/truncation/manifest
  failure, changed guest during export and retained recovery on backend error.
- Public-key parser rejects malformed/truncated/mismatched keys, private keys,
  multiple key lines and authorized_keys options. Clock validation matches the
  actual backend's overnight/full-day window semantics.
- Full MCP normal suite: passed (622 tests in five packages).
- Master api/services/virsh normal tests and vet: passed (58 tests; existing skips
  remain). Master route tests now expect the expanded shared registry.
- Extra true-flag/live-template cases: focused normal and race checks passed.
- Full MCP race suite and final vet: passed (622 tests in five packages).

An initial SSH positive test caught comparison of escaped/pretty-printed recovery
JSON against raw JSON; fixed by comparing decoded values with exact numbers.
Full suite initially caught stale standalone/master 110-tool assertions; updated
them to 117. No production hypervisor, guest customization or NFS transfer was
qualified by these fixtures.

## Coverage and completion

No API route added. The source-reviewed 322-route inventory reconciles to
117 implemented, 157 missing and 48 excluded: 36.34% raw coverage, previously
110/322 (34.16%). VM subtotal is 63/82 (76.83%), previously 56/82 (68.29%).
Seventeen VM exclusions remain; both internal snapshot helpers earn no direct
coverage. Public eligible original VM routes are now 63/63; including internal
helpers, non-excluded direct coverage is 63/65 (96.92%). The API inventory and
VM/tool tables were checked for unique counts and matching status totals.

Source revision and digest: mcp/UPDATE_STATE.md and the selected plan's
mcp-source-digest.json. Canonical scope/risk/limits: mcp/docs/areas/vms.md.

SSH disk export is a retained primary disk file, not a flattened chain or a
multi-disk snapshot. Backing files remain dependencies. Export needs time and
storage for the full disk-file size. External starts and disk changes can race
this registry. No automatic rollback is promised, and a timeout can leave
backend work ongoing. Schedule retention deletion is intentional and approved
per invocation. Implementation is local and deployment remains outstanding.

Canonical knowledge updated and verified: registry-and-coverage,
vm-approval-and-recovery and index under
knowledge/projects/HyperHive/features/mcp/. The plan's knowledge-validation.md
records paths, sizes, links and the pre-existing index size exception.

## Follow-up: missing approval picker

The user reported the generic explicit-approval error with no picker visible.
The exact message is reproducible when a client returns cancellation/decline or
an incomplete form response without displaying UI. Source inspection cannot
establish which response the actual client sent. Error handling now distinguishes
client cancel, decline, selected cancel, absent/invalid decision and unavailable
form elicitation, while retaining argument binding and fail-closed behavior.
Tests cover modern input-required and legacy retained-session form flows. No
production creation was attempted. This improves diagnostics; it does not add
picker support to a client that lacks it or remove the approval requirement.

The client was confirmed as Codex desktop; this chat's effective policy is
approval_policy=never. Official Codex configuration documents the granular
mcp_elicitations permission for surfacing MCP prompts instead of auto-rejection.
CONNECTING.md now documents an MCP-only interactive granular policy and the
need to refresh the effective chat permission profile. No user/global permission
configuration was changed and server approval was not bypassed.
