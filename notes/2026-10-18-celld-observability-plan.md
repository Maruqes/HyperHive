# Plan: celld fleet observability

Generated: 2026-10-18. Status: ready for implementation, with a capability gate in
step 1. Planning only; no live inspection, installation or rollout performed.

## Goal and approach

Give every fleet a useful near-live view of its workloads, logs, health and
capacity, with up to seven days of history bounded by disk space. Debugging and
capacity have equal priority.

Keep the implementation inside the existing Go agents/master and Expo frontend.
Use fixed typed collectors, existing gRPC/authentication and SQLite support. Avoid
an external observability platform, generic plugin framework or new job system.
Add one small OTLP/HTTP receiver to the agent, using official generated protobuf
messages rather than implementing the wire format. Store normalized records in a
separate observability directory on the master; never mix them with fleet state
or the main management database. Reuse the existing frontend polling pattern.

## Contract and defaults

- Collect runtime snapshots every 5 seconds; poll the open detail view every
  2 seconds without overlapping requests. Target normal-load log visibility
  within 10 seconds using a 5-second celld OTLP flush. Show collection timestamps.
- Use OTLP for app logs/traces; capture process lifecycle output and container
  stdout/stderr separately. All sources carry trusted fleet/node identity.
- Start with unsampled celld telemetry so sampling does not silently remove app
  console logs. Bound ingestion, expose dropped records, and measure overhead.
- Persist compact resource samples and normalized logs/spans/events. Request rate,
  errors and latency derived from traces are observed values; mark incomplete
  intervals when collection drops data. Do not advertise them as exact totals.
- Keep up to 7 days of all observability history. Initial configurable budgets:
  1 GiB per fleet, 10 GiB across the master observability store, and filesystem
  free-space reserve max(2 GiB, 10% of filesystem capacity).
- Delete oldest observations when any limit is reached. Account for actual files,
  indexes, journals, temporary files and buffers. Seven days is not guaranteed.
- No duplicate bucket telemetry archive by default. Keep existing bounded local
  process logs; use a bounded in-memory forwarding queue rather than a new spool.
- Offline, stale, unsupported and zero are different states. Retain the last
  successful snapshot with its timestamp; never fabricate counts.

## Workload coverage

Every deployed type must be identified and show supported observations or an
explicit capability limitation. The desired signals below require verification.

| Type | Observations |
| --- | --- |
| Workers, service bindings, Python | Script/version, live and retiring isolates, heap, request rate/latency/errors, startup errors, console logs, outbound failures |
| Dynamic Workers | Loader relationship, loaded runtimes, compile failures, rejected loads and execution-limit errors |
| Cells / Durable Objects | Class/owner, owned versus resident, activity states where exposed, restoring/capacity waits, WebSockets, alarms, overload and durability/handoff failures |
| Facets | Root relationship and available lifecycle/errors; never count as independently owned cells |
| Containers | Class, cell/node identity, running count versus cap, actual CPU/memory, start/exit failures and stdout/stderr |
| Queues | Backlog, oldest-message age, in-flight delivery, throughput, retries, dead letters and admission failures |
| Workflows | Running/waiting/paused/completed/failed, current step, retries, elapsed time and last error |
| Cron | Expression, next/last run, scheduling delay, duration, retries/failures and known missed/skipped occurrences |
| D1 / KV | Binding identity, owner and available operation latency/errors/contention |
| R2 / assets | Binding/deployment identity, storage/serving errors, duration and available cache pressure |

Queues, Workflows, D1 and KV use cells internally: show a breakdown without adding
those counts twice. Waiting Workflows can hold no isolate and are not unhealthy.
Container usage is separate from celld process RSS. Fleet CPU is not an average
of node percentages: retain per-node values and explicit aggregation semantics.
Do not scan all application objects/keys or read arbitrary databases for counters.

## Increment 1 — Prove the capabilities

Goal: a runnable disposable probe and a checked source-to-signal map.

