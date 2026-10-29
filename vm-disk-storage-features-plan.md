# Plan: Managed VM disk transfer and NFS capacity

**Date:** 2026-10-29  
**Complexity:** High

## Outcome and scope

Add move between NFS shares, file download and file import for managed VM disks, and show destination NFS usage when creating, importing or moving a disk. Preserve the disk's identity across a move and register imported disks so they can be attached and managed normally. This plan covers both `/home/marques/projects/HyperHive` and `/home/marques/projects/HyperHiveFront`.

Proposed initial policy: only detached managed disks can be moved or downloaded. This prevents copying a changing image and avoids changing a VM's live libvirt configuration. Support raw and standalone qcow2 files; reject backing chains and unsupported formats. VM import/export is a reference for the streaming interface, not a complete implementation to copy. Disk upload follows the existing browser-only VM upload experience initially. These are planning assumptions, not accepted product decisions.

## Existing implementation

- Managed disks have a database ID, NFS ID, path, format, size and attachment reservation. `master/api/vm_disk.go`, `master/services/vm_disk.go`, `master/db/vm_disk.go`, `master/vm_disk/vm_disk.go`, `api/proto/vm_disk.proto` and `slave/vm_disk/` implement list/create/grow/delete. Names are unique; files live under `vm_disk_<name>`.
- `master/api/virsh.go` implements streamed VM import and export; the VM move in `master/services/virsh.go` moves a primary VM disk and changes VM definitions. It does not implement managed disk relocation. The VM import writes a temporary file and uses a declared length; verify mount visibility before reusing its file access approach.
- `HyperHive/app/vms-disks.tsx` and `HyperHive/services/hyperhive.ts` implement the disk UI and client. `HyperHive/components/modals/ImportVmModal.tsx` and `HyperHive/services/vms-client.ts` provide upload progress patterns. `HyperHive/components/modals/CreateVmModal.tsx` already shows free/total NFS capacity from `listMounts()` and `HyperHive/types/mount.ts`.
- Existing SilverBullet pages for managed disks, attachment reservation, and VM import/export are source-reviewed at older revisions; inspect current source during implementation. No live infrastructure was inspected for this plan.

## Sprint 1: Capacity and safe operation foundation

**Goal:** Show current destination capacity and define safe concurrency for all disk operations.

1. In `HyperHive/app/vms-disks.tsx`, display selected share used, free and total capacity during creation, using `listMounts` and the existing `Mount.Status` fields. Refresh on opening the dialog and distinguish unavailable/unmounted or stale data from zero capacity. Reuse this presentation for later import/move screens. **Validation:** Extend `HyperHive/tests/vm-disks.test.js` for selection, unknown capacity and refresh.
2. In `master/services/vm_disk.go` and `master/db/vm_disk.go`, design and implement an atomic per-disk operation reservation shared by attach, grow, delete, move and download. Guard against concurrent attach or mutation across the entire transfer, including multiple master processes if supported. Define recovery for a master restart and prevent a stale lease from permitting unsafe simultaneous operations. **Dependency:** agree on the persisted operation/recovery design before implementing transfer. **Validation:** concurrent reservation/attach and restart recovery tests.
3. Check server-side share readiness through existing `readyNFSConnection`, and fetch fresh capacity from the actual destination filesystem before writing. Treat UI capacity as advisory; distinguish virtual disk size from actual bytes consumed and do not claim sparse qcow2 virtual size is allocated space. Fail safely on insufficient space and handle ENOSPC during copy. **Validation:** unavailable share, unknown capacity, insufficient space and mid-write ENOSPC.

**Demo:** Open disk creation and switch NFS shares; see accurate capacity or an explicit unknown state. Conflicting disk operations are rejected safely.

## Sprint 2: Download a managed disk

**Goal:** Download the image belonging to an existing disk without loading it into server or browser memory.

1. Add an authenticated streaming download endpoint in `master/api/vm_disk.go` and service logic in `master/services/vm_disk.go`. Resolve the disk by ID and registered share; do not accept client-supplied filesystem paths. Acquire the operation reservation and recheck detached status. Determine whether the master can directly read that share; if not, add a bounded streaming RPC in `api/proto/vm_disk.proto`, `master/vm_disk/`, and `slave/vm_disk/`, regenerate bindings, and propagate cancellation. Keep the reservation through stream completion. **Validation:** unauthorized access, missing disk, attached disk, cancellation, large-file streaming and malicious path/filename tests.
2. Add a download action to `HyperHive/app/vms-disks.tsx` and its client in `HyperHive/services/hyperhive.ts`, following the existing authenticated VM export behavior. Indicate the detached requirement and surface failures; preserve the correct raw/qcow2 filename. Check browser navigation authentication and native behavior explicitly. **Validation:** browser download and auth-expiry handling; verify no full-file buffering.

