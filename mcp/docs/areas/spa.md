# SPA management MCP tools

Source reviewed on 2026-10-19. No production access or deployment.

| Tool | HTTP endpoint | Contract |
| --- | --- | --- |
| `list_spa_ports` | GET `/spa/` | No arguments. Returns `result.spa_ports`, with port, description and creation timestamp. |
| `list_spa_allows` | GET `/spa/allow/{port}` | Required integer `port`, 1–65535. Returns `result.port` and `result.allows`, containing IP and remaining seconds. |

Both use the authenticated shared client, preserve complete safe response objects and exact JSON integers, and retain empty arrays, empty descriptions and zero remaining seconds. Invalid, unknown and null-valued port arguments fail before backend access. Null roots/lists and malformed required response fields fail safely. Passwords and password hashes are absent from the reviewed handler outputs; unexpected password-named response fields are rejected, including nested fields. API error bodies are suppressed. A missing configured port produces HTTP 401 under the backend's shared unauthorized mapping, while an absent ipset or command/database failure produces HTTP 500. Permissions require management authentication; these tools never call guest authorization endpoints.

## Selected operation excluded by deletion policy

POST `/spa/` was requested, inspected and excluded with zero implementation/coverage credit. `master/services/spa.go:23` (`SPAService.Create`) hashes the supplied password, invokes `EnableSPA`, and upserts the port's hash. Existing ports therefore get a new password. `master/spa/spa.go:12` (`EnableSPA`) deletes matching firewall rules before reinserting them. On database failure `master/services/spa.go:38` calls `DisableSPA`, whose implementation at `master/spa/spa.go:34` deletes rules and destroys the ipset, including existing permissions. This required behavior violates `mcp/AGENTS.md`; no create tool, client method or disabled stub exists.

Creation also has partial failure effects: `EnableSPA` can fail after some rules changed, and description storage can fail after the protected port and password were successfully persisted (`master/api/GUESTS.go:486`). MCP does not expose a partial replacement contract or claim creation eligibility.

DELETE `/spa/{port}` remains excluded by policy. Guest allow aliases and SPA page routes are outside the selected scope. Keep all of these endpoints in their raw API denominator; the two registered management GET endpoints earned coverage after coordinator acceptance on 2026-10-19.

## Validation

`TestSPAContracts` uses SDK clients over authenticated HTTP and the actual stdio executable built by the existing shared `TestMain`. Disposable HTTP backends verify method, full prefixed path, token, no query/body, discovery metadata, input rejection, normal/empty results, exact large integers in text output, false/zero preservation, malformed-response and sensitive-field rejection, safe errors and absence of retries. These are backend contract fixtures, not live firewall/database checks. `TestSPAMCPActualHandlers` additionally passed against the real authenticated router/MCP bridge with disposable SQLite: empty/populated port lists, descriptions/timestamps, password-hash omission, missing-port 401, invalid-port 400 and safe description-table failure 500. Successful ipset permission reads remain mock-HTTP tested only. Coordinator serialized full-module/race/vet acceptance passed on 2026-10-19; see the run checkpoint.
