# Virtual machines

Local implementation dated 2026-10-19. The registry has **56 VM tools**. No production server, hypervisor, disk or deployment was accessed. New secure snapshot prerequisites must be deployed together with the matching API/RPC implementations before VM writes work; older backends fail closed.

## Coverage and disposition

The original selected area contained 80 HTTP method/path pairs across `master/api/virsh.go`, `virsh_xml_templates.go`, and `vm_disk.go`. Two additive read-only snapshot helper endpoints bring the current raw inventory to 82. Helpers do not earn coverage without a registered public tool.

| Measure | Supported / denominator | Coverage |
| --- | --- | --- |
| Original selected raw routes | 56 / 80 | 70.00% |
| Current raw routes, including snapshot helpers | 56 / 82 | 68.29% |
| Original policy-permitted selection, excluding 23 resource-deletion capabilities | 56 / 57 | 98.25% |
| Current non-excluded routes, including internal-only helpers | 56 / 59 | 94.92% |

One original permitted operation remains blocked: SSH key addition cannot save the actual prior guest files/ownership through the available API. It is absent from discovery. The selected area is not claimed to have 100% permitted coverage.

## Registered endpoint mapping

Every tool has explicit `hyperhive/risk` metadata and a matching risk description. Medium, high and very high operations require a supported user option picker for each invocation; no argument can self-approve a call.

| Tool | Method | Path | Risk |
| --- | --- | --- | --- |
| `add_vm_video` | POST | `/virsh/novncvideo/add/{vm_name}` | high |
| `attach_vm_disk` | POST | `/virsh/vm_disk/{vm_name}` | high |
| `backup_vm` | POST | `/virsh/backup/{vm_name}/{nfs_id}` | high |
| `clone_vm` | POST | `/virsh/cloneVM/{vm_name}/{dest_nfs}/{dest_machine_name}` | high |
| `create_vm_xml_template` | POST | `/virsh/xmltemplates/` | medium |
| `disable_vm_automatic_backup` | PATCH | `/virsh/autopak/disable/{id}` | medium |
| `download_vm_backup` | GET | `/virsh/downloadbackup/{backup_id}` | low |
| `edit_vm` | POST | `/virsh/editvm/{vm_name}` | high |
| `export_vm` | GET | `/virsh/export/{vm_name}` | medium |
| `force_shutdown_vm` | POST | `/virsh/forceshutdownvm/{vm_name}` | very high |
| `get_vm` | GET | `/virsh/getvmbyname/{vm_name}` | low |
| `get_vm_ballooning` | GET | `/virsh/ballooning/{vm_name}` | low |
| `get_vm_core_isolation` | GET | `/virsh/coreisolation/{machine_name}` | low |
| `get_vm_cpu_disable_features` | GET | `/virsh/getcpudisablefeatures` | low |
| `get_vm_cpu_pinning` | GET | `/virsh/cpupinning/{vm_name}` | low |
| `get_vm_cpu_topology` | GET | `/virsh/cputopology/{machine_name}` | low |
| `get_vm_cpu_xml` | GET | `/virsh/cpuxml/{vm_name}` | low |
| `get_vm_host_hugepages` | GET | `/virsh/hosthugepages/{machine_name}` | low |
| `get_vm_hugepages` | GET | `/virsh/hugepages/{vm_name}` | low |
| `get_vm_hyperv` | GET | `/virsh/hyperv/{vm_name}` | low |
| `get_vm_irqbalance` | GET | `/virsh/irqbalance/{machine_name}` | low |
| `get_vm_kvm` | GET | `/virsh/kvm/{vm_name}` | low |
| `get_vm_tuned_profiles` | GET | `/virsh/tunedadm/{machine_name}` | low |
| `get_vm_video` | GET | `/virsh/novncvideo/{vm_name}` | low |
| `get_vm_xml` | GET | `/virsh/vmxml/{vm_name}` | low |
| `get_vm_xml_template` | GET | `/virsh/xmltemplates/{id}` | low |
| `grow_vm_disk` | POST | `/vm-disk/{id}/grow` | high |
| `list_vm_attached_disks` | GET | `/virsh/vm_disk/{vm_name}` | low |
| `list_vm_automatic_backups` | GET | `/virsh/autobak` | low |
| `list_vm_backups` | GET | `/virsh/backups` | low |
| `list_vm_disks` | GET | `/vm-disk/list` | low |
| `list_vm_machine_types` | GET | `/virsh/machine_types/{machine_name}` | low |
| `list_vm_xml_templates` | GET | `/virsh/xmltemplates/` | low |
| `list_vms` | GET | `/virsh/getallvms` | low |
| `pause_vm` | POST | `/virsh/pausevm/{vm_name}` | high |
| `replace_vm_host_core_isolation` | PUT | `/virsh/coreisolation/{machine_name}` | very high |
| `replace_vm_host_hugepages` | PUT | `/virsh/hosthugepages/{machine_name}` | very high |
| `restart_vm` | POST | `/virsh/restartvm/{vm_name}` | very high |
| `resume_vm` | POST | `/virsh/resumevm/{vm_name}` | medium |
| `set_vm_ballooning` | POST | `/virsh/ballooning/{vm_name}` | high |
| `set_vm_cpu_pinning` | POST | `/virsh/cpupinning/{vm_name}` | high |
| `set_vm_host_core_isolation` | POST | `/virsh/coreisolation/{machine_name}` | very high |
| `set_vm_host_hugepages` | POST | `/virsh/hosthugepages/{machine_name}` | very high |
| `set_vm_hugepages` | POST | `/virsh/hugepages/{vm_name}` | high |
| `set_vm_hyperv` | POST | `/virsh/hyperv/{vm_name}` | high |
| `set_vm_irqbalance` | POST | `/virsh/irqbalance/{machine_name}` | high |
| `set_vm_kvm` | POST | `/virsh/kvm/{vm_name}` | high |
| `set_vm_machine_type` | POST | `/virsh/machine_type/{vm_name}` | high |
| `set_vm_network` | POST | `/virsh/change_vm_network/{vm_name}` | high |
| `set_vm_tuned_profile` | POST | `/virsh/tunedadm/{machine_name}` | high |
| `set_vm_vnc_password` | POST | `/virsh/change_vnc_password/{vm_name}` | high |
| `shutdown_vm` | POST | `/virsh/shutdownvm/{vm_name}` | high |
| `start_vm` | POST | `/virsh/startvm/{vm_name}` | medium |
| `update_vm_cpu_xml` | POST | `/virsh/updatecpuxml/{vm_name}` | high |
| `update_vm_xml` | POST | `/virsh/updatevmxml/{vm_name}` | very high |
| `update_vm_xml_template` | PUT | `/virsh/xmltemplates/{id}` | high |

