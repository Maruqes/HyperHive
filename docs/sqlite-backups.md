# SQLite database backups

The master keeps **at most one completed backup per local calendar day, for today
and the previous 13 days**. Backups live beside `data.db` and are named
`data.db.bak.YYYY-MM-DD`. The server's local timezone defines the date boundary.
Missing days are not backfilled; initial startup, downtime and failed backups can
leave fewer than 14 snapshots.

The master checks immediately when its backup loop starts and every 15 minutes.
It reads completed filenames on every check, so daily restarts, repeated restarts
within a day, and continuous uptime use the same policy. A failed attempt is
retried on the next check. Each attempt, including lock acquisition and copying,
has a 10-minute timeout.

## Cleanup and migration

A cleanup pass runs before database initialization, and again before each backup
attempt, even if today's snapshot already exists. Files outside the 14-day date
window are removed first. Future-dated managed backups are also outside that
window; keep the server clock and timezone configured correctly.

On the first updated startup, legacy filenames such as
`data.db.bak.2026-09-22_15-04-05` are consolidated. For each retained day, the
newest valid legacy snapshot is renamed to the daily name and the other
snapshots are removed. Corrupt legacy candidates are rejected using SQLite
integrity checks. If a daily snapshot already exists, it takes precedence.
Legacy files older than the retention window are removed without reading their
contents.

Only exact managed backup filenames and the fixed temporary filenames are
cleaned automatically. Unrecognized/manual files, symlinks, directories, the
live database and its WAL/SHM files are preserved. Unrecognized backup-prefixed
files are logged for operator review; they are not included in the managed
backup count. Cleanup errors are logged and prevent another backup from being
created until the error is resolved.

## Crash and disk-space behavior

All backup and cleanup operations hold an exclusive lock on the database
directory. The operating system releases it if the process exits or crashes.
This also serializes multiple cooperating master processes on the same host.
The database directory must be on a filesystem that supports `flock`, atomic
rename, and file/directory synchronization, as used by the Linux master.

New snapshots use SQLite's online backup API and include committed WAL data.
The copy is written to `data.db.bak.in-progress`, closed, checked with
`PRAGMA integrity_check`, synchronized, and renamed to its final daily name.
Incomplete copies and their temporary sidecars are removed after errors or on
the next startup. A successful snapshot is never overwritten on a same-day
restart. New backup files use permissions `0600`; migrated files retain their
existing permissions.

Before copying, free space must cover the estimated snapshot size plus the
larger of **256 MiB or 10% of the snapshot size**. Insufficient space skips the
attempt and logs the required and available byte counts. Retained snapshots are
not deleted merely to force a new copy to fit. Reserve capacity for the retained
backups, one temporary snapshot, and live database/WAL growth. The space check
cannot reserve capacity against other writers or guarantee that other workloads
will not fill the disk.

## Rollout and recovery

1. Inspect the production database directory and available filesystem space.
   Confirm that the excessive files have the managed legacy filename format.
2. Start the updated master. Check the logs for pruning results or cleanup
   failures, then verify that the directory has at most 14 daily snapshots.
3. Open today's snapshot separately in read-only mode, run
   `PRAGMA integrity_check`, and query representative application records.
4. Check again after the next local date change (within 15 minutes while the
   backup loop is running). The oldest day should rotate out and a new snapshot
   should appear.

If the system cannot start the executable or reach early cleanup, an operator
must first reclaim space using an inspected inventory. Do not delete the live
`data.db`, `data.db-wal`, or `data.db-shm`, and do not use a blanket `*.bak.*`
deletion. Unknown legacy sidecars/manual files require separate inspection.
These instructions do not deploy the change or perform production recovery.

## Tests

From `master/`:

```sh
go test ./db -count=1
go test -race ./db -count=1
go build -o /tmp/hyperhive-master .
```

Tests drive the actual scheduling loop through all 15-minute checks over 31 days,
recreate runners for daily and repeated same-day restarts, and assert the exact
retained dates. Additional coverage includes calendar boundaries and DST,
legacy migration, disk-space/write/deletion failures, cancellation, concurrent
runners, subprocess crashes with active locks, and readable snapshots from a
live WAL database. All backup fixtures and crash helpers use temporary test
directories.
