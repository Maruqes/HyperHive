# celld fleet

The master automatically starts a persistent `hyperhive-silo` Docker container
and configures celld through the existing slave gRPC connections. The slave agent
on each machine, including the master, owns its celld child. New machines are
configured when they connect; pending setup retries every 30 seconds.

Defaults:

- celld `v0.6.2`; set `CELLD_VERSION` in the master's `.env` to select an exact release.
- SILO `docker.io/pgsty/silo:RELEASE.2026-09-03T13-18-01Z`.
- SILO data: `/var/lib/hyperhive/silo`; S3 listens on the master management IP, port 9000.
- Node data and private configuration: `/var/lib/hyperhive/celld`.
- Master credentials and persistent machine inventory: `/var/lib/hyperhive/celld-fleet.json`.
- celld public/internal listeners: the node management IP on ports 8080/8081.

The master is identified by its existing slave agent (`MASTER_IP == SLAVE_IP`).
Management IPs must be private addresses reachable across the trusted network.
Keep ports 9000 and 8081 inside that network. HyperHive's existing gRPC trust
boundary also carries the bucket credentials. Workers receive a user restricted
to the fleet bucket; SILO administrator credentials stay on the master.

Setup checks conditional creates, conditional overwrites, read-after-write
consistency, and exact ranged reads before starting the fleet. The empty bucket
receives an embedded bootstrap Worker. An existing deployment is preserved;
a nonempty bucket without a deployment is left for operator inspection.
Node/npm/esbuild are provisioned only on the master when bootstrap deployment
is needed. Missing worker download dependencies use the existing Fedora/dnf setup.

The agent restarts a failed child after 10 seconds. Shutdown and the existing
agent re-exec path send SIGTERM, wait up to 60 seconds, then force exit. A file
lock and parent-death handling prevent duplicate children across agent restarts.
Preparing the master binary does not enable automatic resume until storage
validation and bootstrap have completed.

The existing PM2 slave service must have `kill_timeout: 65000` (CLI:
`--kill-timeout 65000`) so it allows the agent's drain to finish.
`scripts/update.sh` applies and saves this setting when restarting the slave.
See [PM2's shutdown configuration](https://pm2.keymetrics.io/docs/usage/signals-clean-restart/).

The documented rolling upgrade from `v0.6.0` to `v0.6.1`, and upgrades or
rollbacks between `v0.6.1` and `v0.6.2`, are allowed. The coordinator waits for readiness and
`restoring=0` before proceeding to the next connected machine. Other transitions
on an existing installation are refused, including rollback to `v0.6.0`, which
has extra release restrictions requiring operator review.
Changing an existing fleet's storage address or credentials requires an explicit
migration; automatic setup refuses to overwrite them.

## Health

`GET /celld/health` uses the existing API authentication. It returns the same JSON
shape with HTTP 200 when storage and every known node are healthy, otherwise 503.

```json
{
  "healthy": false,
  "storage": { "healthy": true },
  "machines": [
    { "name": "master", "ip": "192.168.76.1", "connected": true, "healthy": true },
    { "name": "slave-a", "ip": "192.168.76.2", "connected": false, "healthy": false, "error": "no live reachable celld peer" }
  ]
}
```

Connectivity comes from celld's live leases and signed peer diagnostics, matched
by advertised address. Readiness uses `/.well-known/celld/health`. Disconnected
machines remain in the saved inventory across master restarts. The health
endpoint performs only reads and peer probes; it never changes fleet state.

## Validation

Run `go test ./...` inside `api`, `master`, and `slave`. Focused race checks:

```sh
(cd master && go test -race ./services ./api)
(cd slave && go test -race ./celld)
```

`scripts/test_celld_fleet.sh` downloads the pinned celld binary into a temporary
directory and starts a disposable SILO container. It requires Docker access,
curl, gzip, Node/npm, and Go. It verifies real storage compatibility, bootstrap
deployment, three nodes, a fourth joining, repeated configuration, crash restart,
and agent resume with the same data directory. Containers and temporary data are
removed on exit. It uses loopback addresses `127.0.0.2` through `127.0.0.5` on
ports 8080 and 8081; the SILO port is allocated dynamically.

References: [celld v0.6.2](https://github.com/denoland/celld/blob/v0.6.2/docs/README.md),
[storage guarantees](https://github.com/denoland/celld/blob/v0.6.2/docs/guarantees.md),
[SILO](https://github.com/pgsty/silo).
