# Linux Machine Support template

This document defines the collector streams used by the built-in Linux Machine dashboard.

Choose **Create an App → Start from → Linux Machine**, or create an App with `"template_id":"linux-machine"`. Its 45 widgets are saved atomically and wait for real telemetry. Each machine should have its own App and ingestion credentials. Connectivity and health use the existing Support heartbeat/health fields; see [HH Support integration](hh-support.md).

## Numeric streams

Declare each stream as `number`, with a readable `label` and the indicated `unit`. The dashboard includes percentage gauges, resource trends and current values. Percentages use 0–100. Load averages are raw values, not percentages; compare them with `cpu_core_count`.

| Stream | Unit | Meaning |
| --- | --- | --- |
| `uptime_seconds` | `s` | Time since boot |
| `cpu_core_count` | `cores` | Logical CPUs available to the machine |
| `cpu_usage_percent` | `%` | Aggregate busy CPU time, excluding idle and I/O wait |
| `cpu_user_percent` | `%` | Aggregate user and nice CPU time |
| `cpu_system_percent` | `%` | Aggregate system, IRQ and soft-IRQ CPU time |
| `cpu_iowait_percent` | `%` | Aggregate I/O-wait CPU time |
| `load_average_1m`, `load_average_5m`, `load_average_15m` | empty | Raw load averages |
| `memory_usage_percent` | `%` | `(total - available) / total * 100` |
| `memory_total_bytes` | `bytes` | Total RAM |
| `memory_available_bytes` | `bytes` | Available RAM |
| `memory_cached_bytes` | `bytes` | Cached memory reported by the collector |
| `swap_usage_percent` | `%` | Used swap / total swap; zero when swap is disabled |
| `swap_in_bytes_per_second`, `swap_out_bytes_per_second` | `bytes/s` | Swap traffic rates |
| `root_usage_percent` | `%` | `(total - free) / (total - free + available) * 100` for `/`; includes reserved space as unavailable |
| `root_available_bytes` | `bytes` | Space available to unprivileged users on `/` |
| `root_inode_usage_percent` | `%` | Used inodes / total inodes on `/` |
| `disk_read_bytes_per_second`, `disk_write_bytes_per_second` | `bytes/s` | Aggregate disk throughput |
| `disk_read_operations_per_second`, `disk_write_operations_per_second` | `ops/s` | Aggregate completed I/O operation rates |
| `disk_io_latency_ms` | `ms` | Interval read/write time divided by completed read/write operations; zero if none completed |
| `network_receive_bytes_per_second`, `network_transmit_bytes_per_second` | `bytes/s` | Aggregate network throughput |
| `network_errors_per_second` | `errors/s` | Combined receive/transmit error rate |
| `network_dropped_packets_per_second` | `packets/s` | Combined receive/transmit dropped-packet rate |
| `process_count` | `processes` | Total processes |
| `process_running_count` | `processes` | Running/runnable processes |
| `process_blocked_count` | `processes` | Processes in uninterruptible sleep |
| `process_zombie_count` | `processes` | Zombie processes |
| `failed_service_count` | `services` | Failed services reported by the service manager |
| `cpu_temperature_celsius` | `°C` | Primary CPU/package sensor reading, when available |

Calculate CPU percentages and counter rates from interval deltas. Omit the first interval and intervals affected by counter resets. Disk aggregates must use one consistent device layer, avoiding double-counting partitions and their parent disks. Network aggregates should use the machine's selected uplink interfaces, excluding loopback and duplicate bridge/veth traffic; report the selected devices/interfaces in their detail snapshots. Disk latency is weighted by operation count, not a sum of device averages.

## Identity and detailed snapshots

`machine_info` is a `text` stream containing readable multiline identity: hostname, distribution/version, kernel, architecture, CPU model and boot time. Send it at startup and when those values change.

The following streams are `event` streams displayed as expandable feeds. Send values as `{"name":"Snapshot title","fields":{"items":[...]}}`. Each snapshot replaces the collector's view conceptually; older snapshots remain in Support history. They do not generate additional widgets automatically.

| Stream | Suggested item fields |
| --- | --- |
| `filesystems` | mount point, device, filesystem type, total/free/available bytes, usage %, total/free inodes and inode usage % |
| `network_interfaces` | interface, link state, IP addresses, whether included in aggregate rates, throughput, errors and drops |
| `top_processes` | PID, process name, CPU %, resident memory bytes; include the top ten by CPU and the top ten by memory, deduplicated by PID |
| `services` | service name, active/substate, failed state; include failed services and selected important services |
| `hardware` | sensor/device name, temperature, fan RPM and disk health where supported; identify devices included in disk aggregates |

Keep snapshots bounded: prioritize mounted filesystems, relevant interfaces, selected services and a small hardware summary. Split large snapshots into bounded events. Support limits each event's encoded `fields` to 16 KiB. Optional or inaccessible measurements should be omitted rather than replaced with fabricated zeroes; send a collector warning through `system_logs` when collection fails.

## Logs, events and timing

- `system_logs`: `log` values with `message` and Support severity (`debug`, `info`, `warn`, `error`, `fatal`). Forward selected system/kernel/service warnings and errors, plus collector diagnostics.
- `machine_events`: `event` values with `name` and structured `fields`, for reboots, service failure/recovery, mount changes and network link changes.
- Send a heartbeat and resource metrics every 15 seconds. The default App heartbeat timeout remains 120 seconds and can be changed in App settings.
- Send process/service/detail snapshots every 60 seconds; identity on startup/change. Send logs and machine events as they occur, with bounded batches and backoff on failures.

The template defines the dashboard and this sender contract. Collector installation, privileges and collection logic are separate work. This contract is locally source-reviewed, not evidence of a deployed Linux collector.
