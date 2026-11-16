# Plan: Power page, VM power timeline, monthly cost projection

**Generated**: 2026-11-16
**Estimated Complexity**: Medium (two sprints, two repositories)

## Overview

Three additions on top of the existing power cost estimate (`docs/power-cost.md`):

1. A dedicated **Power** page in the frontend. The energy card and the power
   schedule card move there; the dashboard keeps no power data.
2. **Timeline**: per host and per VM, watts over time and running/stopped
   state, at three resolutions (5 min for 30 days, 30 min for 90 days, 1 h for
   6 months).
3. **Monthly cost projection**: projected kWh and cost for the full current
   month, per host, per VM and in total, next to the stored history.

Decisions taken with the user (2026-11-16):

| Topic | Decision |
| --- | --- |
| Timeline content | Both running/stopped state and estimated watts. |
| Retention | 30 days at 5 min, 90 days at 30 min, 6 months at 1 h. |
| Monthly cost | Projection for the current month plus the existing 12-month history. |
| Dashboard | Both cards are removed from it. |

Design choices:

- **One samples table, three tiers, no roll-up job.** Every tick adds its Wh
  to the 5 min, 30 min and 1 h bucket of each series in one transaction. Each
  tier has its own retention delete. Nothing has to be re-aggregated later.
- **State from the same table.** Each bucket stores `running_s`, the seconds
  the VM was seen running. A bucket where the host has a row and the VM has
  none means "stopped"; no host row means "no data" (master or slave down).

Not in this plan (say so if wanted): MCP tools for the timeline, notifications
on power thresholds, power-schedule savings.

## Prerequisites

- Frontend repo: `/home/marques/projects/HyperHiveFront/HyperHive`. `victory`
  is already installed and used (`components/diskhealth/DiskHealthCharts.tsx`).
- Build/test: `go build -tags libvirt_dlopen ./...` and
  `go test -tags libvirt_dlopen ./...` inside `master/`.

## Sprint 1: Power page (frontend only)

**Goal**: Power has its own page; the dashboard no longer shows it.
**Demo/Validation**:
- Sidebar shows "Power"; the page shows the energy card and the power schedule
  card working exactly as before (price save, schedule save, 10 s refresh).
- Dashboard has neither card. Browser tab title reads "Power".
- Works on web and at phone width.

### Task 1.1: Create the Power page
- **Location**: `app/power.tsx` (new, frontend repo)
- **Description**: New screen using the same auth guard and page shell as
  `app/smartdisk.tsx`. Render `<EnergyCard token>` then
  `<PowerScheduleCard token>` in a scroll view. Leave the two components
  unchanged.
- **Dependencies**: none
- **Acceptance Criteria**: page loads when logged in, redirects when not, both
  cards behave as on the dashboard today.
- **Validation**: open `/power` in the web build; `npx tsc --noEmit`.

### Task 1.2: Add navigation and title
- **Location**: `components/navigation/AppSidebar.tsx` (`MENU_ITEMS`),
  `app/_layout.tsx` (`ROUTE_TITLE_MAP`)
- **Description**: Add a top-level `{label: "Power", route: "/power", icon: Zap}`
  item and `"/power": "Power"` title.
- **Dependencies**: 1.1
- **Acceptance Criteria**: item is highlighted on `/power`; title is correct.
- **Validation**: click through on web and narrow width.

### Task 1.3: Remove the cards from the dashboard
- **Location**: `app/dashboard.tsx` (imports at lines 26-27, blocks at ~1260-1266)
- **Description**: Delete the two `<Box className="mt-8">` blocks and both
  imports. Update the comment in `components/PowerScheduleCard.tsx` that says
  "shown on the main dashboard".
- **Dependencies**: 1.1
- **Acceptance Criteria**: dashboard renders with no power section and no
  unused imports.
- **Validation**: `npx tsc --noEmit`, lint, visual check of the dashboard.

## Sprint 2: Timeline and monthly projection

