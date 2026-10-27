# HyperHive UI improvement timeline and tickets

**Created:** 2026-10-27  
**Status:** Milestone 01 in progress; H03, G01, VM02 and NT01 accepted; paused before NT02  
**Owner:** unassigned  
**Schedule:** milestone order, not committed calendar dates

## Working agreement

- Implement **one ticket or epic child at a time**. Do not deliver this backlog as one large change.
- Each ticket is a separate reviewable commit/change with focused validation and desktop/phone evidence. Stop at the review checkpoint; start the next ticket after acceptance or explicit instruction.
- Work in milestone order. H03 is first because the user emphasized it. **All Docker-specific work is last**, including applying/verifying G01 on Docker pages.
- Use the current components, typography, icons and restrained visual language. Preserve approved VM borders, compact host summaries, neutral companion borders and table-based backups.
- Choose density per page before a layout rework; if the ticket does not settle the choice, show a small preview and get that preference at its review checkpoint. No global density reset.
- No deployments, live power actions, RAID changes, port openings, backup restores or data transfers are authorized merely by this planning document. Use mocks/fixtures for operational verification and agree on any real test resource when needed.

## Selection normalization

- IDs are case-insensitive. `Cr02` is recorded as **CF02** (celld empty-state action).
- Repeated **G01** entries are one shared ticket, with explicit Notes and Docker coverage.
- **D02 is overridden:** remove the storage Update button; do not rename it. Automatic refresh must continue.
- **DK03 is new:** move, download and import standalone disks, following existing VM disk workflows where compatible. It is split below rather than treated as a cosmetic change.
- **WG03 is new:** peer connection/data observability; overlaps with WG01 are owned by WG03.
- The requested large logs rework is **LOGS**, an epic with six child tickets; it is not restricted to only the original LG01–LG03 bullets.
- Unselected catalogue proposals remain out of scope. A selected ticket may require a tightly scoped backend change, but that must be justified by its capability check.

## Timeline

| Order | Milestone | Tickets in execution order | Exit checkpoint |
|---|---|---|---|

| 01 | Host details and shared usability | H03, G01, VM02, NT01, NT02, SE01, SE02 | Demo and acceptance of the milestone; runnable app |

| 02 | Dashboard | D01, D02 | Demo and acceptance of the milestone; runnable app |

| 03 | Storage, disk health and GPU inventory | BR01, BR02, AM01, AM02, SD01, SD02, SD03, NF02, NF03, DK02, XT02, GP01, GP02 | Demo and acceptance of the milestone; runnable app |

| 04 | Backup workflows | BK01, BK02, BK03, AB02 | Demo and acceptance of the milestone; runnable app |

| 05 | VM forms, details and standalone disk transfers | VC01, VC02, VC04, VD01, VD02, DK03 | Demo and acceptance of the milestone; runnable app |

| 06 | Networking, VPN observability, Kubernetes and fleets | WG01, WG02, WG03, K801, K802, CF01, CF02, SP01, SP02 | Demo and acceptance of the milestone; runnable app |

| 07 | Host updates | UP01, UP02, UP03 | Demo and acceptance of the milestone; runnable app |

| 08 | Logs rework epic | LOGS | Demo and acceptance of the milestone; runnable app |

| 09 | Support layout | SU01, SU03, SU04 | Demo and acceptance of the milestone; runnable app |

| 10 | Proxy management and certificates | CE01, CE02, PX02, PX03, US01, US02, RD02, TS01, TS02 | Demo and acceptance of the milestone; runnable app |

| 11 | Docker — final milestone only | DI01, DI02, DC01, DC02, DV01, DV02, DW01, DW02, GT01, GT02 | Demo and acceptance of the milestone; runnable app |


Time estimates are intentionally not invented. Size is relative scope: **S** = focused UI/copy, **M** = a workflow or cross-component change, **L** = route/API/data work. Re-estimate after DK03.1, WG03.1 and LOGS.1. A milestone can span multiple sessions. No parallel execution is implied by independent dependencies.

