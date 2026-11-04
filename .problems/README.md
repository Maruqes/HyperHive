# Open problems from the 2026-11-04 audit

These briefs are for the agent that will fix the bugs found while auditing the two features added on 2026-11-03: **VM backup revert** and **disk health forecasting**. Each brief stands alone: it says what is impacted, how, why, how to start fixing it, what can go wrong, and when it is done.

Nothing here has been fixed yet.

## Start here

1. Read `AGENTS.md` and `master/AGENTS.md`. Read `mcp/AGENTS.md` too if you touch MCP.
2. Read the feature docs: `docs/disk-health.md` and `docs/vm-backup-revert.md`.
3. Run the proofs to get a baseline: `bash .problems/proofs/run.sh`. All 6 should fail today (see [Proofs](#proofs)).
4. Check `git status` in both repositories before editing (see [Work already in progress](#work-already-in-progress)).
5. Work through the problems in the order below.

## Problems

| ID | Severity | Feature | Problem | Evidence | User decision needed |
| --- | --- | --- | --- | --- | --- |
| [01](01-smart-read-failure-hides-dying-disk.md) | High | Disk health | A failed SMART read erases the disk's score and sends no alert | Proof test | Alert timing (default given) |
| [02](02-revert-swaps-disk-without-final-check.md) | High | Revert | The disk is swapped without re-checking that the VM is still off | Proof test | No |
| [03](03-alert-signals-lost-with-slave-loop.md) | Medium | Disk health | Real SMART signals stopped alerting when the slave loop was removed | Proof test | **Yes:** which signals, and their weights |
| [04](04-revert-recovery-runs-before-nfs.md) | Medium | Revert | Restart recovery runs before NFS is mounted | Proof test (simulated mount) | No |
| [05](05-nvme-self-tests-never-parsed.md) | Medium | Disk health | NVMe self-test results are never parsed | Proof test, upstream source | No |
| [06](06-sata-ssd-wear-always-zero.md) | Low | Disk health | SATA SSDs always show `Wear 0%` | Live data | Option A or B (default given) |
| [07](07-smartctl-error-stores-full-json.md) | Low | Disk health | `smartctl_error` stores the whole smartctl JSON document | Proof test, not seen live | No |
| [08](08-minor-issues.md) | Low | Both | Six smaller issues | Source review | One (see the brief) |

**Severity:** High means data loss, or a failing disk going unnoticed. Medium means a feature silently does less than documented, or needs a rare trigger to hurt. Low means misleading output or wasted resources.

## Recommended order

1. **02 first.** It is a small change that protects VM data, and no real revert has been run yet.
2. **01 next.** It is the most serious disk health bug. It adds a schema change, so design it with 03 in mind.
3. **05, then 07.** Both are small slave-side parsing fixes and can ship in one slave upgrade.
4. **04.** Small in code, but read its traps first because it changes startup behaviour.
5. **03 once the user has chosen.** It is the largest change and touches every layer.
6. **06 and 08** when convenient.

Ask the user about 03 early, so the answer is ready when you reach it.

## How the problems interact

- **01 and 03** both add columns to `disk_health.db`. `CREATE TABLE IF NOT EXISTS` will not add a column to the live file, so each needs a real migration.
- **05** changes which disks show `warning` on the SMART page, because of the override in `assessHealth`. Read its traps.
- **05 and 07** both touch the slave and take effect only after a slave upgrade. 01, 02, 03 and 04 are master-side.
- **02 and 04** both edit `master/services/vm_backup_revert.go`, so do them one after the other.

## State of the code

- **Baseline:** HyperHive `b5bf605`, HyperHiveFront `5b16ea5`.
- **Line numbers** in the briefs match those commits for Go code. Frontend references match the working tree on 2026-11-04 around 00:15 UTC, and that code is moving, so search by symbol if a line is off.
- **Deployment:** disk health is running on the master (`list_disk_health` returned 7 disks on 2 hosts, last poll 2026-11-03 22:05 UTC). Slave versions were not checked. Revert deployment is unverified and **no real revert has ever been run**.

### Work already in progress

Someone else was editing both repositories during this audit. Do not revert or overwrite these uncommitted changes:

- **HyperHive:** `docs/disk-health.md`.
- **HyperHiveFront:** `app/smartdisk/[serial].tsx`, `utils/diskhealth.ts`, `metro.config.js`, `package.json`, `package-lock.json`, `tests/diskhealth.test.js`, `tests/jest.diskhealth.config.cjs`, plus the new `components/diskhealth/` and `tests/smartdisk-disk-page.test.js`.

Frontend changes for 01, 03 and 06 land in those same files. If they are still uncommitted, tell the user before editing them.

### Already resolved, do not redo

- **Out-of-order history responses on the disk page:** fixed in the uncommitted work by `requestRef` in `app/smartdisk/[serial].tsx`.
- **A false `Available spare 0%` on SATA SSDs:** avoided by `hasSpare` in `components/diskhealth/DiskHealthOverview.tsx`.

## Proofs

`bash .problems/proofs/run.sh` runs `proofs/audit_master_test.go` and `proofs/audit_slave_test.go` through `go test -overlay`, so nothing is added to the modules.

Each proof asserts the **correct** behaviour. `FAIL` means the bug is still present and `PASS` means it is fixed. When you fix a problem, move its proof into the real test file for that package, so it stays as a regression test.

| Proof | Problem | Note |
| --- | --- | --- |
| `TestAuditSmartReadFailureKeepsCriticalState` | 01 | Update it if your alert timing needs a second failed poll |
| `TestAuditRenameRechecksVMState` | 02 | Should pass as written |
| `TestAuditDroppedSignalsStillAlert` | 03 | Update it to match the signals the user picks |
| `TestAuditRecoveryBeforeMountRemovesTemp` | 04 | Rewrite it: the fix defers recovery, see brief 04 |
| `TestAuditNVMeSelfTestLogIsParsed` | 05 | Should pass as written |
| `TestAuditSmartctlErrorIsNotTheWholeJSON` | 07 | Should pass as written |

The proofs reuse helpers from `master/services/*_test.go`, so they break if those helpers are renamed.

## Rules for every fix

- **Build and test** in each touched module: `go build -tags libvirt_dlopen ./...` and `go test -tags libvirt_dlopen ./...`. There is no `go.work`, so run commands inside the module.
- **Frontend checks** run in HyperHiveFront: `npx tsc --noEmit` and `npx jest --config tests/jest.diskhealth.config.cjs` (or `jest.vm-backup-revert.config.cjs`).
- **Never** run `make run` or `make first`. They use sudo and change the host.
- **No privileged tests.** Run `slave/btrfs` tests only with `-run TestParseBoolJSON`.
- **Live infrastructure:** HyperHive MCP tools act on real systems. Read-only tools are fine. Ask the user before any mutation, and before running a real revert.
- **MCP:** revert stays out of MCP by user decision, and no MCP tool may delete data.
- **Proto:** edit `api/proto/*.proto` and regenerate with root `make setup`. Regeneration causes version-only churn in unrelated `*_grpc.pb.go` files, so revert those and keep the real change.
- **Commit or push only when the user asks.**

## After each fix

1. Update `docs/disk-health.md` or `docs/vm-backup-revert.md`.
2. In SilverBullet, remove the item from the "Open … defects (audit 2026-11-04)" section and correct the feature description:
   - `knowledge/projects/HyperHive/features/storage/smart/health-forecasting`
   - `knowledge/projects/HyperHive/features/backups/vm/revert-in-place`
3. Delete the problem's brief from this folder, or mark it fixed with the commit, and update the table above.

## Known failures that are not these bugs

Do not chase these:

- `master/api/npmapi`: `TestMCPNginxThroughHandlers` and `TestMCPProxyMutationsThroughHandlers` (their clients lack elicitation).
- `master/virsh`: the test build fails on `VMXMLHasDiskPath`.
- HyperHiveFront `tsc`: one error at `app/dashboard.tsx:823`, last changed 2026-10-31.

## What the audit did not cover

- No real revert was run, and no fix has been tried on real hardware.
- The frontend was read and its jest and `tsc` checks were run, but it was not exercised in a browser.
- Features from before 2026-11-03, such as Automations and the power schedule, were only looked at where they touch these two features.
- The only live call was the read-only `list_disk_health`.
- These passed review and need no work: notification severity ordering, routing of the new events, safety backup IDs, history compaction, and three `-race` runs over `services`, `db` and `diskhealth`.
