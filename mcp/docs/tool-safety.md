# MCP tool registration and safety

All MCP tools use `internal/tools.RegisterTool(registry, "tool_name", definition, level)`.
`RegisterAll` composes Proxies, NGINX, Upstreams, Celld, DNS, Notes, SPA, ISOs and
VMs explicitly. Each area has a narrow API interface and one named registration
function. The same registry is used by HTTP and stdio; nested optional capability
registration has been removed. This is locally implemented source, not deployment evidence.

## Where to review each area's safety

Each function below contains every direct registration for its area. Every row
shows the literal tool name and an explicit `safety.Low`, `safety.Medium`,
`safety.High` or `safety.VeryHigh`. Handlers, schemas and recovery callbacks are
prepared in definition helpers; helpers do not choose levels or register tools.
The catalog name must match its definition or server construction fails.

| Area | Complete registration function |
| --- | --- |
| VMs | [RegisterVMs](../internal/tools/vms.go) |
| Celld | [RegisterCelld](../internal/tools/celld.go) |
| DNS | [RegisterDNS](../internal/tools/dns.go) |
| ISOs | [RegisterISOs](../internal/tools/isos.go) |
| Notes | [RegisterNotes](../internal/tools/notes.go) |
| SPA | [RegisterSPA](../internal/tools/spa.go) |
| Proxies | [RegisterProxies](../internal/tools/proxies.go) |
| NGINX | [RegisterNginx](../internal/tools/nginx.go) |
| Upstreams | [RegisterUpstreams](../internal/tools/upstreams.go) |

```go
func RegisterCelld(server *Registry, api CelldAPI) {
    server = server.Area("celld")
    RegisterTool(server, "create_celld_fleet", defineCreateCelldFleet(api), safety.High)
    // Every other Celld tool is declared in this same function.
}
```

## Execution policy

| Level | Server requirement |
| --- | --- |
| `safety.Low` | Normal authentication, schema and domain validation; no HH safety prompt. |
| `safety.Medium` | Validate the target identifiers and exact arguments, summarize expected impact, and durably record a private preflight before dispatch. No human prompt. |
| `safety.High` | Explicit per-invocation human approve/cancel form, bound to exact arguments; missing/declined/cancelled/expired/replayed/altered approval blocks dispatch. |
| `safety.VeryHigh` | The same mandatory approval, with disruption, data loss and recovery limitations highlighted. |

The level is declared once and determines `hyperhive/risk`, `hyperhive/area`,
`hyperhive/required_action`, descriptions and execution policy. Duplicate names,
missing/invalid levels, missing endpoint mappings and HTTP DELETE registrations
fail server construction. Existing deletion-effect exclusions and narrow VM
exceptions still apply. `delete_celld_fleet` was removed at the user's request on
2026-10-21; it is not discoverable or callable. No new feature endpoint was added.

## Medium checks and privacy

Agents are instructed to double-check target, exact arguments and expected impact.
The server independently performs schema/type checks and domain validation before
recording tool, area, time, target, argument SHA-256, redacted argument summary,
expected impact, validated checks and outcome. These are observable input and
operation checks, not evidence of the model's internal reasoning or a guarantee
that the backend will accept the request. Backend preconditions remain authoritative.

Configure `MCP_REVIEW_DIR` (or embedded `Config.SafetyReviewDir`) as an absolute
private directory; otherwise the OS user cache under `hyperhive/mcp/reviews` is
used. Directories are 0700 and files 0600, with file/directory sync before dispatch.
A registry accepts at most 10,000 directory entries and 64 KiB per record; it fails
closed at capacity until an operator archives records. Capacity coordination is
per registry; separate processes should use separate review directories when a
strict aggregate quota is required. Storage/validation/cancellation failures block
dispatch. Final outcome recording is best effort; `preflight_passed` alone does
not prove execution or success. Notes, credentials, XML and arbitrary configuration
strings are represented by sizes rather than raw content. Targets and ordinary
numeric/Boolean settings are visible; the complete canonical argument digest
preserves binding and exact integer identity.

