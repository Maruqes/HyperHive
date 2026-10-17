# Simple celld fleet plan

**HyperHive installs and runs celld on the master and every slave. The master also runs SILO for shared S3 storage. One endpoint shows health and connected machines.**

```text
Master:  HyperHive → SILO + celld
Slave A: HyperHive → celld
Slave B: HyperHive → celld

All celld nodes use the same bucket on the master.
```

## 1. Install automatically

Use the existing HyperHive startup and machine connections, like the Kubernetes integration. No additional manual setup or per-machine commands.

On each machine, check the installed version and run the official installer only when missing or different from the configured version:

```bash
set -euo pipefail
curl -fsSL https://celld.dev/install.sh \
  | env CELLD_VERSION=v0.6.2 CELLD_INSTALL_ROOT=/usr/local sh
```

On the master, reuse existing Docker support to start one SILO container with persistent data at `/var/lib/hyperhive/silo`. Create the bucket and credentials once and share the fleet configuration through existing gRPC connections. Keep admin credentials on the master. Validate SILO's storage compatibility before starting the fleet.

Only the master needs Node/npm/esbuild for deployment. Workers need the celld binary and its download dependencies.

## 2. HyperHive runs the processes

**No celld systemd unit, separate service, or celld container.** The HyperHive slave agent already running on every machine, including the master, starts and watches its local celld child process.

Illustrative Go startup:

```go
cmd := exec.Command("/usr/local/bin/celld",
    "--bucket", "s3://hyperhive-celld",
    "--endpoint", "http://"+masterIP+":9000",
    "--region", "us-east-1",
    "--listen", nodeIP+":8080",
    "--internal-listen", nodeIP+":8081",
    "--advertise", nodeIP+":8081",
)
cmd.Env = append(os.Environ(),
    "AWS_ACCESS_KEY_ID="+accessKey,
    "AWS_SECRET_ACCESS_KEY="+secretKey,
    "CELLD_WATCH=/var/lib/hyperhive/celld",
)
cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
err := cmd.Start()
```

The small process watcher calls `Wait`, reports failures, and restarts after 10 seconds while HyperHive is running. Keep one owned process per machine and prevent duplicates across agent restarts. On intentional shutdown, send SIGTERM and allow up to 60 seconds before forcing exit. The process lifetime belongs to the agent, not a short gRPC request.

Use persistent local data, save credentials privately, and keep internal listeners on the trusted management network. Repeated startup reuses existing data/configuration. Update to a configured version only, one node at a time when that version transition supports it.

## 3. Connect automatically

Same bucket and credentials, different advertised private IP per machine. celld discovers peers through the bucket; no join commands.

For a fresh empty bucket, publish a tiny embedded bootstrap application once before checking readiness. Preserve any deployment already present. Adding a machine repeats only its local install/configure/start steps.

## 4. One health endpoint

```http
GET /celld/health
```

Example response:

```json
{
  "healthy": false,
  "storage": { "healthy": true },
  "machines": [
    { "name": "master", "ip": "192.168.76.1", "connected": true, "healthy": true },
    { "name": "slave-a", "ip": "192.168.76.55", "connected": true, "healthy": true },
    { "name": "slave-b", "ip": "192.168.76.56", "connected": false, "healthy": false }
  ]
}
```

Merge HyperHive's known machines with celld's live fleet information, matching advertised addresses. `connected` means a live reachable celld peer, not merely an online HyperHive agent. `healthy` checks the runtime readiness path `/.well-known/celld/health`. Keep missing machines in the response with false values; include an error when a check fails. Overall health is false while any expected machine is disconnected or unready.

Return HTTP 200 when storage and all expected nodes are healthy, otherwise 503, with the same JSON shape. This endpoint only reads status; it never installs, deploys, or restarts anything.

## 5. Keep the implementation small

- One local celld helper in `slave/celld` for installation, process ownership/restart, and status, with thin handlers on the existing gRPC server.
- One coordinator in `master/services/celld.go` for shared configuration and startup when machines connect; retry pending setup periodically.
- One API handler for `GET /celld/health`. Reuse the existing machine inventory, Docker helpers, and Kubernetes integration pattern.

Done when a clean master plus two slaves starts automatically, another slave joins automatically, a crashed celld restarts, repeated startup preserves data without duplicates, and the endpoint correctly reports healthy/disconnected machines.

References: [celld](https://github.com/denoland/celld/blob/v0.6.2/docs/README.md), [storage and restart requirements](https://github.com/denoland/celld/blob/v0.6.2/docs/guarantees.md). This is a plan; runtime implementation has not been applied.