## Source and file conventions

Frontend paths below are relative to `/home/marques/projects/HyperHiveFront/HyperHive/`. Backend `master/...` paths are relative to `/home/marques/projects/HyperHive/`. They identify starting points, not an exhaustive change list. Inspect current callers, fixtures, exports and API contracts before editing. The original audit is [review.md](review.md); each selected original ticket links its screenshot.

Planning evidence: `app/vms-disks.tsx` imports standalone list/create/grow/delete helpers from `services/hyperhive.ts`. Existing `services/vms-client.ts` has VM-name-based disk movement and attachment operations. `types/wireguard.ts` already defines optional runtime handshake/endpoint/traffic fields. These local source observations do not establish live API availability.

## Ticket ledger



## Milestone 05 — VM forms, details and standalone disk transfers

Improve VM workflows and deliver disk transfer capabilities incrementally.

**Entry:** previous milestone accepted. **Exit demo:** selected behavior works on desktop and phone; no unrelated feature regressions; each ticket has evidence and its own review checkpoint.



### [ ] VC04 — Show host and storage capacity before creation

- **Status:** Todo · **Scope:** M · **Depends on:** VC01; current host/storage capacity contracts

- **Start points:** `components/modals/CreateVmModal.tsx; services/hyperhive.ts; services/vms-client.ts`.

- **Acceptance:** Show known selected-host allocation and NFS capacity before creation; distinguish configured allocation from live utilization, handle unknown capacity and avoid silently imposing new limits.

- **Validation:** Inspect desktop and 375px phone layout, keyboard access and loading/empty/error states relevant to this ticket; run the existing focused suite where it covers changed behavior. Add a small meaningful test for new logic, not for copy-only edits.

- **Evidence / review:** [current screenshot](screenshots/36-new-vm.jpg); attach after screenshots and relevant check results before marking Done.


### [ ] VD01 — Allow full path and per-interface IP inspection and fix the rest of the modal, for example "Open Console"placement

- **Status:** Todo · **Scope:** S · **Depends on:** previous milestone accepted; otherwise independent within this milestone

- **Start points:** `app/vms.tsx`.

- **Acceptance:** Allow full disk-path disclosure/copy and readable per-address presentation with interface context if supplied; preserve every address and avoid truncating the only accessible value also fix the rest of the problems.

- **Validation:** Inspect desktop and 375px phone layout, keyboard access and loading/empty/error states relevant to this ticket; run the existing focused suite where it covers changed behavior. Add a small meaningful test for new logic, not for copy-only edits.

- **Evidence / review:** [current screenshot](screenshots/37-vm-details.jpg); attach after screenshots and relevant check results before marking Done.


### [ ] DK03 — Standalone disk move, download and import (epic)

- **Status:** Todo · **Scope:** L · **Start points:** app/vms-disks.tsx; services/hyperhive.ts; services/vms-client.ts; components/modals/ImportVmModal.tsx; backend master/api/vm_disk.go, master/services/vm_disk.go, master/vm_disk/vm_disk.go if required

- **Completion:** all child tickets below accepted; each child is its own increment, not one large implementation.


#### [ ] DK03.1 — Verify standalone disk transfer contracts

- **Status:** Todo · **Depends on:** Existing inventory operations; no live transfer.

- **Scope / acceptance:** Trace current VM export/import/move patterns, standalone disk IDs versus VM names, storage destinations and attachment rules. Record supported formats, operation progress and missing endpoints. Do not call VM-name operations with standalone disk IDs.

- **Validation:** focused contract/logic tests for new behavior; inspect desktop/phone states and failure recovery. Use fixtures or explicitly agreed disposable resources for transfer/state-changing tests.
- **Review evidence:** changed files, test result, screenshots and remaining limitations; no next child until accepted.


#### [ ] DK03.2 — Download standalone disks

- **Status:** Todo · **Depends on:** DK03.1

