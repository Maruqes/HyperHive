# ISO MCP tools

## Scope and mapping

| Tool | HTTP endpoint | Behavior |
| --- | --- | --- |
| `list_isos` | GET /isos/ | List registered ISO objects and best-effort connected-slave availability |
| `start_iso_download` | POST /isos/download | Initiate asynchronous download, sync and registration; return acceptance only |
| Excluded | DELETE /isos/{id} | Deletes the ISO file and database row; forbidden by deletion policy |

Selected scope contains three endpoints. The two eligible endpoints have registered implementations. Coordinator full test/race/vet and discovery acceptance passed on 2026-10-19: raw coverage is 2/3 (66.67%); eligible coverage is 2/2 (100%).

## List contract

`list_isos` takes no arguments. Returns `result`, an array containing the exact API fields `Id`, `MachineName`, `FilePath`, `Name`, `available_on_slaves` and any future fields. JSON integers retain their precision in serialized results. Empty arrays, empty availability maps and false availability values are preserved.

Availability probes only connected slaves. False can mean an absent file or a failed probe; it does not establish a download outcome. HTTP/database errors fail safely without exposing backend details.

## Download initiation contract

`start_iso_download` requires all three fields:

- `url`: absolute HTTP(S) source URL without credentials or fragment, ending exactly with lowercase `.iso`.
- `iso_name`: leaf filename ending exactly with lowercase `.iso`, without slash, backslash or NUL.
- `nfs_share_id`: positive integer ID of an existing NFS share on a connected slave.

The URL and leaf-filename constraints are deliberate MCP safety bounds. The HTTP handler itself only checks the `.iso` suffix; its slave helper joins `iso_name` with the destination folder without a leaf-name guard. The tool prevents path escape rather than exposing that behavior. Values within the bounds are sent unchanged.

Only HTTP 202 with `status: "started"` is accepted. `result` preserves that response and future fields. This means the background task was initiated. It never means that an ISO was downloaded, synced or registered. The API returns no job ID and exposes no dedicated completion endpoint. The background worker reports later outcomes through notifications. Request cancellation cannot cancel work already accepted by the server. Calls are never automatically retried.

Duplicate registered ISO names return 409; nonexistent NFS share IDs return 404; disconnected slaves or database failures return 500. The worker can later fail during download, sync or registration. Partial files can remain and must be handled outside these tools.

## Side-effect review

The reviewed chain is `master/api/isos.go` → `NFSService.DownloadISOAsync` → master NFS RPC → slave `NFSService.DownloadIso` → `DownloadISO` → `downloadFile`. It writes a new file, syncs, inserts a database row and emits notifications/progress. Existing destinations fail before downloading. No rollback or cleanup deletion is called in this chain. Pinned `grab/v3 v3.0.1` has checksum-mismatch deletion only when configured with `SetChecksum(..., true)`; this call path never sets a checksum/hash, so that branch is unreachable. No cleanup/delete tool is exposed.

## Validation and limits

`TestISOsContracts` exercises discovery and SDK invocation over authenticated HTTP and the actual stdio executable against disposable HTTP fixtures. It checks exact method/path/prefix/auth/body, required/unknown/null/type rejection, invalid roots, safety bounds, response shape and integer preservation, best-effort booleans, empty lists, safe API failures, one request per invocation and strict initiation acceptance.

`TestISODownloadSafetyBounds` and `TestISODownloadExactIntegerAndAcceptance` exercise client safety bounds, large integer request encoding, response precision and status/no-retry handling.

`TestISOsMCPActualHandlers` exercises real master routing/authentication/handlers against isolated SQLite: empty/populated list, suffix/type rejection, missing share, disconnected slave, conflict and database errors. Successful asynchronous download is tested only as initiation acceptance with an HTTP fixture. No real slave RPC, file download, NFS synchronization, completed registration, live backend or deployment was tested. Coordinator full module/race/vet acceptance passed on 2026-10-19; see the run checkpoint.
