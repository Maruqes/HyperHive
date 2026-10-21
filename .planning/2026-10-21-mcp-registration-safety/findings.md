# Baseline findings

- Inspected source baseline: dd63aad991860347d67b1d114d97d8cff810a258 and current working tree.
- Shared exact decoding exists; non-VM registration is nested under proxy registration.
- VM Medium currently requires approval; user explicitly wants recorded review instead.
- High/VeryHigh must obtain human approval in both requested modes. Standard MCP responses do not prove a human click; client qualification is a release gate.
- Existing Celld deletion registration claims prior authorization but current AGENTS.md lists no matching exception. Reconcile before migrating it.
- Preserve unrelated mcp/README.md and mcp/TESTING.md work.
- Knowledge retrieval exceeded the initial approximate 3,000-token budget because the MCP index was unexpectedly long. Follow-up was limited to headings and one client-form section; no broader history was loaded.

## Implementation findings — 2026-10-21

All 117 remaining tools now use the shared registrar and explicit area
composition. Medium records preflight without approval; High/VeryHigh use the
shared argument-bound form. VM recovery remains independent. The user explicitly
removed Celld deletion. Current coverage is 117/322 (36.34%) with 48 exclusions.
MCP and affected master normal/race/vet checks passed, with focused final policy
checks after cleanup. Actual human form presentation in both desktop modes is
unverified; no deployment occurred.
