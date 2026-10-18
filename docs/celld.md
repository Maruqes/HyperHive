# celld fleets

A fleet owns one SILO bucket, one scoped user/policy, and one celld process on
each explicitly selected HyperHive server. A server can host several fleets.
The master hosts shared SILO and CLI tools; it runs a fleet child only when its
machine name is selected. New machines are never automatically enrolled.

The existing agent owns each child, restarts crashes after 10 seconds, and drains
on agent shutdown/re-exec. Keep the existing PM2 `--kill-timeout 65000` setting;
all children drain concurrently within the 60-second agent grace.

## Authenticated HTTP API

These routes use normal HyperHive authentication and are also available with
the existing `/api` prefix. All responses use `Cache-Control: no-store`.

| Method | Route | Behavior |
| --- | --- | --- |
| POST | `/celld/fleets` | Save and start an explicit fleet |
| GET | `/celld/fleets` | Fleets, actual SILO buckets/count and storage status |
| GET | `/celld/fleets/{id}` | Observed status, nodes and CLI deployment settings |
| PATCH | `/celld/fleets/{id}` | Change name, server list or stopped fleet ports |
| DELETE | `/celld/fleets/{id}` | Drain every assigned agent, purge data/bucket/user/policy |
| POST | `/celld/fleets/{id}/start` | Reuse the bucket and local data |
| POST | `/celld/fleets/{id}/stop` | Disable automatic restart and drain; keep data |
| POST | `/celld/fleets/{id}/restart` | Persisted rolling drain/start |
| GET | `/celld/fleets/{id}/credentials` | That fleet's AWS access/secret keys |
| GET | `/celld/health` | Storage plus all fleets whose desired state is running |

Create and lifecycle changes return **202 after durable save**. Creation includes
`Location: /celld/fleets/{id}`. Poll detail for progress; GET never installs,
provisions, or performs live health probes. Observations include `checked_at`.

```sh
api=http://MASTER_MANAGEMENT_IP:9595
curl -fsS -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' "$api/celld/fleets" \
  -d '{"name":"app","servers":["machine1","machine2"]}'
```

Optional explicit ports: `"ports":{"http":18080,"internal":18081}`. Otherwise
choose the first available pair starting at 18080/18081. The same stable pair is
reserved on every selected server; overlapping fleets must use different ports.
Ports must be distinct integers from 1 through 65535. External bind conflicts
appear as node errors during background startup. Ports can change only while
stopped; stop completion precedes a port edit.

Names are unique and nonempty. Unknown/duplicate machine names and empty server
lists return 400; name/reservation conflicts and conflicting pending edits return
409; absent fleet IDs return 404. IDs and buckets remain stable through renames.
A failed background operation retains its progress and retries every 30 seconds.
A failed operation can be stopped or deleted once its current attempt finishes.

```json
{
  "id":"f0123456789abcdef01234567",
  "name":"app",
  "desired_state":"running",
  "status":"running",
  "servers":[{
    "name":"machine1","ip":"192.168.76.1","connected":true,
    "running":true,"healthy":true,
    "ports":{"http":18080,"internal":18081},
    "url":"http://192.168.76.1:18080"
  }],
  "ports":{"http":18080,"internal":18081},
  "storage":{
    "bucket":"hyperhive-celld-f0123456789abcdef01234567",
    "endpoint":"http://192.168.76.1:9000","region":"us-east-1"
  },
  "urls":["http://192.168.76.1:18080"],
  "deploy":{
    "command":"celld deploy . --bucket s3://hyperhive-celld-f0123456789abcdef01234567 --endpoint http://192.168.76.1:9000 --region us-east-1",
    "credentials_url":"/celld/fleets/f0123456789abcdef01234567/credentials",
    "environment":["AWS_ACCESS_KEY_ID","AWS_SECRET_ACCESS_KEY","AWS_REGION=us-east-1"]
  },
  "checked_at":"2026-10-18T12:00:00Z"
}
```

Status is `provisioning`, `running`, `stopped`, `degraded`, `deleting` or `error`;
`desired_state` independently records `running`, `stopped` or `deleting`. Node
errors and fleet errors explain pending work. During membership edits all new
members become ready before removed members drain and lose their local data.
An edit of a stopped fleet keeps it stopped. Restart replaces one node at a time;
a one-node fleet has an interruption.

Deletion waits for every assigned/retiring agent, including offline agents,
before purging the bucket and scoped credentials. Cleanup acknowledgements and
rolling-restart progress survive master restarts. A successful DELETE response
acknowledges intent; only a subsequent detail 404 means cleanup finished.

