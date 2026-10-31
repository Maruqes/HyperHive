# HH Power Schedule: Grill / Discovery Notes
Date: 2026-10-31 · Goal: Define a daily power schedule (RTC wake startup time + power-off time) set from `/dashboard/{machine}`, with ordered shutdown (slaves first, master last) re-applied every day and on every boot.

## Context already known (no need to ask)
- SilverBullet: both hosts (marques512sv, marques2673sv) wake from S5 via `rtcwake -u` only when BIOS "Power On By RTC" is **disabled** (firmware overwrites the alarm otherwise). RTC is UTC. Alarm max ~1 month ahead. No wake if power cut/crash before shutdown; WoL suggested as fallback. Verify via `/sys/class/rtc/rtc0/wakealarm` and `/proc/driver/rtc`.
- Source: existing `ExtraService.ShutDown` RPC (`api/proto/extra.proto` `RestartShutdownNow{now}`) → slave `shutdownPc` runs `shutdown`. Master HTTP `shutdownPc` in `master/api/extra.go`. Master host also runs a slave.
- Frontend route: `HyperHiveFront/HyperHive/app/dashboard/[machine].tsx`.

## Summary / key decisions
- **One cluster-wide schedule** (start `HH:MM`, stop `HH:MM`, enabled), edited from any `/dashboard/{machine}`; UI states it is global (Q1, Q9).
- **Times in the master's local TZ**; master computes absolute UTC epochs (next stop, next wake) and sends them to hosts (Q7). Stop may cross midnight; start ≠ stop (Q11).
- **Each host powers itself off** via an `at` job (queue `h`) running `rtcwake -u -m off -t <wake epoch>` — arm + power-off in one step. No new systemd units; ensure `at`/`atd` like other on-demand packages (Q4–Q6).
- **Order: fire-and-forget.** Slaves at stop; the master host (detected via `AreWeMasterSlave`) at stop + **3 min** constant (Q2, Q11).
- **VMs:** rely on OS shutdown (Q5).
- **Re-apply:** on every slave (re)connect, on save, and daily for hosts still online. Idempotent: replace previous HH jobs. A host booted inside the off window stays on until the next stop (Q3, Q8, Q11).
- **Disable** removes HH at jobs + `rtcwake -m disable`; offline hosts get cleaned on next connect (Q9).
- **Status:** per-host last apply (stop/wake epoch, job id, error, applied-at) on the dashboard; failures → HH notification; BIOS "Power On By RTC" must be disabled warning (Q10).
- **MCP:** read-only `get_power_schedule` (Q12).
- **Post-v1 ideas:** skip-tonight, Wake-on-LAN fallback, pre-shutdown warning.

## Implementation plan
1. **Contract** — `api/proto/extra.proto`: `rpc ApplyPowerSchedule(PowerScheduleRequest) returns (PowerScheduleResult)`; `PowerScheduleRequest{bool enabled=1; int64 stop_unix=2; int64 wake_unix=3;}`, `PowerScheduleResult{string job_id=1; int64 stop_unix=2; int64 wake_unix=3;}`. Regenerate with root `make setup`; commit generated code.
2. **Slave** — `slave/extra/power_schedule.go` (+ handler in `extraGrpc.go`): validate epochs (wake > stop > now, wake ≤ now + ~30 days); ensure `at` installed and `atd` active; `atq -q h` / `atrm` to remove previous HH jobs; if enabled, schedule `rtcwake -u -m off -t <wake>` with `at -q h -t <stop in local time>`; if disabled, `rtcwake -m disable`. Every exec with timeout; return job id. Unit tests for command building and validation (exec behind a seam, no privileged tests).
3. **Master DB** — `master/db/power_schedule.go`: singleton `power_schedule` (enabled, start_hhmm, stop_hhmm, tz, updated_at) and `power_schedule_status` per machine (stop_unix, wake_unix, job_id, last_error, applied_at).
4. **Master client + service** — `master/extra` thin client with timeout; `master/services/power_schedule.go`: pure `nextWindow(now, start, stop, tz)` (DST, midnight crossing, inside-off-window ⇒ next stop); per-host apply (master host = stop + 3 min via `IsMasterSlave`); hooks: on slave registration (`master/protocol`), on save (all connected hosts, slaves first), daily loop (stoppable, re-applies hosts whose stored stop has passed and are online). Failures → status row + notification. Table tests for `nextWindow`.
5. **Master API** — `master/api/power_schedule.go`: `GET /power-schedule` (schedule + per-host status), `PUT /power-schedule` (validate `HH:MM`, start ≠ stop) in the authed group.
6. **MCP** — read-only `get_power_schedule` (follow `mcp/AGENTS.md`).
7. **Frontend** (`HyperHiveFront/HyperHive/app/dashboard/[machine].tsx`) — "Power schedule (all machines)" card: enable toggle, two time pickers, save, per-host status list, BIOS warning.
8. **Verify + docs** — `go build/test -tags libvirt_dlopen` in api/slave/master/mcp; frontend tests; live test only on user request (short window on one slave). Update `docs/` and SilverBullet feature/inventory pages.

