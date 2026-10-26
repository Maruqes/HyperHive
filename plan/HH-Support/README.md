# HH Support planning

Date: 2026-10-26

Plan the HH observability and automation platform through three sequential discovery sessions. The user authorized API/frontend implementation on 2026-10-26. See the [technical contract](../../docs/designs/hh-support.md) and [setup guide](../../docs/hh-support.md). Automations and VM agents remain future work.

## Sequence

1. API and frontend — active discovery. Shared HTTP telemetry ingestion for applications and VM agents, health monitoring, WebSocket delivery, App registration, tabs and tags. Capture: [API/frontend discovery](01-api-frontend-grill.md).
2. HH Automations — future discovery. Conditions based on health, time and metrics trigger controlled script execution. Choose Bash, Python, Go or JavaScript during that session.
3. VM agents — future discovery. Agent installation, enrollment, collection and delivery to the shared API.

## Evidence and boundaries

- Accepted deployment decision: separate HH Support service and database, HH-integrated UI, and proxy routing of `/api/support` to the new service while existing `/api` traffic continues to the HH backend.

- Requirements originate from the user conversation on 2026-10-26.
- Existing SilverBullet context consulted: `knowledge/projects/HyperHive/features/observability/index` and the project feature catalog. These describe source-reviewed logging/network telemetry, not verified live deployment.
- Earlier assistant examples are proposals, not accepted API schemas or architecture decisions.
- Complete API/frontend discovery before producing its implementation plan. Keep automation and agent details deferred except where their API contracts affect this phase.