List has `{ "fleets": [...], "storage": {...}, "reset_pending": false }`.
Storage includes `healthy`, `error`, `checked_at`, `bucket_count`, `buckets`, and
`managed_bucket_count`. A top-level `error` describes coordinator/reset failures
without replacing the observed storage counts. Actual SILO buckets include unrelated buckets; the
managed count includes retained/deleting fleet records. Storage failures return
unknown actual counts/names (`null`) and an error. There are no object/size scans.
Health returns 200 when storage and every expected running fleet are healthy,
otherwise 503, including while the legacy reset is pending. It includes
per-fleet summaries and the observed machines for expected running fleets. Stopped fleets and
unassigned servers do not make health fail. Zero fleets can be healthy.

## Frontend

The separate HyperHiveFront checkout exposes fleet management at
/celld-fleet (sidebar: **celld Fleets**). It uses the existing login and machine
inventory, shows explicit fleet membership, actual/managed bucket counts and
cached check timestamps, and polls without overlapping reads every 30 seconds.

Create/edit forms select servers and optional ports; running fleets must stop
before their ports can change. Lifecycle controls show 202 as accepted progress.
Deletion requires the fleet name and stays visible while offline agents keep it
pending. Detail shows server readiness/errors, application URLs and CLI settings.
Scoped credentials are fetched only on reveal and cleared when detail closes;
copying the deploy command or credential environment is an explicit action.

Frontend sources live in app/celld-fleet.tsx, components/celld/,
services/celld.ts, types/celld.ts and the polling hooks. Its browser fixture
and checklist are under tests/; they use synthetic loopback data, alongside
the separate real backend integration tests described below.

## Deploy through the CLI

Use a machine on the trusted management network. Obtain scoped credentials from
`GET /celld/fleets/{id}/credentials`, which returns
`aws_access_key_id` and `aws_secret_access_key`. Set `AWS_ACCESS_KEY_ID`,
`AWS_SECRET_ACCESS_KEY`, and `AWS_REGION=us-east-1` in your operator environment,
then run the exact command from detail's `deploy.command` in your Wrangler
project. Install `esbuild` on PATH when building Worker code.

Credentials grant control of that fleet. Ordinary fleet/list/health responses
and lifecycle logs exclude them; SILO administrator credentials stay on the
master. There is no upload/build/deploy API, proxy, TLS or load balancer in this
integration. Reach applications using the reported node URLs.

## Runtime and ports

No new required environment variables. Continue using the existing master/slave
`.env` files. The master optionally reads `CELLD_VERSION` (default `v0.6.2`).
SILO stays pinned to `docker.io/pgsty/silo:RELEASE.2026-09-03T13-18-01Z`.

| Service | Listener |
| --- | --- |
| Fleet application HTTP | Selected node management IP, fleet HTTP port |
| Fleet peers/operators | Selected node management IP, fleet internal port |
| SILO S3 | Master management IP:9000 |
| SILO console | Container:9001, unpublished |
| HyperHive API | Existing :9595 |
| Master/agent gRPC | Existing :50051 / :50052 |

Keep storage, peer/operator ports and gRPC inside the management network. The
agent validates storage against its configured master address. Existing gRPC
trust carries the scoped configuration. Every bucket passes conditional-create,
conditional-update, read-after-write and exact-range probes before startup.
An empty bucket receives the bootstrap Worker; existing deployments are retained.

| State | Location |
| --- | --- |
| Master fleets, desired state, progress, admin/scoped credentials | `/var/lib/hyperhive/celld-fleet.json` (0600) |
| Shared SILO data / container | `/var/lib/hyperhive/silo` / `hyperhive-silo` |
| Master-only SILO environment | `/var/lib/hyperhive/silo-admin.env` |
| Fleet config, lock, logs, SQLite/replication data | `/var/lib/hyperhive/celld/fleets/{id}/` |
| Shared installed executable | `/usr/local/bin/celld`, exposed at `/usr/bin/celld` |
| Bootstrap project and build tools | `/var/lib/hyperhive/celld-bootstrap`, `/var/lib/hyperhive/celld-tools` |

Agent startup never resumes children from local enabled files. Master Sync sends
assigned and running membership before Configure; removed, stopped and deleting
fleets cannot resume from stale state. Cold recovery allows the previous lease
to expire. Installation is serialized per machine; ownership locks and parent
death handling prevent duplicate/orphan children. The managed installation
folders/binary are repaired to 0755, while fleet state remains private.

Each fleet's `logs/celld.log` retains at most 10 MiB plus three rotated copies
(40 MiB total). Logging preserves 256 MiB free space, discards output under
pressure without killing the child, and resumes automatically. Agent lifecycle
messages stay in PM2 output.

