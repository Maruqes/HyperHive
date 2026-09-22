# SQLite backup retention implementation and verification plan

Date: 2026-09-22
Status: Implemented and locally verified on 2026-09-23. Production deployment/recovery has not been performed.
Discovery: [Interview and decisions](2026-09-22-sqlite-backup-retention-grill.md).

## Objective

Prevent unbounded SQLite backup accumulation. Keep one successful backup per day, at most 14 retained backups, with behavior independent of process uptime and restarts. Verify both daily restarts and at least 31 days of uninterrupted operation using simulated time.

## Evidence and failure modes

`master/db/sqlite_backup.go` currently runs immediately at startup, then on a process-relative 24-hour ticker. Every run creates a filename containing seconds. Cleanup uses one calendar month of age retention, without daily deduplication or a count limit. Cleanup is skipped when backup creation fails. Writes go directly to the final filename, so interrupted writes can leave misleading artifacts.

`master/main.go` starts the loop after database initialization and table creation. A full disk can therefore block startup before the cleanup loop is reached. The code supports the reported failure mechanism; production disk contents and logs have not been inspected to prove the exact incident cause.

## Retention contract

Confirmed by the user: use today plus the previous 13 calendar dates, keep at most one successful backup for each date, and delete older managed backups even after downtime. Fewer than 14 is expected during initial rollout, downtime, or failed backup days. Never manufacture historical backups for days when the service was down.

Confirmed daily boundary: use the server's local timezone, matching existing timestamp parsing. Reconcile at startup and every 15 minutes; no exact wall-clock execution time was requested. A restart should create today's backup only if one has not already completed; repeated restarts on the same date should skip creation.

## Implementation sequence

1. **Separate inventory, policy, and backup execution.** Introduce a small internal runner with injectable clock, database path, and scheduling dependencies. Inventory disk files on each reconciliation so filenames and completed files supply persistent state. Avoid an in-memory “last backup” flag as the authority. Preserve public entry points where practical.

2. **Bound and safely migrate existing backups.** Recognize exact legacy `data.db.bak.YYYY-MM-DD_HH-MM-SS` filenames and a new daily filename such as `data.db.bak.YYYY-MM-DD`. Resolve same-day legacy duplicates deterministically, retaining the newest usable snapshot for each day, then apply the selected retention contract and a hard maximum of 14. Verify a legacy candidate before treating it as a completed daily backup; fall back to an earlier valid same-day candidate when necessary. Do not follow symlinks or delete directories, the live database, its WAL/SHM files, or unrelated/manual files based on a broad prefix or modification time. Log unrecognized matching files for operator review. Handle new-format crash leftovers explicitly.

3. **Make cleanup independent of successful writes.** Reconcile and prune before allocating a new backup, even if today's backup already exists or a later backup attempt fails. Add a filesystem-only cleanup pass early in startup, before database initialization can need free space. Serialize cleanup and creation with a lock released automatically on process exit, covering overlapping processes as well as repeated calls. If deletions fail, report each failure and avoid adding backups when the cap cannot be maintained. Never convert a cleanup error into a server-wide fatal error.

4. **Publish only complete snapshots.** Keep the SQLite online backup API for a consistent copy of a live WAL database. Write into one owned temporary file on the same filesystem, finish and close the backup and destination handles while checking errors, validate the destination, sync as appropriate, then atomically rename to the daily final path. Sync the directory for durable publication on supported production platforms. On cancellation or failure, remove owned partial output and associated temporary SQLite sidecars. On restart, clean abandoned temporary artifacts while holding the lock. Never overwrite a completed daily backup just because the service restarted.

5. **Use a schedule that survives both uptime patterns.** Reconcile immediately at startup and every 15 minutes. Compare the current calendar date with the on-disk inventory on each pass; do not wait 24 hours from every process start. Retry failed daily backups on later passes and stop promptly on context cancellation. Capture one logical date per attempt so crossing midnight cannot publish under inconsistent dates. Apply the inclusive date window from today minus 13 days through today; future-dated managed files are outside that window and cannot suppress today's backup. Log any observed clock rollback. A large system clock change can expire backups according to the new local date, so operational time synchronization remains necessary.

6. **Protect disk headroom.** Before copying, estimate the snapshot size and check available filesystem space with a documented safety margin for application writes. If insufficient space remains after allowed pruning, skip the backup, log the reason, and retry later; do not delete still-retained recovery points merely to force success. Recheck publication and write errors because preflight checks cannot reserve space against other writers. The retained-count cap alone cannot guarantee disk safety as database size or unrelated disk usage grows. Budget for up to 14 completed backups plus one temporary snapshot and database/WAL growth; no temporary-file accumulation is allowed.

7. **Add focused logs and operator documentation.** Report created/skipped/failed status, pruning results, counts, and relevant paths without secrets. Document retention boundaries, schedule, legacy migration, temporary space needs, and recovery when the service cannot start. Explain that deleting managed excess backups is part of the first updated startup. Production rollout and incident recovery commands must use an inspected file inventory, not an unreviewed wildcard deletion.

## Verification matrix

Use temporary directories and an injected clock; do not wait a month or exercise production files.

