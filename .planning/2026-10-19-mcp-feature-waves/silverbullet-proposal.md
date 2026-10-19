# Proposed SilverBullet update — 2026-10-19

Status: historical initial proposal. Superseded by silverbullet-mcp-final.md and silverbullet-memory-final.md, successfully saved and read back on 2026-10-19 after additional review. This describes source behavior and local validation, not a deployment.

## knowledge/projetos/HyperHive/features/mcp/index

- Selected MCP batch adds 68 named tools: DNS 3, Notes 5, SPA 2, ISOs 2, VMs 56. Shared HTTP/stdio discovery now contains 105 tools; previous NGINX/Celld capabilities are preserved.
- DNS, Notes and ISOs cover all permitted endpoints. SPA includes selected management reads; creation remains excluded because replacement/rollback deletes firewall rules and can destroy an existing allow set. No new deletion-policy exception was granted.
- VM tools expose low/medium/high/very high risk. Medium+ requires explicit per-invocation user approval through the MCP harness form picker, with argument-bound one-use approval and fail-closed handling. No self-approval argument exists.
- Before VM XML/settings mutations, capture actual secure inactive/current XML and relevant previous settings, including CPU pinning. Host boot setters preserve every kernel's arguments, default kernel and current command line. Private retained before.json records use 0700 directories/0600 files, file/directory sync and no automatic cleanup; failed snapshot/save blocks mutation. Configure absolute MCP_VM_BACKUP_DIR, otherwise the process user's cache under hyperhive/mcp/vm-backups is used. Records can contain secrets and must remain private.
- Backups preserve configuration/metadata, not guest disks or lost RAM, and do not imply automatic rollback. Serialization covers one registry only. All 23 VM deletion exclusions remain; SSH injection is unavailable because actual prior guest authorized_keys/skel content and ownership cannot be retrieved. Original permitted VM selection: 56/57.
- Added authenticated no-store GET /virsh/snapshot/{vm_name} and GET /virsh/hostsnapshot/{machine_name}, backed by additive RPCs. Updated master and slaves are required before using the protected operations; missing snapshot support fails closed. No existing getter or mutator semantics changed.
- VM export/backup download stream to private files on the MCP server filesystem and return byte count/SHA256. ISO download, VM backup and clone report acceptance/background initiation only, without claiming completion.
- Source-reviewed inventory: 316 endpoints, 105 implemented, 158 missing, 53 excluded (33.23% raw). Two recovery helpers earn no separate tool coverage. Full MCP normal/race/vet, affected master checks, focused slave snapshot normal/race/vet and generated RPC compile passed locally. Actual HTTP/stdio and disposable authenticated handler/RPC fixtures were used; no live hypervisor/DNS/firewall/NFS qualification or deployment occurred.
- Evidence: repository mcp/README.md, mcp/UPDATE_STATE.md, mcp/docs/areas/{dns,notes,spa,isos,vms}.md and dated run checkpoints. Source digest: 073bfc0342b1741b1e98a350cc59f9b47fa345d2f502762ea31852ae2dde031d, revision 5df87ec9923e330a6e25cc0cdb3110113fc28741 plus working-tree changes.

## knowledge/infraestrutura/HyperHive/memoria-operacional

2026-10-19 — Local selected MCP implementation accepted: 68 additions, 105 tools total. VM runtime approval and durable pre-change configuration capture are mandatory; SSH injection is withheld for missing recoverable guest-file state. Two additive snapshot RPCs/helpers require matching master/slave versions when deployed. Local tests/race/vet passed; no servers changed or deployment performed. Link to [[knowledge/projetos/HyperHive/features/mcp/index]] for contracts, coverage and limitations.
