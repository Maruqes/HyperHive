Build a Linux telemetry collector for the existing HyperHive Support “Linux Machine” template. Implement the working collector, tests, configuration example and operating instructions. Collect real information from the machine on which the collector runs and send it to the configured HTTP ingestion endpoint. Prioritize service failures, journal messages and actionable system errors; provide the essential machine/resource context listed below.

## Configuration

Read a `.env` file with these keys:

```dotenv
SUPPORT_INGEST_URL=https://YOUR_HYPERHIVE_HOST/api/support/ingest
SUPPORT_APP_ID=YOUR_LINUX_MACHINE_APP_ID
SUPPORT_APP_SECRET=YOUR_APP_INGESTION_SECRET
SUPPORT_SERVICES=sshd.service,nginx.service,postgresql.service
SUPPORT_NETWORK_INTERFACES=
```

The first three values are required. `SUPPORT_INGEST_URL` is the complete ingestion URL; use it as provided without appending another path. Obtain the App ID and one-time secret by creating an App from the Linux Machine template in HyperHive. The secret is the App ingestion secret, not a HyperHive login token. The operator should adjust `SUPPORT_SERVICES` to the machine’s actual important units. When interface selection is empty, discover active uplinks and document which interfaces contribute to aggregate traffic.

Provide `.env.example` with placeholders. Keep `.env` out of version control, never print its secrets, and load its values as data rather than executing it as a shell script. Validate configuration before starting. Verify TLS certificates normally. Keep HTTP credentials out of redirects to other origins.

## Implementation scope

Prefer Python 3 and its standard library, using existing Linux interfaces and installed commands. Use `/proc`, `/sys`, `os.statvfs`, `systemctl` and `journalctl` where appropriate. Use argument arrays for subprocesses. Avoid a new framework or monitoring stack. Keep dependencies minimal and document any optional tools needed for hardware information.

Produce a runnable collector with a continuous mode and a one-shot mode for verification. Include a systemd service example with an explicit working directory, restart behavior and the required journal access. Document how to run it manually and how to install the service. Do not modify the monitored services. If access is insufficient, report the missing capability and the required permission instead of silently pretending collection succeeded.

Use one Support App per machine. Omit `instance_id` from all requests for direct machine reporting; introducing a changing hostname or boot ID as an instance would leave permanently monitored old instances behind.

## HTTP contract

Send JSON with:

- Method: `POST`.
- URL: the exact `SUPPORT_INGEST_URL` value.
- Headers: `Content-Type: application/json`, `X-App-ID: <SUPPORT_APP_ID>`, `Authorization: Bearer <SUPPORT_APP_SECRET>`.

The request accepts only these top-level fields: `heartbeat`, `health`, `streams`, `samples`, and optional `instance_id` (omit it for this collector).

Declare stream metadata before or in the same batch as its first samples. A definition is `{"name":"cpu_usage_percent","type":"number","unit":"%","label":"CPU usage"}`. Types are immutable. It is safe to resend matching metadata on startup; keep labels and units stable. Register all 18 required stream definitions, including log/event streams that have not produced a sample yet.

A sample is `{"stream":"cpu_usage_percent","timestamp":"2026-10-27T10:00:00Z","value":24.5}`. Use UTC RFC 3339 timestamps for source time, actual numeric JSON values for measurements, and the exact stream names/types below. Do not send formatted values such as `"32 GiB"` or `"14d 11h"`; the frontend formats raw bytes and seconds.

Only `heartbeat:true` renews connectivity. Send a heartbeat every 15 seconds even if a slow collector or a large journal backlog is being processed. The App’s default timeout is 120 seconds. `health` has the shape `{"status":"healthy","message":""}`; supported statuses are `healthy`, `degraded` and `unhealthy`, and the message has at most 1024 characters. Base health on monitored service state and collection failures, not merely successful HTTP delivery. Missing optional hardware sensors alone should not mark the machine unhealthy. Publish health recovery when the underlying issue clears.

