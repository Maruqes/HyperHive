# 01. A failed SMART read erases a disk's health state and sends no alert

| | |
| --- | --- |
| **Severity** | High |
| **Feature** | Disk health forecasting |
| **Modules** | `master`, plus `mcp` and HyperHiveFront for display |
| **Evidence** | Reproduced by `TestAuditSmartReadFailureKeepsCriticalState` |
| **User decision** | Alert timing. Default below; confirm it if you change it. |

## Summary

When the master cannot read SMART from a disk it already scored, it overwrites the score, band and wear forecast with empty values and sends nothing. A disk that gets so bad it stops answering SMART looks unknown instead of critical, and nobody is told.

## What is impacted

- **Alerts:** no notification fires when a disk stops answering SMART, even if it was critical.
- **`GET /diskhealth` and `GET /diskhealth/{serial}`:** `score` is null, `band` is empty and `smartAvailable` is false.
- **MCP `list_disk_health`:** same. An agent reading it sees no risk.
- **SMART page and per-disk page:** the badge changes to a grey "No SMART data". The page then says the disk "does not report SMART", which is false for a disk that was reporting until now.
- **Wear forecast:** `wear_out_at` is cleared too.

Not impacted: stored samples, and `alert_band` and `alert_counters`, so a later good read does not alert twice.

## How it is impacted

1. A disk has been scored for days. It is `critical` with score 10, and one alert was already sent.
2. The disk degrades further and `smartctl` hangs, so the 60 s timeout in `master/smartdisk/smartdisk.go:13` expires.
3. `recordDisk` gets an error and takes the "no SMART" branch.
4. `UpdateDiskScore` writes `smart_available=0, score=NULL, band='', wear_out_at=NULL`.
5. `evaluateAlert` is never called.
6. `UpsertDiskSeen` already ran, so `last_seen` is fresh. `lastPollAt` looks healthy and the disk never retires.

Two consequences matter:

- **The worst case is the silent one.** Failing to answer SMART is a common late symptom of a dying drive, and this feature exists to catch dying drives.
- **It also hits healthy disks.** One timeout on a busy disk, or a slave restart during a poll, blanks a good disk for 6 hours until the next poll.

## Root cause

`master/services/disk_health.go:214` treats "never had SMART" and "had SMART and stopped answering" as the same case:

```go
if smartErr != nil || info == nil || (info.GetPowerOnHours() <= 0 && info.GetTemperatureC() <= 0) {
    reason := "No SMART data"
    if smartErr != nil {
        reason = "SMART read failed: " + smartErr.Error()
    }
    return db.UpdateDiskScore(ctx, seen.Serial, db.DiskScoreUpdate{Reason: reason})
}
```

`UpdateDiskScore` (`master/db/disk_health.go:505`) writes all five columns every time, so the zero-valued struct wipes them.

## Evidence

```
poll 1: score=10 band="critical" alerts=1
poll 2: score=<nil> band="" smartAvailable=false alerts=0 reason="SMART read failed: rpc error: code = DeadlineExceeded"
```

## How to start fixing

Separate three cases in `recordDisk`:

| Case | Today | Should be |
| --- | --- | --- |
| Never scored, read fails or returns nothing | Blank row with a reason | **Keep as is** |
| Scored before, read fails | Wiped, silent | Keep score, band and wear; record the failure; alert |
| Scored before, read succeeds but is empty | Wiped, silent | Same as above |

Steps:

1. Call `db.GetDisk` before the failure branch and check `SmartAvailable && Score != nil`.
2. Add a separate DB function, for example `RecordDiskReadFailure(ctx, serial, reason, at)`, that leaves `score`, `band` and `wear_out_at` alone.
3. Add columns to `disk_health_disks`: a failure count, the last failure time, and whether the alert was sent. Reset them on the next successful read.
4. Send a `health.disk_health` alert. **Recommended timing:** alert on the first failure if the stored band is `at_risk` or `critical`; otherwise on the second consecutive failure, so one timeout on a healthy disk stays quiet. Severity at least `error`.
5. Alert once per outage, using the stored flag. Do not repeat every 6 hours.
6. Expose the failure in `DiskHealthView` (for example `readFailedAt`), then in the MCP client struct and the frontend types.
7. Change the frontend text so a disk with a score and a read failure says "SMART read failed" with a time, not "does not report SMART".

A smaller first step, if you want one: stop wiping, and put the failure in `reason`. That fixes the display but needs the columns before it can alert only once.

## Traps

- **`CREATE TABLE IF NOT EXISTS` will not add columns** to the live `disk_health.db`. Follow `ensureNotesTitleColumn` in `master/db/notes.go:29`, but use `DiskHealthDB`, not `DB`. If 03 lands too, share one migration helper.
- **An existing test pins the never-scored case.** `TestDiskHealthSkipsAndFailures` (`master/services/disk_health_test.go:245`) requires the `BROKEN` disk to stay unscored with `smartctl timeout` in its reason. Keep that passing; if it fails you have changed the wrong case.
- **`History` needs no change**, since no sample is written for a failed read. Check that charts still render when the latest sample is old.
- **`reason` is shown to users.** `smartErr.Error()` is raw RPC text, while 500 responses in this feature deliberately hide internal errors. Consider a plain message and log the detail.
- **Do not touch `alert_band` on a failed read,** or the next good read will alert again.

## Files to touch

- `master/services/disk_health.go`: `recordDisk`, `evaluateAlert`, `DiskHealthView`
- `master/db/disk_health.go`: schema, migration, new update function, `diskColumns`, `scanDisk`
- `mcp/internal/client/disk_health.go`: `DiskHealthDisk`
- HyperHiveFront: `types/diskhealth.ts`, `services/diskhealth.ts`, `utils/diskhealth.ts` (`hasScore`, `diskHealthSummary`), `app/smartdisk/[serial].tsx:147-151`, `components/diskhealth/DiskHealthOverview.tsx:184`

## Done when

- `TestAuditSmartReadFailureKeepsCriticalState` passes, adjusted if your alert timing differs.
- `TestDiskHealthSkipsAndFailures` still passes.
- New tests cover: one failure on a healthy disk stays quiet; repeated failures alert once; recovery clears the state with no duplicate alert.
- The UI and MCP show a disk that stopped answering as a problem.
- `docs/disk-health.md` describes the behaviour (polling step 4 and the Alerts section).
