# Plan: Finish managed VM disk transfers

**Date:** 2026-10-29  
**Complexity:** High  
**Scope:** HyperHive backend and HyperHiveFront managed-disk screen.

## Outcome and accepted decisions

Complete browser import and moves between NFS shares, and finish validating the preliminary download. An imported raw or standalone qcow2 image becomes a managed disk. A move preserves its ID, content and attachment; if attached, the VM must be stopped when the operation starts. The operator is responsible for keeping it stopped throughout: do not add a VM-start guard. Capacity is shown for managed-disk workflows. Browser upload is the initial supported upload platform.

This plan supersedes the detached-only assumption and unanswered decisions in `vm-disk-storage-features-plan.md`. It does not authorize live operations or deployment.

## Current state

- Working-tree changes add a download RPC, endpoint and frontend action; they are preliminary, not end-to-end verified. Import and NFS-to-NFS move are absent.
- The frontend already has create-time NFS capacity and attach/detach controls; the backend attachment APIs predate these changes. Existing tests passed locally; no live VM or NFS transfer is verified.
- NFS shares are resolved through `readyNFSConnection` and storage host agents. Do not assume the master sees either filesystem or that two shares share a host.
- A stopped VM's persistent libvirt definition still refers to the original disk path. An attached move must update that definition and coordinate it with disk metadata and recovery.

## Sprint 1: Complete download and define shared transfer contract

**Goal:** A bounded-memory download works from an agent, reports failure accurately, and transfer primitives have defined ownership and recovery semantics.

1. **Audit and repair download** — `api/proto/vm_disk.proto`, `slave/vm_disk/setupgrpc.go`, `master/services/vm_disk.go`, `master/api/vm_disk.go`, `HyperHiveFront/HyperHive/app/vms-disks.tsx`, `services/hyperhive.ts`. Verify share/path resolution, regular-file handling, supported image policy, auth, browser/native behavior and cancellation. Send a known file length before response body so a short stream cannot look complete; ensure server-side failures before streaming are visible. Treat an attached VM as eligible only when its state is confirmed shut off. Test missing/unreachable VM, running VM, unauthorized access, interrupted stream, large files, unsafe names and auth expiry. **Dependency:** existing download changes.
2. **Define durable operation records and staging ownership** — `master/db/vm_disk.go`, `master/services/vm_disk.go`, `slave/vm_disk/`. Define unique operation ID, disk/source/destination, owned staging paths, phase and errors. Use conditional DB transitions for move and cleanup; no timed expiry may allow an old worker to delete or overwrite a new operation's files. Coordinate overlapping disk mutations where necessary to preserve source data; this is not a VM-start guard. On restart leave uncertain work visibly recoverable rather than blindly clearing or deleting it. Test conflicting operations, cancellation and restart. **Dependency:** none.
3. **Destination readiness and capacity** — `master/services/vm_disk.go`, `slave/vm_disk/`. Resolve registered share and validate its mount; obtain fresh filesystem free bytes on the writing host. Treat UI capacity as advisory and distinguish virtual size from file bytes. Handle insufficient space and ENOSPC without publishing a partial disk. Test inaccessible and full shares and unknown capacity. **Dependency:** task 2 for cleanup ownership.

**Demo:** Download detached and stopped-VM-attached disks, compare file hashes, and interrupt a download without presenting a complete file.

## Sprint 2: Import a managed disk

**Goal:** Upload a raw or standalone qcow2 image into a selected NFS share and use it as a normal managed disk.

1. **Agent streaming and staging** — `api/proto/vm_disk.proto`, generated bindings, `slave/vm_disk/`, `master/vm_disk/`. Add bounded streaming to the destination agent; validate name and declared length, create an exclusive operation-owned temporary file on the destination filesystem, check exact byte count, cancellation and write/flush errors. Never overwrite or delete unrelated paths. Test traversal, collisions, short uploads, cancellation, ENOSPC and cleanup. **Dependency:** sprint 1 tasks 2–3.
2. **Safe image validation and publication** — `slave/vm_disk/`. Determine actual format independently of filename. Accept only raw and standalone qcow2; reject unsupported formats, malformed images, backing chains and external data files without allowing image metadata to open arbitrary local or remote resources. Derive virtual size from validated image; use no-replace publication on the destination filesystem, sync data and directory, and record ownership. Test actual raw/qcow2 images and hostile metadata. **Dependency:** task 1.
3. **Authenticated import and registration** — `master/api/vm_disk.go`, `master/services/vm_disk.go`, `master/db/vm_disk.go`. Stream a declared-length body to the agent, validate inputs and duplicates, register only the verified published image, and record/reconcile the crash window between publication and database commit. On failure clean up only resources owned by the operation. Test duplicate names, disconnect, DB failure and restart at each phase. **Dependency:** tasks 1–2.
4. **Import UI** — `HyperHiveFront/HyperHive/app/vms-disks.tsx`, `services/hyperhive.ts`, `tests/vm-disks.test.js`. Reuse existing browser upload progress/cancellation patterns; add name, file, destination and fresh used/free/total or unavailable capacity, with errors and a browser-only message. Refresh disks and capacity after completion. Test selection, progress, cancel and errors. **Dependency:** task 3.