Medium reviews are separate from retained VM recovery artifacts. Existing VM
snapshot, disk-copy, locking, backup failure and backup-path return contracts
remain required at their existing levels, including Medium. No automatic retry
or rollback was added. Refer to [VM recovery](areas/vms.md#approval-and-retained-prior-state).

## Approval clients and trust boundary

High/VeryHigh always request the server form, independently of client tool-call
approval hints. The form has no approving default and accepts only an explicit
`approve` choice. Automatic tool permission, blanket chat consent or an argument
cannot satisfy this gate. Modern input continuations use `tool_approval` and a
single-use, ten-minute random request state; legacy clients use elicitation.
Changing registry process invalidates pending requests. Stateful session IDs and
client initialization identity are included in binding. Stateless HTTP has no
per-user identity beyond its fixed Bearer principal; client names are not
independent authentication. Shared-key holders share administrator authority.

Use the human form in both “Ask for Approval” and “Approve for me”. Standard MCP
returns a client response and cannot attest who selected it or inspect the chat
mode. An automatically answering client is not qualified for human-only approval.
Actual Codex UI qualification in both modes remains a release check; SDK fixture
acceptance does not prove that a person saw a prompt. HH does not change client
permissions or reviewer settings. See [connection setup](../CONNECTING.md).

## Classification rationale and preflight/recovery requirements

Ordinary inventory/diagnostic reads are Low. Note create/replace/patch and ISO
download initiation are Medium: bounded operations with validated identifiers,
required content or URL/share constraints and a recorded impact review. DNS
upserts/reloads, routing/access-list changes, pool activation, fleet create/start/
stop and sensitive credential/certificate exports are High due to service impact
or secret exposure. VM levels retain their individually reviewed declarations;
VeryHigh covers force stop, full XML replacement, host boot settings, retention
schedules and SSH customization. Each tool's discovery description explains its
specific effects, backend prerequisites and partial-failure limits. The tables
below are a snapshot exported from actual discovery on 2026-10-21.

All rows retain schema/type and domain validation. Medium rows additionally
require the private review described above; High/VeryHigh rows require the bound
form. VM writes/transfers retain the feature-specific recovery checks in their
[contracts](areas/vms.md); other areas do not claim newly implemented backups.

## Registered tool inventory

| Area | Tool | Level | API endpoint |
| --- | --- | --- | --- |
| celld | `create_celld_fleet` | high | `POST /celld/fleets` |
| celld | `get_celld_credentials` | high | `GET /celld/fleets/{id}/credentials` |
| celld | `get_celld_events` | low | `GET /celld/fleets/{id}/events` |
| celld | `get_celld_health` | low | `GET /celld/health` |
| celld | `get_celld_logs` | low | `GET /celld/fleets/{id}/logs` |
| celld | `list_celld_fleets` | low | `GET /celld/fleets` |
| celld | `start_celld_fleet` | high | `POST /celld/fleets/{id}/start` |
| celld | `stop_celld_fleet` | high | `POST /celld/fleets/{id}/stop` |
| dns | `dns_alias_add` | high | `POST /dnsmasq/alias/add` |
| dns | `dns_alias_get` | low | `GET /dnsmasq/alias/get` |
| dns | `dns_aliases_list` | low | `GET /dnsmasq/alias/all` |
| isos | `list_isos` | low | `GET /isos/` |
| isos | `start_iso_download` | medium | `POST /isos/download` |
| nginx | `create_404_host` | high | `POST /404/create` |
| nginx | `create_access_list` | high | `POST /access-lists/create` |
| nginx | `create_redirect` | high | `POST /redirection/create` |
| nginx | `create_stream` | high | `POST /stream/create` |
| nginx | `disable_404_host` | high | `POST /404/disable` |
| nginx | `disable_redirect` | high | `POST /redirection/disable` |
| nginx | `disable_stream` | high | `POST /stream/disable` |
| nginx | `download_certificate` | high | `POST /certs/download` |
| nginx | `enable_404_host` | high | `POST /404/enable` |
| nginx | `enable_redirect` | high | `POST /redirection/enable` |
| nginx | `enable_stream` | high | `POST /stream/enable` |
| nginx | `list_404_hosts` | low | `GET /404/list` |
| nginx | `list_access_lists` | low | `GET /access-lists/list` |
| nginx | `list_certificates` | low | `GET /certs/list` |
| nginx | `list_dns_providers` | low | `GET /certs/dns-providers` |
| nginx | `list_redirects` | low | `GET /redirection/list` |
| nginx | `list_streams` | low | `GET /stream/list` |
| nginx | `renew_certificate` | high | `POST /certs/renew` |
| nginx | `update_404_host` | high | `PUT /404/edit` |
| nginx | `update_redirect` | high | `PUT /redirection/edit` |
| nginx | `update_stream` | high | `PUT /stream/edit` |
| notes | `create_note` | medium | `POST /notes/` |
| notes | `get_note` | low | `GET /notes/{id}` |
| notes | `list_notes` | low | `GET /notes/` |
| notes | `patch_note` | medium | `PATCH /notes/{id}` |
| notes | `replace_note` | medium | `PUT /notes/{id}` |
| proxies | `create_proxy` | high | `POST /proxy/create` |
| proxies | `disable_proxy` | high | `POST /proxy/disable` |
| proxies | `enable_proxy` | high | `POST /proxy/enable` |
| proxies | `list_proxies` | low | `GET /proxy/list` |
| proxies | `setup_frontend_proxy` | high | `POST /proxy/setupFrontEnd` |
| proxies | `setup_secure_frontend_proxy` | high | `POST /proxy/setupFrontEndSecure` |
| proxies | `update_proxy` | high | `PUT /proxy/edit` |
| spa | `list_spa_allows` | low | `GET /spa/allow/{port}` |
| spa | `list_spa_ports` | low | `GET /spa/` |
| upstreams | `create_upstream` | high | `POST /upstream/create` |
| upstreams | `get_upstream` | low | `GET /upstream/{id}` |
| upstreams | `get_upstream_capabilities` | low | `GET /upstream/capabilities` |
| upstreams | `list_upstreams` | low | `GET /upstream/list` |
| upstreams | `update_upstream` | high | `PUT /upstream/{id}` |
| vms | `add_vm_ssh_key` | very high | `POST /virsh/add_ssh_key/{vm_name}` |
| vms | `add_vm_video` | high | `POST /virsh/novncvideo/add/{vm_name}` |
| vms | `attach_vm_disk` | high | `POST /virsh/vm_disk/{vm_name}` |
| vms | `backup_vm` | high | `POST /virsh/backup/{vm_name}/{nfs_id}` |
| vms | `clone_vm` | high | `POST /virsh/cloneVM/{vm_name}/{dest_nfs}/{dest_machine_name}` |
| vms | `create_vm` | high | `POST /virsh/createvm` |
| vms | `create_vm_automatic_backup` | very high | `POST /virsh/autobak` |
| vms | `create_vm_xml_template` | medium | `POST /virsh/xmltemplates/` |
| vms | `disable_vm_automatic_backup` | medium | `PATCH /virsh/autopak/disable/{id}` |
| vms | `download_vm_backup` | low | `GET /virsh/downloadbackup/{backup_id}` |
| vms | `edit_vm` | high | `POST /virsh/editvm/{vm_name}` |
| vms | `enable_vm_automatic_backup` | very high | `PATCH /virsh/autopak/enable/{id}` |
| vms | `export_vm` | medium | `GET /virsh/export/{vm_name}` |
| vms | `force_shutdown_vm` | very high | `POST /virsh/forceshutdownvm/{vm_name}` |
| vms | `get_vm` | low | `GET /virsh/getvmbyname/{vm_name}` |
| vms | `get_vm_ballooning` | low | `GET /virsh/ballooning/{vm_name}` |
| vms | `get_vm_core_isolation` | low | `GET /virsh/coreisolation/{machine_name}` |
| vms | `get_vm_cpu_disable_features` | low | `GET /virsh/getcpudisablefeatures` |
| vms | `get_vm_cpu_pinning` | low | `GET /virsh/cpupinning/{vm_name}` |
| vms | `get_vm_cpu_topology` | low | `GET /virsh/cputopology/{machine_name}` |
| vms | `get_vm_cpu_xml` | low | `GET /virsh/cpuxml/{vm_name}` |
| vms | `get_vm_host_hugepages` | low | `GET /virsh/hosthugepages/{machine_name}` |
| vms | `get_vm_hugepages` | low | `GET /virsh/hugepages/{vm_name}` |
| vms | `get_vm_hyperv` | low | `GET /virsh/hyperv/{vm_name}` |
| vms | `get_vm_irqbalance` | low | `GET /virsh/irqbalance/{machine_name}` |
| vms | `get_vm_kvm` | low | `GET /virsh/kvm/{vm_name}` |
| vms | `get_vm_tuned_profiles` | low | `GET /virsh/tunedadm/{machine_name}` |
| vms | `get_vm_video` | low | `GET /virsh/novncvideo/{vm_name}` |
| vms | `get_vm_xml` | low | `GET /virsh/vmxml/{vm_name}` |
| vms | `get_vm_xml_template` | low | `GET /virsh/xmltemplates/{id}` |
| vms | `grow_vm_disk` | high | `POST /vm-disk/{id}/grow` |
| vms | `list_vm_attached_disks` | low | `GET /virsh/vm_disk/{vm_name}` |
| vms | `list_vm_automatic_backups` | low | `GET /virsh/autobak` |
| vms | `list_vm_backups` | low | `GET /virsh/backups` |
| vms | `list_vm_disks` | low | `GET /vm-disk/list` |
| vms | `list_vm_machine_types` | low | `GET /virsh/machine_types/{machine_name}` |
| vms | `list_vm_xml_templates` | low | `GET /virsh/xmltemplates/` |
| vms | `list_vms` | low | `GET /virsh/getallvms` |
| vms | `pause_vm` | high | `POST /virsh/pausevm/{vm_name}` |
| vms | `remove_vm_isos` | high | `POST /virsh/removeiso/{vm_name}` |
| vms | `replace_vm_host_core_isolation` | very high | `PUT /virsh/coreisolation/{machine_name}` |
| vms | `replace_vm_host_hugepages` | very high | `PUT /virsh/hosthugepages/{machine_name}` |
| vms | `restart_vm` | very high | `POST /virsh/restartvm/{vm_name}` |
| vms | `resume_vm` | medium | `POST /virsh/resumevm/{vm_name}` |
| vms | `set_vm_autostart` | medium | `POST /virsh/autostart/{vm_name}` |
| vms | `set_vm_ballooning` | high | `POST /virsh/ballooning/{vm_name}` |
| vms | `set_vm_cpu_pinning` | high | `POST /virsh/cpupinning/{vm_name}` |
| vms | `set_vm_host_core_isolation` | very high | `POST /virsh/coreisolation/{machine_name}` |
| vms | `set_vm_host_hugepages` | very high | `POST /virsh/hosthugepages/{machine_name}` |
| vms | `set_vm_hugepages` | high | `POST /virsh/hugepages/{vm_name}` |
| vms | `set_vm_hyperv` | high | `POST /virsh/hyperv/{vm_name}` |
| vms | `set_vm_irqbalance` | high | `POST /virsh/irqbalance/{machine_name}` |
| vms | `set_vm_kvm` | high | `POST /virsh/kvm/{vm_name}` |
| vms | `set_vm_live` | high | `POST /virsh/vmlive/{vm_name}` |
| vms | `set_vm_machine_type` | high | `POST /virsh/machine_type/{vm_name}` |
| vms | `set_vm_network` | high | `POST /virsh/change_vm_network/{vm_name}` |
| vms | `set_vm_tuned_profile` | high | `POST /virsh/tunedadm/{machine_name}` |
| vms | `set_vm_vnc_password` | high | `POST /virsh/change_vnc_password/{vm_name}` |
| vms | `shutdown_vm` | high | `POST /virsh/shutdownvm/{vm_name}` |
| vms | `start_vm` | medium | `POST /virsh/startvm/{vm_name}` |
| vms | `update_vm_automatic_backup` | very high | `PUT /virsh/autobak/{id}` |
| vms | `update_vm_cpu_xml` | high | `POST /virsh/updatecpuxml/{vm_name}` |
| vms | `update_vm_xml` | very high | `POST /virsh/updatevmxml/{vm_name}` |
| vms | `update_vm_xml_template` | high | `PUT /virsh/xmltemplates/{id}` |