## Policy exclusions

These 23 routes are excluded from eligible coverage and have no registered tool. The no-deletion rule also applies to indirect deletion and rollback of business resources. Entire endpoint contracts are excluded when only a subset of argument values would avoid deletion.

| Method/path | Reviewed reason |
| --- | --- |
| DELETE `/virsh/deletevm/{vm_name}` | Deletes VM definition, storage and related database records. |
| DELETE `/virsh/vm_disk/{vm_name}/{vm_disk_id}` | Explicit removal endpoint. |
| DELETE `/virsh/cpupinning/{vm_name}` | Explicit removal endpoint. |
| DELETE `/virsh/coreisolation/{machine_name}` | Explicit removal endpoint. |
| DELETE `/virsh/hosthugepages/{machine_name}` | Explicit removal endpoint. |
| DELETE `/virsh/deleteBackup/{backup_id}` | Deletes backup records/files. |
| DELETE `/virsh/autobak/{id}` | Deletes schedule record. |
| DELETE `/virsh/xmltemplates/{id}` | Deletes XML template record. |
| DELETE `/vm-disk/{id}` | Detaches and deletes disk data/record. |
| POST `/virsh/removeiso/{vm_name}` | Explicit ISO device removal capability. |
| POST `/virsh/novncvideo/remove/{vm_name}` | Explicit graphics/video/audio device removal capability. |
| POST `/virsh/createvm` | Calls AutoStart with caller-controlled false branch that removes the database record; no true-only partial contract exposed. |
| POST `/virsh/migratevm/{vm_name}` | Slave migration uses `MIGRATE_UNDEFINE_SOURCE`, removing source definition. |
| POST `/virsh/moveDisk/{vm_name}/{dest_nfs}` | Calls `DeleteVM` after copying. |
| POST `/virsh/coldMigrate/{vm_name}/{dest_machine_name}` | Undefines source VM. |
| PUT `/virsh/import` | Failure cleanup removes final destination disk/folder, including after a late domain-create failure. |
| POST `/virsh/useBackup/{backup_id}` | Copy-failure rollback removes destination business disk/folder. |
| POST `/virsh/autostart/{vm_name}` | False branch removes autostart database record; no partial branch exposed. |
| POST `/virsh/vmlive/{vm_name}` | False branch removes live VM database record; no partial branch exposed. |
| POST `/virsh/autobak` | Enables future automatic backup retention deletion. |
| PUT `/virsh/autobak/{id}` | Replaces/enables schedule that performs retention deletion. |
| PATCH `/virsh/autopak/enable/{id}` | Enables future retention deletion. |
| POST `/vm-disk/create` | Database-registration failure calls `DeleteVMDisk` rollback. |

Normal replacement of settings is permitted with an actual prior-state record. Forced VM power-off and restart call libvirt Destroy to stop execution; they preserve definition/storage but can lose unsaved guest data, so both are very high risk.

## Blocked and internal-only routes