- **Scope / acceptance:** Reuse the verified authenticated streaming/export path or add the smallest backend support. Download original bytes safely for large files; preserve format/name and handle unauthorized/missing disks. Explain consistency requirements for attached disks.

- **Validation:** focused contract/logic tests for new behavior; inspect desktop/phone states and failure recovery. Use fixtures or explicitly agreed disposable resources for transfer/state-changing tests.
- **Review evidence:** changed files, test result, screenshots and remaining limitations; no next child until accepted.


#### [ ] DK03.3 — Import standalone disks

- **Status:** Todo · **Depends on:** DK03.1

- **Scope / acceptance:** Provide the verified existing-file/upload/URL modes that backend supports, with destination, format, collision checks, progress and cleanup of partial failure. Any remote URL mode must retain server network/path protections; imports do not silently overwrite.

- **Validation:** focused contract/logic tests for new behavior; inspect desktop/phone states and failure recovery. Use fixtures or explicitly agreed disposable resources for transfer/state-changing tests.
- **Review evidence:** changed files, test result, screenshots and remaining limitations; no next child until accepted.


#### [ ] DK03.4 — Move standalone disks

- **Status:** Todo · **Depends on:** DK03.1; DK02

- **Scope / acceptance:** Choose a verified destination with capacity/attachment checks; show progress and explicit impact; update metadata only after successful transfer. On failure retain a usable source and clean partial output safely. Never delete source before destination success.

- **Validation:** focused contract/logic tests for new behavior; inspect desktop/phone states and failure recovery. Use fixtures or explicitly agreed disposable resources for transfer/state-changing tests.
- **Review evidence:** changed files, test result, screenshots and remaining limitations; no next child until accepted.


## Milestone 06 — Networking, VPN observability, Kubernetes and fleets

Add connection insight and clearer network/access setup.

**Entry:** previous milestone accepted. **Exit demo:** selected behavior works on desktop and phone; no unrelated feature regressions; each ticket has evidence and its own review checkpoint.


### [ ] WG03 — WireGuard connection and traffic observability (epic)

- **Status:** Todo · **Scope:** L · **Start points:** app/wireguard.tsx; services/wireguard.ts; types/wireguard.ts

- **Completion:** all child tickets below accepted; each child is its own increment, not one large implementation.


#### [ ] WG03.1 — Verify runtime telemetry and state semantics

- **Status:** Todo · **Depends on:** WG01

- **Scope / acceptance:** Validate optional wireguard runtime response against LastHandshakeTime, Endpoint, ReceiveBytes and TransmitBytes. Define never-handshaken, stale, unknown and fresh-handshake states; a fresh handshake is evidence of activity, not a guaranteed persistent connection.

- **Validation:** focused contract/logic tests for new behavior; inspect desktop/phone states and failure recovery. Use fixtures or explicitly agreed disposable resources for transfer/state-changing tests.
- **Review evidence:** changed files, test result, screenshots and remaining limitations; no next child until accepted.


#### [ ] WG03.2 — Show peer connection observability

- **Status:** Todo · **Depends on:** WG03.1

- **Scope / acceptance:** Show handshake age, endpoint, received/transmitted byte counters, freshness and explicit unknowns. Reuse the existing runtime payload; expose no private/preshared keys or full peer configs.

- **Validation:** focused contract/logic tests for new behavior; inspect desktop/phone states and failure recovery. Use fixtures or explicitly agreed disposable resources for transfer/state-changing tests.
- **Review evidence:** changed files, test result, screenshots and remaining limitations; no next child until accepted.


#### [ ] WG03.3 — Add live transfer rates and recovery

- **Status:** Todo · **Depends on:** WG03.2

- **Scope / acceptance:** Derive rates only from timestamped consecutive samples; handle counter resets, missing/stale data, reconnect and unit changes. Preserve last-known readings with stale labels; do not present totals as rates or fabricate historical charts.

- **Validation:** focused contract/logic tests for new behavior; inspect desktop/phone states and failure recovery. Use fixtures or explicitly agreed disposable resources for transfer/state-changing tests.
- **Review evidence:** changed files, test result, screenshots and remaining limitations; no next child until accepted.


