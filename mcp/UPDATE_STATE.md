# Update MCP state

## Refresh procedure

Use this file when asked to refresh MCP compatibility:

> Review the current HyperHive API and registered MCP tools. Recalculate endpoint
> coverage, replace the report below, update the README coverage summary, and
> report the percentage and changes. Follow AGENTS.md and keep everything in English.

1. Inspect `master/api/api.go` and the setup functions it actually registers,
   including `master/api/npmapi/`. List unique HTTP method/path pairs with full
   nested prefixes. Expand route constants and loops; preserve registered aliases.
2. Review MCP registration in `internal/tools`, the shared client, API handlers,
   request/response types and relevant tests. Map tools to endpoints. Review any
   contract changes before retaining implemented status.
3. Mark endpoints implemented, missing, partial, stale, disabled or excluded.
   Only implemented endpoints with enabled tools and passing contract tests count.
   Keep exclusions in the stated denominator; explain scope changes explicitly.
   Apply the repository deletion policy, subject to explicit user overrides.
   Preserve the prior narrow exception for `delete_celld_fleet`; the current
   DNS/Notes/SPA/ISO/VM batch grants no new deletion exception. Classify HTTP
   DELETE endpoints and other operations with deletion behavior as excluded,
   unless explicitly authorized. Keep them in raw totals with zero credit and
   out of eligible tool queues. "All tools" or "100%" never grants an exception.
4. Calculate `implemented / total * 100` for every leaf area and the overall API.
   Sum counts for NGINX and other parent areas. Use N/A for zero endpoints.
5. Run `go test ./...` from `mcp/`; run additional checks only when relevant.
   Record failures and unverified behavior honestly. Do not contact production.
6. Replace the report and endpoint inventory below with the date, source revision
   and content digest, totals, limitations and validation. Update the README's
   date, percentages, counts and available-tool summary in the same change.
7. Return the new overall percentage, area changes, missing capabilities and checks.
   Keep AGENTS.md general. Do not add tools or change runtime behavior during a refresh.

This is an agent workflow and source-reviewed report, not an executable command
or background updater. Reinspect the working tree on each refresh; the date and
Git revision alone do not prove freshness. There is no committed coverage CLI yet.

## Latest report

Checked: **2026-10-20**, from current local source and SDK HTTP/stdio fixtures. The seven-tool VM increment is locally verified; earlier NPM validation remains historical evidence. No production deployment check.

- Source revision: `e8813e4be4fb416190f91f7922b42e61d660b099` plus the existing NPM working changes and VM MCP increment.
- Source content SHA-256: `67a37753f30fa41d49c1159d4fbcd5513d6c903ac3f48fa46491db0f643508c6`.
- Digest input: sorted repository-relative paths; non-test Go/protobuf sources under master/slave/api, all MCP Go sources, those modules' go.mod/go.sum, and the four pinned master/npm/upstream_templates/*.conf inputs. Hash path + NUL + file bytes + NUL. The task's manifest is `.planning/2026-10-20-mcp-vm-creation-flags-schedules-ssh/mcp-source-digest.json`.
- Inventory: **322** unique explicit method/path pairs. Existing inventory 316 is unchanged except the six authenticated /upstream registrations in master/api/npmapi/upstreams.go, mounted in master/api/api.go. /api remains an alias; /mcp transport lifecycle and the separate pprof listener remain outside capability coverage. This is a task-local source review, not a shipped coverage generator.
- Status: **117 implemented, 157 missing, 48 excluded**. The VM increment adds seven supported endpoints: six specifically authorized prior exclusions and blocked SSH injection. Helper calls earn no credit. No partial/stale/disabled endpoint is counted as implemented.
- Scope: the seven named VM capabilities in [the VM run](runs/2026-10-20-vm-increment-01.md), plus previously implemented list/get/capabilities/create/update upstream tools and optional proxy/location/stream bindings. No upstream deletion tool or alias. Confirmed creates complete through journal reconciliation instead of deleting newly created resources as rollback; failed/unknown creates are not retried.
- Validation: this VM increment passed full MCP normal/race/vet checks (622 tests), master API/services/virsh normal tests (58 tests) and vet, plus focused normal/race checks for additional live/template/true-flag variants. SDK discovery expects 117 tools. Authenticated HTTP and actual executable stdio fixtures exercise modern/legacy approval, exact wire contracts, prior-state persistence, safe failures, no retries and fail-closed SSH disk export/manifest checks. Previous NPM handler/exact-image validation remains documented in its upstream checkpoint; it was not rerun as part of this VM increment. See [the VM run](runs/2026-10-20-vm-increment-01.md) for current checks and limits.
- Limits: these local checks do not establish production deployment, real ACME issuance or distributed atomicity. Existing DNS/Notes/SPA/ISO/VM contracts and policy exclusions retain their reviewed boundaries in their feature documents. Prior Celld deletion authorization remains limited to that tool. The 2026-10-20 user request additionally authorizes only VM creation, autostart/live flag changes, schedule create/update/enable and SSH injection; all other exclusions remain.

| Scope | Implemented / total | Missing | Coverage |
| --- | ---: | ---: | ---: |
| **HyperHive API** | 117 / 322 | 157 | 36.34% |
| NGINX subtotal | 33 / 43 | 0 | 76.74% |
| VM subtotal, including templates/disks/helpers | 63 / 82 | 2 | 76.83% |

Subtotals are subsets of the API. Leaf areas partition the total. Missing omits policy exclusions; both new internal helper routes remain missing for direct tool coverage. Eligible-only percentages are meaningful only for reviewed selected scopes, not the entire API's still-unclassified browser/stream routes.

| Area | Implemented / total | Missing | Coverage | Route source |
| --- | ---: | ---: | ---: | --- |
| Guest access and SPA | 2 / 12 | 8 | 16.67% | [GUESTS.go](../master/api/GUESTS.go) |
| Core, login and notifications | 0 / 12 | 11 | 0.00% | [api.go](../master/api/api.go) |
| BTRFS | 0 / 22 | 19 | 0.00% | [btrfs.go](../master/api/btrfs.go) |
| Celld | 9 / 17 | 7 | 52.94% | [celld.go](../master/api/celld.go) |
| DNS aliases | 3 / 4 | 0 | 75.00% | [dnsmasq.go](../master/api/dnsmasq.go) |
| Docker | 0 / 26 | 21 | 0.00% | [docker.go](../master/api/docker.go) |
| Host maintenance | 0 / 4 | 4 | 0.00% | [extra.go](../master/api/extra.go) |
| GoAccess | 0 / 3 | 3 | 0.00% | [goaccess.go](../master/api/goaccess.go) |
| Host information | 0 / 15 | 15 | 0.00% | [info.go](../master/api/info.go) |
| ISOs | 2 / 3 | 0 | 66.67% | [isos.go](../master/api/isos.go) |
| Kubernetes | 0 / 3 | 3 | 0.00% | [k8s.go](../master/api/k8s.go) |
| Logs | 0 / 1 | 1 | 0.00% | [logs.go](../master/api/logs.go) |
| NFS | 0 / 5 | 4 | 0.00% | [nfs.go](../master/api/nfs.go) |
| Notes | 5 / 6 | 0 | 83.33% | [notes.go](../master/api/notes.go) |
| Notification history | 0 / 1 | 1 | 0.00% | [nots.go](../master/api/nots.go) |
| noVNC | 0 / 3 | 3 | 0.00% | [novnc.go](../master/api/novnc.go) |
| NGINX / 404 hosts | 5 / 6 | 0 | 83.33% | [404hosts.go](../master/api/npmapi/404hosts.go) |
| NGINX / Access lists | 2 / 4 | 0 | 50.00% | [access_list.go](../master/api/npmapi/access_list.go) |
| NGINX / Certificates | 4 / 7 | 0 | 57.14% | [certs.go](../master/api/npmapi/certs.go) |
| NGINX / Proxies | 7 / 8 | 0 | 87.50% | [proxy.go](../master/api/npmapi/proxy.go) |
| NGINX / Redirects | 5 / 6 | 0 | 83.33% | [redirection.go](../master/api/npmapi/redirection.go) |
| NGINX / Streams | 5 / 6 | 0 | 83.33% | [streams.go](../master/api/npmapi/streams.go) |
| NGINX / Upstreams | 5 / 6 | 0 | 83.33% | [upstreams.go](../master/api/npmapi/upstreams.go) |
| PCI / GPU | 0 / 6 | 6 | 0.00% | [pci.go](../master/api/pci.go) |
| Protocol connections | 0 / 1 | 1 | 0.00% | [protocol.go](../master/api/protocol.go) |
| SMART disks | 0 / 14 | 12 | 0.00% | [smartdisk.go](../master/api/smartdisk.go) |
| Stream observability | 0 / 35 | 33 | 0.00% | [streamInfo.go](../master/api/streamInfo.go) |
| VMs and host tuning | 57 / 73 | 2 | 78.08% | [virsh.go](../master/api/virsh.go) |
| VM XML templates | 4 / 5 | 0 | 80.00% | [virsh_xml_templates.go](../master/api/virsh_xml_templates.go) |
| VM disks | 2 / 4 | 0 | 50.00% | [vm_disk.go](../master/api/vm_disk.go) |
| WireGuard | 0 / 4 | 3 | 0.00% | [wireguard.go](../master/api/wireguard.go) |