## Reset and rollout

Deploy the new agents **before** enabling the new master coordinator/API.
Existing singleton state migrates to a persisted reset with zero new fleets.
Sync first disables/drains legacy children; their local state/data is removed.
The reset waits for every remembered legacy agent, then removes only
`hyperhive-celld` and its scoped user/policy. Shared SILO, unrelated buckets and
admin credentials remain. Creation returns 409 while reset is pending; list
reports `reset_pending`. Reconnecting agents receive authoritative state even
when no new fleets exist.

This purge is destructive when deployed. Code rollback before purge can retain
legacy data; afterward recovery requires a separate existing backup. Do not run
old binaries against the new fleet state. Source implementation alone performs
no live server reset or rollout.

## Validation

Run `go test ./...` inside `api`, `master`, and `slave`, then:

```sh
(cd master && go test -race ./services ./api)
(cd slave && go test -race ./celld)
scripts/test_celld_fleet.sh
```

The disposable script needs Docker, curl/gzip, Node/npm and Go. It installs the
pinned binary into a temporary directory, uses temporary loopback SILO storage,
and cleans up on exit. It checks compatibility, bootstrap preservation, child
crash/recovery, overlapping fleets, independent counter deployments/data, scoped
permissions, port conflicts, synchronized agent restart and complete deletion
while retaining an unrelated bucket. It never starts the privileged mains or
connects to physical HyperHive servers.