| Method/path | Status and concrete reason |
| --- | --- |
| POST `/virsh/add_ssh_key/{vm_name}` | Blocked. The guest customization modifies authorized_keys for guest users and `/etc/skel`, with ownership changes. No API returns actual old guest files/ownership; domain XML is insufficient. Temporary script cleanup alone is not the blocker. No generic guest-file execution tool was added. |
| GET `/virsh/snapshot/{vm_name}` | Internal prerequisite only; no standalone MCP tool or coverage credit. Returns secure inactive XML and, for an active domain, secure current XML with name/UUID/host/active identity. |
| GET `/virsh/hostsnapshot/{machine_name}` | Internal prerequisite only; no standalone MCP tool or coverage credit. Returns exact all-kernel grubby output, parsed entries, current cmdline and default kernel identity. |

## Approval and retained prior state

The harness must offer a real per-invocation option picker. Modern clients use argument-bound input requests; older supported HTTP and stdio clients use retained-session form elicitation. Missing, declined, canceled, forged, reused or changed-argument approval cannot mutate. Pending approval returns no structured success or backend calls. Operation, target, risk and meaningful requested values appear in the picker; XML is identified by size/SHA-256 and passwords are hidden. Configuration and credentials are never tool arguments.

After approval, one per-registry lock serializes the fresh snapshot, durable save and single backend request. Any read/shape/save failure blocks that request. VM setting records include actual identity, legacy domain response and complete secure inactive/current XML, plus exact setting getters where available, including the CPU pinning getter. XML replacement must preserve target name and actual UUID; an explicit machine override must match the current VM host. For positive `edit_vm` disk growth, the prior inventory must also contain a positive integral disk size and a path matching the primary file-backed disk in secure inactive XML; disk inspection failures block growth. CPU-only edits do not require a disk. Host boot mutations retain every kernel's exact previous arguments and current/default identities plus the relevant old setting response. Template, disk and schedule changes retain the actual corresponding records; disk attachment/grow also retain affected VM state when applicable.

`MCP_VM_BACKUP_DIR` configures the operator-owned absolute private directory. The default is the OS user cache under `hyperhive/mcp/vm-backups`. Directories are private (0700), records/artifacts 0600, recovery files and directory entries are synced before the backend request, transfer artifacts are synced before success, existing public/symlink backup directories fail closed, and records are retained without automatic cleanup. Records may contain passwords and secure XML; they are never logged. Successful mutations and post-save backend failures expose the retained `before.json` path. No automatic rollback is attempted. These records capture configuration/metadata; they are not whole guest-disk backups and cannot reverse disk growth or recover lost guest RAM. External non-MCP changes are not serialized by this lock.

Host setters keep existing backend behavior: they update boot arguments for ALL kernels and may require a reboot; MCP does not reboot. Existing guest inactive/live/off-state restrictions remain enforced by the backend. The new VM snapshot fails if secure inactive XML is unavailable, even if an older getter could fall back to current XML.

## Transfers and asynchronous work

`export_vm` and `download_vm_backup` genuinely stream the full binary response to a new private file on the **MCP server filesystem**. Returned paths are server-side artifacts; clients do not automatically receive or download their bytes. Results include the exact byte count, SHA-256 and prior-state-record path. The stream has a six-hour maximum preserving any earlier caller deadline/cancellation, verifies the content type and declared length when present, and publishes success only after file persistence. It does not buffer/base64 a disk or silently impose the 16 MiB JSON response cap. Failed/canceled partial local transport artifacts are removed; the prior-state record remains. No backend resource is deleted for transfer cleanup.

`backup_vm` and `clone_vm` accurately return `accepted:true`, `background:true`, `completion_confirmed:false`. The backend starts detached work and supplies no job ID; later failure is possible. A running source may require guest-agent filesystem freeze/thaw. MCP does not claim completion from the initial HTTP response. Source/target names replace the existing clone debug dump that previously included the VNC password.

## Local verification

- SDK discovery/request/response contracts over authenticated HTTP and the actual stdio executable for all registered tools; ordinary write contracts additionally exercise both modern and legacy protocol generations.
- Negative shapes/unknown/null/type inputs, explicit false/zero semantics, safe API failures, no retries, precise large JSON integers, exact decoded prior XML/settings before mutation, retained records after backend failure, missing/declined/canceled/forged/reused/changed approvals and backup failures.
- Real binary artifacts larger than 16 MiB, exact digest/length/privacy checks, transfer failure/partial cleanup and cancellation.
- Injected slave readers verify exact secure XML flags and fail-closed identity/state/cancellation paths. Fixed-command host tests validate every boot entry and assert no writes or real host commands.
- Isolated subprocess master fixture exercises real API authentication, both snapshot HTTP/service/gRPC bridges, no-store behavior, and an actual MCP XML-write chain with secure prior XML persisted before the disposable RPC mutation. The registry dies with the child; loopback bind failure is an explicit test failure.

No real libvirt guest, host boot configuration, production NFS or deployment was tested. Coordinator full module/race/vet and affected backend acceptance passed on 2026-10-19; see the run checkpoint for final command evidence.

Additional review regressions verify exact full-int64 target IDs through both
transports (including approval and retained schedule identity), rejection of raw
fractional/overflow IDs, and fail-closed incomplete primary-disk recovery. Shared
registration decodes original JSON after SDK schema validation so SDK float
normalization cannot select another target.