### Available capabilities and safety

Both transports share 117 registrations: 33 NGINX, nine Celld, three DNS, five Notes, two SPA, two ISO and 63 VM tools. Feature contracts: [upstreams](docs/areas/upstreams.md), [NGINX](docs/areas/nginx.md), [proxies](docs/areas/nginx-proxies.md), [Celld](docs/areas/celld.md), [DNS](docs/areas/dns.md), [Notes](docs/areas/notes.md), [SPA](docs/areas/spa.md), [ISOs](docs/areas/isos.md), [VMs](docs/areas/vms.md).

Every VM tool publishes low/medium/high/very high risk. Medium+ requires per-call explicit user form approval bound to complete arguments; stale/replayed/forged/changed continuations or unsupported/declined/canceled pickers cannot execute writes. Modern HTTP remains stateless; legacy HTTP retains authenticated sessions for form elicitation. Stdio supports both protocol generations.

VM writes capture actual previous settings and secure inactive/current XML; CPU pinning includes the exact prior pinning getter. Host boot setters additionally capture every grubby entry, default kernel and current command line. Persistence must succeed before dispatch. Private 0700 directories and 0600 retained before.json files live in MCP_VM_BACKUP_DIR, or the process user's cache under hyperhive/mcp/vm-backups. Paths are returned even after subsequent backend failure. Configuration records do not contain disk snapshots or automatic rollback; SSH injection additionally retains the complete primary disk file before dispatch. Disk growth/guest RAM loss cannot be undone by them. Locking covers this registry, not external API/hypervisor changes or independent server processes. Updated master/slave snapshot support is required; older backends fail closed.

VM export and backup download stream full binary responses to private artifacts on the MCP server filesystem, with size/hash and a six-hour bound honoring earlier cancellation. Clients do not automatically download these files. ISO download, manual VM backup and clone return initiation/acceptance, never fabricated completion/job IDs. Ordinary JSON calls retain shared timeout, cancellation, response-size, redirect and safe-error protections; mutations are never automatically retried.

DNS/Notes/ISO permitted endpoint coverage is 3/3, 5/5 and 2/2. SPA has the two selected reads; POST creation is excluded because firewall replacement/rollback deletes rules and can destroy an existing allow set. VM policy-permitted original selection is now 63/63 (100.00%) after the explicit seven-capability request. SSH injection requires a shut-off guest and a persisted primary-disk artifact. The remaining 17 exclusions and two internal helpers remain visible below. No arbitrary API dispatcher or confirmation argument was added.

