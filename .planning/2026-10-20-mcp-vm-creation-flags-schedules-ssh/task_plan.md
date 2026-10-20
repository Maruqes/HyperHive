# MCP VM implementation

## Scope
Seven tools: create_vm, set_vm_autostart, set_vm_live, create_vm_automatic_backup, update_vm_automatic_backup, enable_vm_automatic_backup, add_vm_ssh_key. User explicitly authorized these formerly excluded behaviors; other exclusions remain.

## Phases
1. Contract review and baseline: complete
2. Implement seven typed client/tools with approval and recovery: complete
3. Verify HTTP and executable stdio, negative cases, race/vet: complete
4. Refresh coverage, documentation and canonical knowledge: complete

5. Diagnose missing approval picker and improve client-response diagnostics: complete

## Next Step
Local diagnostics fix verified. To execute protected tools in Codex, enable MCP elicitation in the effective permission policy and start a fresh chat. No policy setting changed here.

## Constraints
Preserve unrelated master/npm working changes. No production mutation or deployment. SSH requires shut-off guest and retained primary disk export before injection; capture transfer manifest privately and fail closed. External hypervisor operations remain outside registry lock.
