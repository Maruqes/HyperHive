# Findings

- DNS CRUD uses master/api/dnsmasq.go and master/dnsmasq/dnsmasq.go; state is /etc/hyperhive/dnsmasq-aliases.conf.
- VM identity is its globally managed name; getAllVms uses VirshService.GetAllVms.
- VirshService.DeleteVM is also used by NFS deletion and VM restore, so shared cleanup belongs there, after successful RemoveVM.
- Existing DNS writes precede service reload; preserve truthful partial-failure errors.
- SilverBullet DNS notes describe the original two-field contract and lack of locking. No live hosts were contacted.
- Repository has extensive unrelated user changes; preserve them.

- MCP list already preserves all API extension fields but discovery only describes alias/IP. MCP add currently rejects the new fields. Keep get inputs and existence-only output unchanged.
- MCP policy prohibits deletion tools; document backend cascade without exposing deletion.
- Follow-up includes explicit authorization to update SilverBullet; no additional approval is needed.

- Follow-up review found the initial backend field change accidentally rejected slash-separated multidomain aliases. Restored the pre-existing supported form and added persistence coverage rather than weakening the MCP contract test.
- MCP list discovery documents metadata but preserves older API omission without inventing fields; present metadata must be strings. Add uses separate typed optional pointer fields, keeping get inputs alias/IP only and preserving explicit empty values on the wire.