1. Add representative fixtures and a probe under `scripts/` and
   `slave/celld/` integration tests. Reuse `scripts/test_celld_fleet.sh` setup.
   Inspect pinned v0.6.2 `/state`, capacity records, deployment metadata, OTLP
   exports and container identity. Include Workers, cells, containers, queues,
   Workflows, cron and bindings; inspect dynamic/facet coverage too.
2. Save `docs/celld-observability-capabilities.md`: for each desired field record
   source, scope, freshness, aggregation and availability. Verify queue backlog,
   workflow steps and cron history specifically; docs alone do not prove these
   are available to operators. Verify container labels distinguish overlapping
   fleets on one engine.
3. For missing fields, document the smallest read-only adapter or optional app
   instrumentation with its effort. Do not silently drop a requirement or fork
   celld. Any required upstream change is a concrete scope decision before
   dependent implementation; independent counters/logging can proceed.

Validation/demo: run fixtures and show real snapshots/logs for each supported
workload, including waiting, failed and idle states. A fabricated unsupported
value fails this gate. Produce no dependency-heavy implementation before this map.

## Increment 2 — Collect and forward observations

Depends on increment 1's verified sources.

1. Extend `api/proto/celld.proto` with typed snapshots, capability/freshness state
   and bounded record batches; regenerate using the existing Makefile. Include
   source IDs/sequences and process generation for retry deduplication.
2. Add `slave/celld/observability.go` and `telemetry.go`; wire them through
   `fleets.go`, `celld.go` and `grpc.go`. Collect allowlisted `/state` fields,
   bounded deployment metadata and verified container counts/stats. Use deadlines
   and fixed concurrency. No arbitrary URL/file-path/operator proxy.
3. Receive celld OTLP on loopback using a fleet-specific route and random token,
   injected through managed child configuration and OTLP headers. Bind fleet/node
   identity from trusted agent configuration, never payload attributes. Forward
   through master-initiated existing gRPC reads with batch acknowledgement.
4. Extend `logs.go` with a nonblocking bounded output tap; add fleet lifecycle
   records, restart/exit information and safely identified container log capture.
   Reattach after child/container restart. Avoid duplicating app console records
   across process/OTLP sources. Bound line size, batch bytes and queue memory.

Validation/demo: two fleets sharing one node remain isolated; a Worker console
error joins its request trace; container logs attach to the correct fleet; child
restart/rotation and failed forwarding do not block application output. Forged
identity, oversized telemetry and missing engine are handled explicitly.

## Increment 3 — Store, enforce limits and expose the API

Depends on increment 2's records and snapshots.

1. Add `master/services/celld_observability.go` for bounded polling/cache and
   `celld_observability_store.go` for a dedicated SQLite store using the existing
   driver. Use small per-fleet time/size-rotated segments, indexed by timestamp,
   source and trace ID. Seal/checkpoint segments before rotation. Delete closed
   segments to reclaim physical space; avoid large VACUUM/compaction jobs.
2. Enforce age, per-fleet/global actual-file budgets and free-space reserve before
   append and on periodic cleanup/startup. Reserve bounded active-segment/journal
   headroom inside each budget. Cap transaction/journal growth and query duration.
   Prune the active data safely if needed; reject/drop ingestion if no safe space
   remains. Bound overshoot to a documented maximum batch, never unlimited WAL.
3. Add authenticated read routes in `master/api/celld.go`:
   - `GET /celld/fleets/{id}/observability`: cached overview, nodes, workloads,
     capabilities, storage use, stale/error/drop/pruning state.
   - `GET /celld/fleets/{id}/logs`: bounded time range, cursor, node, workload,
     source, severity and text filters; explicit capped download mode.
   - `GET /celld/fleets/{id}/traces/{trace_id}`: fleet-scoped correlated spans.
   - `GET /celld/fleets/{id}/metrics`: bounded resource history and request summary.
   - `GET /celld/fleets/{id}/events`: lifecycle and verified workload events.
   Follow existing authentication and `Cache-Control: no-store`. GET does not
   provision or synchronously query all remote nodes.
