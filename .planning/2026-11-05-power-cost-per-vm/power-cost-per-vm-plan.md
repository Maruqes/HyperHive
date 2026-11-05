# Plan: Power and cost per VM, milestone 1

**Generated**: 2026-11-05
**Estimated Complexity**: Medium
**Decisions**: see `grill-notes.md` in this folder. They replace Q13, Q15, Q17 and Q18 of `notes/2026-11-03-hh-feature-ideas-grill.md` for this milestone.

## Overview
The user enters only an electricity price. HH estimates each slave's watts from detected hardware and CPU load, splits load watts across running VMs, and accumulates energy per month. Each VM card, each slave and the dashboard show live watts plus this month's kWh and EUR, always labelled as estimates.

There is no Energy page, MCP tool, savings figure, watt override, RAPL or sensor reading.

## Estimation model
All constants live in one Go file, so they are easy to tune.

- `cpuTDP` = sum of table TDP per socket, from `cpu-tdp-table.csv` (1504 models, focused on Xeon and EPYC, plus Intel and AMD consumer parts). Unknown model: `physical cores x median W per core of the same brand family` (Xeon, EPYC, Ryzen, Core, computed from the table), or `8 W x cores` if the brand is unknown too. Both fallbacks are flagged `generic`.
- `idleW` = `30` (board and fans) + `0.15 x cpuTDP` + `0.4 x RAM GB` + `7 per HDD` + `3 per SATA SSD` + `5 per NVMe`.
- `loadW` = `0.85 x cpuTDP x hostCPU%`, where `hostCPU%` is the mean of per-core usage.
- `hostW` = `idleW + loadW`.
- `vmW` = `loadW x weight / sum(weights)`, where `weight = currentCpuUsage x cpuCount` for running VMs. If the sum is 0, every VM is 0 W.

## Prerequisites
- Back end in this repo, UI in `HyperHiveFront/HyperHive`; both need a matching deploy.
- Slaves must be updated to expose hardware. An old slave gets no estimate and shows no number.
- `make setup` rewrites all `pb.go` files: keep only the generated files for `info.proto`.

## Sprint 1: Hardware profile from the slave
**Goal**: the master can read what each slave is made of.
**Demo/Validation**: unit tests pass, and the RPC returns sensible data on a dev slave.

### Task 1.1: Add the `GetPowerHardware` RPC
- **Location**: `api/proto/info.proto`, generated bindings
- **Description**: add a response with `repeated CpuSocket {model, cores}`, `ram_mb` and `repeated PowerDisk {name, kind}` (`hdd`, `ssd`, `nvme`). Add fields only.
- **Dependencies**: none
- **Acceptance Criteria**: existing field numbers are unchanged and only info bindings differ.
- **Validation**: `go build -tags libvirt_dlopen ./...` in `api/`.

### Task 1.2: Implement it on the slave
- **Location**: new `slave/info/power_hardware.go`, slave info gRPC service
- **Description**: reuse the `cpu.Info()` logic from `GetCPUModel`. Read disks from `/sys/block`, skipping `loop`, `ram`, `dm-`, `zram`, `sr` and `md`; use `queue/rotational` and the `nvme` prefix for kind.
- **Dependencies**: 1.1
- **Acceptance Criteria**: virtual block devices are not counted.
- **Validation**: test the classifier against a fake sysfs directory.

### Task 1.3: Add the master client
- **Location**: `master/info/`
- **Description**: thin client function with its own timeout.
- **Dependencies**: 1.1
- **Validation**: build `master/`.

## Sprint 2: Estimator and accumulation
**Goal**: the master records energy every minute.
**Demo/Validation**: table tests pass, and rows grow for a dev slave and its VMs.

### Task 2.1: Build the estimator and TDP table
- **Location**: new `master/services/energy_model.go`, `energy_model_test.go`
- **Description**: pure functions for the model above. Copy `cpu-tdp-table.csv` next to the code and load it with `go:embed`, dropping the `source` column is optional. Lookup lowercases the model string, then picks the longest `model_key` that matches on token boundaries and belongs to the same brand. Boundaries and brand are required: the table has short keys such as `634` and `9460`, and keys that contain each other, such as `e5-2680` and `e5-2680 v4`.
- **Dependencies**: none
- **Acceptance Criteria**: `Intel(R) Xeon(R) CPU E5-2673 v4 @ 2.30GHz` gives 135 W and `E5-2680 v4` gives 120 W; `E5-2680 0` does not match a v4 row; known model, unknown model, multi-socket, zero-VM-load and zero-disk cases are covered.
- **Validation**: table-driven tests.