A successful response is HTTP 200 with `{"accepted":N}`, where N is the number of accepted samples. A request is atomic: a bad sample or conflicting stream definition rejects the whole batch.

Respect these server limits:

- At most 1 MiB for the entire UTF-8 JSON request body.
- At most 1000 samples and 100 stream definitions in one request.
- Finite numbers only; no null sample values, NaN or infinity.
- Text values and log messages: at most 16 KiB each in UTF-8.
- Event names: 1–120 characters. Event `fields` must be an object whose encoded JSON is at most 16 KiB.
- A log value contains only `message` and `severity`. An event value contains only `name` and `fields`. Place extra event metadata inside `fields`, not beside it.

Example request shape, with illustrative values only:

```json
{
  "heartbeat": true,
  "health": {"status":"healthy","message":"Monitored services are running"},
  "streams": [
    {"name":"cpu_usage_percent","type":"number","unit":"%","label":"CPU usage"},
    {"name":"service_failures","type":"event","unit":"","label":"Service failures"}
  ],
  "samples": [
    {"stream":"cpu_usage_percent","value":24.5}
  ]
}
```

## Required streams: all 18

### Machine identity and essential measurements

| Exact stream name | Type | Unit | Required content |
| --- | --- | --- | --- |
| `machine_info` | text | empty | Concise multiline hostname, Linux distribution/version, kernel, architecture, CPU model/core count, total RAM and boot time. Keep it readable in a narrow identity card. |
| `uptime_seconds` | number | s | Machine uptime in seconds. |
| `cpu_usage_percent` | number | % | Aggregate busy CPU percentage from interval CPU-time deltas, excluding idle and I/O wait. Avoid double-counting guest time. |
| `memory_usage_percent` | number | % | `(MemTotal - MemAvailable) / MemTotal * 100`. |
| `root_usage_percent` | number | % | For `/`, `(total - free) / (total - free + available) * 100`, using filesystem block counts. Guard zero denominators. |
| `network_receive_bytes_per_second` | number | bytes/s | Combined receive rate of the selected uplinks, calculated from counter deltas over monotonic elapsed time. |
| `network_transmit_bytes_per_second` | number | bytes/s | Combined transmit rate using the same selection and timing. |

Do not double-count loopback, bridge/veth traffic or duplicate network layers. Discard invalid rate intervals after counter resets, interface changes or reboots. Establish a real baseline before publishing CPU percentages and rates. Missing values must be omitted with a diagnostic, not replaced by fabricated zeroes.

Send identity at startup and when its content changes. Send measurements every 15 seconds. Keep raw capacities and rates in bytes and durations in seconds. The default template intentionally does not need extra CPU-mode, swap, disk-operation or process-count streams.

### Journal and error streams

All four streams below have type `log`, empty unit, and values shaped exactly as `{"message":"...","severity":"error"}`.

- `journal_logs`: the main system journal. Preserve the original message and add unit/identifier and PID context when available.
- `system_logs`: system warnings/errors plus collector diagnostics. The dashboard defaults to showing `warn`, `error` and `fatal` from this stream.
- `kernel_logs`: kernel diagnostics, including I/O faults, out-of-memory events, driver failures and network issues.
- `service_logs`: journal entries for `SUPPORT_SERVICES`, with the originating unit visible in the message.

Use one incremental journal reader where practical, then route each record to the applicable streams. Intentional routing of an error into both the journal and the error stream is allowed; do not accidentally duplicate a record within one stream. Prevent feedback loops from collecting the collector’s own delivery logs.

Map journal priority 0–2 to `fatal`, 3 to `error`, 4 to `warn`, 5–6 to `info`, and 7 to `debug`. These are the only accepted severity values. Preserve source timestamps. Handle non-text journal messages safely, and bound UTF-8 payloads without producing invalid JSON.

Persist journal cursors so restarts resume from acknowledged records. On first startup, start from a bounded recent window and then follow new entries; do not upload the entire historical journal. Handle journal rotation or an invalid cursor with a clear diagnostic and bounded recovery. Flush modest log batches promptly, typically every 1–2 seconds, without delaying heartbeats.