**Demo:** Import raw and qcow2 images, verify format and size, and attach each to a disposable VM. Interrupt an upload and verify no unrelated file or managed record is removed.

## Sprint 3: Move a disk between NFS shares

**Goal:** Move a disk to another share while preserving its ID and any stopped-VM attachment, with recoverable partial outcomes.

1. **Cross-host copy and validation** — `master/services/vm_disk.go`, `api/proto/vm_disk.proto`, `master/vm_disk/`, `slave/vm_disk/`. Resolve both registered shares; reject same destination and conflicts, and confirm an attached VM is shut off. Stream between agents with bounded memory when needed, stage exclusively on destination, preserve sparseness where feasible, compare source and destination content and validate format, then sync and publish without replacement. Verify source/destination failure, sparse images and interruption. **Dependency:** sprint 1 tasks 2–3 and sprint 2 staging primitives.
2. **Attached VM definition and DB commit** — `master/services/vm_disk.go`, `master/db/vm_disk.go`, `master/virsh/`, `slave/virsh/`, applicable proto. For an attached disk, inspect the stopped VM's persistent configuration, verify it refers to the expected source, and update precisely that device to the destination without disturbing other devices. Verify the resulting definition. Conditionally update the same disk ID, NFS ID and paths; record every transition durably, including old and new paths and VM definition state. Do not delete the source until destination content, persistent VM configuration and DB metadata are verified. If any step fails or its outcome is uncertain, preserve both copies and expose reconciliation status. Test DB, RPC and restart failures before/after each transition. **Dependency:** task 1.
3. **Cleanup and recovery** — service, DB and agent paths above. Delete only the verified operation-owned source after successful commit, with explicit cleanup-required status if deletion fails. Provide an idempotent operator reconciliation procedure for each recorded phase, including uncertain libvirt outcome; retries must not overwrite or delete unrelated data. Test repeated retries and crash injection at every phase. **Dependency:** task 2.
4. **Move UI** — `HyperHiveFront/HyperHive/app/vms-disks.tsx`, `services/hyperhive.ts`, `tests/vm-disks.test.js`. Select a different destination; show source and destination capacity, stopped-VM requirement and progress/status. Report cleanup-required and failures distinctly and refresh disks and shares. Test same-share selection, unknown capacity and failures. **Dependency:** tasks 1–3.

**Demo:** Move detached and stopped-VM-attached disks between shares on different agents; verify the unchanged ID, destination hash, VM persistent path and successful attachment/start. Inject a failure and reconcile it without losing either copy.

## Verification, release and rollback

- Run focused agent, master service/API/database and frontend tests, generated-proto checks and frontend type check. Exercise authentication, size limits, proxy timeouts, cancellation and browser upload/download with disposable images. The previous full type check reported an existing `dashboard.tsx` `strokeWidth` error; record whether it remains.
- Validate actual NFS mount identity, permissions, durability and cross-host behavior in an authorized disposable environment. No live verification is implied by unit tests. Check behavior when older agents do not support the new RPCs.
- Deploy compatible backend and agent versions before enabling UI actions. Confirm active operation state before rollout or rollback.
- To roll back, disable transfer actions and reconcile active operations before reverting components. Preserve committed destinations, source copies, operation records and VM definitions; never automatically delete either copy solely for rollback.

## Known limitations

The operator must keep the VM stopped for the full transfer. A start or external disk modification during a copy can invalidate its result; this plan deliberately does not add VM-start guards. UI capacity is a point-in-time estimate, not a reservation. Actual NFS and libvirt crash behavior must be verified before production use.
