# Disk health forecasting

The master polls SMART on every host every 6 hours, stores the key counters per disk serial, and turns them into a 0–100 health score with a band and a plain-language reason. SSD/NVMe disks also get an estimated wear-out date. Alerts fire only when a disk gets worse.

- History lives in its own SQLite file, `disk_health.db`, next to `data.db`. The SQLite backup does not include it; the data is non-critical.
- No proto change: the master reuses the existing `GetAllDisks` (Btrfs) and `GetSmartInfo` (SMART) RPCs.
- The slave's old 6-hourly SMART alert loop (`StartSmartTestChecker`) is removed. The failed self-test alert it raised is now part of the score.

## API

All routes are in the authenticated group.

| Route | Behavior |
| --- | --- |
| `GET /diskhealth?state=active\|retired\|all` | `{lastPollAt, disks}`. Each disk: serial, model, host, device, transport, size, `smartAvailable`, `score`, `band`, `reasons`, `wearOutAt`, `pastRatedEndurance`, `retiredAt` and the `latest` sample. Default state is `all`. |
| `GET /diskhealth/{serial}?days=N` | `{disk, points}`, oldest first. Points from the last 90 days are raw samples (`resolution: "6h"`); older points are daily rows (`resolution: "daily"`). `days` is optional (1–3650). 404 for an unknown serial. |
| `DELETE /diskhealth/{serial}` | Purge a retired disk and all its history. 409 if the disk is not retired, 404 if unknown. UI-only, never MCP. |

Every route returns 503 when `disk_health.db` could not be opened at startup.

## Polling

`DiskHealthService.RunLoop` waits 2 minutes after master start, then polls every 6 hours. A guard skips a poll while the previous one is still running.

1. Up to 4 hosts are polled in parallel. Disks on one host are read one at a time, because smartctl on the slave has no exec timeout. `GetAllDisks` has a 30 s timeout and `GetSmartInfo` a 60 s timeout per disk.
2. The serial comes from lsblk, with the smartctl serial as a fallback. Disks with no serial at all are not stored.
3. `virtio` disks are recorded as having no SMART and are never read.
4. A disk is SMART-available when the RPC succeeds and it reports power-on hours or a temperature. A failing drive that returns parsed data plus `smartctl_error` is still scored, and the error is stored with the sample.
5. For each SMART-available disk: score it against its stored history, insert the sample, update the disk row, then evaluate alerts.
6. At the end of the pass, disks not seen for 90 days are marked retired, and history is compacted. A retired disk that is seen again becomes active.

One failing host or disk is logged and skipped; the pass continues.

## Scoring

The score is `100 − sum(penalties)`, clamped to 0–100. Each rule that fires adds a reason line.

| Rule (current value) | Penalty |
| --- | --- |
| Pending sectors 1–9 / ≥10 | 25 / 40 |
| Offline uncorrectable 1–9 / ≥10 | 20 / 35 |
| Reallocated sectors 1–9 / 10–99 / ≥100 | 10 / 20 / 35 |
| Reported uncorrectable > 0 | 15 |
| NVMe media errors 1–9 / ≥10 | 20 / 35 |
| NVMe critical warning ≠ 0 | 40 |
| Available spare below its threshold (threshold > 0) | 40 |
| CRC errors > 0 (usually a cable problem) | 5 |
| Wear (`percentage_used`) ≥80 / ≥90 / ≥100 | 10 / 20 / 30 |
| Latest self-test failed | 30 |
| Growth of any of the five counters above: within 7 days / within 30 days but not 7 | +15 / +5 per counter |
| CRC growth within 7 days | +5 |
| Projected rated wear (100 %) within 90 days | +15 |
| SMART overall health check failed | score capped at 10 |

- **Bands:** good 80–100, watch 60–79, at risk 30–59, critical 0–29.
- **Trends** compare the current value with the oldest stored sample inside each window.
- **Wear-out estimate** (SSD/NVMe only): a least-squares line over `percentage_used` in the last 90 days. It needs at least two samples spanning 7 days and an upward slope. At 100 % or more the disk is reported as past rated endurance instead.
- **Temperature** is charted but not scored; the metrics alert engine already alerts on disk temperature.
- **Self-test** uses `self_tests[0]`: the current ATA self-test status or the newest NVMe log entry. Running, aborted and interrupted tests do not count as failed.

Source: `master/diskhealth/score.go`.

## Alerts

A notification is sent when the band is worse than the last evaluated band, or any of the five critical counters is higher than at the last evaluation. Both in the same poll give one notification.

- Event `health.disk_health` (category `health`). Severity: watch → warning, at risk → error, critical → critical; a counter increase alone is at least warning.
- Title `<machine> disk <model> (<serial>): <band>`, body = the reasons, URL `/smartdisk/<serial>`.
- The last evaluated band and counters are stored on the disk row (`alert_band`, `alert_counters`), so a master restart neither forgets nor repeats alerts. After each evaluation they move to the current values: a later drop alerts again and an improvement stays silent. If sending fails, the old state is kept so the next poll retries.
- A newly seen disk starts from "good" and zero counters, so a disk that is already degraded alerts once.

## Retention

| Data | Kept |
| --- | --- |
| Raw 6-hourly samples (`disk_health_samples`) | 90 days |
| Daily rows (`disk_health_daily`): worst score, highest counters and temperature, last wear, power-on hours and SMART verdict | 2 years |
| Disk rows (`disk_health_disks`) | Until purged |

Compaction runs at the end of each poll in one transaction and is safe to rerun. A serial not seen for 90 days is retired; purge is manual from the UI.

## MCP

Two read-only tools at low risk: `list_disk_health` and `get_disk_health_history`. Purge is excluded by the no-deletion policy.

## Frontend

`HyperHiveFront`: score badge and reason per disk plus a "Retired disks" section on the SMART page (`app/smartdisk.tsx`), and a page per disk with charts (`app/smartdisk/[serial].tsx`).

## Known limits

- Serials are assumed to be globally unique.
- `disk_health.db` is not backed up. If it is lost or corrupt, the tables are recreated empty, history starts over and degraded disks alert once more.
- If the file cannot be opened at startup, the feature is disabled until the next master start (routes return 503, no polling).
- Older history exists only as daily rows; nothing is kept beyond 2 years. Trends and the wear projection use only the raw 90-day window.
- A host offline for more than 90 days has its disks retired; they return when seen again.
- Alerts can be up to 6 hours late.
- An older slave keeps its own SMART alert loop until it is upgraded, so both alert paths can fire during a mixed-version rollout.

## Source

- `master/diskhealth/score.go` — scoring, bands, trends, wear projection, self-test parsing.
- `master/db/disk_health.go` — `disk_health.db`, tables, compaction, purge.
- `master/services/disk_health.go` — poll loop, alerts, list/history views.
- `master/api/disk_health.go` — routes.
- `master/smartdisk/smartdisk.go`, `master/btrfs/btrfs.go` — RPC wrappers with timeouts.
- `mcp/internal/tools/disk_health.go`, `mcp/internal/client/disk_health.go`.
