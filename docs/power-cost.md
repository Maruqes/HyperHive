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

## Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/energy/summary` | Live watts, current-month kWh and cost per machine and per VM, plus totals. `pricePerKWh` is `null` when unset. |
| `PUT` | `/energy/price` | Sets `{"pricePerKWh": number}`. Accepts 0 to 10; missing, non-finite, negative or larger values are rejected with 400. |

Both routes sit in the authenticated API group.

The summary is a union of the current month's stored host rows and the live
snapshot. VM rows are summed by name, so a VM moved between hosts has a single
line. Totals are computed from machine rows only; VM watts are a subset of the
host watts and are never added again. Arrays are sorted by name and always
present.

`months` holds the last 12 months, oldest first and ending with the current
one, as `{month, kWh, cost}` summed from host rows. Months with no stored usage
are present with zeros. The web energy card draws them as a column chart scaled
to the highest month.

## Limitations

- Estimates only. Real power depends on PSU efficiency, temperature, workload
  type and hardware not in the table.
- GPUs, HBAs, fans and other PCI devices are not counted.
- All CPU load is attributed to VMs. Host processes, storage and network work
  are visible only as part of the host row.
- There is no backfill: months before the first tick stay empty.
- A slave that does not implement the power hardware RPC (or reports no CPU
  sockets) records nothing and disappears from the live summary.
- A failed read for one slave never blocks or corrupts the others; that host is
  simply absent from that tick.