## Q&A log

### Q1 — Schedule scope
- Asked: Per-machine schedule or one cluster-wide schedule?
- Captured: **One cluster schedule.** A single start/stop pair for the whole fabric; any `/dashboard/{machine}` edits that same schedule. At stop time all slaves shut down first, then the master.
- Flags: UI should make clear the setting is global even though it appears on a per-machine dashboard.

### Q2 — Shutdown ordering
- Asked: How is "slaves first, master last" enforced (master host also runs a slave)?
- Captured: **Fire-and-forget order.** Send shutdown to all slaves, wait a fixed delay (e.g. ~2 min), then shut down the master host. No verification that slaves actually went down.
- Flags: delay value configurable or constant -> later.

### Q3 — Wake arming / re-apply on boot
- Asked: How does each machine get its RTC alarm armed and keep the behavior every boot?
- Captured: "on startup... when the slaves connect we use a grpc function to set the slave RTC wake alarm and shutdown (with a cronjob or something like that". On slave registration, master calls a new gRPC that (a) arms the RTC wake alarm for the next start time and (b) installs a local scheduled shutdown (cron / systemd timer) on that slave.
- Flags: who actually fires the shutdown (local timer vs master RPC) -> Q4.

### Q4 — Who fires the shutdown
- Asked: Local timer per host or master orchestration?
- Captured: "local time on each host but keep it simple no need for a systemd service or anything like that". Each host schedules its own power-off locally with plain built-in commands (e.g. `shutdown -h HH:MM` + `rtcwake -u -m no -t <next start>`), no new unit files. Master host uses stop time + delay so slaves go first.
- Implication: a scheduled `shutdown` does not survive reboot — that's fine because registration re-applies it every boot.

### Q5 — VMs at power-off / power-off command
- Asked: What happens to running VMs?
- Captured: **Rely on OS shutdown** (libvirt-guests if enabled); HH does nothing extra. The power-off itself should arm the wake: "sudo rtcwake -u -m off -s {time_difference_to_turn_on}" — "the command does not need to be exatly the same" (e.g. `-t <epoch>` is fine). So arming and power-off are one step at stop time.

### Q6 — Local timer mechanism
- Asked: What waits until stop time on each host (no systemd units)?
- Captured: **`at` job.** Registration/apply gRPC schedules an `at` job on the slave that runs `rtcwake -u -m off -t <next start epoch>` at stop time. Master host gets the same job at stop time + delay.
- Source note: slave already installs host packages on demand via `sudo dnf -y install …` (e.g. `slave/nfs/nfs.go`), so ensure `at` installed and `atd` running the same way.
- Design needs (see Q7 for TZ): identify HH's own job (tag/queue letter, e.g. `at -q h`) so re-apply removes previous HH jobs only (idempotent), and disable removes them.

### Q7 — Timezone
- Asked: Which TZ are the times in?
- Captured: **Master's local TZ.** Store `HH:MM` start + stop (and the master's IANA zone). Master computes absolute UTC epochs for next stop and next start and sends them to slaves; slave TZ and UTC RTC don't matter; DST is handled by the master.

### Q9 — Toggle / weekdays
- Asked: On/off switch and day-of-week selection?
- Captured: **Enable toggle, every day.** Disabling removes HH at jobs and clears the RTC alarm (`rtcwake -m disable`) on all online hosts. No weekday selection in v1.
- Flags: hosts offline at disable time must be cleaned on next connect (connect-apply sends "disabled").

### Q10 — Status visibility
- Asked: What per-host status does the dashboard show?
- Captured: **Per-host apply status.** Master stores last apply per machine: stop epoch, wake epoch, at job id, last error, applied-at. Dashboard shows it; failures send an HH notification. UI warns that BIOS "Power On By RTC" must be disabled.
- Source note: master-host detection exists — slave `AreWeMasterSlave()` exposed via `K8sService.IsMasterSlave` (`slave/our_k8s/k8_grpc.go`, `master/services/k8s.go`). Reuse it to give the master host stop + delay.

### Q11 — Delay and validation
- Asked: Master-host delay and time rules?
- Captured: **Fixed 3 min** delay (constant): master host powers off at stop + 3 min. Start must differ from stop; stop may cross midnight (e.g. start 08:00, stop 01:00). A host applied inside the off window (e.g. booted manually at 02:00) is **not** shut down immediately; it powers off at the next stop time.

### Q12 — MCP exposure
- Asked: Expose the schedule through MCP?
- Captured: **Read-only `get_power_schedule`** (schedule + per-host status). Setting/enabling/disabling stays UI-only, since it powers off the whole fabric.

### Q8 — Re-apply triggers
- Asked: When does master (re)apply the at job + wake epoch?
- Captured: **On every slave (re)connect, on schedule save, and daily** (after each stop time passes, for any host still online — covers manual boots or a failed job). Repeats every day without depending on a reboot.

## Open flags (pending input)
- "ponytail" skill requested but not installed in this session -> user (which skill/tool was meant?)
- Confirm `at` package name and `atd` on all hosts (Fedora: `at`) -> check during implementation
