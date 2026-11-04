# 03. Real SMART signals stopped alerting when the slave loop was removed

| | |
| --- | --- |
| **Severity** | Medium |
| **Feature** | Disk health forecasting |
| **Modules** | `master`, `mcp`, HyperHiveFront |
| **Evidence** | Reproduced by `TestAuditDroppedSignalsStillAlert` |
| **User decision** | **Required.** Which signals to restore, and their weights. |

## Summary

Commit `4fd4dbc` removed the slave's 6-hourly SMART alert loop and moved alerting to the master's score. The score only knows a subset of what the loop checked, so some serious signals now produce neither an alert nor a lower score. `docs/disk-health.md:7` mentions only the self-test alert moving over.

## What is impacted

- **Alerts:** a disk with end-to-end errors or spin retries no longer alerts.
- **Health score:** the disk stays `Good · 100`.
- **Consistency on the SMART page:** the badge can read `SMART · warning` next to `Good · 100`, because the slave's own assessment still counts these signals. The page explains that the two differ, but a user will still trust the green one.

Weights and bands were a deliberate design choice (`Decisions` in the SilverBullet page), which is why the user must choose here.

## How it is impacted

Before: the slave checked about 30 conditions. Many were noise, but some were real.

After: the score reads only pending, offline uncorrectable, reallocated, reported uncorrectable, media errors, CRC, critical warning, spare, wear, self-test and the SMART verdict.

| Signal | Attribute | Old alert | In the score now | Recommendation |
| --- | --- | --- | --- | --- |
| End-to-end errors | 184 | Critical | No | **Restore** |
| Spin retry count | 10 | Critical | No | **Restore** (HDD) |
| Recent ATA errors in the log | error log | Critical | No | Restore on **growth only** |
| Recent NVMe errors in the log | error log | Critical | No | Probably skip, see traps |
| Command timeouts | 188 | Warning | No | Growth only, see traps |
| High-fly writes | 189 | Warning | No | Optional |
| Reallocation events | 196 | Warning | No | Optional |
| Slave `health_status` | derived | Critical or warning | No | Do not score, it would double count |
| Raw read error rate, seek error rate | 1, 7 | Warning | No | **Leave out** (vendor noise) |
| Power-on hours, load cycles, ECC recovered | various | Warning | No | **Leave out** |
| Temperature | 194 | Warning or critical | No | Covered by `metrics.disk_temperature` (`master/info/info.go:158`) |

Attribute 187 feeds both `UncorrectableReadErrs` and `ReportedUncorrectable`, so it is already covered.

The old conditions are in `git show 4fd4dbc^:slave/smartdisk/helper.go`. The slave's assessment is `assessHealth` at `slave/smartdisk/smartdisk.go:436`.

## Evidence

The proof adds 12 end-to-end errors, 5 spin retries, 40 command timeouts and slave status `warning`:

```
score=100 band="good" alerts=0
```

## How to start fixing

**First, ask the user.** Show the table with a recommended default: restore end-to-end errors and spin retries as counters, and ATA error log entries on growth only. Propose weights in line with the existing table, for example end-to-end 1-9 / ≥10 = 25 / 40, spin retry 1-9 / ≥10 = 15 / 30.

**Then choose an approach.** Decide based on how many signals are restored.

**Option A, full counters (recommended if restoring two or more).** They get trends, charts and alerts on increase, like the existing five:

1. `master/diskhealth/score.go`: add to `Counters` (`:39`), `fields()` (`:64`) and `levels` (`:181`).
2. `master/services/disk_health.go`: `sampleFromSmart` (`:248`), `scoreSample` (`:279`), `DiskHealthPoint`, and the points in `History`.
3. `master/db/disk_health.go`: both tables (`:163`, `:193`), `InsertDiskSample` (`:300`), `diskSampleColumns` (`:316`), `scanDiskSample` (`:321`), the daily reads, and the compaction SQL (`:563`).
4. Add a migration for the new columns.
5. `mcp/internal/client/disk_health.go`: `DiskHealthSample` and `DiskHealthPoint`.
6. HyperHiveFront: `types/diskhealth.ts`, `services/diskhealth.ts`, `CounterKey` and `COUNTER_SERIES` in `utils/diskhealth.ts`.

**Option B, score only (smaller).** Add the values to `diskhealth.Sample` for the current reading and apply a flat penalty. No schema change and no trends, and an alert fires only when the penalty crosses a band.

No proto change is needed: `spin_retry_count = 18`, `command_timeouts = 24`, `end_to_end_errors = 26` and `device_error_count = 42` already exist in `api/proto/smartdisk.proto`.

## Traps

- **A missing `levels` entry panics.** `levels[f.label](f.value)` at `score.go:192` calls a map entry directly. Add a field to `fields()` without adding it to `levels` and a non-zero value crashes the poll.
- **Counters are stored as JSON in `alert_counters`.** Old rows decode new fields as zero, so the first poll after upgrade alerts once for any disk with a non-zero new counter. That is probably acceptable, but tell the user.
- **Scoring ignores old rows.** `disk_health_samples` rows from before the migration have zero in new columns, so a trend can look like sudden growth for up to 30 days. Consider treating the first sample after upgrade as the baseline.
- **Vendor raw values.** `getAttrValue` returns the raw value, and attribute 188 is vendor-specific and can be large on healthy drives. Check real data before penalising its level; growth-only is safer.
- **Error logs never shrink.** A level penalty on ATA log entries would punish a disk forever for an old event. Use growth.
- **NVMe `num_err_log_entries` is often non-zero on healthy drives.** Do not score its level.
- **Keep `fields()` labels stable.** They are the keys into `levels` and appear in reason text.

## Done when

- The user has picked the signals.
- `TestAuditDroppedSignalsStillAlert` passes, updated to those signals.
- `diskhealth` tests cover each new rule and its trend.
- `db` tests cover the migration on an existing `disk_health.db` and compaction of the new columns.
- `mcp` tests and frontend jest tests pass.
- `docs/disk-health.md` lists each rule in the scoring table, and line 7 says plainly which old alerts were dropped on purpose.
