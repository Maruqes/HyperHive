# HH Support frontend and automatic deployment

User request, 2026-10-26: rework the frontend, place HH Support under Logs, make the workflows work and improve presentation; containerize with host networking and deploy automatically on master startup.

## Scope and plan
- [x] Frontend: Logs group with System logs and HH Support; deliberate observability design, working onboarding/layout/settings/history/reconnect, readable widgets and responsive UI. Preserve /support links and /logs.
- [x] Deployment: multi-stage Dockerfile and Go healthcheck; embed Support build sources in the master; startup manager builds a source-versioned local image and reconciles a labelled Compose service with host networking, durable named volume, restart policy, health checks and protected generated shared secret.
- [x] Verification: focused frontend and deployment tests; actual Docker image/run against disposable data; API integration and browser checks using local services.
- [x] Review/docs/knowledge: resolve findings, document configuration and verified limits, update canonical SilverBullet page.

## Decisions
Interpret “host Internet” as Docker host networking; bind Support to loopback and expose ingestion/UI through the existing master API. No production hosts are being accessed. Docker is locally available (29.9.1). No existing published Support image or release workflow was found, so embed a source build context in the standalone master binary rather than depend on an invented registry image. Default automatic deployment can be disabled for an explicitly external Support service.

## Design direction
Audience: HH operators investigating App failures. A compact observability workspace with an App rail and focused dashboard, calm slate surfaces, teal live signal, amber attention and coral failure. Existing HH typography/UI primitives remain the house style; tabular measurements and a compact connectivity strip supply the visual signature. Prefer human-readable logs/events and useful empty states over raw JSON and huge blank widgets.

## Ownership
Frontend worker owns nested frontend checkout. Backend worker owns support Docker/embedded assets/healthcheck and master/supportdeploy. Coordinator owns master startup integration, shared docs/knowledge and end-to-end verification.

## Verified outcome
Support race tests: 19 pass. Master deployment/proxy race tests: 21 pass. Go vet and standalone master build pass. Frontend Jest: 17 pass; Expo web export passes. TypeScript remains blocked only by seven preexisting heading WebkitBoxOrient errors. Real Docker validation passes nonroot/read-only host-network health, all five stream types, HTTP trust boundaries, realtime frames, sender credential reuse, and restart/container-recreation SQLite persistence. Disposable containers/volumes were removed. Independent deployment review found and fixed dotenv special-character handling; actual Compose/container roundtrip verifies the fix. Compose 2.30+ is required.

Browser preview uses synthetic loopback fixtures, not production. Verified Logs submenu, seven widget renderers, settings form, layout size/save and revision persistence after reload, and 390px viewport without document overflow. Saved dashboard-preview.png. No native-device gestures or production rollout tested. A failed managed upgrade preserves data but leaves generated proxying disabled for that master boot; retry after repairing the cause. No Git-push deployment pipeline was added; automatic deployment means HH master startup.

Canonical knowledge updated and verified: knowledge/projects/HyperHive/features/observability/hh-support.
