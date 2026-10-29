# Managed VM disk transfers

The master streams images between the browser and storage agents, or between two registered NFS storage agents. The master does not open share paths locally. Existing `readyNFSConnection`, managed-disk discovery/growth/deletion, secure `GetVMSnapshot`, and `UpdateVMXml` paths are reused. The older whole-VM `MoveDisk`/`copyFile` flow recreates a VM under a different name and requires master-local paths, so it is unsuitable for these managed disks.

## API and compatibility

All routes are behind the existing authentication middleware:

| Route | Request | Result |
| --- | --- | --- |
| `GET /vm-disk/{id}/download` | Existing session/header or browser download token | Binary attachment with exact `Content-Length`; an interrupted stream aborts the HTTP response. |
| `PUT /vm-disk/import?name=NAME&nfs_id=ID&length=BYTES` | Raw binary body; declared positive length must match body | Verified managed disk. Browser XHR supports upload progress and cancellation. |
| `POST /vm-disk/{id}/move` | `{"nfs_id": DESTINATION}` | Durable operation; the disk ID and stopped-VM attachment are preserved. |
| `GET /vm-disk/operations` | None | Unfinished operations, phases and errors. |
| `POST /vm-disk/operations/{operation}/reconcile` | `{"action":"finish"}` or `{"action":"abort"}` | Explicit retry or staging discard; terminal retries are harmless. |

Agents need the new `UploadVMDisk` and `TransferVMDisk` RPCs and framed `DownloadVMDisk` headers. Unsupported agents fail without falling back to unframed downloads or unsafe copying. Upgrade master and agents before exposing transfer controls. Existing two-hour HTTP middleware still bounds a request; reverse proxies also need upload size and timeout settings suitable for the actual disk sizes.

Capacity is advisory in the UI. The destination host checks fresh filesystem available bytes against file length before staging. Virtual capacity comes from image validation, not the uploaded file length. Zero chunks become holes where supported; nonzero chunks preserve their exact bytes. An actual ENOSPC or write/sync error never registers a partial image.

## Image policy and ownership

Raw images and standalone qcow2 v2/v3 images are supported. Filename extensions do not select the imported format. Known unsupported container signatures, malformed qcow2, backing files, encryption, external data files and unsupported qcow2 features are rejected. Raw has no format signature. Qcow2 headers are screened before QEMU opens the image with an explicit format and local path, then `qemu-img check` verifies consistency. No image-provided backing chain is followed.

Uploads exclusively create `.vm-transfer-UUID` on the destination filesystem and retain an operation receipt. Publication uses `RENAME_NOREPLACE`; filesystems lacking it use exclusive directory creation and hard links. Neither path replaces an existing destination. File data, receipts and directories are synced before registration. A crash between fallback directory creation and receipt linking requires manual review because ownership cannot be proven; neither copy is automatically removed.

`vm_disk_operations` reserves names, disk IDs and attached VM disk configuration changes. Active reservations have no timeout or lease expiry. Move/attach/detach/grow/delete and VM deletion coordinate through them; VM start remains unguarded. The operator must keep an attached VM stopped throughout transfer and avoid external disk/XML changes.

## Reconciliation procedure

First inspect the operation and confirm the old worker has ended. The agent rejects staging discard while its upload worker remains active. Do not clear reservations, staging or receipts just because time has elapsed.

| Recorded phase | Safe next action |
| --- | --- |
| `reserved`, `uploading`, `copying` | Try **finish** if publication may have succeeded. It requires a matching durable expected digest and owned destination receipt. If no publication exists and the worker has ended, **abort** removes only that operation's staging and releases its reservation. |
| `published`, `definition-pending`, `definition-verified` | **Finish** rehashes/revalidates the owned destination, reads current secure inactive VM XML and checks for precisely the old or new source. It retries only the necessary definition update, verifies it and conditionally commits the same disk ID. Both copies remain if anything differs or is uncertain. |
| `committed`, `cleanup-required` | **Finish** checks committed metadata, destination content and persistent VM source before retrying source cleanup. Keep a running attached VM stopped before retrying. |
| `complete`, `aborted` | Reconciliation returns the existing terminal result without repeating work. |

Source deletion requires the copied digest, length and exact source file identity. A durable `.vm-source-UUID.json` journal and `.done` marker prevent delayed cleanup retries from deleting a later disk that reuses the same name and content. Keep these small tombstones. Unknown files are preserved and produce cleanup-required status. Publication receipts belong to the managed disk and are handled by the existing delete helper, including after growth.

For a missing receipt, missing expected digest, changed share target, changed source/destination, ambiguous disk device or unexplained VM definition: preserve both copies and the operation. Compare current catalog, receipt, content hashes, source journal and secure persistent VM definition. Do not guess which copy is authoritative. Resolve the external discrepancy before retrying **finish**. If manual removal is necessary, verify ownership and worker termination first and retain an audit copy of the operation. Never expose secure XML/credentials in logs or knowledge pages.

An uncertain pre-existing mutation (`create`, `grow`, `attach`, `detach`, `delete`, `delete-vm`) remains reserved. Review its agent outcome and catalog/VM definition directly; reconcile those with existing supported operations before conditionally marking that exact operation terminal. Do not release it while an RPC may still run. Transfer reconciliation deliberately does not guess the result of an unrelated mutation.

## Verification and rollout boundary

Local checks cover real raw/qcow2 files, streamed RPCs, sparse copying, hostile metadata, ownership, short uploads, capacity rejection, cancellation, publication recovery, source cleanup retries/name reuse, conditional DB commits, HTTP framing/auth and UI/XHR behavior. Master service fixtures exercise lost publication/definition acknowledgements, DB failures and cleanup retry using disposable local fake agents.

These checks do not prove actual NFS durability, mount identity across failures, libvirt behavior or browser/proxy behavior in deployment. Validate those with authorized disposable shares and VMs before production rollout. The existing frontend `dashboard.tsx` `strokeWidth` type error remains outside this change.

For rollback, disable transfer actions and reconcile active operations before reverting binaries. Preserve committed destinations, source copies, operation records, journals and VM definitions. Reverting code is never a reason to delete either copy.
