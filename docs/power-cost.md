# Power cost estimate

HyperHive estimates the electrical power of every connected host and attributes
the CPU load to its running VMs. It is an estimate built from hardware
inventory and usage, not a measurement from a power meter.

## What is estimated

One tick runs every 10 seconds for each connected slave:

1. Read the host CPU usage and the VM list from the slave.
2. Estimate the host idle and load watts from the cached hardware profile.
3. Split the load watts across running VMs by `CPUUsage * CPUCount`.
4. Store `watts * tickHours` watt-hours for the host and for each running VM.
   `tickHours` is the real time since the last tick, capped at one minute so
   master downtime is not billed.

The hardware profile (CPU sockets/models/cores, RAM, disk kinds) is cached per
machine for one hour and refreshed after a failed read.

### Host formula

```
cpuTDP   = table package TDP, brand median * cores, or 8 W * cores (generic)
idleW    = 30 + 0.15*cpuTDP + 0.4*RAM_GB + 7*HDDs + 3*SSDs + 5*NVMes
loadW    = 0.85 * cpuTDP * meanCPUUsage   (meanCPUUsage is 0..1)
hostW    = idleW + loadW
```

Usage is sampled at each tick, so live watts follow CPU load within about 10
seconds. The web energy card and VM list also refresh every 10 seconds.

`cpuEstimate` reports `table` when every socket matched the bundled TDP table,
or `generic` when a fallback was used.

### VM split

```
weight(vm)  = vmCPUUsagePercent * vmCPUCount   (running VMs only)
vmWatts(vm) = loadW * weight(vm) / sum(weights)
```

A stopped VM gets 0 W and writes no usage row. A running VM with 0% usage still
writes a row with 0 Wh. Every live VM, including 0 W ones, appears in the
summary.

### Storage

Usage is stored per month (`YYYY-MM`, master local time), machine and VM name:

```
wh   = watts * tickHours
cost = wh / 1000 * pricePerKWh   (0 when no price is set)
```

The cost is stored with each row at the price in force then, so changing the
price does not rewrite history. Rows older than 24 months are deleted on the
first tick of a new month.

### Timeline samples

Every tick also adds each ok host and each running VM to `energy_samples`, in
one transaction, at three bucket sizes:

| Step | Kept for | Timeline range |
| --- | --- | --- |
| 5 min (300 s) | 30 days | `30d` |
| 30 min (1800 s) | 90 days | `90d` |
| 1 h (3600 s) | 183 days | `6m` |

A row is `(step, ts, kind, machine, name, wh, running_s)`: `ts` is the bucket
start in Unix seconds, `kind` is `host` (empty `name`) or `vm`, and `running_s`
is the time the series was seen in that bucket (the same capped tick time as
`wh`). There is no roll-up job; each tier is trimmed once per hour. A failed
sample write is logged and never blocks the monthly totals.

State is read from the same rows: a VM bucket with no row while a host has one
means "stopped"; a bucket where no host has a row means "no data" (master or
host down).

### Monthly projection

```
avgW      = Wh of the last 7 days of hourly samples / hours since the series' first sample in that window
remaining = avgW * whole hours left in the month / 1000
projectedMonthKWh  = monthKWh + remaining
projectedMonthCost = monthCost + remaining * pricePerKWh   (price 0 while unset)
```

The average runs over elapsed time, not only running hours, so a host on a
power schedule or a VM that is often stopped is not projected as always on.
Totals are the sum of the host projections. With no samples the projection
equals the month to date.

## Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/energy/summary` | Live watts, current-month kWh and cost per machine and per VM, plus totals. `pricePerKWh` is `null` when unset. |
| `GET` | `/energy/timeline?range=30d\|90d\|6m[&kind=host\|vm&name=][&machine=]` | Stored buckets per host and VM. Invalid `range` or `kind`, or `name` without `kind`, is rejected with 400. |
| `PUT` | `/energy/price` | Sets `{"pricePerKWh": number}`. Accepts 0 to 10; missing, non-finite, negative or larger values are rejected with 400. |

All routes sit in the authenticated API group.

The summary is a union of the current month's stored host rows and the live
snapshot. VM rows are summed by name, so a VM moved between hosts has a single
line. Totals are computed from machine rows only; VM watts are a subset of the
host watts and are never added again. Arrays are sorted by name and always
present.

`months` holds the last 12 months, oldest first and ending with the current
one, as `{month, kWh, cost}` summed from host rows. Months with no stored usage
are present with zeros. The web energy card draws them as a column chart scaled
to the highest month.

The summary's `totals`, `machines` and `vms` entries also carry
`projectedMonthKWh` and `projectedMonthCost`.

The timeline answers `{start, stepSeconds, count, series:[{kind, machine, name, wh, runningS}]}`.
`wh` and `runningS` have `count` values; index `i` is the bucket starting at
`start + i*stepSeconds`, and the last one is the bucket still filling. `null`
means no data; `0` on a VM means stopped. With `kind` and `name` it returns
that one series at the range's own step (`name` is the machine name for a
host). Without `name` it returns every series at one hour per bucket. VM series
are merged by name across machines and have an empty `machine`.

The web Power page shows the energy card, the timeline card and the power
schedule card.

## Limitations

- Estimates only. Real power depends on PSU efficiency, temperature, workload
  type and hardware not in the table.
- GPUs, HBAs, fans and other PCI devices are not counted.
- All CPU load is attributed to VMs. Host processes, storage and network work
  are visible only as part of the host row.
- There is no backfill: months before the first tick stay empty, and the
  timeline starts when the master first runs this version.
- The projection is noisy in the first days, and master downtime inside the
  7-day window lowers it.
- A merged VM series shows "stopped" while its own host is down if any other
  host is up.
- A slave that does not implement the power hardware RPC (or reports no CPU
  sockets) records nothing and disappears from the live summary.
- A failed read for one slave never blocks or corrupts the others; that host is
  simply absent from that tick.