### Task 2.2: Add storage and price
- **Location**: new `master/db/energy.go`, `energy_test.go`
- **Description**: table `energy_usage(month TEXT, machine TEXT, vm TEXT, wh REAL, cost REAL, PRIMARY KEY(month, machine, vm))`; `vm = ''` is the host row. Add a one-row `energy_settings(price_per_kwh)`. Use an upsert that adds to `wh` and `cost`, so a price change applies only from then on. Delete rows older than 24 months.
- **Dependencies**: none
- **Acceptance Criteria**: repeat upserts add up; a price change leaves earlier cost unchanged.
- **Validation**: DB tests, following `network_history_test.go`.

### Task 2.3: Add the stoppable loop
- **Location**: new `master/services/energy.go`, `master/main.go`
- **Description**: `EnergyService.RunLoop(ctx)` ticks every minute. Per connected slave it reads CPU, uses a cached hardware profile (refreshed hourly or on failure), and groups `GetAllVms` output by machine. It writes 1/60 of the watts as Wh and keeps the latest watts in memory behind a mutex. Month uses the master's local timezone.
- **Dependencies**: 1.3, 2.1, 2.2
- **Acceptance Criteria**: a failed slave read records nothing for that slave and logs at debug level; the loop stops on context cancel.
- **Validation**: test one tick with fake readers, including a failed slave.

## Sprint 3: API
**Goal**: the UI gets everything from one call.
**Demo/Validation**: authenticated `curl` returns the summary and saves a price.

### Task 3.1: Add the endpoints
- **Location**: new `master/api/energy.go`, route registration in the authenticated group
- **Description**: `GET /energy/summary` returns price, totals, `machines[]` and `vms[]`, each with `liveWatts`, `monthKWh`, `monthCost`, plus `cpuEstimate: table|generic` per machine. VM rows are summed by name, so a VM that moved host keeps one total. `PUT /energy/price` rejects negative, NaN and absurd values (over 10).
- **Dependencies**: 2.3
- **Acceptance Criteria**: invalid prices return 400; both routes need auth.
- **Validation**: handler tests.

## Sprint 4: Frontend
**Goal**: the numbers appear where the user asked.
**Demo/Validation**: check in the browser with mock data, not a logged-in real master, then run the type check and tests.

### Task 4.1: Add the service
- **Location**: new `services/energy.ts`
- **Description**: typed `getEnergySummary` and `setEnergyPrice`, following `services/power-schedule.ts`.
- **Dependencies**: 3.1

### Task 4.2: Add the dashboard total and price input
- **Location**: new `components/EnergyCard.tsx`, `app/dashboard.tsx` beside `PowerScheduleCard`
- **Description**: cluster live watts, month kWh and EUR, and the price field. Label as "Estimated". With no price set, show kWh and a prompt to enter one.
- **Dependencies**: 4.1

### Task 4.3: Show per-slave energy
- **Location**: `app/dashboard.tsx`, `app/dashboard/[machine].tsx`
- **Description**: a compact line per slave, for example `~180 W · 42 kWh · €8.82`. Note when the generic CPU estimate is used.
- **Dependencies**: 4.1

### Task 4.4: Show per-VM energy
- **Location**: `app/vms.tsx`
- **Description**: fetch the summary once per list refresh, not per card, and add a small muted line to each card. Stopped VMs show 0 W plus their month total.
- **Dependencies**: 4.1
- **Validation**: check narrow card widths and long values.

## Testing Strategy
- Run `go test -tags libvirt_dlopen ./...` in `api/`, `slave/` and `master/`. Known failures already exist in `master/virsh` and `npmapi`.
- Run frontend tests and type check. Sidebar tests and dashboard type errors already fail.
- Before accepting the constants, compare the estimate with a wall meter on one real host if one is available.

## Potential Risks & Gotchas
- **Accuracy**: this is a guess, and can be well off. Every figure carries a `~` or "Estimated" label.
- **`currentCpuUsage` meaning**: confirm in the slave source whether it is per-VM or per-host percent before fixing the weight in 2.1. Drop `x cpuCount` if it is already host-relative.
- **GPUs are not counted**: GPU hosts will read low. Deliberately left out of M1.
- **All load goes to VMs**: host processes such as backups or NFS are attributed to whichever VMs are running.
- **Undercounting**: master downtime and failed reads are not backfilled. A powered-off slave correctly counts 0.
- **Price unset**: energy before the first price has no cost, so the first month's EUR is partial.
- **Deleted VMs**: rows stay until retention and count in the slave's history only through the host row. A recreated VM with the same name continues the same total for that month.
- **`GetAllVms` load**: it adds one fleet-wide call a minute. Check its cost; add a longer timeout rather than overlapping ticks.

## Rollback Plan
Remove the loop start in `master/main.go` and the UI lines. The tables and RPC are additive and can stay unused; no existing data is changed.

## After finishing
Update `docs/` and the canonical SilverBullet pages, stating what was verified locally and what was verified live.