| Tool | Endpoint | VM risk |
| --- | --- | --- |
| `create_vm` | `POST /virsh/createvm` | high |
| `set_vm_autostart` | `POST /virsh/autostart/{vm_name}` | medium |
| `set_vm_live` | `POST /virsh/vmlive/{vm_name}` | high |
| `create_vm_automatic_backup` | `POST /virsh/autobak` | very high |
| `update_vm_automatic_backup` | `PUT /virsh/autobak/{id}` | very high |
| `enable_vm_automatic_backup` | `PATCH /virsh/autopak/enable/{id}` | very high |
| `add_vm_ssh_key` | `POST /virsh/add_ssh_key/{vm_name}` | very high |
| `list_upstreams` | `GET /upstream/list` | — |
| `get_upstream` | `GET /upstream/{id}` | — |
| `get_upstream_capabilities` | `GET /upstream/capabilities` | — |
| `create_upstream` | `POST /upstream/create` | — |
| `update_upstream` | `PUT /upstream/{id}` | — |
| `add_vm_video` | `POST /virsh/novncvideo/add/{vm_name}` | high |
| `attach_vm_disk` | `POST /virsh/vm_disk/{vm_name}` | high |
| `backup_vm` | `POST /virsh/backup/{vm_name}/{nfs_id}` | high |
| `clone_vm` | `POST /virsh/cloneVM/{vm_name}/{dest_nfs}/{dest_machine_name}` | high |
| `create_404_host` | `POST /404/create` | — |
| `create_access_list` | `POST /access-lists/create` | — |
| `create_celld_fleet` | `POST /celld/fleets` | — |
| `create_note` | `POST /notes/` | — |
| `create_proxy` | `POST /proxy/create` | — |
| `create_redirect` | `POST /redirection/create` | — |
| `create_stream` | `POST /stream/create` | — |
| `create_vm_xml_template` | `POST /virsh/xmltemplates/` | medium |
| `delete_celld_fleet` | `DELETE /celld/fleets/{id}` | — |
| `disable_404_host` | `POST /404/disable` | — |
| `disable_proxy` | `POST /proxy/disable` | — |
| `disable_redirect` | `POST /redirection/disable` | — |
| `disable_stream` | `POST /stream/disable` | — |
| `disable_vm_automatic_backup` | `PATCH /virsh/autopak/disable/{id}` | medium |
| `dns_alias_add` | `POST /dnsmasq/alias/add` | — |
| `dns_alias_get` | `GET /dnsmasq/alias/get` | — |
| `dns_aliases_list` | `GET /dnsmasq/alias/all` | — |
| `download_certificate` | `POST /certs/download` | — |
| `download_vm_backup` | `GET /virsh/downloadbackup/{backup_id}` | low |
| `edit_vm` | `POST /virsh/editvm/{vm_name}` | high |
| `enable_404_host` | `POST /404/enable` | — |
| `enable_proxy` | `POST /proxy/enable` | — |
| `enable_redirect` | `POST /redirection/enable` | — |
| `enable_stream` | `POST /stream/enable` | — |
| `export_vm` | `GET /virsh/export/{vm_name}` | medium |
| `force_shutdown_vm` | `POST /virsh/forceshutdownvm/{vm_name}` | very high |
| `get_celld_credentials` | `GET /celld/fleets/{id}/credentials` | — |
| `get_celld_events` | `GET /celld/fleets/{id}/events` | — |
| `get_celld_health` | `GET /celld/health` | — |
| `get_celld_logs` | `GET /celld/fleets/{id}/logs` | — |
| `get_note` | `GET /notes/{id}` | — |
| `get_vm` | `GET /virsh/getvmbyname/{vm_name}` | low |
| `get_vm_ballooning` | `GET /virsh/ballooning/{vm_name}` | low |
| `get_vm_core_isolation` | `GET /virsh/coreisolation/{machine_name}` | low |
| `get_vm_cpu_disable_features` | `GET /virsh/getcpudisablefeatures` | low |
| `get_vm_cpu_pinning` | `GET /virsh/cpupinning/{vm_name}` | low |
| `get_vm_cpu_topology` | `GET /virsh/cputopology/{machine_name}` | low |
| `get_vm_cpu_xml` | `GET /virsh/cpuxml/{vm_name}` | low |
| `get_vm_host_hugepages` | `GET /virsh/hosthugepages/{machine_name}` | low |
| `get_vm_hugepages` | `GET /virsh/hugepages/{vm_name}` | low |
| `get_vm_hyperv` | `GET /virsh/hyperv/{vm_name}` | low |
| `get_vm_irqbalance` | `GET /virsh/irqbalance/{machine_name}` | low |
| `get_vm_kvm` | `GET /virsh/kvm/{vm_name}` | low |
| `get_vm_tuned_profiles` | `GET /virsh/tunedadm/{machine_name}` | low |
| `get_vm_video` | `GET /virsh/novncvideo/{vm_name}` | low |
| `get_vm_xml` | `GET /virsh/vmxml/{vm_name}` | low |
| `get_vm_xml_template` | `GET /virsh/xmltemplates/{id}` | low |
| `grow_vm_disk` | `POST /vm-disk/{id}/grow` | high |
| `list_404_hosts` | `GET /404/list` | — |
| `list_access_lists` | `GET /access-lists/list` | — |
| `list_celld_fleets` | `GET /celld/fleets` | — |
| `list_certificates` | `GET /certs/list` | — |
| `list_dns_providers` | `GET /certs/dns-providers` | — |
| `list_isos` | `GET /isos/` | — |
| `list_notes` | `GET /notes/` | — |
| `list_proxies` | `GET /proxy/list` | — |
| `list_redirects` | `GET /redirection/list` | — |
| `list_spa_allows` | `GET /spa/allow/{port}` | — |
| `list_spa_ports` | `GET /spa/` | — |
| `list_streams` | `GET /stream/list` | — |
| `list_vm_attached_disks` | `GET /virsh/vm_disk/{vm_name}` | low |
| `list_vm_automatic_backups` | `GET /virsh/autobak` | low |
| `list_vm_backups` | `GET /virsh/backups` | low |
| `list_vm_disks` | `GET /vm-disk/list` | low |
| `list_vm_machine_types` | `GET /virsh/machine_types/{machine_name}` | low |
| `list_vm_xml_templates` | `GET /virsh/xmltemplates/` | low |
| `list_vms` | `GET /virsh/getallvms` | low |
| `patch_note` | `PATCH /notes/{id}` | — |
| `pause_vm` | `POST /virsh/pausevm/{vm_name}` | high |
| `renew_certificate` | `POST /certs/renew` | — |
| `replace_note` | `PUT /notes/{id}` | — |
| `replace_vm_host_core_isolation` | `PUT /virsh/coreisolation/{machine_name}` | very high |
| `replace_vm_host_hugepages` | `PUT /virsh/hosthugepages/{machine_name}` | very high |
| `restart_vm` | `POST /virsh/restartvm/{vm_name}` | very high |
| `resume_vm` | `POST /virsh/resumevm/{vm_name}` | medium |
| `set_vm_ballooning` | `POST /virsh/ballooning/{vm_name}` | high |
| `set_vm_cpu_pinning` | `POST /virsh/cpupinning/{vm_name}` | high |
| `set_vm_host_core_isolation` | `POST /virsh/coreisolation/{machine_name}` | very high |
| `set_vm_host_hugepages` | `POST /virsh/hosthugepages/{machine_name}` | very high |
| `set_vm_hugepages` | `POST /virsh/hugepages/{vm_name}` | high |
| `set_vm_hyperv` | `POST /virsh/hyperv/{vm_name}` | high |
| `set_vm_irqbalance` | `POST /virsh/irqbalance/{machine_name}` | high |
| `set_vm_kvm` | `POST /virsh/kvm/{vm_name}` | high |
| `set_vm_machine_type` | `POST /virsh/machine_type/{vm_name}` | high |
| `set_vm_network` | `POST /virsh/change_vm_network/{vm_name}` | high |
| `set_vm_tuned_profile` | `POST /virsh/tunedadm/{machine_name}` | high |
| `set_vm_vnc_password` | `POST /virsh/change_vnc_password/{vm_name}` | high |
| `setup_frontend_proxy` | `POST /proxy/setupFrontEnd` | — |
| `setup_secure_frontend_proxy` | `POST /proxy/setupFrontEndSecure` | — |
| `shutdown_vm` | `POST /virsh/shutdownvm/{vm_name}` | high |
| `start_celld_fleet` | `POST /celld/fleets/{id}/start` | — |
| `start_iso_download` | `POST /isos/download` | — |
| `start_vm` | `POST /virsh/startvm/{vm_name}` | medium |
| `stop_celld_fleet` | `POST /celld/fleets/{id}/stop` | — |
| `update_404_host` | `PUT /404/edit` | — |
| `update_proxy` | `PUT /proxy/edit` | — |
| `update_redirect` | `PUT /redirection/edit` | — |
| `update_stream` | `PUT /stream/edit` | — |
| `update_vm_cpu_xml` | `POST /virsh/updatecpuxml/{vm_name}` | high |
| `update_vm_xml` | `POST /virsh/updatevmxml/{vm_name}` | very high |
| `update_vm_xml_template` | `PUT /virsh/xmltemplates/{id}` | high |

### Endpoint inventory and remaining work

One row is one method/path pair. Only registered, enabled, contract-reviewed and validated tools count as implemented. Exclusions remain in raw coverage with zero credit; all/all-remaining/100% does not authorize deletion. The existing Celld exception does not authorize deletion elsewhere. Selected SSH injection is blocked and unregistered; the two snapshot routes are internal prerequisites with zero direct coverage. Unselected browser/stream routes remain visible for later disposition.