**Demo:** Download a detached disk and inspect its format and image integrity; attempts on attached disks are rejected.

## Sprint 3: Import a disk file

**Goal:** Upload a standalone disk to a chosen NFS and make it a normal managed disk.

1. Add a streamed, authenticated import endpoint and service/RPC support as required by the actual mount topology. Accept disk name, destination NFS and binary body; validate length, name, share, collisions and limits at the trust boundary. Write to a uniquely owned temporary file with exclusive creation, observe cancellation, verify exact byte count, flush and publish atomically on the destination filesystem. Never remove pre-existing files during cleanup. **Validation:** duplicate names, traversal, truncated/cancelled upload, ENOSPC and cleanup tests.
2. Inspect the staged file with `qemu-img` using explicit safe format handling. Accept only raw and standalone qcow2; reject unsupported formats and external backing/data files before registration, and derive actual format and virtual size from the image. Do not trust the filename. Register only after successful validation and publish; clean up only files owned by the operation if DB registration fails. Define recovery for crashes between publish and registration. **Validation:** raw/qcow2 round-trip, malformed image, backing chain and database failure.
3. Add an import dialog to `HyperHive/app/vms-disks.tsx` and upload client in `HyperHive/services/hyperhive.ts`, reusing the VM upload progress/cancellation approach. Show selected destination capacity, filename, progress, errors and browser-only limitation; refresh disk list and capacity after success. **Validation:** frontend tests plus a real browser upload with interruption.

**Demo:** Import a downloaded raw or qcow2 image, verify it appears with correct size and format, then attach it to a test VM.

## Sprint 4: Move between NFS shares

**Goal:** Relocate a detached managed disk without losing it or changing its ID.

1. Add a move endpoint and service orchestration. Reserve the disk, re-read its state, reject attachment and same destination, validate both shares and destination conflicts, then copy to a uniquely owned temporary destination. Select a transfer implementation only after confirming whether both mounts are visible from one host; otherwise stream between hosts with bounded memory. Preserve sparse files where supported, validate copied content and standalone format, sync the destination, and atomically publish it. **Validation:** same share, inaccessible source/destination, sparse images, copy errors and cancellation.
2. Persist the destination NFS ID and paths with a conditional DB update while holding the reservation. Only after destination verification and committed metadata may source deletion occur. Record recoverable phases durably so crashes or DB/delete failures leave at least one known good copy and can be reconciled without overwriting or deleting unrelated files. Return a distinct cleanup-required status if source removal fails. Release reservation only when safe. **Validation:** inject failure and restart at every phase, DB update failure, source deletion failure and simultaneous attachment/other operations.
3. Add move action/dialog to `HyperHive/app/vms-disks.tsx` and client in `HyperHive/services/hyperhive.ts`. Show source and selected destination capacity, explain the detached requirement, prevent same-share selection, show operation status and refresh disk and mounts. For long-running transfers, define durable status/progress and retry behavior based on the chosen backend execution model; do not report success merely because the request was accepted. **Validation:** frontend selection/error/status tests and end-to-end move with source and destination on different hosts.

**Demo:** Move a detached image, confirm unchanged ID and content on the destination, attach it, and confirm the source is removed only after commit. Demonstrate safe recovery from an interrupted move.

## Cross-cutting verification and rollout

- Extend `slave/vm_disk/vm_disk_test.go`, relevant master service/API/DB tests and `HyperHive/tests/vm-disks.test.js`; run applicable Go tests, frontend tests and type checks. Use disposable files and test shares, not production disks.
- Verify authentication, access checks, request/response timeouts, reverse proxy upload/download limits, content length, network interruptions and browser/native behavior. Confirm format validation cannot access external paths or remote backing files.
- Validate mounted share identity, cross-host visibility, filesystem durability/rename behavior, source and destination permissions, and cleanup on actual NFS before rollout. Capacity is a point-in-time estimate and concurrent writes can exhaust storage.
- Deploy compatible backend/protobuf changes to participating hosts before enabling corresponding frontend actions. Confirm old/new agent compatibility and handle unavailable agents explicitly.
- Roll back by disabling new UI/actions and deploying compatible prior versions only after active operations finish or are reconciled. Preserve committed destination files and DB records; never automatically delete either copy solely to roll back code. Provide an operator reconciliation procedure for unfinished transfers.

## Decisions to confirm

1. Is requiring disks to be detached for move and download acceptable, or must attached disks be supported? Attached support needs a separate consistency and libvirt migration design.
2. Should import support only raw and standalone qcow2, and is browser-only upload acceptable initially?
3. Should capacity also be shown in VM creation/import/move dialogs, or is the requested scope limited to managed disk workflows? VM creation already shows free/total space.
