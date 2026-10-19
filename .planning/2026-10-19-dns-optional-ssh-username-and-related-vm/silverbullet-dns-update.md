---
project: HyperHive
type: behavior
status: implemented-locally
reviewed_on: 2026-10-19
source_revision: 5df87ec9923e330a6e25cc0cdb3110113fc28741 plus working-tree changes
sources:
  - master/api/dnsmasq.go
  - master/dnsmasq/dnsmasq.go
  - master/services/virsh.go
  - docs/dns.md
  - mcp/docs/areas/dns.md
  - mcp/dns_metadata_test.go
---

# DNS alias CRUD

## Request and response contracts

The existing authenticated DNS endpoints remain in place; no new route was added.

| Endpoint | Contract |
| --- | --- |
| POST /dnsmasq/alias/add | Required alias/IP strings; optional ssh_username and related_vm strings. Creates or replaces the alias, including metadata. Success is HTTP 200 with an empty body. |
| GET /dnsmasq/alias/all | Returns an aliases array. Each record on the updated backend has alias, ip, ssh_username and related_vm. Existing records have empty metadata strings. |
| GET /dnsmasq/alias/get?alias=&ip= | Returns the boolean exists for the exact alias/IP pair. This is not a metadata/detail lookup. |
| DELETE /dnsmasq/alias/remove | Still accepts alias/IP JSON and returns HTTP 200; a missing pair returns 404. Metadata is not needed to remove the record. |

Example create/update request:

```json
{"alias":"app.home","ip":"192.0.2.10","ssh_username":"ubuntu","related_vm":"app-vm"}
```

Example list response:

```json
{
  "aliases": [
    {"alias":"legacy.home","ip":"192.0.2.1","ssh_username":"","related_vm":""},
    {"alias":"app.home","ip":"192.0.2.10","ssh_username":"ubuntu","related_vm":"app-vm"}
  ]
}
```

The fields are independent and completely optional. ssh_username is manually entered text and never runs SSH. related_vm is the exact VM name from GET /virsh/getallvms; the frontend should use that inventory for its dropdown. Selection does not populate or synchronize the DNS IP or SSH username. Unknown VM names return 400; inventory lookup failure returns 500 without saving. Invalid JSON/types return 400. API errors are plain text.

Omitted fields and explicit empty strings both clear the previous values on upsert. Include both current metadata values when preserving them. Metadata-only edits work without changing alias/IP. Only an identical alias, IP and metadata is a no-op. No data migration is required.

## Execution and state

Aliases use address=/alias/ip directives matching the domain and subdomains. Escaped JSON metadata comments are stored beside those directives in /etc/hyperhive/dnsmasq-aliases.conf. Legacy directives without metadata read as empty strings. Slash-separated multidomain aliases remain supported.

A process mutex serializes DNS reads and mutations. Changes ensure inclusion in the LAN dnsmasq configuration and restart the systemd LAN service and applicable directly managed WireGuard instance. This lock does not cover independent processes or direct file edits.

## VM deletion and partial failures

After successful hypervisor removal in the shared VirshService.DeleteVM path, all aliases explicitly linked to that VM name are removed, followed by one DNS reload. Multiple linked records are removed; other records, including records sharing the IP, remain. Failed VM removal leaves DNS unchanged. Direct hypervisor deletions outside HyperHive bypass this cleanup. The shared service path is also used by other backend operations such as VM replacement during restore.

DELETE /virsh/deletevm/{vm_name} still returns 200 and VM deleted successfully on success. If DNS cleanup fails after removal, it returns 500 explaining that the VM was deleted but DNS cleanup failed. Refresh the VM and DNS lists after such a partial failure; do not assume the VM survived.

DNS mutations write configuration before service reload. Include/reload failure does not roll back the file. No cross-system atomicity or automatic rollback is promised. Alias/IP validation, persistence and reload failures use the existing 500 handling unless an endpoint explicitly maps them otherwise.

## MCP contracts

- dns_aliases_list returns the complete list object above as both structured content and JSON text fallback, preserving extension fields and blank values. Discovery documents ssh_username and related_vm. Older APIs omitting metadata are preserved without fabricated fields; present metadata must be strings.
- dns_alias_add accepts required alias/IP plus optional ssh_username/related_vm. Omitted arguments stay omitted on the wire; explicit empty strings remain explicit. Both clear stored values on the updated backend. It returns only {"success":true}; call dns_aliases_list for saved records.
- dns_alias_get remains alias/IP-only and returns {"exists":true} or false. It accepts no metadata inputs.

MCP rejects unknown, explicit-null and wrong-type inputs before dispatch. Its unsafe whitespace/control guard applies to alias/IP, not manual metadata. It delegates VM existence validation to the backend, suppresses raw error bodies and never retries mutations. Deploy updated backend and MCP together for metadata writes because older backends may ignore new request fields.

No DNS or VM deletion tool was added. DNS coverage remains 3/4 raw endpoints and 3/3 permitted; total MCP registry and API coverage remain unchanged. See [[knowledge/projetos/HyperHive/features/mcp/index]].

## Verification and deployment boundary

Local backend storage/API tests cover legacy blank records, optional fields, metadata edits, escaping, multidomain aliases, multiple linked aliases, unrelated same-IP preservation, concurrency and failure handling. An isolated loopback gRPC fixture verifies DNS cleanup runs only after successful VM removal. Full master tests passed during initial implementation; affected backend suites and focused race checks passed during MCP integration. A dnsmasq syntax-only check accepted the metadata comment format.

MCP contract tests exercise authenticated SDK HTTP and the actual stdio executable, optional/empty/invalid inputs, discovery, request encoding, structured/text metadata, malformed metadata responses and existing error/no-retry contracts. No production DNS restart, hypervisor operation or server deployment occurred. Frontend controls are not part of this backend/MCP change.

## Relationships

- [[knowledge/projetos/HyperHive/features/networking/dns-dhcp/index]]
- [[knowledge/projetos/HyperHive/features/mcp/index]]
