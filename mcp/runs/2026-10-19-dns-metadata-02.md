# DNS MCP metadata compatibility — 2026-10-19

## Scope and source contracts

User requested updating the existing DNS MCP tools for the new backend fields and
saving the return contracts in SilverBullet. No new tool, endpoint, deletion
capability, production access or deployment is in scope.

- `dns_alias_add`: required alias/IP; independent optional strings ssh_username
  and related_vm. Typed pointers retain omission versus explicit empty strings
  on the wire. Either clears the old value on the updated backend. Username is
  manual text; related_vm is a VM name from list_vms, validated by the backend.
  Success remains acknowledgement-only `{ "success": true }`.
- `dns_aliases_list`: documents and preserves both metadata fields in structured
  content and text, including blank values for legacy records from the updated
  backend. Extension fields remain intact. Present metadata must be strings;
  older responses omitting the fields are preserved without fabricated values.
- `dns_alias_get`: remains alias/IP-only and existence-only; no metadata inputs.
- Backend shared VM deletion removes linked DNS records after successful VM
  removal. Neither DNS nor VM deletion is exposed by this update.

Sources: master/api/dnsmasq.go, master/dnsmasq/dnsmasq.go,
master/services/virsh.go, mcp/internal/client/dns.go,
mcp/internal/tools/dns.go and mcp/server_dns.go.

## Verification

- Existing DNS baseline contracts and unsafe-write regressions passed.
- New HTTP/actual-executable stdio tests failed on missing metadata discovery,
  then passed after implementation. Covered optional/omitted/explicit-blank
  inputs, exact authenticated POST body, manual text escaping, null/wrong types,
  unchanged get inputs, structured/text response preservation and invalid output
  metadata. Existing tests retain status suppression and single-attempt checks.
- Full MCP `go test ./...` and `go vet ./...` passed.
- Full MCP `go test -race ./...` passed.
- Affected backend dnsmasq/API/services suites and focused race checks passed.
- Review restored the pre-existing slash-separated multidomain alias behavior
  inadvertently restricted by the initial backend change; a storage regression
  verifies its metadata and IPv6 round trip. Existing MCP acceptance remains.
- No real DNS service restart, production API call or hypervisor action.

## Coverage and artifacts

Existing registry remains 105 tools; no new registrations. Existing endpoint
inventory consistency was checked: 316 unique rows, 105 implemented, 158 missing
and 53 excluded; 33.23% overall. DNS remains 3/4 raw (75%) and 3/3 permitted
(100%). No fresh full audit of unrelated API contracts is claimed.

README, DNS area docs and the coverage report describe the return shapes and
compatibility. Current digest/manifest and inventory-consistency evidence are in
`.planning/2026-10-19-dns-optional-ssh-username-and-related-vm/`.

## SilverBullet

User explicitly authorized updating canonical DNS behavior, the MCP behavior
page and operational memory. Save only verified local behavior, exact outputs,
validation and deployment limits. All three pages were updated and their persisted content verified through
SilverBullet readback. No deployment was performed.
