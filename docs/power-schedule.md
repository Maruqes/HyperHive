# Cluster power schedule

One schedule powers off every HyperHive host each day and wakes it again with the RTC alarm.

## Configure

Open the main dashboard (`/dashboard`) and use the **Power schedule (all machines)** card. The setting is global: it applies to every machine.

- `start` and `stop` are `HH:MM` in the master's local timezone. Stop may be after midnight (for example start `08:00`, stop `01:00`).
- Start and stop must differ, and the off window must be at least 10 minutes.
- BIOS "Power On By RTC" must be **disabled** on every host. When it is enabled, the firmware overwrites the alarm and the host does not wake.

## How it works

1. The master computes the next stop and wake as UTC epochs.
2. It calls `ExtraService.ApplyPowerSchedule` on each host. The slave installs `at` if needed, enables `atd`, removes earlier jobs in queue `h`, and queues `rtcwake -u -m off -t <wake>` at the stop time.
3. Slaves power off at the stop time. The master host powers off 3 minutes later. Running VMs get the normal OS shutdown.
4. The master re-applies on every slave (re)connect, after a save, and every 10 minutes for online hosts that never applied, last failed, or whose stop passed more than 15 minutes ago.

A host started inside the off window stays on until the next stop. Disabling removes the queue-`h` jobs and runs `rtcwake -m disable`; offline hosts are cleaned on their next connect.

## API

| Method | Path | Notes |
| --- | --- | --- |
| `GET` | `/power-schedule` | Schedule, master timezone and time, next window, per-host status |
| `PUT` | `/power-schedule` | Body `{"enabled", "start", "stop"}`. 400 on invalid times, 202 `applying` on success; hosts are applied in the background |

Failures are stored per host (`lastError`) and sent as notification event `system.power_schedule`. MCP exposes only the read-only `get_power_schedule` tool.

## Inspect a host

```bash
atq -q h
cat /sys/class/rtc/rtc0/wakealarm
cat /proc/driver/rtc
```

The wake alarm is armed by `rtcwake` at power-off time, so `wakealarm` stays empty until then.

## Limits

Hosts do not wake after a power cut or a crash before the stop time. The RTC alarm reaches about a month ahead.
