# Findings

- Implementation boundary: source and disposable local tests only. Agent startup currently resumes before gRPC connection; remove that path and require authoritative Sync before any fleet Configure.
- Existing installer, bounded drain, process locks and rotating logs can be reused per fleet. Master has no mandatory membership in new fleets; prepare its binary through a dedicated RPC.

- Current source and SilverBullet describe one automatic fleet, all known machines, fixed bucket `hyperhive-celld`, ports 8080/8081, one agent-owned child per machine.
- Master state/credentials: `/var/lib/hyperhive/celld-fleet.json`; agent state: `/var/lib/hyperhive/celld`.
- HTTP currently exposes authenticated health only; gRPC exposes Configure/GetStatus with no fleet ID.
- Master owns SILO admin credentials; workers have bucket-scoped credentials.
- Source docs: `docs/celld.md`; prior single-fleet plan: `notes/2026-10-17-celld-fleet-plan.md`.
- SilverBullet read: infrastructure index, both known hosts, operational memory, celld index and health API page. These describe prior source review, not current live state.
- No server access or deployment performed. No unfamiliar host encountered beyond illustrative documentation examples.

- Source review confirms fixed bucket names in provisioning policy and S3 URL builder, plus fixed ports in readiness/diagnostics and child launch. All need fleet configuration, not only new HTTP handlers.
- Agent manager currently combines binary installation, one child, one file lock and one enabled marker. Multi-fleet support needs a small fleet-ID registry reusing the child manager, with serialized machine-wide installer access and concurrent child shutdown.
- Coordinator currently forces the master into the fleet and auto-adds every connected machine. New fleets must use explicit membership; master remains storage/bootstrap host without mandatory worker membership.
- Existing reconcile retries every 30 seconds. It can also provision explicit fleets without a separate job system, provided one pending fleet cannot block unrelated fleets.
- Existing S3 helper reads only 2 MiB per response; total bucket counts must not silently count a truncated list. Distinguish managed/retained fleet buckets from all SILO buckets.
- Important recovery case: offline removed/deleted members may retain enabled markers and resume old children. Reconciliation must handle stale local fleets on reconnect, and bucket teardown must wait for stop acknowledgements or an explicit policy.

- Official celld v0.6.2 README verified: nodes share a fleet bucket; deploy writes the application into that bucket and nodes adopt it. Docs: https://github.com/denoland/celld/blob/v0.6.2/docs/README.md . Guarantees: https://github.com/denoland/celld/blob/v0.6.2/docs/guarantees.md . SILO upstream: https://github.com/pgsty/silo .
- Context7 skill/tool is not exposed; use official pinned-release documentation directly.

- Final plan review: optional ports are modest incremental work because shared-server isolation already requires configurable ports. Restrict edits to stopped fleets.
- Critical reset/reconnect rule: authoritative master sync must precede agent resume, otherwise stale enabled markers can resurrect deleted fleets. Keep reset/delete progress until all old agents acknowledge shutdown.
- Keep shared administrator state and unrelated SILO buckets during singleton reset. No frontend or build/deploy API requested.
- Root Makefile exists and contains protobuf generation; previous missing `api/Makefile` was a mistaken path, not a missing project capability.

## Implemented and verified
- New fleet state schema 2 retains shared SILO administrator state and durably migrates legacy singleton state into pending reset; zero new fleets are enrolled automatically.
- Agent registry isolates config/data/locks/logs per fleet ID, requires assigned/running Sync before Configure, checks reserved/external ports and drains children concurrently. Existing installer/watcher/log rotation are reused.
- Master persists explicit membership, desired state, reservations, rolling restart cursor, retiring members and deletion acknowledgements; bounded workers keep fleet work independent. HTTP handlers stay thin and authenticated.
- Scoped bucket/user/policy provisioning is parameterized. Cached actual bucket names/counts use bucket listing only; truncated/oversized/failed responses remain unknown. Coordinator errors are separate from storage observations.
- All api/master/slave module suites, focused race tests, and the disposable SILO/celld script passed. Real tests verified two overlapping fleets, independent Durable Object counters, credential isolation and retryable deletion while retaining an unrelated bucket.
- No physical server access, deployment or production reset occurred. Deploy synchronized agents before the new master. Enabling migration will purge legacy application data only after remembered agents acknowledge cleanup.

## Proposed SilverBullet update (not saved; confirmation pending)
- `knowledge/projetos/HyperHive/features/containers/celld/index`: replace singleton/automatic enrollment description with explicit fleets, state paths, membership synchronization and lifecycle persistence.
- `knowledge/projetos/HyperHive/features/containers/celld/health-api-and-frontend`: authenticated CRUD/actions, secret-only credentials endpoint, cached health/counts and CLI handoff; frontend remains outside this change.
- `knowledge/projetos/HyperHive/operations/celld-startup-deploy-and-test`: agent-first rollout, destructive legacy reset boundary, new examples and disposable tests.
- `knowledge/infraestrutura/HyperHive/memoria-operacional`: dated source-only implementation/validation entry linking those project pages; explicitly record no live rollout.

- Frontend design retains Inter typography, existing dark navy/light surfaces, rounded cards and shared modals. New components live under components/celld; networking/types/polling stay in services/types/hooks. Credentials remain component memory only.
- Initial frontend TypeScript and existing Jest suites pass. Browser validation uses a loopback-only synthetic API fixture; production infrastructure is not accessed.