### [ ] K801 — Explain the connection address choice

- **Status:** Todo · **Scope:** S · **Depends on:** previous milestone accepted; otherwise independent within this milestone

- **Start points:** `app/k8s-cluster.tsx`.

- **Acceptance:** Explain reachability of each connection-file address using actual interface/network data where available; selecting/downloading retains the existing endpoint semantics.

- **Validation:** Add or update a focused test/fixture for the verified eligibility, state, parsing or capability rule, including missing/invalid values and rejection/recovery. Verify desktop/phone presentation without executing real infrastructure mutations.

- **Evidence / review:** [current screenshot](screenshots/15-k8s.jpg); attach after screenshots and relevant check results before marking Done.


### [ ] K802 — Prioritize node state above download setup

- **Status:** Todo · **Scope:** S · **Depends on:** K801

- **Start points:** `app/k8s-cluster.tsx`.

- **Acceptance:** Put node state before download setup; reduce empty disconnected-group space; retain complete node inventory and connection-file access.

- **Validation:** Inspect desktop and 375px phone layout, keyboard access and loading/empty/error states relevant to this ticket; run the existing focused suite where it covers changed behavior. Add a small meaningful test for new logic, not for copy-only edits.

- **Evidence / review:** [current screenshot](screenshots/15-k8s.jpg); attach after screenshots and relevant check results before marking Done.


### [ ] CF01 — Separate no resources from healthy resources

- **Status:** Todo · **Scope:** S · **Depends on:** previous milestone accepted; otherwise independent within this milestone

- **Start points:** `app/celld-fleet.tsx`.

- **Acceptance:** Clearly distinguish service reachability, no resources, and workload health. Zero fleets/buckets are not presented as proof of healthy workloads.

- **Validation:** Inspect desktop and 375px phone layout, keyboard access and loading/empty/error states relevant to this ticket; run the existing focused suite where it covers changed behavior. Add a small meaningful test for new logic, not for copy-only edits.

- **Evidence / review:** [current screenshot](screenshots/16-celld.jpg); attach after screenshots and relevant check results before marking Done.


### [ ] CF02 — Make the empty state actionable

- **Status:** Todo · **Scope:** S · **Depends on:** CF01

- **Start points:** `app/celld-fleet.tsx`.

- **Acceptance:** Add Create fleet to the empty-state panel, explain SILO storage briefly and place deleted-fleet recovery as a secondary action; preserve recovery functionality.

- **Validation:** Inspect desktop and 375px phone layout, keyboard access and loading/empty/error states relevant to this ticket; run the existing focused suite where it covers changed behavior. Add a small meaningful test for new logic, not for copy-only edits.

- **Evidence / review:** [current screenshot](screenshots/16-celld.jpg); attach after screenshots and relevant check results before marking Done.


### [ ] SP01 — Fix browser autofill and port input semantics

- **Status:** Todo · **Scope:** M · **Depends on:** previous milestone accepted; otherwise independent within this milestone

- **Start points:** `app/spa.tsx`.

- **Acceptance:** Use meaningful labels, numeric port input and appropriate autocomplete semantics; validate valid port range at trust boundaries and prevent account email autofill. Do not disable password-manager support globally.

- **Validation:** Add or update a focused test/fixture for the verified eligibility, state, parsing or capability rule, including missing/invalid values and rejection/recovery. Verify desktop/phone presentation without executing real infrastructure mutations.

- **Evidence / review:** [current screenshot](screenshots/17-spa.jpg); attach after screenshots and relevant check results before marking Done.

## Milestone 07 remove updates from everywhere

## Milestone 08 — Logs rework epic

Rebuild the logs workflow in small, independently reviewable increments.

**Entry:** previous milestone accepted. **Exit demo:** selected behavior works on desktop and phone; no unrelated feature regressions; each ticket has evidence and its own review checkpoint.