**Goal**: The Power page shows watts and running state over time for hosts and
VMs, and a projected cost for the current month.
**Demo/Validation**:
- After the master has run about 15 minutes, the timeline shows points for
  every host and running VM; stopping a VM shows a "stopped" gap within one
  5 min bucket.
- Range switch 30 d / 90 d / 6 m changes the point step (5 min / 30 min / 1 h).
- Totals, each host and each VM show "projected this month".
- `go test -tags libvirt_dlopen ./...` passes in `master/`.

### Task 2.1: Samples table and queries
- **Location**: `master/db/energy_samples.go` (new), `master/db/energy_samples_test.go` (new), `master/main.go` (call the create function next to `CreateEnergyTables`)
- **Description**: Table
  `energy_samples(step INTEGER, ts INTEGER, kind TEXT, machine TEXT, name TEXT, wh REAL, running_s REAL, PRIMARY KEY(step, kind, machine, name, ts))`.
  `step` is 300, 1800 or 3600; `ts` is the bucket start in Unix seconds;
  `kind` is `host` or `vm`.
  Functions: `AddEnergySamples(ctx, []EnergySample)` (one transaction, upsert
  adding `wh` and `running_s`, writing all three steps per sample),
  `GetEnergySamples(ctx, step, from, kind, machine, name)` (empty filters mean
  all), `DeleteEnergySamplesBefore(ctx, step, ts)`.
- **Dependencies**: none
- **Acceptance Criteria**: two adds in the same bucket sum; a sample lands in
  three rows; delete only touches its own step.
- **Validation**: unit tests on an in-memory/temp SQLite, as in `master/db/power_schedule_test.go`.

### Task 2.2: Write samples from the energy tick
- **Location**: `master/services/energy.go` (`Tick`, `readMachine`, `applyRetention`), `master/services/energy_test.go`
- **Description**: In `Tick`, collect one sample per ok host (`kind=host`,
  `wh=hostWh`, `running_s=tick seconds`) and one per running VM (`kind=vm`,
  `wh`, `running_s=tick seconds`) and call `AddEnergySamples` once. Tick
  seconds use the same capped interval as `energyTickHours`, so downtime is not
  recorded. Extend `applyRetention` to delete, once per hour, samples older
  than 30 d (step 300), 90 d (step 1800) and 183 d (step 3600). A failed
  sample write is logged and does not block the monthly `energy_usage` write.
- **Dependencies**: 2.1
- **Acceptance Criteria**: monthly totals are unchanged; stopped VMs write no
  sample; retention removes only expired rows.
- **Validation**: extend the existing fake-reader tick tests; assert bucket
  contents and retention boundaries with a fake clock.

