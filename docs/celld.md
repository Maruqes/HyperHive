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

## Command on server PATH

Each agent installs the official executable at `/usr/local/bin/celld` and creates
`/usr/bin/celld` as a symlink to it. This makes the command available in normal
shells and the standard reduced `sudo` PATH, without changing `.env` or shell
profiles. Setup also creates a missing link when the correct release is already
installed. An existing different file or symlink at `/usr/bin/celld` is preserved
and reported as a setup error for the operator to resolve.

The agent also sets the managed `lib/celld`, `releases`, active release directory,
and executable to mode 0755. The upstream installer moves a `mktemp` directory
(0700) into the release path; without this repair, root can execute celld while
ordinary users receive `Permission denied` despite the PATH symlink. The repair
runs for already installed releases too, without changing private data/config.

To repair an existing default v0.6.2 installation immediately:

```sh
sudo chmod 755 /usr/local/lib/celld /usr/local/lib/celld/releases \
  /usr/local/lib/celld/releases/v0.6.2 \
  /usr/local/lib/celld/releases/v0.6.2/celld
hash -r
celld --version
```

For a different configured release, substitute its version. If it still fails,
inspect directory permissions with `sudo namei -l /usr/bin/celld`.

After deploying the updated slave agent on **every node, including the master**,
restart its existing PM2 registration with the configured drain grace:

```sh
sudo pm2 restart hyperhive-slave --kill-timeout 65000
sudo pm2 save
# Once automatic fleet setup has completed:
command -v celld
celld --version
sudo celld --version
```

This code change takes effect when those agents are updated; it does not itself
deploy to physical servers. Continue using the agent to run the daemon. Operator
commands such as `celld deploy` still need the existing bucket configuration and
credentials; placing the executable on PATH does not load them into your shell.

## Ports

All ports below use TCP and are fixed in the current source, not `.env` options.

| Port | Service | Where it listens / purpose |
| --- | --- | --- |
| 8080 | celld HTTP | Each node's management IP; application requests and `/.well-known/celld/health` |
| 8081 | celld peers/operators | Each node's management IP; keep inside the trusted management network |
| 9000 | SILO S3 | Master management IP; shared storage used by every node |
| 9001 | SILO console | Inside the container only; **not published** on the host |
| 9595 | HyperHive HTTP API | Master `:9595`; authenticated `GET /celld/health` (also through `/api/celld/health`) |
| 8079 | HyperHive frontend | Existing frontend Docker host port; separate from celld application HTTP |
| 50051 | HyperHive master gRPC | Master `:50051`; existing agent connections |
| 50052 | HyperHive slave gRPC | Each agent `:50052`; includes celld Configure/GetStatus |

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

## Process logs

Each agent writes celld stdout and stderr to its own private file:

```sh
sudo tail -F /var/lib/hyperhive/celld/logs/celld.log
```

The active file is limited to **10 MiB**, with **three rotated copies**
(`celld.log.1` through `.3`, newest to oldest): at most **40 MiB of retained
log files per node**. Rotation happens while writing, including large writes,
and an agent restart appends to the existing file. The directory uses mode
0700 and files use 0600. These limits are fixed defaults; no `.env` change is
required.

Before writing, the logger checks space on the log filesystem and preserves
at least **256 MiB available**. Under disk pressure it removes the oldest
rotated copies first. If space is still insufficient, or log storage fails,
it drains and discards new output so logging cannot close the child's output
pipe or restart celld. Capture resumes automatically on a successful later
write. A warning is emitted to the agent log at most once per minute while
capture fails, with one recovery summary for an announced outage. Brief
space/error fluctuations do not reset the warning throttle.

Normal celld process output is no longer copied to PM2/agent output. Process
exit/restart messages and installation messages remain there. Existing PM2
log history is not deleted. Application state under `celld/data` is separate
from this text-log rotation and is never removed by the logger.

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
