# Celld observability backend
## Goal
Implement the backend portions of notes/2026-10-18-celld-observability-plan.md with small typed collectors, bounded telemetry and separate retained history. No frontend or deployment.
## Phases
1. Capability probe and source map — complete
2. Typed agent snapshots, OTLP and bounded forwarding — complete
3. Dedicated bounded SQLite segments, polling and read API — complete
4. Backend tests, disposable probe and documentation — complete
## Next Step
Implementation and verification complete. User-approved SilverBullet updates saved and verified; frontend contract handoff delivered. Deployment is separate.
## Constraints
Preserve pre-existing planning edits. Read SilverBullet context before infrastructure work. Ask once at completion before persisting knowledge notes. Unsupported signals must be explicit; no invented counters.
