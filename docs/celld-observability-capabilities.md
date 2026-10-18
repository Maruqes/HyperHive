# celld observability capabilities (v0.6.2)

Source checked against the pinned v0.6.2 archive on 2026-10-18. Run
`CELLD_TEST_BINARY=/path/celld python3 scripts/probe_celld_observability.py /tmp/probe`
with esbuild on PATH. The disposable probe uses real celld dev storage, not
production fleet buckets. Snapshots, HTTP results, process output and OTLP
protobuf batches are written to the selected output directory. It removes its
child processes and application data on exit.

Initial local probe: Worker, cell, queue, waiting/failed Workflow, D1/KV/R2,
dynamic Worker and facet requests returned 200; deliberately failed request
returned 500. Six real OTLP batches were exported. Idle and post-request state
were inspected. Cron expression was deployed; history is not exposed. Container
and Python fixtures can be selected with CELLD_PROBE_PROJECT; they require their
own build dependencies and remain separately qualified below.

| Signal | Source and scope | Freshness / aggregation | Availability / limitation |
| --- | --- | --- | --- |
| CPU, RSS, allocator in-use, FDs, headroom, pressure | `/state.node_load`, one celld process | collected every 5s; preserve node values, do not average percentages | verified; CPU percent x100 can exceed 10000 on multicore nodes; container usage excluded |
| Owned/resident cells, WebSockets, restoring | `/state.node_load` | per node; owned can be null during recovery | verified; resident is a subset of owned, not additive |
| Activation/capacity wait, phases, handoff failures | `/state` | per-node gauges/cumulative counters | verified schema; cell state census includes internal services |
| Primary/service/cell isolate pools, retiring, heap | `/state.deployment.isolates` and draining generations | per pool/generation, heap is sampled from accessible isolates | verified; primary also appears in services: emit service pools once, not a second primary total |
| Script/version/bindings/cron/container limits | bounded `deploy/current.json` then referenced manifest | current deployment, poll in background | fixed deployment metadata, never raw vars/secrets; secondary scripts represented by service pools |
| Worker logs, handler/outbound/cell spans | official OTLP/HTTP protobuf | 250ms OTLP flush + notification-driven gRPC tail + authenticated SSE; observed request summaries only | real exports verified; console has trace/span IDs; managed probes exposed missing matching spans for some thrown Worker failures; unsampled incoming headers overridden by always_on |
| Runtime/start/exit failures | child stdout/stderr and agent lifecycle tap | bounded memory queue, live gRPC tail with durable unary acknowledgements; 5s unary recovery for older agents | verified process path; console output is OTLP only in pinned runtime |
| Dynamic Workers / Python | loader or python feature in manifest, OTLP and shared pool context | per deployment/request | dynamic fixture verified; no independent loaded-runtime census; Python runtime separately dependency gated |
| Facets | loader metadata, parent cell and OTLP | per parent/request | real facet fixture verified; never independently counted as owned cells; no facet census API |
| Containers running by class | `/state.node_load.container_instances` | per node; compare manifest max_instances | source verified; absence means zero only for pinned supported schema |
| Container identity/stats/stdout/stderr | Docker Unix socket; labels `celld.node`, `celld.cell`, `celld.class` | bounded per-container reads | source `container.rs`; agent injects unique CELLD_NODE per child so overlapping fleets are isolated; engine absence explicit. Real container/fence execution requires platform qualification |
| Queue backlog/oldest/in-flight/retries/dead letters | no read-only operator endpoint | unknown | unsupported; optional app-maintained counters/logs, small app change; do not query internal cell SQL |
| Workflow step/status totals/retries/elapsed | app binding `instance.status()` for an already known ID only | no fleet-wide enumeration | unsupported fleet census; optional app status instrumentation, small app adapter; waiting does not imply unhealthy |
| Cron next/last/delay/missed/history | manifest gives expressions only | no operator history | unsupported; app scheduled-handler logs provide observed execution only; exact missed history needs upstream scheduler API |
| D1/KV/R2/assets operations | binding metadata, cell/outbound spans and runtime errors | observed request context | no dedicated exact operation counters/contention/cache API; optional app instrumentation |
| Internal celld telemetry drops | warning `telemetry shed`, not `/state` | process log evidence | no exact exported drop counter; summaries always explicitly observed/incomplete-capable |

No application object/key scan, arbitrary SQL read, request body capture or
upstream fork is required. Missing detailed queue/Workflow/cron fields are
reported as unsupported rather than zero. A richer census would require a new
upstream read-only operator API and is independent of this backend.

Primary references: [pinned runtime docs](https://github.com/denoland/celld/tree/v0.6.2/docs),
[actor snapshots](https://github.com/denoland/celld/blob/v0.6.2/crates/celld/actor.rs),
[load schema](https://github.com/denoland/celld/blob/v0.6.2/crates/celld/ownership_store.rs),
[container labels](https://github.com/denoland/celld/blob/v0.6.2/crates/celld/container.rs),
[telemetry](https://github.com/denoland/celld/blob/v0.6.2/docs/telemetry.md).

A longer disposable run also observed cron console output (16 logs/34 spans in
that capture) and app binding responses explicitly showed waiting/errored
Workflows. Managed S3-backed tests confirmed console-error trace IDs but exposed
missing matching spans for some thrown Worker failures. Such a trace returns its
retained logs and an incomplete marker; no synthetic span is created. Server-span
errors count only statuses/HTTP error attributes actually exported by celld.

The managed two-fleet integration also verifies completed HTTP 500 responses
with console-error logs correlated to request spans. Resident IDs/generations
from `/state.residents` and `deployment.cells` are exposed as bounded workload
rows, including internal Queue/Workflow/D1/KV/cron breakdown. Per-cell activity
beyond residency is unavailable; node phase totals remain separate.
