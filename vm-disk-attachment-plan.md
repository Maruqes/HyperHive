# Plan: Managed disk attachment controls

**Date:** 2026-10-29

## Overview

Allow operators to detach a managed disk and attach it to another VM from the disk inventory. Reuse the existing VM attachment endpoints; this changes the VM association, not the disk's NFS location.

## Sprint 1: Inventory controls

1. In `HyperHiveFront/HyperHive/app/vms-disks.tsx`, add an Attach action for unattached disks with a freshly loaded VM selector and a Detach action for attached disks with confirmation. Reuse `getAllVMs`, `attachVmDisk`, and `detachVmDisk` from `services/vms-client.ts`. Disable repeated submissions, report errors, and refresh the disk inventory after either operation.
2. Verify attach selection, detach confirmation, failures and refresh with `tests/vm-disks.test.js`.

**Demo:** Detach a disk from VM A, then select VM B and attach it. Confirm the same disk ID and NFS location remain.

## Risks and rollout

The existing backend validates file readability on the destination host and prevents concurrent attachment reservations. Detaching a mounted guest filesystem can disrupt the guest; unmount it first. A partial backend failure can leave attachment metadata inconsistent, so report the error and refresh. Test against disposable VMs before rollout; deploy the frontend with a compatible backend. Roll back by removing the new controls; existing VM-screen actions remain available.