| Scenario | Required result |
| --- | --- |
| First run; repeated calls and many same-day restarts | One readable completed backup for that date |
| Restart once per day for 31 days | After each reconciliation, at most 14 completed backups with the exact expected dates |
| Continuous service for 31 days | Scheduler triggers daily creation and cleanup without process restarts; oldest date rotates out |
| Restart repeatedly before 24 hours of uptime | New calendar days still receive backups; same-day duplicates never accumulate |
| Midnight, month/year boundaries, leap day, DST | Calendar retention follows the chosen timezone and does not depend on 24-hour day lengths |
| Downtime longer than 14 days | Selected downtime retention behavior is followed; no fabricated catch-up snapshots |
| Hundreds of legacy files and same-day duplicates | First reconciliation converges to the selected daily policy and count limit before the next copy |
| Corrupt/empty legacy snapshot | Does not suppress a usable same-day backup; valid fallback candidate is handled |
| Full disk or injected write failure | Eligible old/excess files are pruned first; failed copy leaves no final backup; later pass can recover |
| Permission or removal failure | Error is visible; count does not grow through repeated backup attempts |
| Interrupted backup or process kill | No incomplete final file; abandoned owned temporary files are removed on restart |
| Crash around atomic publication | Next startup recognizes a completed daily file or safely retries; no duplicate completion |
| Concurrent calls/processes | One active writer/cleanup owner; at most one successful backup per day |
| Clock moves backward or forward | Predictable reconciliation with no future-file suppression of today's backup or unbounded growth |
| Live SQLite database in WAL mode | Open snapshot separately, run integrity checks, and query known committed rows |
| Unknown names, symlinks, directories, unrelated files | Preserved; live database and WAL/SHM remain untouched |
| Context cancellation | Scheduler and copying stop promptly; partial output is cleaned |

Exercise the actual scheduler through a fake timer/clock in addition to calling the reconciliation function. Recreate the runner between simulated restarts so tests prove that state is on disk. Use a subprocess test for lock release and crash recovery where needed. Inject filesystem/space failures for deterministic disk-full and deletion-failure coverage.

Planned commands from `master/`: `go test ./db`, `go test -race ./db`, and a master build with the repository's required native dependencies available. Broaden to master tests if integration changes warrant it. Record actual results and any environment blockers; planning does not establish that the fix works.

Baseline check during planning: `go test ./db` passed on 2026-09-22 (cached). The current backup tests do not cover the new contract; the full verification matrix remains to be implemented and run.

## Completion and rollout criteria

- The discovery decisions are reflected in code and tests.
- Both 31-day uptime scenarios pass and assert the exact retained set, not just a file count.
- Real SQLite snapshots pass integrity and data checks, including WAL contents.
- Legacy accumulation, failed writes, restarts, and concurrency cannot create unbounded managed files.
- Review the implementation diff and test evidence before production rollout.
- During rollout, inspect available space and inventory first, then confirm initial pruning and one valid daily snapshot. If the old service cannot initialize, perform targeted operator cleanup before starting the update.
- Verify retained file count and available space across the next daily transition. Automated ongoing monitoring is outside this planning task unless requested.

## Scope

SQLite control-plane database backups only. VM backup policies and unrelated backup scripts are outside this change. This plan does not claim that the production server has been recovered or that the proposed fix has been tested.


## Implementation and verification record — 2026-09-23

Implemented in `master/db/sqlite_backup.go`, with early cleanup wired into
`master/main.go`. Operational documentation is in
[SQLite backups](../docs/sqlite-backups.md).

- Daily filenames persist completion across restarts; reconciliation runs at startup and every 15 minutes.
- Cleanup enforces the inclusive 14-date window, consolidates validated legacy duplicates, and runs before copying and before database initialization.
- Directory locks serialize processes without allocating a lock file on a full disk. Temporary files are cleaned after failures and process crashes.
- Snapshots are validated and synchronized before atomic publication. Legacy migration synchronizes the selected file and directory before deleting alternatives.
- Free-space checks require the estimated snapshot plus the larger of 256 MiB or 10% headroom.
- A cancellation test exposed SQLite's native busy wait inside backup steps. Backup connections now temporarily use short busy waits, with the application timeout restored before the connection returns to the pool.

Verification completed:

| Check | Result |
| --- | --- |
| `go test ./db -count=1 -coverprofile=/tmp/hyperhive-db-coverage.out` | Passed; 16 backup test functions plus subtests and existing database tests |
| `go test -race ./db -count=1` | Passed |
| `go test ./... -count=1` from `master/` | Passed |
| `go build -o /tmp/hyperhive-master-retention .` from `master/` | Passed |
| `git diff --check` | Passed |

The new backup implementation has 82.7% statement coverage (210/254 statements).
The wider database package's coverage is lower because unrelated database
features are outside these backup tests. The 31-day continuous test drives all
2,976 scheduled checks; the restart test recreates the runner four times per
day. Tests also cover 350 legacy duplicates, corrupt newest-candidate fallback
with distinct snapshot contents, DST/leap/year boundaries, downtime, concurrent
runners, subprocess lock release, pre/post-publication crash recovery,
insufficient space, write/deletion failures, cancellation, midnight crossings,
symlink protection, and real committed WAL data read from the resulting snapshot.

Commands used `GOCACHE=/tmp/hyperhive-go-cache` because the default cache is
read-only in this workspace sandbox. The broad suite initially hit sandbox
restrictions on loopback sockets; rerunning with sandbox escalation passed the
existing HTTP tests. No dependencies or production data were changed.

Remaining operational step: deploy the updated master and follow the inspected
inventory, initial-cleanup, and next-day checks in the operational documentation.
