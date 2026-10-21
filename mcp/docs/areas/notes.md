# Notes MCP tools

Registration and safety: [shared policy and classifications](../tool-safety.md).

Source-reviewed against `master/api/notes.go` and `master/db/notes.go` on 2026-10-19. Both embedded authenticated HTTP and standalone stdio use the same registration and shared API client.

| Tool | API endpoint | Required arguments | Behavior |
| --- | --- | --- | --- |
| `list_notes` | `GET /notes/` | none | All notes, ordered by descending ID; empty array when none exist |
| `get_note` | `GET /notes/{id}` | `id` | One note; HTTP 404 for missing notes |
| `create_note` | `POST /notes/` | `titulo`, `nota` | Creates and returns the note, including its API-generated ID |
| `replace_note` | `PUT /notes/{id}` | `id`, `titulo`, `nota` | Replaces both existing content fields |
| `patch_note` | `PATCH /notes/{id}` | `id`, `titulo`, `nota` | The API's full-replacement alias; both fields remain required |

IDs must be positive integers. Title (`titulo`) and body (`nota`) must be nonblank strings. Original whitespace and multiline content are sent unchanged. Omitted required fields, explicit null roots or fields, non-object argument roots, unknown arguments and wrong types are rejected before calling the API. `list_notes` accepts omitted arguments or an empty object. There are no optional flags, boolean defaults or valid zero IDs. PUT and PATCH overwrite both fields; PATCH does not merge a partial object. The client exposes distinct named methods with fixed HTTP methods.

Results use a `result` envelope containing the complete API object or array. Each note contains `id`, `titulo` and `nota`; future response fields and exact JSON numbers are retained. Read responses permit legacy blank titles. Malformed, incomplete or null backend results become tool errors. API failures report safe HTTP status information without echoing backend bodies. All operations honor shared authentication, cancellation, timeout, response-size and redirect protections. Mutations are sent once without automatic retries.

`DELETE /notes/{id}` remains excluded by policy. Permitted endpoint coverage is 5/5 (100%); raw Notes coverage is 5/6 (83.33%). Coordinator full test/race/vet and discovery acceptance passed on 2026-10-19. No deletion method or generic dispatch tool is provided.

Validation includes SDK discovery and calls over authenticated HTTP and the actual stdio executable using disposable HTTP backends. `master/api/notes_mcp_test.go` separately exercises actual authenticated handlers and the MCP bridge with an isolated in-memory SQLite database. No production backend or deployment was used. Final full-module test/race/vet results are recorded by the coordinator.
