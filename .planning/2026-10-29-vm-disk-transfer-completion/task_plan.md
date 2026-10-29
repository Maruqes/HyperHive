# Managed disk transfers
Implement vm-disk-transfer-completion-plan.md; preserve pre-existing download edits.

## Phases
1. Inspect reuse paths and contracts — complete.
2. Agent staging, safe image validation, download framing and capacity — complete, focused tests pass.
3. Durable DB operations, authenticated import/move, persistent VM update and recovery — complete; failure-path and race checks pass.
4. Browser UI and shared upload client — complete; 14 focused tests pass.
5. Final validation, recovery documentation and canonical knowledge update — complete for local implementation; live verification remains a release prerequisite.

## Next Step
No implementation work remains. Authorized disposable NFS/libvirt and deployed-browser verification is required before production rollout.

## Constraints
No live operations/deployment. Keep VM stopped as operator responsibility. Never replace unrelated files or delete uncertain copies. Existing modifications belong to the user.

## Validation issues
Native libvirt pkg-config headers are unavailable; use libvirt_dlopen for local tests. Full frontend typecheck retains the pre-existing dashboard.tsx strokeWidth error; no new errors remain in the latest check.
