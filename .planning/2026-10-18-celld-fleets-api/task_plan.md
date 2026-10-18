# celld fleets API planning

## Goal
Implement notes/2026-10-18-celld-fleets-api-plan.md in the existing Go integration, with compact code and no live rollout.

## Next Step
Adapt the celld frontend to fleet management; validate in the browser; publish the approved SilverBullet update.

## Phases
- Frontend fleet service, polling and management page: in_progress.
- Browser verification and frontend checks: pending.
- Approved SilverBullet canonical pages and operational memory: pending.
- Source and SilverBullet context: complete.
- One-question-at-a-time interview: complete.
- Short implementation plan and edge-case review: complete.
- Isolated agent processes and protobuf: complete.
- Durable fleet coordination and storage: complete.
- HTTP, documentation and disposable integration coverage: complete.
- Module suites, focused races and review: complete.

## Decisions
- Servers can run multiple fleets; isolate processes, ports, data directories and buckets.

## Errors Encountered
- Initial rg used nonexistent route/server globs (exit 2); use discovered directories/files instead.

- Q2: creation starts fleet; add start/stop/restart API controls.
- Additional discovery-path errors: frontend has no `src` path and API has no Makefile; avoid assuming these paths.

- Implementation checks: an rg call referenced absent per-module .gitignore files (exit 2); use root .gitignore. Storage refactor initially retained an out-of-scope err assignment; corrected to a scoped declaration.

- Frontend checkout is nested at /home/marques/projects/HyperHiveFront/HyperHive; parent directory is not a Git repository. Preserve existing unrelated dirty files.

- Frontend tooling: initial JS tool call failed to parse before any command ran; switched to raw heredocs. Expo localhost initially bound IPv6 only; restarted with Node IPv4-first DNS ordering for browser access.
