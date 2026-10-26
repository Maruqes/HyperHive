# Findings

- Backend already persists per-App layouts in SQLite. Frontend creation only updates a local draft until Save layout, causing apparent loss on reload.
- Existing seven widget kinds: value, line, gauge, text, boolean, logs, events. Add area, bar, sparkline, progress and table.
- Shared layout revisions reject stale saves; preserve conflict behavior.
- Canonical memory: knowledge/projects/HyperHive/features/observability/hh-support, read 2026-10-26.
- Initial broad knowledge path listing returned excessive content; subsequent reads are restricted to the canonical support page. No further discovery needed.
