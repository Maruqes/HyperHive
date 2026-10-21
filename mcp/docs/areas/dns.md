# DNS alias tools

Registration and safety: [shared policy and classifications](../tool-safety.md).

The DNS tools call the authenticated HyperHive API through the shared client.
They use the same registration on embedded HTTP and standalone stdio.

| Tool | Endpoint | Inputs | Result |
| --- | --- | --- | --- |
| `dns_aliases_list` | `GET /dnsmasq/alias/all` | None | Complete API object containing `aliases`, with `alias`, `ip`, `ssh_username` and `related_vm` on the updated backend |
| `dns_alias_get` | `GET /dnsmasq/alias/get` | Required strings `alias`, `ip` | Complete API object containing boolean `exists`; no metadata lookup |
| `dns_alias_add` | `POST /dnsmasq/alias/add` | Required strings `alias`, `ip`; optional strings `ssh_username`, `related_vm` | `{ "success": true }` after successful empty API response; does not return the saved record |

## Optional metadata and return shape

`ssh_username` is manually entered text, stored without executing SSH commands.
`related_vm` is the exact VM `name` returned by `list_vms` (the existing
`GET /virsh/getallvms` API). These inputs work independently. The backend validates
that a nonempty related VM exists; no extra inventory request is made by MCP.

Example `dns_alias_add` arguments:

```json
{"alias":"app.home","ip":"192.0.2.10","ssh_username":"ubuntu","related_vm":"app-vm"}
```

Both metadata fields may be omitted or explicitly set to `""`. Either clears the
previous stored value on upsert; include the current values to preserve them.
Explicit empty strings are forwarded unchanged and omitted fields stay omitted.
Use `dns_aliases_list` to read the saved values. Its updated API response is also
returned as MCP structured content and JSON text fallback:

```json
{
  "aliases": [
    {"alias":"legacy.home","ip":"192.0.2.1","ssh_username":"","related_vm":""},
    {"alias":"app.home","ip":"192.0.2.10","ssh_username":"ubuntu","related_vm":"app-vm"}
  ]
}
```

Existing records need no migration; the updated backend returns blank metadata.
Older API responses that omit metadata are preserved without fabricated fields.
Present metadata must be strings. Deploy the updated backend and MCP together for
metadata writes; older backends can silently ignore new request fields.

## Validation and mutations

Aliases must be nonblank and, after trimming, contain no spaces, tabs, newlines,
carriage returns or commas. IPs must parse as IPv4 or IPv6. Missing required,
unknown, explicit-null and incorrectly typed inputs are rejected before API calls.
GET trims the query values as its handler does. POST rejects whitespace and
control characters in alias/IP before backend access. These constraints do not
apply to manual metadata text, which the backend escapes as JSON comments.
Valid values are forwarded unchanged; slash-separated multidomain aliases remain
supported. The existence-check tool accepts only alias/IP, not metadata inputs.

`dns_alias_get` checks the exact alias/address pair. `exists: false` is a successful
missing result. Lists preserve order and return an empty array for an empty
configuration. Response extension fields and large integer precision are retained
in serialized structured responses and text fallback. SDK clients decoding
arbitrary JSON objects into floats may need the text fallback for exact integers.

Adding an alias replaces its IP and metadata or appends a new alias. Metadata-only
edits work; only an identical alias, IP and metadata is a no-op. Changes write the
managed configuration, ensure its include directive and restart applicable DNS
instances. Configuration can remain changed after include/restart failure. DNS
writes are serialized within one master process. MCP never retries mutations
and never fabricates resource identifiers or saved record responses.

An unknown related VM returns HTTP 400. Inventory lookup, persistence and DNS
reload failures return HTTP 500. MCP reports safe status errors without exposing
raw backend error bodies.

## VM deletion and policy

The backend's shared VM deletion path removes all aliases explicitly linked to a
successfully deleted VM and reloads DNS once. Aliases sharing its IP without that
link remain. Failed VM removal keeps DNS intact; later DNS cleanup failure is a
partial failure reported by the API. Direct hypervisor deletions bypass this
cleanup. This backend behavior does not expose an MCP deletion capability.

`DELETE /dnsmasq/alias/remove` and VM deletion remain excluded by repository
policy. No removal tool, generic dispatcher or rollback deletion was added.
Existing scope remains three DNS tools: 3/4 raw endpoints (75%), 3/3 permitted
endpoints (100%). Registry and overall API endpoint counts are unchanged.

## Verification

Sources: `master/api/dnsmasq.go`, `master/dnsmasq/dnsmasq.go`,
`master/services/virsh.go`, and `docs/dns.md`. `TestDNSContracts` and
`TestDNSOptionalMetadataContracts` cover authenticated SDK HTTP and the actual
stdio executable, discovery, exact payloads, optional/blank values, metadata in
both output forms, malformed metadata responses, safe failures and no retries.
`TestReviewDNSWritesRejectUnsafeConfigValues` covers unsafe alias/IP rejection
through HTTP, stdio and the direct client. Backend tests separately cover legacy
config, metadata persistence, multidomain aliases and VM deletion ordering using
temporary files and isolated loopback gRPC. No production DNS or hypervisor was
contacted, and no deployment occurred.