4. Preserve central history on fleet stop/restart/removal until retention expires;
   reject ingestion after fleet deletion and clean empty stores. Expired historical
   fleet IDs must never expose another fleet's records. Store no credentials,
   request bodies or SQL values by default; sanitize known secret fields.

Validation/demo: recent data survives master/node restart and node loss. Tiny
quota tests prove actual disk reclamation, age expiry, noisy-fleet isolation,
free-space protection and bounded journals under slow readers. Retry duplicates,
late timestamps, corrupted segments and store failure produce visible gaps while
management and app serving continue.

## Increment 4 — Fleet UI and end-to-end acceptance

Depends on increment 3's API. Frontend root:
`/home/marques/projects/HyperHiveFront/HyperHive`.

1. Extend `types/celld.ts`, `services/celld.ts` and polling hooks for validated
   observability responses, cancellation, stable cursors and no overlapping polls.
2. Extend `components/celld/FleetDetailsModal.tsx` with small Overview, Workloads,
   Logs and Events components. Overview shows health, node distribution and CPU/
   memory/headroom history. Workloads show typed rows, capacity limits and gaps.
3. Logs support follow/pause, filters, bounded history/download and trace drilldown.
   Selecting a node/workload or a chart interval opens matching logs. Show observed
   time range, quota usage, early pruning, unsupported fields and dropped data.
4. Update `docs/celld.md`, browser fixture/checklist and disposable integration.
   Run backend module suites and focused race checks; frontend TypeScript/tests,
   production web export and browser flows against fixtures. Run the real disposable
   backend integration for collection/retention and one UI-to-backend smoke flow.

Acceptance/demo checklist:
- [ ] Fleet and per-node health/capacity, including saturation and stale nodes.
- [ ] All workload types recognized; verified fields and limitations explicit.
- [ ] Correct counts and relationships; no cell/isolate/container double counting.
- [ ] App, runtime, container logs and correlated request failures are debuggable.
- [ ] Normal-load observations appear within the 10-second target.
- [ ] Central history survives node outage and is searchable within bounded queries.
- [ ] Seven-day maximum plus per-fleet/global/free-space limits reclaim real bytes.
- [ ] Storage/collector failure sheds telemetry without blocking application serving.
- [ ] Authentication, fleet isolation and mobile/desktop UI work end to end.

## Risks, rollout and rollback

celld operator/telemetry schemas are unstable: parse only version-tested fields
and report unsupported versions. Missing rich queue/workflow/cron data is the main
scope risk; resolve it in increment 1, not at UI delivery. Trace loss makes derived
traffic summaries incomplete; expose that. Bound cardinality and never sum node
percentiles to produce a fleet percentile.

Implementation is local first. Deployment is a separate action: update compatible
agents before enabling master collection; introduce telemetry per fleet and check
its resource cost before broader enablement. Default application lifecycle is
unchanged. Rollback disables collection/OTLP and leaves fleet buckets, processes
and application data intact; observability files can expire separately.

## References

- Official pinned celld docs: https://github.com/denoland/celld/tree/v0.6.2/docs
- Runtime counters: https://github.com/denoland/celld/blob/v0.6.2/docs/README.md#feed-an-autoscaler
- Logs, traces, sampling and flush behavior: https://github.com/denoland/celld/blob/v0.6.2/docs/telemetry.md
- OTLP/HTTP protocol: https://opentelemetry.io/docs/specs/otlp/#otlphttp
- SQLite physical storage/journals: https://sqlite.org/pragma.html and https://sqlite.org/wal.html
- Existing source: `docs/celld.md`, `api/proto/celld.proto`, `slave/celld/`,
  `master/services/celld*`, `master/api/celld.go` and frontend celld components.
