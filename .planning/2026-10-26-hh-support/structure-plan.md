# HH Support backend structure

User requested a scalable project layout on 2026-10-26. Preserve API/database behavior while separating actual responsibilities.

- [x] Extract domain types/rules, security helpers and SQLite repository.
- [x] Extract application operations/maintenance and HTTP/realtime adapters.
- [x] Wire cmd/hh-support with validated configuration and lifecycle.
- [x] Adapt existing regressions, subprocess build path and documentation.
- [x] Run service race/vet/build and master integration; update canonical knowledge.

Scope: support module, master integration test executable target, support docs/design and canonical knowledge. Frontend wire contracts and persisted SQLite schema stay unchanged. No runtime scaling claims or deployment.

Initial compile exposed mechanically qualified field names and one missing test type import; corrected. Existing integration regressions pass after extraction. Added real versioned SQL migrations with transaction rollback/newer-version protection, a non-HTTP concurrent service test, and package-local config/realtime tests. Domain errors no longer contain HTTP status codes.

Final verification: 13 Support tests pass with the race detector; vet and command build pass. Eight master Support tests pass, including rebuilding/running the new command through the real proxy. Existing unversioned SQLite data preservation is tested. No frontend files changed during this restructuring. Updated and verified canonical SilverBullet `knowledge/projects/HyperHive/features/observability/hh-support` with package paths and migration behavior. No deployment or runtime load validation.