References: [celld v0.6.2](https://github.com/denoland/celld/blob/v0.6.2/docs/README.md),
[storage and shutdown guarantees](https://github.com/denoland/celld/blob/v0.6.2/docs/guarantees.md).

## Backend observability

The agent collects the pinned v0.6.2 `/state` every five seconds. The master reads
bounded acknowledged batches through the existing authenticated gRPC connection,
with four concurrent readers. HTTP GETs read cache/history and never fan out to
nodes or provision resources. A separate background metadata reader checks only
`deploy/current.json` and its validated manifest path; it never reads app keys,
SQL values, Worker variables or request bodies. See
[the field-by-field capability map](celld-observability-capabilities.md).

Managed children export unsampled OTLP/HTTP logs and traces every five seconds
into a private loopback receiver, with a separate random header token and route
per fleet. Official generated OpenTelemetry protobuf types decode the payload.
Fleet/node identities come from the assigned agent and requested fleet, not
resource attributes. A console error retains trace/span IDs for correlation.
Some thrown Worker failures in managed v0.6.2 export their console error and trace
ID without a matching span. Trace reads preserve these gaps and never synthesize
spans. Error summaries count only the statuses/error attributes celld exports.
Runtime stdout/stderr and lifecycle events are separate sources. Docker reads
are scoped to the unique `CELLD_NODE` session injected into each child, then
labels are checked again before reading stats/logs. Engine absence is reported;
container resource usage is separate from celld RSS. Podman-only discovery is
not implemented: use the same `DOCKER_HOST=unix://...` as the runtime.

Typed snapshots retain nullable resource values: unknown ownership during
recovery is not zero. Workload pool counters are optional so configured binding
rows do not fabricate runtime counts. The primary script also appears in the
service census and is counted once. Cell pools include the internal Queue,
Workflow, D1 and KV cells; never add those breakdowns to the total cell count.
Facets have no independent owned-cell count. CPU is returned per node/process:
100% means one core, and node percentages are not averaged into a fleet value.
Missing queue backlog, Workflow fleet/step census and cron history are explicit
capability gaps. Optional app instrumentation can add observed logs/spans.

Authenticated routes, all with `Cache-Control: no-store`:

| GET route | Result |
| --- | --- |
| `/celld/fleets/{id}/observability` | cached nodes/resources/workloads/capabilities, deployment metadata, storage/gap state |
| `/celld/fleets/{id}/logs` | normalized log records and continuation cursor |
| `/celld/fleets/{id}/traces/{trace_id}` | fleet-scoped correlated spans and logs |
| `/celld/fleets/{id}/metrics` | bounded resource history and observed server-span count/errors/rate/mean/max duration |
| `/celld/fleets/{id}/events` | captured process start/exit events |

History queries accept `from`/`to` as Unix milliseconds (default: the last hour,
maximum seven days), `cursor`, `node` (management IP), `workload`, `source`,
`severity`, `text` and `limit` (default 200, maximum 500). Trace IDs are 32
lowercase hexadecimal characters. The cursor pins the end timestamp; reuse the
original filters and start time when continuing. Records sort by observation
time then stable ID, including late arrivals. Pagination is a bounded live
history view: new late records before an already consumed cursor are not replayed.
`download=true` on logs returns NDJSON, capped at 2,000 records and 1 MiB;
`X-Observability-Truncated` and `X-Next-Cursor` report a capped export. Query
execution has a two-second deadline. Resource-history pages are paginated;
request summaries cover the selected time interval and are always marked observed
and potentially incomplete. They are not exact application totals or fleet
percentiles. Empty successful ranges differ from unavailable storage/query errors.

History is kept separately at `/var/lib/hyperhive/celld-observability/{fleet}/`,
mode 0700, with private SQLite files. No bucket telemetry archive or new agent
spool is created. Agents keep at most 2 MiB / 2,048 queued records; each forwarded
batch is at most 256 KiB / 128 records and each message body at most 8 KiB.
Oversized/invalid input is rejected and losses are exposed. The receiver accepts
at most two concurrent 1 MiB protobuf bodies, and remembers 64 recent batch
hashes for OTLP retries. Master retries use persisted record identities across
segments; the next read acknowledges only a committed batch. Known secret fields,
URL query values and the fleet/storage credentials are redacted. Application
free text must still avoid secrets. Agent loss counters are process-local and
include rejected batches as loss signals; they are not exact upstream loss totals.
celld's internal exporter can also shed records and reports that in runtime logs.

Each segment rotates at one hour or at most 8 MiB (smaller for small quotas).
Databases close after each transaction, use DELETE rollback journals and fixed
page limits, and never leave persistent WAL readers. Physical file allocation,
indexes and sidecars count toward the budgets. Before each append the master
reserves two whole segment sizes plus 256 KiB inside both budgets, covering the
new database, rollback journal and maximum forwarding batch. This reduces usable
history near the limit. Under a slow reader, a write times out and remains
unacknowledged rather than allowing an ever-growing journal. No VACUUM job runs.

Default limits are seven days, 1 GiB per fleet, 10 GiB globally and a filesystem
free reserve of the larger of 2 GiB or 10% of capacity. Optional master settings
`CELLD_OBSERVABILITY_FLEET_BYTES` and `CELLD_OBSERVABILITY_TOTAL_BYTES` accept
integer byte budgets (minimum 1 MiB). Cleanup runs at startup, before append and
every collection pass. It deletes whole oldest closed segments, reclaiming real
bytes; expiry follows the oldest observation timestamp in each segment, so
late arrivals cannot extend retention. Whole-segment deletion can also prune
newer observations early (normally up to one hour, longer for late timestamps). A noisy fleet reaches its own quota before other fleets are
considered for global/free-space pressure. Seven days is a maximum, not a promise.
Corrupt/unreadable history produces visible query gaps and refuses unsafe writes;
management and application serving continue. Storage/drop/pruning counters reset
with the master; retained samples and records survive restart.

Stop/restart keep central history. Delete stops ingestion immediately when delete
intent is accepted, and leaves history readable by the same historical fleet ID
until retention reclaims it. Expired/unknown IDs return 404. Overview falls back
to retained last snapshots after master restart/node loss and marks them stale.
Samples older than 15 seconds are marked stale; failed collection keeps the last
successful values and timestamp. Runtime records normally become available in
about five seconds of flush plus five seconds of master polling; this is a target
under normal load, not a latency guarantee.

Apply compatible agents before the master. No deployment is performed by this
source change. To roll back collection, set `CELLD_OBSERVABILITY=0` on agents and
master and restart them: it disables collectors/OTLP without changing fleet data.
Existing central history files remain available for manual retention/removal;
with master collection disabled its periodic cleanup is disabled too.

Validation tools: `scripts/probe_celld_observability.py` creates an isolated real
fixture and exports snapshots/OTLP for decoder checks. Set
`CELLD_OBSERVABILITY_PROBE` to that output directory when running slave tests.
`scripts/test_celld_fleet.sh` additionally exercises actual SILO-backed managed
children and two overlapping fleets. No privileged HyperHive entrypoints or
production endpoints are started by these tests.

Collection has four concurrent slots per agent, and reports stale snapshots if
scheduled work cannot run. Runtime resident rows include class and deployment
generation, with internal service cell kinds identified. They are a bounded
breakdown, never additional fleet cell totals. Container log stream identity is
kept in `attributes.stream`; a capped tail or unavailable stats marks collection
partial.

`storage.rejected_batches` counts refused ingestion attempts, which can later
succeed on retry; it is separate from observations discarded for expiry. Master
storage failures do not acknowledge pending records. Agent drops and upstream
exporter loss remain separate signals.
