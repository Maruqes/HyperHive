
## 2026-10-19 — Selected MCP features and independent correctness review

Added 68 selected tools, bringing the local registry to 105: DNS3, Notes5, SPA2, ISO2 and VM56. No new deletion exception was granted. VM medium+ operations require per-invocation human picker approval and durable actual pre-change configuration capture; SSH injection is withheld because prior guest-file state cannot be recovered. Two additive snapshot helpers/RPCs require matching master/slave versions when deployed. Configuration backups do not imply disk snapshots or automatic rollback.

Additional agent/coordinator review reproduced and fixed integer-ID rounding that could select a different mutation target, incomplete pre-growth VM disk recovery, unsafe DNS whitespace/control writes and proxy-list numeric precision. Both transports have regressions, and full local normal/race/vet and affected backend checks passed after the fixes. API inventory: 105 implemented/316 total (33.23%), 158 missing, 53 excluded. No server access, deployment, live hypervisor/firewall/DNS/NFS qualification, commit or push occurred. User explicitly authorized saving this update.

Canonical behavior, risk/backup requirements, exact coverage and validation limits: [[knowledge/projetos/HyperHive/features/mcp/index]]. Existing operational entries remain historical snapshots; no host inventory or deployment state changed.