### Service failures and machine activity

Both streams have type `event`, empty unit, and values shaped as `{"name":"Readable event title","fields":{...}}`.

- `service_failures`: report initial detected failed units, subsequent failure transitions, restart-limit conditions and recoveries. Include unit name, state, result, exit code, restart count and diagnostic message when available. Make the title immediately useful, for example `backup-agent.service · Permission denied writing backup directory`. Do not emit the same unchanged failure every collection cycle.
- `machine_events`: collector startup, detected reboots, mount/unmount changes and network link changes. Persist the boot ID so a collector restart is not falsely reported as a machine reboot.

Use journal events for prompt notification and periodic service reconciliation to recover from missed events. A failed service should appear in both the appropriate log stream and the failure-event feed, with useful context. Do not manufacture failure events when the machine is healthy.

### Detailed snapshots

These five streams have type `event`, empty unit. Send snapshots with `{"name":"Concise human-readable summary","fields":{"items":[...]}}`. Emit them every 60 seconds, with bounded payloads. Include only actual observations; explicitly identify unavailable optional capabilities.

| Exact stream name | Snapshot items |
| --- | --- |
| `filesystems` | Mounted persistent filesystem mount point, device, filesystem type, total/free/available bytes, usage percentage and inode totals/usage where supported. |
| `network_interfaces` | Interface name, link state, IP addresses, inclusion in aggregate rates, receive/transmit rates, errors and drops. |
| `top_processes` | PID, process name, CPU percentage and resident memory bytes. Combine the top ten by CPU and top ten by memory, deduplicated by PID. Handle processes exiting between reads. |
| `services` | Failed services and the configured important units: name, active/substate, failed state and useful failure result. Distinguish a missing unit from a healthy one. |
| `hardware` | Available temperature sensors, fan RPM and disk health, including device/sensor names. Use installed hardware tools only when available; explain permission/tool limitations. |

Keep titles short and informative, such as `3 filesystems · root 67% used` or `nginx failed · 5 monitored services running`. Split snapshots before exceeding the event field limit; identify snapshot parts in their titles. Do not expose full process command lines or environment variables as routine process metadata.

## Delivery reliability

Use HTTP timeouts and exponential backoff with jitter for transient failures, rate limits and 5xx responses. Keep fresh heartbeats independent of backlog delivery. Treat 401/403 as configuration/credential failures and avoid a tight retry loop. Report 400/409 validation/type conflicts clearly; do not retry an unchanged invalid request indefinitely. Split oversized batches on 413.

Use a bounded persistent queue for unsent logs/events and save a journal checkpoint only after its routed records are acknowledged or explicitly accounted for by the documented overflow policy. State the queue limit and report dropped counts if that limit is reached. Avoid flooding the server after a long outage. The endpoint is at-least-once: a timeout after a successful server commit can produce duplicate samples on retry. Do not claim exactly-once delivery or invent an unsupported idempotency field.

Ensure shutdown terminates child processes cleanly and saves state. Redact credentials and known secrets from collector diagnostics and forwarded messages while retaining useful diagnostic context. Keep state and configuration permissions appropriately restrictive.

## Deliverables and verification

Provide the collector source, `.env.example`, systemd service example, concise setup/run instructions and focused tests. Tests must cover metric calculations and resets, unit/type correctness, journal severity/routing/cursor behavior, failure/recovery deduplication, payload limits, secret handling and HTTP error/retry behavior using a local test server.

Include a command to run once and a redacted dry-run output mode to inspect payloads before sending them. Validate that all 18 stream definitions exactly match the contract and that each currently available measurement can be sent successfully. Explain unavailable capabilities honestly. Use synthetic values only inside tests.

Finish by listing the files created, commands to run, required permissions and verification results. If actual endpoint credentials are absent, complete the implementation and local tests, then explain how to populate `.env`; do not invent credentials or claim a successful live delivery.
