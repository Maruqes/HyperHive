# MCP correctness review

## Scope
Review all pending MCP changes and affected backend snapshot contracts. Preserve existing work. Use local disposable fixtures; no production mutations.

## Phases
1. Inventory, instructions and recorded context — complete.
2. Trace shared transport, exact argument decoding, approvals, recovery, feature clients and backend contracts — complete.
3. Evaluate defects and required fixes — complete; no new actionable defect found, no application changes required.
4. Run full normal/race/vet checks and affected backend suites; record limitations — complete.

## Next Step
Deliver review result and link review.md. No required local work remains.

## Errors Encountered
Initial sandboxed Go tests could not write the default build cache. Approved rerun outside sandbox passed. A source search named a nonexistent slave/virsh/virsh.go file; inspected the actual snapshot and feature files instead. A file inventory with no nested AGENTS.md matches exited 1; applicable MCP instructions were already read.