#### Guest access and SPA

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /guest_api/guest_page/{vm_name}` | missing | [GUESTS.go:839](../master/api/GUESTS.go#L839) |
| `POST /guest_api/guest_vm` | missing | [GUESTS.go:840](../master/api/GUESTS.go#L840) |
| `GET /guest_api/novnc/ws` | missing | [GUESTS.go:846](../master/api/GUESTS.go#L846) |
| `GET /guest_api/novnc/sprite/{vm_name}` | missing | [GUESTS.go:847](../master/api/GUESTS.go#L847) |
| `GET /guest_api/novnc/*` | missing | [GUESTS.go:848](../master/api/GUESTS.go#L848) |
| `POST /guest_api/spa/allow-ip` | missing | [GUESTS.go:852](../master/api/GUESTS.go#L852) |
| `POST /guest_api/spa/allow` | missing | [GUESTS.go:853](../master/api/GUESTS.go#L853) |
| `GET /guest_api/spa/pageallow/{port}` | missing | [GUESTS.go:854](../master/api/GUESTS.go#L854) |
| `POST /spa/` | excluded (deletion policy: creation deletes firewall rules and can destroy allow set) | [GUESTS.go:861](../master/api/GUESTS.go#L861) |
| `GET /spa/` | implemented | [GUESTS.go:862](../master/api/GUESTS.go#L862) |
| `GET /spa/allow/{port}` | implemented | [GUESTS.go:863](../master/api/GUESTS.go#L863) |
| `DELETE /spa/{port}` | excluded (deletion policy) | [GUESTS.go:864](../master/api/GUESTS.go#L864) |

#### Core, login and notifications

| Endpoint | Status | Route source |
| --- | --- | --- |
| `POST /login` | missing | [api.go:282](../master/api/api.go#L282) |
| `GET /nots/register` | missing | [api.go:287](../master/api/api.go#L287) |
| `GET /static/notification-icon.png` | missing | [api.go:288](../master/api/api.go#L288) |
| `GET /icon.png` | missing | [api.go:292](../master/api/api.go#L292) |
| `GET /new-slave-count` | missing | [api.go:304](../master/api/api.go#L304) |
| `GET /notification/public-key` | missing | [api.go:308](../master/api/api.go#L308) |
| `POST /notification/subscribe` | missing | [api.go:309](../master/api/api.go#L309) |
| `GET /notification/test` | missing | [api.go:310](../master/api/api.go#L310) |
| `DELETE /notification/subscriptions` | excluded (deletion policy) | [api.go:311](../master/api/api.go#L311) |
| `GET /sw.js` | missing | [api.go:313](../master/api/api.go#L313) |
| `GET /protected` | missing | [api.go:317](../master/api/api.go#L317) |
| `GET /ws` | missing | [api.go:319](../master/api/api.go#L319) |

#### BTRFS

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /btrfs/getFreeDisks/{machine_name}` | missing | [btrfs.go:654](../master/api/btrfs.go#L654) |
| `GET /btrfs/getAllDuckingDisks/{machine_name}` | missing | [btrfs.go:655](../master/api/btrfs.go#L655) |
| `GET /btrfs/getraids/{machine_name}` | missing | [btrfs.go:656](../master/api/btrfs.go#L656) |
| `POST /btrfs/createraid/{machine_name}` | missing | [btrfs.go:657](../master/api/btrfs.go#L657) |
| `DELETE /btrfs/removeraid/{machine_name}` | excluded (deletion policy) | [btrfs.go:658](../master/api/btrfs.go#L658) |
| `POST /btrfs/mount_raid/{machine_name}` | missing | [btrfs.go:660](../master/api/btrfs.go#L660) |
| `POST /btrfs/umount_raid/{machine_name}` | missing | [btrfs.go:661](../master/api/btrfs.go#L661) |
| `GET /btrfs/automatic_mount/{machine_name}` | missing | [btrfs.go:664](../master/api/btrfs.go#L664) |
| `POST /btrfs/automatic_mount` | missing | [btrfs.go:665](../master/api/btrfs.go#L665) |
| `DELETE /btrfs/automatic_mount` | excluded (deletion policy) | [btrfs.go:666](../master/api/btrfs.go#L666) |
| `POST /btrfs/add_diskraid/{machine_name}` | missing | [btrfs.go:668](../master/api/btrfs.go#L668) |
| `POST /btrfs/remove_diskraid/{machine_name}` | excluded (deletion policy) | [btrfs.go:669](../master/api/btrfs.go#L669) |
| `POST /btrfs/replace_diskraid/{machine_name}` | missing | [btrfs.go:670](../master/api/btrfs.go#L670) |
| `POST /btrfs/change_raid_level/{machine_name}` | missing | [btrfs.go:672](../master/api/btrfs.go#L672) |
| `POST /btrfs/balance_raid/{machine_name}` | missing | [btrfs.go:673](../master/api/btrfs.go#L673) |
| `POST /btrfs/pause_balance/{machine_name}` | missing | [btrfs.go:674](../master/api/btrfs.go#L674) |
| `POST /btrfs/resume_balance/{machine_name}` | missing | [btrfs.go:675](../master/api/btrfs.go#L675) |
| `POST /btrfs/cancel_balance/{machine_name}` | missing | [btrfs.go:676](../master/api/btrfs.go#L676) |
| `POST /btrfs/defragment_raid/{machine_name}` | missing | [btrfs.go:677](../master/api/btrfs.go#L677) |
| `POST /btrfs/scrub_raid/{machine_name}` | missing | [btrfs.go:678](../master/api/btrfs.go#L678) |
| `GET /btrfs/scrub_stats/{machine_name}` | missing | [btrfs.go:679](../master/api/btrfs.go#L679) |
| `GET /btrfs/raid_status/{machine_name}` | missing | [btrfs.go:682](../master/api/btrfs.go#L682) |

#### Celld

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /celld/health` | implemented | [celld.go:14](../master/api/celld.go#L14) |
| `GET /celld/fleets` | implemented | [celld.go:15](../master/api/celld.go#L15) |
| `POST /celld/fleets` | implemented | [celld.go:16](../master/api/celld.go#L16) |
| `GET /celld/fleets/{id}` | missing | [celld.go:17](../master/api/celld.go#L17) |
| `PATCH /celld/fleets/{id}` | excluded (deletion policy: removes node data) | [celld.go:18](../master/api/celld.go#L18) |
| `DELETE /celld/fleets/{id}` | implemented | [celld.go:19](../master/api/celld.go#L19) |
| `POST /celld/fleets/{id}/start` | implemented | [celld.go:21](../master/api/celld.go#L21) |
| `POST /celld/fleets/{id}/stop` | implemented | [celld.go:21](../master/api/celld.go#L21) |
| `POST /celld/fleets/{id}/restart` | missing | [celld.go:21](../master/api/celld.go#L21) |
| `GET /celld/fleets/{id}/credentials` | implemented | [celld.go:23](../master/api/celld.go#L23) |
| `GET /celld/fleets/{id}/observability` | missing | [celld.go:24](../master/api/celld.go#L24) |
| `GET /celld/fleets/{id}/logs/stream` | missing | [celld.go:25](../master/api/celld.go#L25) |
| `GET /celld/fleets/{id}/events/stream` | missing | [celld.go:26](../master/api/celld.go#L26) |
| `GET /celld/fleets/{id}/logs` | implemented | [celld.go:27](../master/api/celld.go#L27) |
| `GET /celld/fleets/{id}/events` | implemented | [celld.go:28](../master/api/celld.go#L28) |
| `GET /celld/fleets/{id}/traces/{trace_id}` | missing | [celld.go:29](../master/api/celld.go#L29) |
| `GET /celld/fleets/{id}/metrics` | missing | [celld.go:30](../master/api/celld.go#L30) |

#### DNS aliases

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /dnsmasq/alias/all` | implemented | [dnsmasq.go:126](../master/api/dnsmasq.go#L126) |
| `GET /dnsmasq/alias/get` | implemented | [dnsmasq.go:127](../master/api/dnsmasq.go#L127) |
| `POST /dnsmasq/alias/add` | implemented | [dnsmasq.go:128](../master/api/dnsmasq.go#L128) |
| `DELETE /dnsmasq/alias/remove` | excluded (deletion policy) | [dnsmasq.go:129](../master/api/dnsmasq.go#L129) |

#### Docker

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /docker/images/{machineName}` | missing | [docker.go:716](../master/api/docker.go#L716) |
| `POST /docker/images/download/{machineName}` | missing | [docker.go:717](../master/api/docker.go#L717) |
| `DELETE /docker/images/remove/{machineName}` | excluded (deletion policy) | [docker.go:718](../master/api/docker.go#L718) |
| `GET /docker/containers/{machineName}` | missing | [docker.go:722](../master/api/docker.go#L722) |
| `POST /docker/containers/create/{machineName}` | missing | [docker.go:723](../master/api/docker.go#L723) |
| `DELETE /docker/containers/remove/{machineName}` | excluded (deletion policy) | [docker.go:724](../master/api/docker.go#L724) |
| `POST /docker/containers/stop/{machineName}` | missing | [docker.go:725](../master/api/docker.go#L725) |
| `POST /docker/containers/start/{machineName}` | missing | [docker.go:726](../master/api/docker.go#L726) |
| `POST /docker/containers/restart/{machineName}` | missing | [docker.go:727](../master/api/docker.go#L727) |
| `POST /docker/containers/pause/{machineName}` | missing | [docker.go:728](../master/api/docker.go#L728) |
| `POST /docker/containers/unpause/{machineName}` | missing | [docker.go:729](../master/api/docker.go#L729) |
| `POST /docker/containers/kill/{machineName}` | missing | [docker.go:730](../master/api/docker.go#L730) |
| `POST /docker/containers/logs/{machineName}` | missing | [docker.go:731](../master/api/docker.go#L731) |
| `POST /docker/containers/update/{machineName}` | missing | [docker.go:732](../master/api/docker.go#L732) |
| `POST /docker/containers/rename/{machineName}` | missing | [docker.go:733](../master/api/docker.go#L733) |
| `POST /docker/containers/exec/{machineName}` | missing | [docker.go:734](../master/api/docker.go#L734) |
| `GET /docker/volumes/{machineName}` | missing | [docker.go:738](../master/api/docker.go#L738) |
| `POST /docker/volumes/create/{machineName}` | missing | [docker.go:739](../master/api/docker.go#L739) |
| `DELETE /docker/volumes/remove/{machineName}` | excluded (deletion policy) | [docker.go:740](../master/api/docker.go#L740) |
| `GET /docker/networks/{machineName}` | missing | [docker.go:744](../master/api/docker.go#L744) |
| `POST /docker/networks/create/{machineName}` | missing | [docker.go:745](../master/api/docker.go#L745) |
| `DELETE /docker/networks/remove/{machineName}` | excluded (deletion policy) | [docker.go:746](../master/api/docker.go#L746) |
| `GET /docker/git/{machineName}` | missing | [docker.go:750](../master/api/docker.go#L750) |
| `POST /docker/git/clone/{machineName}` | missing | [docker.go:751](../master/api/docker.go#L751) |
| `DELETE /docker/git/remove/{machineName}` | excluded (deletion policy) | [docker.go:752](../master/api/docker.go#L752) |
| `POST /docker/git/update/{machineName}` | missing | [docker.go:753](../master/api/docker.go#L753) |

#### Host maintenance

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /extra/getUpdates/{machineName}` | missing | [extra.go:81](../master/api/extra.go#L81) |
| `POST /extra/performUpdate/{machineName}` | missing | [extra.go:82](../master/api/extra.go#L82) |
| `POST /extra/shutdown/{machineName}` | missing | [extra.go:83](../master/api/extra.go#L83) |
| `POST /extra/restart/{machineName}` | missing | [extra.go:84](../master/api/extra.go#L84) |

#### GoAccess

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /goaccess` | missing | [goaccess.go:623](../master/api/goaccess.go#L623) |
| `GET /ws_goaccess` | missing | [goaccess.go:624](../master/api/goaccess.go#L624) |
| `GET /goaccess/status` | missing | [goaccess.go:625](../master/api/goaccess.go#L625) |

#### Host information

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /info/cpu/{machine_name}` | missing | [info.go:773](../master/api/info.go#L773) |
| `GET /info/mem/{machine_name}` | missing | [info.go:774](../master/api/info.go#L774) |
| `GET /info/disk/{machine_name}` | missing | [info.go:775](../master/api/info.go#L775) |
| `GET /info/network/{machine_name}` | missing | [info.go:776](../master/api/info.go#L776) |
| `GET /info/processes/{machine_name}` | missing | [info.go:777](../master/api/info.go#L777) |
| `GET /info/processes/{machine_name}/{pid}` | missing | [info.go:778](../master/api/info.go#L778) |
| `POST /info/processes/{machine_name}/{pid}/kill` | missing | [info.go:779](../master/api/info.go#L779) |
| `POST /info/processes/{machine_name}/{pid}/terminate` | missing | [info.go:780](../master/api/info.go#L780) |
| `GET /info/history/cpu/{machine_name}` | missing | [info.go:781](../master/api/info.go#L781) |
| `GET /info/history/mem/{machine_name}` | missing | [info.go:782](../master/api/info.go#L782) |
| `GET /info/history/disk/{machine_name}` | missing | [info.go:783](../master/api/info.go#L783) |
| `GET /info/history/network/{machine_name}` | missing | [info.go:784](../master/api/info.go#L784) |
| `POST /info/stress-cpu/{machine_name}` | missing | [info.go:785](../master/api/info.go#L785) |
| `POST /info/test-ram/{machine_name}` | missing | [info.go:786](../master/api/info.go#L786) |
| `GET /info/time-since/{machine_name}` | missing | [info.go:788](../master/api/info.go#L788) |

#### ISOs

| Endpoint | Status | Route source |
| --- | --- | --- |
| `POST /isos/download` | implemented | [isos.go:144](../master/api/isos.go#L144) |
| `GET /isos/` | implemented | [isos.go:145](../master/api/isos.go#L145) |
| `DELETE /isos/{id}` | excluded (deletion policy) | [isos.go:146](../master/api/isos.go#L146) |

#### Kubernetes

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /k8s/tls-sans` | missing | [k8s.go:67](../master/api/k8s.go#L67) |
| `GET /k8s/cluster/status` | missing | [k8s.go:68](../master/api/k8s.go#L68) |
| `POST /k8s/connection-file` | missing | [k8s.go:69](../master/api/k8s.go#L69) |

#### Logs

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /logs/list` | missing | [logs.go:53](../master/api/logs.go#L53) |

#### NFS

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /nfs/list` | missing | [nfs.go:166](../master/api/nfs.go#L166) |
| `POST /nfs/create` | missing | [nfs.go:167](../master/api/nfs.go#L167) |
| `DELETE /nfs/delete` | excluded (deletion policy) | [nfs.go:168](../master/api/nfs.go#L168) |
| `POST /nfs/remount/{id}` | missing | [nfs.go:169](../master/api/nfs.go#L169) |
| `POST /nfs/contents/{machine}` | missing | [nfs.go:170](../master/api/nfs.go#L170) |

#### Notes

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /notes/` | implemented | [notes.go:140](../master/api/notes.go#L140) |
| `GET /notes/{id}` | implemented | [notes.go:141](../master/api/notes.go#L141) |
| `POST /notes/` | implemented | [notes.go:142](../master/api/notes.go#L142) |
| `PUT /notes/{id}` | implemented | [notes.go:143](../master/api/notes.go#L143) |
| `PATCH /notes/{id}` | implemented | [notes.go:144](../master/api/notes.go#L144) |
| `DELETE /notes/{id}` | excluded (deletion policy) | [notes.go:145](../master/api/notes.go#L145) |

#### Notification history

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /nots/` | missing | [nots.go:45](../master/api/nots.go#L45) |

#### noVNC

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /novnc/ws` | missing | [novnc.go:225](../master/api/novnc.go#L225) |
| `GET /novnc/sprite/{vm_name}` | missing | [novnc.go:226](../master/api/novnc.go#L226) |
| `GET /novnc/*` | missing | [novnc.go:227](../master/api/novnc.go#L227) |

#### NGINX / 404 hosts

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /404/list` | implemented | [404hosts.go:126](../master/api/npmapi/404hosts.go#L126) |
| `POST /404/create` | implemented | [404hosts.go:127](../master/api/npmapi/404hosts.go#L127) |
| `PUT /404/edit` | implemented | [404hosts.go:128](../master/api/npmapi/404hosts.go#L128) |
| `DELETE /404/delete` | excluded (deletion policy) | [404hosts.go:129](../master/api/npmapi/404hosts.go#L129) |
| `POST /404/disable` | implemented | [404hosts.go:130](../master/api/npmapi/404hosts.go#L130) |
| `POST /404/enable` | implemented | [404hosts.go:131](../master/api/npmapi/404hosts.go#L131) |

#### NGINX / Access lists

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /access-lists/list` | implemented | [access_list.go:82](../master/api/npmapi/access_list.go#L82) |
| `POST /access-lists/create` | implemented | [access_list.go:83](../master/api/npmapi/access_list.go#L83) |
| `PUT /access-lists/edit` | excluded (deletion policy: replacement deletes authentication/client entries) | [access_list.go:84](../master/api/npmapi/access_list.go#L84) |
| `DELETE /access-lists/delete` | excluded (deletion policy) | [access_list.go:85](../master/api/npmapi/access_list.go#L85) |

#### NGINX / Certificates

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /certs/dns-providers` | implemented | [certs.go:265](../master/api/npmapi/certs.go#L265) |
| `GET /certs/list` | implemented | [certs.go:266](../master/api/npmapi/certs.go#L266) |
| `POST /certs/create` | excluded (deletion policy: certificate rollback deletes resource) | [certs.go:267](../master/api/npmapi/certs.go#L267) |
| `POST /certs/create-lets-encrypt` | excluded (deletion policy: upstream rollback deletes certificate) | [certs.go:268](../master/api/npmapi/certs.go#L268) |
| `POST /certs/download` | implemented | [certs.go:269](../master/api/npmapi/certs.go#L269) |
| `POST /certs/renew` | implemented | [certs.go:270](../master/api/npmapi/certs.go#L270) |
| `DELETE /certs/delete` | excluded (deletion policy) | [certs.go:271](../master/api/npmapi/certs.go#L271) |

#### NGINX / Proxies

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /proxy/list` | implemented | [proxy.go:466](../master/api/npmapi/proxy.go#L466) |
| `POST /proxy/create` | implemented | [proxy.go:467](../master/api/npmapi/proxy.go#L467) |
| `PUT /proxy/edit` | implemented | [proxy.go:468](../master/api/npmapi/proxy.go#L468) |
| `DELETE /proxy/delete` | excluded (deletion policy) | [proxy.go:469](../master/api/npmapi/proxy.go#L469) |
| `POST /proxy/disable` | implemented | [proxy.go:470](../master/api/npmapi/proxy.go#L470) |
| `POST /proxy/enable` | implemented | [proxy.go:471](../master/api/npmapi/proxy.go#L471) |
| `POST /proxy/setupFrontEnd` | implemented | [proxy.go:473](../master/api/npmapi/proxy.go#L473) |
| `POST /proxy/setupFrontEndSecure` | implemented | [proxy.go:474](../master/api/npmapi/proxy.go#L474) |

#### NGINX / Redirects

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /redirection/list` | implemented | [redirection.go:126](../master/api/npmapi/redirection.go#L126) |
| `POST /redirection/create` | implemented | [redirection.go:127](../master/api/npmapi/redirection.go#L127) |
| `PUT /redirection/edit` | implemented | [redirection.go:128](../master/api/npmapi/redirection.go#L128) |
| `DELETE /redirection/delete` | excluded (deletion policy) | [redirection.go:129](../master/api/npmapi/redirection.go#L129) |
| `POST /redirection/disable` | implemented | [redirection.go:130](../master/api/npmapi/redirection.go#L130) |
| `POST /redirection/enable` | implemented | [redirection.go:131](../master/api/npmapi/redirection.go#L131) |

#### NGINX / Streams

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /stream/list` | implemented | [streams.go:235](../master/api/npmapi/streams.go#L235) |
| `POST /stream/create` | implemented | [streams.go:236](../master/api/npmapi/streams.go#L236) |
| `PUT /stream/edit` | implemented | [streams.go:237](../master/api/npmapi/streams.go#L237) |
| `DELETE /stream/delete` | excluded (deletion policy) | [streams.go:238](../master/api/npmapi/streams.go#L238) |
| `POST /stream/disable` | implemented | [streams.go:239](../master/api/npmapi/streams.go#L239) |
| `POST /stream/enable` | implemented | [streams.go:240](../master/api/npmapi/streams.go#L240) |

#### NGINX / Upstreams

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /upstream/list` | implemented | [upstreams.go:141](../master/api/npmapi/upstreams.go#L141) |
| `GET /upstream/capabilities` | implemented | [upstreams.go:142](../master/api/npmapi/upstreams.go#L142) |
| `POST /upstream/create` | implemented | [upstreams.go:143](../master/api/npmapi/upstreams.go#L143) |
| `GET /upstream/{id}` | implemented | [upstreams.go:144](../master/api/npmapi/upstreams.go#L144) |
| `PUT /upstream/{id}` | implemented | [upstreams.go:145](../master/api/npmapi/upstreams.go#L145) |
| `DELETE /upstream/{id}` | excluded (deletion policy) | [upstreams.go:146](../master/api/npmapi/upstreams.go#L146) |

#### PCI / GPU

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /pci/host/{machine_name}` | missing | [pci.go:177](../master/api/pci.go#L177) |
| `GET /pci/host/iommu/{machine_name}` | missing | [pci.go:178](../master/api/pci.go#L178) |
| `GET /pci/vm/{machine_name}/{vm_name}` | missing | [pci.go:179](../master/api/pci.go#L179) |
| `POST /pci/attach/{machine_name}` | missing | [pci.go:180](../master/api/pci.go#L180) |
| `POST /pci/detach/{machine_name}` | missing | [pci.go:181](../master/api/pci.go#L181) |
| `POST /pci/return/{machine_name}` | missing | [pci.go:182](../master/api/pci.go#L182) |

#### Protocol connections

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /protocol/list` | missing | [protocol.go:22](../master/api/protocol.go#L22) |

#### SMART disks

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /smartdisk/{machine_name}` | missing | [smartdisk.go:455](../master/api/smartdisk.go#L455) |
| `POST /smartdisk/{machine_name}/self-test` | missing | [smartdisk.go:456](../master/api/smartdisk.go#L456) |
| `GET /smartdisk/{machine_name}/self-test/progress` | missing | [smartdisk.go:457](../master/api/smartdisk.go#L457) |
| `POST /smartdisk/{machine_name}/self-test/cancel` | missing | [smartdisk.go:458](../master/api/smartdisk.go#L458) |
| `POST /smartdisk/{machine_name}/schedules/` | missing | [smartdisk.go:461](../master/api/smartdisk.go#L461) |
| `GET /smartdisk/{machine_name}/schedules/` | missing | [smartdisk.go:462](../master/api/smartdisk.go#L462) |
| `PUT /smartdisk/{machine_name}/schedules/{id}/` | missing | [smartdisk.go:464](../master/api/smartdisk.go#L464) |
| `DELETE /smartdisk/{machine_name}/schedules/{id}/` | excluded (deletion policy) | [smartdisk.go:465](../master/api/smartdisk.go#L465) |
| `PUT /smartdisk/{machine_name}/schedules/{id}/enable` | missing | [smartdisk.go:466](../master/api/smartdisk.go#L466) |
| `POST /smartdisk/{machine_name}/realloc/full-wipe` | excluded (deletion policy) | [smartdisk.go:470](../master/api/smartdisk.go#L470) |
| `POST /smartdisk/{machine_name}/realloc/non-destructive` | missing | [smartdisk.go:471](../master/api/smartdisk.go#L471) |
| `GET /smartdisk/{machine_name}/realloc/status` | missing | [smartdisk.go:472](../master/api/smartdisk.go#L472) |
| `GET /smartdisk/{machine_name}/realloc/` | missing | [smartdisk.go:473](../master/api/smartdisk.go#L473) |
| `POST /smartdisk/{machine_name}/realloc/cancel` | missing | [smartdisk.go:474](../master/api/smartdisk.go#L474) |

#### Stream observability

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /streamInfo/` | missing | [streamInfo.go:2204](../master/api/streamInfo.go#L2204) |
| `DELETE /streamInfo/logs` | excluded (deletion policy) | [streamInfo.go:2208](../master/api/streamInfo.go#L2208) |
| `GET /streamInfo/ip-aliases` | missing | [streamInfo.go:2209](../master/api/streamInfo.go#L2209) |
| `POST /streamInfo/ip-aliases` | missing | [streamInfo.go:2210](../master/api/streamInfo.go#L2210) |
| `DELETE /streamInfo/ip-aliases` | excluded (deletion policy) | [streamInfo.go:2211](../master/api/streamInfo.go#L2211) |
| `GET /streamInfo/streamInfo.css` | missing | [streamInfo.go:2218](../master/api/streamInfo.go#L2218) |
| `GET /streamInfo/streamInfo-core.js` | missing | [streamInfo.go:2219](../master/api/streamInfo.go#L2219) |
| `GET /streamInfo/streamInfo-overview.js` | missing | [streamInfo.go:2220](../master/api/streamInfo.go#L2220) |
| `GET /streamInfo/streamInfo-tabs.js` | missing | [streamInfo.go:2221](../master/api/streamInfo.go#L2221) |
| `GET /streamInfo/streamInfo-profiles.js` | missing | [streamInfo.go:2222](../master/api/streamInfo.go#L2222) |
| `GET /streamInfo/streamInfo-interactions.js` | missing | [streamInfo.go:2223](../master/api/streamInfo.go#L2223) |
| `GET /streamInfo/data` | missing | [streamInfo.go:2226](../master/api/streamInfo.go#L2226) |
| `GET /streamInfo/analytics` | missing | [streamInfo.go:2229](../master/api/streamInfo.go#L2229) |
| `GET /streamInfo/intel` | missing | [streamInfo.go:2230](../master/api/streamInfo.go#L2230) |
| `GET /streamInfo/active-connections` | missing | [streamInfo.go:2231](../master/api/streamInfo.go#L2231) |
| `GET /streamInfo/summary` | missing | [streamInfo.go:2232](../master/api/streamInfo.go#L2232) |
| `GET /streamInfo/traffic/by-ip` | missing | [streamInfo.go:2235](../master/api/streamInfo.go#L2235) |
| `GET /streamInfo/traffic/by-country` | missing | [streamInfo.go:2236](../master/api/streamInfo.go#L2236) |
| `GET /streamInfo/traffic/by-port` | missing | [streamInfo.go:2237](../master/api/streamInfo.go#L2237) |
| `GET /streamInfo/traffic/by-protocol` | missing | [streamInfo.go:2238](../master/api/streamInfo.go#L2238) |
| `GET /streamInfo/traffic/by-ip-port` | missing | [streamInfo.go:2239](../master/api/streamInfo.go#L2239) |
| `GET /streamInfo/traffic/by-country-port` | missing | [streamInfo.go:2240](../master/api/streamInfo.go#L2240) |
| `GET /streamInfo/country/{country}` | missing | [streamInfo.go:2241](../master/api/streamInfo.go#L2241) |
| `GET /streamInfo/server/{upstream}/activity` | missing | [streamInfo.go:2242](../master/api/streamInfo.go#L2242) |
| `GET /streamInfo/stats/daily` | missing | [streamInfo.go:2245](../master/api/streamInfo.go#L2245) |
| `GET /streamInfo/stats/hourly` | missing | [streamInfo.go:2246](../master/api/streamInfo.go#L2246) |
| `GET /streamInfo/timeline` | missing | [streamInfo.go:2247](../master/api/streamInfo.go#L2247) |
| `GET /streamInfo/top/talkers` | missing | [streamInfo.go:2250](../master/api/streamInfo.go#L2250) |
| `GET /streamInfo/top/countries` | missing | [streamInfo.go:2251](../master/api/streamInfo.go#L2251) |
| `GET /streamInfo/top/ports` | missing | [streamInfo.go:2252](../master/api/streamInfo.go#L2252) |
| `GET /streamInfo/upstream` | missing | [streamInfo.go:2255](../master/api/streamInfo.go#L2255) |
| `GET /streamInfo/failed` | missing | [streamInfo.go:2256](../master/api/streamInfo.go#L2256) |
| `GET /streamInfo/flow` | missing | [streamInfo.go:2259](../master/api/streamInfo.go#L2259) |
| `GET /streamInfo/daterange` | missing | [streamInfo.go:2260](../master/api/streamInfo.go#L2260) |
| `GET /streamInfo/anomalies` | missing | [streamInfo.go:2261](../master/api/streamInfo.go#L2261) |

#### VMs and host tuning

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /virsh/getcpudisablefeatures` | implemented | [virsh.go:2507](../master/api/virsh.go#L2507) |
| `GET /virsh/getallvms` | implemented | [virsh.go:2508](../master/api/virsh.go#L2508) |
| `POST /virsh/createvm` | implemented | [virsh.go:2509](../master/api/virsh.go#L2509) |
| `POST /virsh/migratevm/{vm_name}` | excluded (deletion policy: migration undefines source domain) | [virsh.go:2511](../master/api/virsh.go#L2511) |
| `POST /virsh/updatecpuxml/{vm_name}` | implemented | [virsh.go:2512](../master/api/virsh.go#L2512) |
| `GET /virsh/cpuxml/{vm_name}` | implemented | [virsh.go:2513](../master/api/virsh.go#L2513) |
| `POST /virsh/updatevmxml/{vm_name}` | implemented | [virsh.go:2514](../master/api/virsh.go#L2514) |
| `GET /virsh/vmxml/{vm_name}` | implemented | [virsh.go:2515](../master/api/virsh.go#L2515) |
| `DELETE /virsh/deletevm/{vm_name}` | excluded (deletion policy) | [virsh.go:2519](../master/api/virsh.go#L2519) |
| `POST /virsh/startvm/{vm_name}` | implemented | [virsh.go:2520](../master/api/virsh.go#L2520) |
| `POST /virsh/shutdownvm/{vm_name}` | implemented | [virsh.go:2521](../master/api/virsh.go#L2521) |
| `POST /virsh/forceshutdownvm/{vm_name}` | implemented | [virsh.go:2522](../master/api/virsh.go#L2522) |
| `POST /virsh/restartvm/{vm_name}` | implemented | [virsh.go:2523](../master/api/virsh.go#L2523) |
| `POST /virsh/editvm/{vm_name}` | implemented | [virsh.go:2524](../master/api/virsh.go#L2524) |
| `POST /virsh/pausevm/{vm_name}` | implemented | [virsh.go:2525](../master/api/virsh.go#L2525) |
| `POST /virsh/resumevm/{vm_name}` | implemented | [virsh.go:2526](../master/api/virsh.go#L2526) |
| `GET /virsh/getvmbyname/{vm_name}` | implemented | [virsh.go:2527](../master/api/virsh.go#L2527) |
| `POST /virsh/removeiso/{vm_name}` | excluded (deletion policy) | [virsh.go:2528](../master/api/virsh.go#L2528) |
| `POST /virsh/vm_disk/{vm_name}` | implemented | [virsh.go:2529](../master/api/virsh.go#L2529) |
| `GET /virsh/vm_disk/{vm_name}` | implemented | [virsh.go:2530](../master/api/virsh.go#L2530) |
| `DELETE /virsh/vm_disk/{vm_name}/{vm_disk_id}` | excluded (deletion policy) | [virsh.go:2531](../master/api/virsh.go#L2531) |
| `POST /virsh/change_vm_network/{vm_name}` | implemented | [virsh.go:2533](../master/api/virsh.go#L2533) |
| `POST /virsh/change_vnc_password/{vm_name}` | implemented | [virsh.go:2534](../master/api/virsh.go#L2534) |
| `POST /virsh/add_ssh_key/{vm_name}` | implemented | [virsh.go:2535](../master/api/virsh.go#L2535) |
| `POST /virsh/novncvideo/add/{vm_name}` | implemented | [virsh.go:2536](../master/api/virsh.go#L2536) |
| `POST /virsh/novncvideo/remove/{vm_name}` | excluded (deletion policy) | [virsh.go:2537](../master/api/virsh.go#L2537) |
| `GET /virsh/novncvideo/{vm_name}` | implemented | [virsh.go:2538](../master/api/virsh.go#L2538) |
| `GET /virsh/ballooning/{vm_name}` | implemented | [virsh.go:2539](../master/api/virsh.go#L2539) |
| `POST /virsh/ballooning/{vm_name}` | implemented | [virsh.go:2540](../master/api/virsh.go#L2540) |
| `GET /virsh/hugepages/{vm_name}` | implemented | [virsh.go:2541](../master/api/virsh.go#L2541) |
| `POST /virsh/hugepages/{vm_name}` | implemented | [virsh.go:2542](../master/api/virsh.go#L2542) |
| `GET /virsh/machine_types/{machine_name}` | implemented | [virsh.go:2543](../master/api/virsh.go#L2543) |
| `POST /virsh/machine_type/{vm_name}` | implemented | [virsh.go:2544](../master/api/virsh.go#L2544) |
| `GET /virsh/kvm/{vm_name}` | implemented | [virsh.go:2545](../master/api/virsh.go#L2545) |
| `POST /virsh/kvm/{vm_name}` | implemented | [virsh.go:2546](../master/api/virsh.go#L2546) |
| `GET /virsh/hyperv/{vm_name}` | implemented | [virsh.go:2547](../master/api/virsh.go#L2547) |
| `POST /virsh/hyperv/{vm_name}` | implemented | [virsh.go:2548](../master/api/virsh.go#L2548) |
| `POST /virsh/cpupinning/{vm_name}` | implemented | [virsh.go:2551](../master/api/virsh.go#L2551) |
| `DELETE /virsh/cpupinning/{vm_name}` | excluded (deletion policy) | [virsh.go:2552](../master/api/virsh.go#L2552) |
| `GET /virsh/cpupinning/{vm_name}` | implemented | [virsh.go:2553](../master/api/virsh.go#L2553) |
| `GET /virsh/cputopology/{machine_name}` | implemented | [virsh.go:2554](../master/api/virsh.go#L2554) |
| `GET /virsh/tunedadm/{machine_name}` | implemented | [virsh.go:2555](../master/api/virsh.go#L2555) |
| `POST /virsh/tunedadm/{machine_name}` | implemented | [virsh.go:2556](../master/api/virsh.go#L2556) |
| `GET /virsh/irqbalance/{machine_name}` | implemented | [virsh.go:2557](../master/api/virsh.go#L2557) |
| `POST /virsh/irqbalance/{machine_name}` | implemented | [virsh.go:2558](../master/api/virsh.go#L2558) |
| `GET /virsh/coreisolation/{machine_name}` | implemented | [virsh.go:2559](../master/api/virsh.go#L2559) |
| `POST /virsh/coreisolation/{machine_name}` | implemented | [virsh.go:2560](../master/api/virsh.go#L2560) |
| `PUT /virsh/coreisolation/{machine_name}` | implemented | [virsh.go:2561](../master/api/virsh.go#L2561) |
| `DELETE /virsh/coreisolation/{machine_name}` | excluded (deletion policy) | [virsh.go:2562](../master/api/virsh.go#L2562) |
| `GET /virsh/hosthugepages/{machine_name}` | implemented | [virsh.go:2563](../master/api/virsh.go#L2563) |
| `POST /virsh/hosthugepages/{machine_name}` | implemented | [virsh.go:2565](../master/api/virsh.go#L2565) |
| `PUT /virsh/hosthugepages/{machine_name}` | implemented | [virsh.go:2566](../master/api/virsh.go#L2566) |
| `DELETE /virsh/hosthugepages/{machine_name}` | excluded (deletion policy) | [virsh.go:2567](../master/api/virsh.go#L2567) |
| `POST /virsh/moveDisk/{vm_name}/{dest_nfs}` | excluded (deletion policy: deletes source VM) | [virsh.go:2570](../master/api/virsh.go#L2570) |
| `POST /virsh/coldMigrate/{vm_name}/{dest_machine_name}` | excluded (deletion policy: undefines source domain) | [virsh.go:2571](../master/api/virsh.go#L2571) |
| `POST /virsh/cloneVM/{vm_name}/{dest_nfs}/{dest_machine_name}` | implemented | [virsh.go:2572](../master/api/virsh.go#L2572) |
| `GET /virsh/export/{vm_name}` | implemented | [virsh.go:2575](../master/api/virsh.go#L2575) |
| `PUT /virsh/import` | excluded (deletion policy: rollback can delete destination business disk) | [virsh.go:2576](../master/api/virsh.go#L2576) |
| `POST /virsh/backup/{vm_name}/{nfs_id}` | implemented | [virsh.go:2579](../master/api/virsh.go#L2579) |
| `GET /virsh/backups` | implemented | [virsh.go:2580](../master/api/virsh.go#L2580) |
| `GET /virsh/downloadbackup/{backup_id}` | implemented | [virsh.go:2581](../master/api/virsh.go#L2581) |
| `POST /virsh/useBackup/{backup_id}` | excluded (deletion policy: rollback deletes destination business disk) | [virsh.go:2582](../master/api/virsh.go#L2582) |
| `DELETE /virsh/deleteBackup/{backup_id}` | excluded (deletion policy) | [virsh.go:2583](../master/api/virsh.go#L2583) |
| `POST /virsh/autostart/{vm_name}` | implemented | [virsh.go:2585](../master/api/virsh.go#L2585) |
| `POST /virsh/vmlive/{vm_name}` | implemented | [virsh.go:2586](../master/api/virsh.go#L2586) |
| `POST /virsh/autobak` | implemented | [virsh.go:2589](../master/api/virsh.go#L2589) |
| `GET /virsh/autobak` | implemented | [virsh.go:2590](../master/api/virsh.go#L2590) |
| `PUT /virsh/autobak/{id}` | implemented | [virsh.go:2591](../master/api/virsh.go#L2591) |
| `DELETE /virsh/autobak/{id}` | excluded (deletion policy) | [virsh.go:2592](../master/api/virsh.go#L2592) |
| `PATCH /virsh/autopak/enable/{id}` | implemented | [virsh.go:2593](../master/api/virsh.go#L2593) |
| `PATCH /virsh/autopak/disable/{id}` | implemented | [virsh.go:2594](../master/api/virsh.go#L2594) |
| `GET /virsh/snapshot/{vm_name}` | missing (internal recovery helper; no direct tool credit) | [virsh.go:2516](../master/api/virsh.go#L2516) |
| `GET /virsh/hostsnapshot/{machine_name}` | missing (internal recovery helper; no direct tool credit) | [virsh.go:2564](../master/api/virsh.go#L2564) |

#### VM XML templates

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /virsh/xmltemplates/` | implemented | [virsh_xml_templates.go:22](../master/api/virsh_xml_templates.go#L22) |
| `GET /virsh/xmltemplates/{id}` | implemented | [virsh_xml_templates.go:23](../master/api/virsh_xml_templates.go#L23) |
| `POST /virsh/xmltemplates/` | implemented | [virsh_xml_templates.go:24](../master/api/virsh_xml_templates.go#L24) |
| `PUT /virsh/xmltemplates/{id}` | implemented | [virsh_xml_templates.go:25](../master/api/virsh_xml_templates.go#L25) |
| `DELETE /virsh/xmltemplates/{id}` | excluded (deletion policy) | [virsh_xml_templates.go:26](../master/api/virsh_xml_templates.go#L26) |

#### VM disks

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /vm-disk/list` | implemented | [vm_disk.go:103](../master/api/vm_disk.go#L103) |
| `POST /vm-disk/create` | excluded (deletion policy: rollback deletes disk resource) | [vm_disk.go:104](../master/api/vm_disk.go#L104) |
| `DELETE /vm-disk/{id}` | excluded (deletion policy) | [vm_disk.go:105](../master/api/vm_disk.go#L105) |
| `POST /vm-disk/{id}/grow` | implemented | [vm_disk.go:106](../master/api/vm_disk.go#L106) |

#### WireGuard

| Endpoint | Status | Route source |
| --- | --- | --- |
| `POST /wireguard/createvpn` | missing | [wireguard.go:203](../master/api/wireguard.go#L203) |
| `POST /wireguard/newPeer` | missing | [wireguard.go:204](../master/api/wireguard.go#L204) |
| `GET /wireguard/` | missing | [wireguard.go:205](../master/api/wireguard.go#L205) |
| `DELETE /wireguard/{id}` | excluded (deletion policy) | [wireguard.go:206](../master/api/wireguard.go#L206) |
