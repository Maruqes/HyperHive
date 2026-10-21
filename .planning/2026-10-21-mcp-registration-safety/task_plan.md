# MCP registration and safety implementation

Status: corrected repository implementation verified; external client qualification pending.

- [x] Inspect plan/current source and consult canonical knowledge.
- [x] Remove delete_celld_fleet per user decision.
- [x] Add shared registrar, explicit area composition and risk classification.
- [x] Centralize High/VeryHigh forms and durable Medium preflight records.
- [x] Preserve VM recovery; add/adapt transport, validation and policy regressions.
- [x] Pass MCP normal/race/vet and affected master normal/race/vet checks.
- [x] Refresh safety inventory, report, source manifest and canonical knowledge.
- [ ] Qualify a real human form in both actual desktop modes before release.

No deployment or production mutation performed. The remaining check requires a
real connected client; SDK acceptance cannot prove a human saw a prompt.

## Registration layout correction

The user clarified that each area's Register function must directly show every
tool registration, with its explicit name and safety level in one compact place.
Prepared definitions may hold handlers and schemas, but may not select levels or
register additional tools. All 64 VM registrations must also appear together.

- [x] Extract prepared typed tool definitions without risk selection.
- [x] Add nine register_<area>.go files with every direct RegisterTool call.
- [x] Verify unchanged discovery/contracts and centralized declarations.
- [x] Update documentation and canonical source evidence.

## Final file placement

- [x] Move every Register function to the end of its existing area file.
- [x] Remove the nine separate registration files.
- [x] Update source/discovery checks and documentation links for this location.
