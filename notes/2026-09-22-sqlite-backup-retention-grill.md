# SQLite backup retention: Grill / Discovery Notes
Date: 2026-09-22 · Goal: Plan a reliable 14-day SQLite backup policy that survives daily restarts and continuous uptime, preventing unbounded backup disk usage.

## Summary / key decisions
- User reports that excessive SQLite backups filled the production disk and crashed the server.
- Requested retention: “14 days so 14 backups”.
- Must work with daily server restarts and a server running continuously for a month.
- User requested discovery and a written plan first, then authorized implementation with unit tests on 2026-09-22.
- Confirmed: retain today plus the previous 13 calendar days, at most 14 backups; delete older backups even after downtime.
- Confirmed: use the server's local timezone for calendar days.
- Plan written in `notes/2026-09-22-sqlite-backup-retention-plan.md`; implementation and local verification completed on 2026-09-23; production rollout remains.

## Q&A log

### Initial request — constraints
- Captured: “the retention should be 14 days so 14 backups”.
- Captured: “first create a plan use grill me skill and write the plan”.

### Q1 — Calendar retention versus successful backup count
- Asked: Should retention mean today plus the previous 13 calendar days, deleting older backups even after downtime?
- Captured: “Yes—14 calendar days, at most 14 backups (recommended)”.
- Decision: Missing days are not backfilled; fewer than 14 backups is acceptable after downtime or failures.
- Flags: None for this decision.

### Q2 — Daily timezone
- Asked: Which timezone should define a backup day? Recommended server local time, with reconciliation at startup and every 15 minutes.
- Captured: “Server’s local timezone (recommended)”.
- Decision: Preserve server-local calendar dates for retention and daily deduplication.
- Flags: None. The 15-minute reconciliation cadence is an implementation recommendation, not a separately requested business constraint.

### Implementation authorization
- Captured: “do it -- dont forget unit testing”.
- Decision: Implement the written plan and add/run unit tests. No production deployment was requested.

## Code findings
- `master/db/sqlite_backup.go` creates a backup immediately each time its loop starts, then every 24 hours.
- Names include seconds, allowing multiple backups on the same day following restarts.
- Existing retention is age-only: one calendar month, without a maximum file count.
- Pruning only happens after a successful backup, so a failed backup on a full disk prevents cleanup.
- Backup writes target the final filename directly, allowing failed or interrupted writes to leave apparent backups.
- `master/main.go` initializes the database and creates tables before starting the backup loop; cleanup needs an earlier startup entry point.
- Existing backup tests cover age pruning and modification-time fallback only.
- Baseline `go test ./db` from `master/` passed on 2026-09-22 (cached result); this does not verify the proposed behavior.

## Open flags (pending input)
- No unresolved user decisions are required to implement the written plan.
- Production file inventory, free space, and exact incident cause remain unverified -> inspect during production recovery/rollout.
- Migration and low-space policy are implementation recommendations: normalize recognized legacy snapshots; prune before writes; skip copying if insufficient headroom remains.

## Retrieval and handoff
- Capture and plan are cross-linked Markdown files, searchable in the repository with `rg`.
- No separate retrieval/indexing service is configured for this session; no external indexing was performed.
- Reusable decisions have been incorporated into the explicitly requested implementation plan. No standing skill or repository instruction changes were made.

## Outcome — 2026-09-23
- Implementation and unit tests completed; database tests, race detection, the full master test suite, and master build passed.
- Final details and validation evidence are recorded in the linked implementation plan and `docs/sqlite-backups.md`.
- No production deployment or recovery was performed.
