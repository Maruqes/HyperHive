# Findings
- Initial working tree: modified master/npm/upstream_apply.go and upstream_capabilities.go; untracked upstream_read_test.go. Preserve.
- VM baseline 56/82; overall 110/322. Current source/report already includes upstreams.
- Creation accepts complete 14-field configuration; successful response is HTTP 201 text, no ID.
- Autostart takes auto_start boolean; live flag takes enable boolean. GetVM includes autoStart/isLive.
- Schedule create/update accept HH:MM clocks, positive frequency/retention/NFS, window >=45 minutes; update always enables. Enable route spelling is autopak.
- Existing SSH helper uses virt-customize and recursively changes .ssh ownership. Domain XML alone cannot recover. Existing export streams shut-off primary disk; reuse as recovery artifact before injection. Backend does not provide an atomic snapshot/injection lock against external operations.
- Knowledge discovery found dedicated MCP registry-and-coverage, tool-contracts, and vm-approval-and-recovery pages in addition to an oversized older index. Next bounded retrieval: canonical page metadata/headers and relevant sections to avoid duplicating mutable facts.
- Actual baseline HEAD is e8813e4be4fb416190f91f7922b42e61d660b099; previous report recorded its parent as the implementation revision. Corrected report to actual HEAD plus working changes. Unrelated NPM working files preserved.