### [ ] LOGS — Rework logs (epic)

- **Status:** Todo · **Scope:** L · **Start points:** app/logs.tsx; existing log services/types/components; backend master/api/logs.go and master/services/logs.go only if required

- **Completion:** all child tickets below accepted; each child is its own increment, not one large implementation.



## Milestone 09 — Support layout

Improve widget usability without silently rearranging saved dashboards.

**Entry:** previous milestone accepted. **Exit demo:** selected behavior works on desktop and phone; no unrelated feature regressions; each ticket has evidence and its own review checkpoint.


### [ ] SU01 — Prevent widgets shrinking below usable sizes

- **Status:** Todo · **Scope:** M · **Depends on:** previous milestone accepted; otherwise independent within this milestone

- **Start points:** `app/support.tsx`.

- **Acceptance:** Enforce usable minimum widget dimensions during resize and restored-layout normalization; preview any fit/recovery action and preserve saved positions with a recoverable migration if required.

- **Validation:** Inspect desktop and 375px phone layout, keyboard access and loading/empty/error states relevant to this ticket; run the existing focused suite where it covers changed behavior. Add a small meaningful test for new logic, not for copy-only edits.

- **Evidence / review:** [current screenshot](screenshots/20-support.jpg); attach after screenshots and relevant check results before marking Done.


### [ ] SU03 — Offer alignment and layout recovery tools

- **Status:** Todo · **Scope:** M · **Depends on:** SU01

- **Start points:** `app/support.tsx`.

- **Acceptance:** Offer alignment/snap and explicit layout recovery in edit mode, with preview/cancel and undo or a saved pre-change layout; retain free placement and existing saved dashboards.

- **Validation:** Inspect desktop and 375px phone layout, keyboard access and loading/empty/error states relevant to this ticket; run the existing focused suite where it covers changed behavior. Add a small meaningful test for new logic, not for copy-only edits.

- **Evidence / review:** [current screenshot](screenshots/20-support.jpg); attach after screenshots and relevant check results before marking Done.


### [ ] SU04 — Make empty widgets concise and useful

- **Status:** Todo · **Scope:** S · **Depends on:** SU01

- **Start points:** `app/support.tsx`.

- **Acceptance:** Use short no-data messages with exact expected stream and optional details; narrow widgets do not render long word-by-word paragraphs.

- **Validation:** Inspect desktop and 375px phone layout, keyboard access and loading/empty/error states relevant to this ticket; run the existing focused suite where it covers changed behavior. Add a small meaningful test for new logic, not for copy-only edits.

- **Evidence / review:** [current screenshot](screenshots/20-support.jpg); attach after screenshots and relevant check results before marking Done.


## Milestone 10 — Proxy management and certificates

Improve certificate lifecycle information, proxy lists and routing clarity.

**Entry:** previous milestone accepted. **Exit demo:** selected behavior works on desktop and phone; no unrelated feature regressions; each ticket has evidence and its own review checkpoint.


### [ ] CE01 — Make expiry actionable

- **Status:** Todo · **Scope:** M · **Depends on:** previous milestone accepted; otherwise independent within this milestone

- **Start points:** `app/certificates.tsx`.

- **Acceptance:** Show days until expiry alongside an unambiguous date; add expiry sorting/due-soon filtering; handle expired, missing and malformed dates without presenting false validity.

- **Validation:** Add or update a focused test/fixture for the verified eligibility, state, parsing or capability rule, including missing/invalid values and rejection/recovery. Verify desktop/phone presentation without executing real infrastructure mutations.

- **Evidence / review:** [current screenshot](screenshots/22-certificates.jpg); attach after screenshots and relevant check results before marking Done.


### [ ] CE02 — Match certificate actions to provider capabilities

- **Status:** Todo · **Scope:** S · **Depends on:** CE01; provider capability inspection

- **Start points:** `app/certificates.tsx`.

- **Acceptance:** Trace provider capabilities; label Renew/Replace according to the actual supported operation, including custom certificates; preserve validation and never renew live certificates as a UI check.

