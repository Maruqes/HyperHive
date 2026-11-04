# 06. SATA SSDs always show `Wear 0%`

| | |
| --- | --- |
| **Severity** | Low |
| **Feature** | Disk health forecasting |
| **Modules** | HyperHiveFront for option A; `slave`, `api/proto`, `master`, HyperHiveFront for option B |
| **Evidence** | Live `list_disk_health` output plus source review |
| **User decision** | Option A or B. Default below. |

## Summary

Only the NVMe health log fills `percentage_used`. A SATA SSD therefore stores 0 forever, and the UI presents that as a real measurement: a `Wear 0%` tile and a flat wear chart. It never gets a wear penalty or a forecast.

## What is impacted

- **Per-disk page:** the `Wear` tile reads `0%` and the wear chart is a flat line at 0.
- **Forecast:** `docs/disk-health.md:3` and `:57` promise an estimate for "SSD/NVMe" disks. SATA SSDs never get one.
- **Score:** wear penalties cannot apply, so a worn SATA SSD stays `good`.
- **MCP:** `percentageUsed: 0` reads as "no wear".

Not impacted: HDDs (the UI hides wear for them), NVMe disks, and `Available spare` (the new `hasSpare` check hides it).

## How it is impacted

Live example from `list_disk_health` on 2026-11-04: `/dev/sda` on `marques512sv`, model `SATA SSD`, `rotational: false`, 23,139 power-on hours, `percentageUsed: 0`.

1. `summarizeSMART` sets `info.PercentageUsed` only inside `if resp.NVMESmartHealth != nil`.
2. For a SATA SSD the field stays 0.
3. `percentage_used` is a plain `int64` in the proto, so 0 and "unknown" look the same.
4. `isFlashDisk` is just `!rotational`, so the UI treats the disk as having wear data.

## Root cause

- `slave/smartdisk/smartdisk.go:284` is the only place that sets `PercentageUsed`.
- `runSmartctlJSON` (`smartdisk.go:181`) runs `smartctl -a -j`. For ATA, `-a` does not include the device statistics log, which is where SATA SSDs report endurance.
- HyperHiveFront `components/diskhealth/DiskHealthOverview.tsx:200-205` and `app/smartdisk/[serial].tsx:265-267` show wear for every non-rotational disk.

## How to start fixing

**Option A, stop showing a false value (recommended first, frontend only).**

Wear data exists only on NVMe today, so:

1. Add `hasWearData(disk)` to `utils/diskhealth.ts`, true when `transport` is `nvme` (compare case-insensitively).
2. Use it for the `Wear` tile and the wear chart instead of `flash`.
3. Leave `flash` as it is for the `SSD/NVMe` label.
4. Change the docs from "SSD/NVMe" to "NVMe" for wear and the forecast.

Do not use `percentageUsed > 0` as the test. A new NVMe disk at a real 0% should still show its value.

**Option B, read real wear from SATA SSDs (larger, after A).**

1. Add `-l devstat` to the smartctl call. The binary contains `ata_device_statistics` and `Percentage Used Endurance Indicator`.
2. Parse `ata_device_statistics.pages[].table[]` for the entry with that name, and set `info.PercentageUsed`. **Confirm this layout on a real disk first:** only the two names above were verified, not the nesting.
3. Add a presence flag so 0 and unknown differ, for example `bool percentage_used_known = 50` in `SmartDiskInfo` (49 is the last used field). Carry it to `DiskHealthSample` and the frontend, and have `hasWearData` use it.
4. Regenerate the proto (see the rules in the README).

## Traps

- **Device statistics are optional.** The man page says only some recent devices support them. Option B still needs the unknown state from option A.
- **Do not start from vendor attributes.** Attributes such as 177, 202, 231 and 233 report wear in vendor-specific ways, usually in the normalized value. `ataAttribute` does not even decode the normalized value today. Treat this as a last resort, per vendor.
- **`-l devstat` adds a command to every read.** Check that a slow disk still fits the 60 s master timeout.
- **With option B, real wear values will change scores.** The first poll can move a band and fire an alert. That is correct, but tell the user.
- **Forecasts need history.** A disk needs samples spanning 7 days before `wearOutAt` appears.
- **`app/smartdisk/[serial].tsx` and `components/diskhealth/` have uncommitted work.** See the README before editing.

## Verifying on real hardware

Ask the user to run this on `marques512sv`, or to authorize it, to see whether this disk supports the statistic:

```bash
sudo smartctl -a -l devstat -j /dev/sda | jq '.ata_device_statistics'
```

## Done when

- **Option A:** a SATA SSD shows no `Wear` tile and no wear chart. NVMe disks are unchanged, including at a real 0%. A jest test covers `hasWearData`. Docs say NVMe.
- **Option B also:** a supported SATA SSD shows real wear and gets a forecast. An unsupported one stays unknown. Slave tests cover both.
