# celld fleets API
2026-10-18 · Plan only · Final scope check pending

One fleet = one SILO bucket, scoped credentials, and one celld process per selected server. Servers can host multiple fleets. Reuse the existing Go services, private JSON state, gRPC, process watcher and retry loop; no new dependency or job system.

## API
Existing HyperHive authentication. `servers` contains known machine names; unknown/duplicate names and empty lists are rejected. Names are unique (`409` on conflict); IDs and bucket names stay stable through renames.

```http
POST   /celld/fleets
GET    /celld/fleets
GET    /celld/fleets/{id}
PATCH  /celld/fleets/{id}
DELETE /celld/fleets/{id}
POST   /celld/fleets/{id}/start
POST   /celld/fleets/{id}/stop
POST   /celld/fleets/{id}/restart
GET    /celld/fleets/{id}/credentials
```

```json
// POST body; ports optional, otherwise assigned automatically
{"name":"app","servers":["machine1","machine2"],"ports":{"http":18080,"internal":18081}}

// 202 response; GET returns the same fleet shape with current status
{"id":"f123","name":"app","desired_state":"running","status":"provisioning","servers":[{"name":"machine1","running":false,"healthy":false},{"name":"machine2","running":false,"healthy":false}],"ports":{"http":18080,"internal":18081},"storage":{"bucket":"hyperhive-celld-f123","endpoint":"http://<master-ip>:9000","region":"us-east-1"},"urls":[]}
```

Create returns immediately after durable save, with `Location` pointing to GET; provisioning/start continues in the background. Other lifecycle operations also return `202` and expose progress/errors through GET. Status distinguishes provisioning, running, stopped, degraded, deleting and error. Nodes include resolved IPs, actual ports, readiness/errors and application URLs once known. Preserve desired state separately from observed health.

List returns fleets plus actual SILO bucket count/names, managed bucket count and storage status. Unavailable storage reports an error/unknown counts, never a misleading zero. No object counts or size scans. Serve last observed health with a check timestamp; GET does not run installs or provisioning. Keep `/celld/health` as an aggregate of storage and fleets expected to run.

CLI deployment only: detail includes a command template and settings for `celld deploy . --bucket ... --endpoint ... --region ...`. The credentials endpoint supplies that fleet's AWS access/secret keys with `Cache-Control: no-store`; ordinary responses/logs exclude secrets. CLI access requires the management network. No application upload/build/deploy API, proxy, TLS or load balancer in this change.

## Lifecycle rules
- **EDIT:** name and server list; add/verify nodes before draining removed ones, preserving bucket/data. Editing a stopped fleet keeps it stopped. Optional HTTP/internal ports on create or while stopped; choose one stable pair per fleet, reserve it on every selected server, and report external bind conflicts. No live port migration or arbitrary runtime settings.
- **STOP:** disable automatic resume, drain processes, keep bucket and local data. **START:** reuse them. **RESTART:** roll selected nodes through drain/start and persist progress; one-node fleets have an interruption.
- **DELETE:** persist deleting state, disable/drain every assigned agent, then remove local data, bucket contents/bucket and scoped user/policy. Offline agents keep deletion pending; never report success early. Retain cleanup state until finished and retry partial failures.
- On agent startup/reconnect, synchronize authoritative fleet membership/state before resuming children. Removed/stopped/deleting fleets must not restart from stale local files. Serialize mutations per fleet and reserve ports atomically; reject conflicting in-flight edits (`409`).

## Three implementation increments
1. **Isolated node processes.** Extend `api/proto/celld.proto` with fleet IDs, ports, status and stop/remove/sync operations; regenerate through the root Makefile. Add a small fleet registry around `slave/celld/`'s existing child manager; wire startup/shutdown in `slave/main.go`. Separate directories/locks/logs under each fleet ID; share the installed version and serialize installation. Drain all children concurrently within the existing shutdown grace. **Check:** two fleets share a server; stopping/restarting one preserves the other; agent restart respects master state.
2. **Fleet storage and coordination.** Extend `master/services/celld.go` and `celld_storage.go`: persist fleets, desired state, reservations and cleanup progress alongside shared SILO admin state. Parameterize every fixed bucket/port; provision one bucket/user/policy per fleet, run existing compatibility/bootstrap checks, and reconcile explicit members only. Master hosts storage/tools without mandatory fleet membership. Bound concurrent fleet work so one offline fleet cannot block another; keep locks out of network calls. **Check:** independent deployments/data, failed provisioning retries, membership changes, offline removal/delete, and restart during each lifecycle operation.
3. **HTTP and handoff.** Add thin handlers/routes in `master/api/celld.go` and `api.go`; expose counts, details and scoped credentials. Update `docs/celld.md` with examples. Extend `scripts/test_celld_fleet.sh` to exercise two overlapping fleets, independent CLI deployments, port conflicts and full deletion. **Check:** authenticated CRUD/actions, secret exclusion, correct status/counts, existing module suites and focused race tests.

Each increment builds on the previous one and is testable before proceeding. Keep changes inside the existing celld integration; frontend work is outside this plan.

## Reset and rollout
Start fresh as requested: disable old automatic enrollment/resume, drain all legacy children, then delete their local state/data and `hyperhive-celld` bucket/user/policy. Preserve the shared SILO container, unrelated buckets and admin credentials. Persist reset progress; wait for offline legacy agents before purging. New startup begins with zero fleets.

Deploy agents with fleet synchronization before enabling the new coordinator/API and performing the reset. Verify explicit creation on two machines, then overlapping fleets and independent lifecycle controls. Run `go test ./...` in `api`, `master`, `slave`; race checks for `master/services`, `master/api`, `slave/celld`; then the disposable integration script.

Before reset, code rollback can preserve legacy state. After purge, deleted application data is recoverable only from a separate existing backup; old binaries must not resume against new fleet state.

Sources: current repository files above; [celld v0.6.2](https://github.com/denoland/celld/blob/v0.6.2/docs/README.md), [storage and shutdown guarantees](https://github.com/denoland/celld/blob/v0.6.2/docs/guarantees.md). Existing SILO compatibility probes remain required. No live-server inspection or changes were performed to prepare this plan.