- **Validation:** Add or update a focused test/fixture for the verified eligibility, state, parsing or capability rule, including missing/invalid values and rejection/recovery. Verify desktop/phone presentation without executing real infrastructure mutations.

- **Evidence / review:** [current screenshot](screenshots/22-certificates.jpg); attach after screenshots and relevant check results before marking Done.


### [ ] PX02 — Offer searchable compact proxy rows

- **Status:** Todo · **Scope:** M · **Depends on:** previous milestone accepted; otherwise independent within this milestone

- **Start points:** `app/proxy.tsx`.

- **Acceptance:** Add free-text search and compact aligned proxy rows with domain, destination, TLS and enabled state; preserve grouping, sorting and existing actions.

- **Validation:** Inspect desktop and 375px phone layout, keyboard access and loading/empty/error states relevant to this ticket; run the existing focused suite where it covers changed behavior. Add a small meaningful test for new logic, not for copy-only edits.

- **Evidence / review:** [current screenshot](screenshots/24-proxy.jpg); attach after screenshots and relevant check results before marking Done.



### [ ] US01 — Offer purpose-specific empty-state actions

- **Status:** Todo · **Scope:** S · **Depends on:** previous milestone accepted; otherwise independent within this milestone

- **Start points:** `app/upstreams.tsx`.

- **Acceptance:** Provide clear HTTP and TCP/UDP pool entry points and explain consumers; reuse the existing pool type contract and creation validation.

- **Validation:** Inspect desktop and 375px phone layout, keyboard access and loading/empty/error states relevant to this ticket; run the existing focused suite where it covers changed behavior. Add a small meaningful test for new logic, not for copy-only edits.

- **Evidence / review:** [current screenshot](screenshots/25-upstreams.jpg); attach after screenshots and relevant check results before marking Done.



### [ ] RD02 — Show source-to-destination mapping directly

- **Status:** Todo · **Scope:** S · **Depends on:** previous milestone accepted; otherwise independent within this milestone

- **Start points:** `app/redirection.tsx`.

- **Acceptance:** Display exact source-to-destination mapping, protocol and full inspect/copy affordances; do not silently normalize or rewrite configured destinations.

- **Validation:** Inspect desktop and 375px phone layout, keyboard access and loading/empty/error states relevant to this ticket; run the existing focused suite where it covers changed behavior. Add a small meaningful test for new logic, not for copy-only edits.

- **Evidence / review:** [current screenshot](screenshots/26-redirection.jpg); attach after screenshots and relevant check results before marking Done.


### [ ] TS01 — Align listener and destination fields

- **Status:** Todo · **Scope:** S · **Depends on:** previous milestone accepted; otherwise independent within this milestone

- **Start points:** `app/streams.tsx`.

- **Acceptance:** Align listener, target host/port, protocol, TLS and enabled state with description and copyable values; preserve stream behavior.

- **Validation:** Inspect desktop and 375px phone layout, keyboard access and loading/empty/error states relevant to this ticket; run the existing focused suite where it covers changed behavior. Add a small meaningful test for new logic, not for copy-only edits.

- **Evidence / review:** [current screenshot](screenshots/27-streams.jpg); attach after screenshots and relevant check results before marking Done.


### [ ] TS02 — Use protocol-appropriate security copy

- **Status:** Todo · **Scope:** S · **Depends on:** TS01

- **Start points:** `app/streams.tsx`.

- **Acceptance:** Use neutral TLS-off wording suitable for TCP/UDP; keep security/exposure facts accurate and preserve actual transport configuration.

- **Validation:** Inspect desktop and 375px phone layout, keyboard access and loading/empty/error states relevant to this ticket; run the existing focused suite where it covers changed behavior. Add a small meaningful test for new logic, not for copy-only edits.

- **Evidence / review:** [current screenshot](screenshots/27-streams.jpg); attach after screenshots and relevant check results before marking Done.


## Milestone 11 — full rework Docker