### Task 2.3: Timeline endpoint
- **Location**: `master/services/energy_timeline.go` (new), `master/api/energy.go` (route in `setupEnergyAPI`), tests beside each
- **Description**: `GET /energy/timeline?range=30d|90d|6m[&kind=host|vm&machine=&name=]`.
  With `kind` and `name`: one series at the range's step (300 / 1800 / 3600).
  Without: every series at step 3600, capped to the range (the overview).
  Response is compact: `{start, stepSeconds, count, series:[{kind, machine, name, wh:[...], runningS:[...]}]}`
  with arrays indexed by bucket and `null` where the host has no row ("no
  data"). VM series are merged by name across machines, as `Summary` does.
  Invalid `range` or `kind` returns 400.
- **Dependencies**: 2.1
- **Acceptance Criteria**: array length equals `count`; a stopped VM bucket is
  `0`, a master-down bucket is `null`; authenticated group only.
- **Validation**: service tests for gaps, merge by name and bucket alignment;
  handler test for validation errors in `master/api/energy_test.go`.

### Task 2.4: Monthly projection in the summary
- **Location**: `master/services/energy.go` (`Summary`, the summary structs), `master/services/energy_test.go`
- **Description**: Add `projectedMonthKWh` and `projectedMonthCost` to
  `EnergyTotals`, `EnergyMachineSummary` and `EnergyVMSummary` (additive JSON
  fields). Projection = month to date + average watts over the last 7 days of
  hourly samples (only hours that have data) × hours left in the month.
  Projected cost = stored cost so far + remaining kWh × current price; 0 when
  no price is set. With no samples yet, the projection equals the month to date.
- **Dependencies**: 2.1, 2.2
- **Acceptance Criteria**: existing fields and values are unchanged; on the
  last hour of the month the projection equals the month to date; totals come
  from host rows only (VM rows never added again).
- **Validation**: table tests with a fake clock: mid-month, first day with
  little data, last hour, no price.

### Task 2.5: Frontend service types
- **Location**: `services/energy.ts` (frontend repo)
- **Description**: Add optional `projectedMonthKWh` / `projectedMonthCost` to
  `EnergyUsage`, the `EnergyTimeline` type and `getEnergyTimeline(range, series?, signal?)`.
  Optional fields keep older masters working.
- **Dependencies**: 2.3, 2.4
- **Acceptance Criteria**: `npx tsc --noEmit` passes; `app/vms.tsx` still compiles.
- **Validation**: typecheck.

### Task 2.6: Projection in the energy card
- **Location**: `components/EnergyCard.tsx`
- **Description**: Add a fourth headline "Projected this month" (kWh, and cost
  when a price is set) and append the projection to each machine line. Hide it
  when the fields are missing.
- **Dependencies**: 2.5
- **Acceptance Criteria**: readable at phone width; no layout shift on refresh.
- **Validation**: visual check with and without a price.

### Task 2.7: Timeline card
- **Location**: `components/PowerTimelineCard.tsx` (new), `app/power.tsx`
- **Description**: Range buttons (30 d / 90 d / 6 m). Overview: one row per VM
  with a running/stopped/no-data strip from the hourly overview call. Selecting
  a host or VM loads its series and draws average watts per bucket
  (`wh / bucket hours`) with `victory`, following `components/diskhealth/DiskHealthCharts.tsx`.
  Add a "VM cost this month" list (month to date and projection per VM) from
  the summary. Text states that values are estimates.
- **Dependencies**: 2.5, 1.1
- **Acceptance Criteria**: empty state before the first samples; "no data" is
  visually different from "stopped"; the strip has a text alternative
  (`aria-label` per VM with running hours); works on web and phone width.
- **Validation**: run the web build against a master with real samples; check
  the gotchas in memory note `hh-frontend-web-gotchas`.

### Task 2.8: Docs
- **Location**: `docs/power-cost.md`
- **Description**: Document the samples table, tiers and retention, the
  timeline endpoint, the projection formula and its limits.
- **Dependencies**: 2.2-2.4
- **Validation**: read-through against the source.

### Task 2.9: Knowledge base
- **Location**: SilverBullet, via the `hyperhive-infrastructure-memory` skill
- **Description**: Update the power cost feature page, HTTP inventory and
  source map. State what was verified locally vs live.
- **Dependencies**: all

## Testing Strategy

- Go unit tests per layer with fake readers and a fake clock (pattern already
  in `master/services/energy_test.go`).
- Per sprint: build and test `master/`; `npx tsc --noEmit` and lint in the
  frontend; run the web build against a real master for the UI checks.
- Live verification (needs the user): deploy the master, wait for samples,
  check the timeline against a VM you stop and start.

## Potential Risks & Gotchas

- **SQLite write volume.** Three upserts per series every 10 s. One transaction
  per tick; if it shows up in latency, buffer in memory and flush per minute.
- **Projection in the first days** rests on few hours of data and is noisy.
- **VM renamed or moved host**: series are keyed by VM name, as today.
- **Frontend/master version skew**: all new fields are optional and the new
  cards show an "update the master" empty state on 404.
- **Clock/timezone**: buckets are Unix time; months stay master local time as today.

## Rollback Plan

- Sprint 1: revert the frontend commit; the cards return to the dashboard.
- Sprint 2: revert the master and frontend commits. The new `energy_samples`
  table is unused by older code and can stay or be dropped by hand;
  `energy_usage` is never altered.
