---
project: HyperHive
type: behavior
status: implemented-locally
reviewed_on: 2026-10-19
source_sha256: 11f838c3d7efcd67f9591abf54c4040efaa426ec69f02ea84f85d6e38f8dcbad
sources:
  - mcp/README.md
  - mcp/UPDATE_STATE.md
  - mcp/internal/tools/proxies.go
  - mcp/internal/tools/celld.go
  - mcp/internal/tools/nginx.go
  - mcp/internal/client/nginx.go
  - master/api/npmapi/nginx_contract_test.go
  - mcp/internal/tools/arguments.go
  - mcp/internal/vmsafety
  - mcp/docs/areas/vms.md
  - mcp/docs/areas/dns.md
  - mcp/review_vm_safety_test.go
  - mcp/review_vm_contracts_test.go
  - mcp/review_dns_test.go
  - mcp/review_proxies_test.go
---

# HyperHive MCP server

Current source implementation has 105 named tools: 28 NGINX, nine Celld, three DNS, five Notes, two SPA, two ISO and 56 VM tools. The standalone Go module uses the pinned official MCP Go SDK v1.8.0 and Go 1.25+. This supersedes the earlier one-tool/stdio-only/public-HTTP descriptions. The work is implemented and verified locally; no production access or deployment is claimed.

## Transport and authentication

HyperHive master automatically serves Streamable HTTP MCP on the shared API listener at `http://<host>:<API_PORT>/mcp`; API_PORT defaults to 9595. Clients send the configured fixed MCP_TOKEN as an Authorization Bearer header on each request. Missing/wrong keys yield 401; unset/invalid server key disables MCP with 503. There is no OAuth flow, and cookie/query keys are rejected. Modern protocol clients (2026-07-28 or later) use stateless JSON responses and argument-bound multi-round-trip input requests. Older supported clients use retained authenticated sessions for form elicitation (20-minute idle timeout). GET/DELETE without a legacy session returns 405; the fixed Bearer key protects every request and picker response. Existing /api prefix handling remains available.

The REST API retains ordinary login sessions OR the fixed Bearer key, which grants administrator-level access. After validating the key, master reads local NPM signing/SQLite state in read-only mode and generates a two-minute RS256 JWT for an active administrator. NPM receives the short-lived credential, never the fixed key, and remains responsible for its authorization. Missing signing/database/admin state fails closed. No NPM users or credentials are modified by the bridge.

Standalone stdio uses the same registrations and HYPERHIVE_API_URL plus MCP_TOKEN from its environment; stdout is reserved for MCP traffic. The command does not load .env itself. Changing API_PORT changes REST/MCP together and targets for newly configured frontend proxies; existing proxy/client configuration may need operator adjustment. Connection instructions: mcp/CONNECTING.md. No key values, private keys or actual credentials are stored here.

## Available tools

Existing proxy management: list_proxies, create_proxy, update_proxy, enable_proxy, disable_proxy, setup_frontend_proxy and setup_secure_frontend_proxy.

NGINX continuation added 21 tools:

- `list_certificates` — `GET /certs/list`.
- `list_dns_providers` — `GET /certs/dns-providers`.
- `download_certificate` — `POST /certs/download`.
- `renew_certificate` — `POST /certs/renew`.
- `list_access_lists` — `GET /access-lists/list`.
- `create_access_list` — `POST /access-lists/create`.
- `list_streams` — `GET /stream/list`.
- `create_stream` — `POST /stream/create`.
- `update_stream` — `PUT /stream/edit`.
- `enable_stream` — `POST /stream/enable`.
- `disable_stream` — `POST /stream/disable`.
- `list_redirects` — `GET /redirection/list`.
- `create_redirect` — `POST /redirection/create`.
- `update_redirect` — `PUT /redirection/edit`.
- `enable_redirect` — `POST /redirection/enable`.
- `disable_redirect` — `POST /redirection/disable`.
- `list_404_hosts` — `GET /404/list`.
- `create_404_host` — `POST /404/create`.
- `update_404_host` — `PUT /404/edit`.
- `enable_404_host` — `POST /404/enable`.
- `disable_404_host` — `POST /404/disable`.

Celld: list_celld_fleets, get_celld_health, create_celld_fleet, start_celld_fleet, stop_celld_fleet, delete_celld_fleet, get_celld_credentials, get_celld_logs and get_celld_events.

## Selected DNS, Notes, SPA, ISO and VM additions — 2026-10-19

This selected batch adds 68 tools, preserving the prior 37. DNS, Notes and ISO endpoints cover all operations permitted by repository policy. No new deletion exception was granted.

- DNS: `dns_aliases_list`, `dns_alias_get`, `dns_alias_add`. Get checks the exact alias/address pair; add upserts an alias and restarts applicable DNS instances. Write values cannot contain whitespace or control characters because the backend interpolates the original values into configuration. Reads preserve the handler's trim behavior. Include/restart failures can still leave configuration changed.
- Notes: `list_notes`, `get_note`, `create_note`, `replace_note`, `patch_note`. PUT and PATCH both require replacement of the complete title/body pair. API-generated IDs and missing-note 404 behavior are preserved.
- SPA: `list_spa_ports`, `list_spa_allows`. Requested creation is excluded: firewall replacement deletes rules and failed persistence can destroy an existing allow set. No create stub or guest-authorization tool is registered.
- ISOs: `list_isos`, `start_iso_download`. Download inputs require an HTTP(S) URL without credentials/fragment and a leaf ISO filename. HTTP 202 with status started is initiation only; the backend has no completion endpoint/job ID. Availability is best-effort and is not completion evidence.
- VMs: 56 tools cover reviewed reads, configuration/actions, disk/template operations, host tuning and streaming transfers. Original permitted VM selection is 56/57; SSH-key injection remains absent because actual prior guest authorized_keys/skel content and filesystem ownership cannot be retrieved. All 23 deletion-effect exclusions remain. Exact mappings and risk ratings are in `mcp/docs/areas/vms.md`.

## VM approval and retained recovery

Every VM/host/disk/template tool publishes low, medium, high or very high risk. Medium+ requires an explicit human choice through the runtime MCP form picker on each invocation. Approval is single-use, time-limited and bound to the exact tool and arguments. Missing, unsupported, denied, canceled, expired, replayed or altered approval cannot execute the operation. No tool argument can self-approve.

After approval, one registry lock serializes fresh actual-state capture, durable private save and the single backend request. VM setting records preserve secure inactive/current domain XML and relevant previous getters, including old CPU pinning. XML replacement must retain actual name/UUID; explicit host overrides must match current identity. Disk growth through edit_vm additionally requires a positive integral previous disk size and a path matching the primary disk in secure inactive XML. Host boot changes save every grubby entry, default kernel and current command line, because the setters update all kernels.

Recovery records are retained `before.json` files in private 0700 directories with 0600 permissions and file/directory sync before dispatch. Configure an absolute operator-controlled `MCP_VM_BACKUP_DIR`; otherwise the process user's cache under `hyperhive/mcp/vm-backups` is used. Invalid snapshots, insecure destinations or persistence failures block execution. The path is returned on success and subsequent backend failure. Records can contain secure XML/passwords and must remain private; they are not logged or automatically removed.

These are configuration/metadata records, not guest-disk snapshots or automatic rollback. They cannot undo disk growth or recover lost guest RAM. Serialization covers one registry, not external API/hypervisor operations or independent server processes.

Two additive authenticated, no-store recovery helpers are prerequisites: GET /virsh/snapshot/{vm_name} and GET /virsh/hostsnapshot/{machine_name}, with corresponding RPCs. Updated master and slaves are required when deploying; old backends fail closed. Existing getter/mutator semantics were preserved.

VM export and backup download stream complete binary responses to new private artifacts on the MCP server filesystem, returning byte count and SHA-256. Clients do not automatically download those paths. Transfers have a six-hour maximum honoring earlier caller cancellation/deadlines and clean up incomplete local artifacts. Manual VM backup and clone return acceptance/background initiation only; no fabricated completion or job IDs.

## Contracts and limits

All tools call HyperHive's authenticated API through the shared HTTP client; none bypasses handlers with direct NPM/database access. API URL/key are configuration, never tool arguments. Unknown fields, explicit nulls and invalid local types/bounds are rejected before dispatch. Full writable configuration is required for create/update, preserving false, zero, empty strings and empty arrays. Enabled state uses dedicated actions; read-only server IDs are not create arguments. Updates replace configuration rather than silently merging.

Proxy listing returns {proxies:[...]}; other NGINX lists return {items:[...]}, preserving all fields returned by HyperHive. Null root lists normalize to empty arrays; malformed/wrong-shaped responses are rejected. Proxy and ordinary NGINX mutations acknowledge empty API success as {success:true}; downstream created IDs are not fabricated. Frontend secure setup writes private then public configuration without rollback, so a public-stage error can leave the private change applied.

Stream create/update writes NPM first and SQLite description text second. A description failure can leave NPM changed despite HTTP 500. Empty description explicitly writes blank text. No automatic mutation retry or deletion rollback is provided; inspect lists before a user-initiated retry.

Certificate download returns exact archive bytes as data_base64 with content_type and content_disposition, can contain private keys, and writes no local file. It is read-only despite POST and has a three-minute client timeout covering the helper's two-minute operation. Renewal returns API acceptance only and has an eleven-minute timeout covering the helper's ten-minute operation. Earlier caller deadlines/cancellation still apply, and timeout/cancellation cannot prove that backend work did not complete. DNS-provider credential-format templates are not configured secrets.

Ordinary JSON calls have a 30-second timeout and a 16 MiB response-body limit; certificate operations and VM streaming transfers use their documented operation-specific bounds. All paths retain caller cancellation and redirect refusal. API errors expose status without raw backend bodies or credential-bearing URLs. No mutation is automatically retried. NGINX list metadata is decoded without float conversion and emitted as raw structured/text JSON to retain large integers; clients remain responsible for their own numeric representation.

Celld results wrap the complete API object in result. Create/start/stop/delete preserve the queued JSON fleet state from HTTP 202 rather than claiming completion. Health preserves diagnostic HTTP 503 with healthy=false. Credentials explicitly returns sensitive fleet-scoped keys; a deleting fleet rejects access. Logs/events support all API filters, opaque pagination, partial/incomplete/error fields and bounded NDJSON export with continuation/truncation headers. Missing export completeness/time fields are not invented. Raw structured/text output preserves large observation integers, avoiding the SDK's object-default float conversion. Celld edit, detail, restart, observability snapshot, traces, metrics and live-stream tools were not added in these batches.

## Coverage and deletion policy

Source-reviewed inventory now contains 316 explicit API method/path pairs: the previous 314 plus two internal recovery helpers. UI/static/WebSocket/alias routes remain included; /mcp and the separate pprof listener remain outside capability scope. Current API coverage is 105/316 (33.23%), comprising 105 implemented, 158 missing and 53 excluded. Internal helpers earn no direct tool coverage. NGINX remains 28/37 raw (75.68%), eligible 28/28; proxies 7/8; Celld 9/17 (52.94%). DNS is 3/4 raw and 3/3 permitted; Notes 5/6 and 5/5; ISOs 2/3 and 2/2. SPA management is 2/4 raw. VMs/templates/disks/helpers are 56/82 raw (68.29%); original permitted VM selection is 56/57. No partial/stale/disabled implementation earns support credit.

All six NGINX DELETE routes remain excluded. Three additional NGINX endpoints are excluded after effect review: manual certificate creation invokes local deletion rollback; Let's Encrypt creation hard-deletes newly created certificate records on failure; access-list editing deletes stored authentication/client entries while replacing them. No partial workaround or generic API/action dispatcher was added. Nine NGINX exclusions remain visible with zero raw coverage credit.

The earlier user's explicit request authorized delete_celld_fleet only, as a narrow exception to the repository's no-deletion instruction. It does not authorize NGINX or generic deletion. Celld editing remains excluded because it removes node data.

Exclusion evidence: master/npm/certs.go and cert_upload.go, master/npm/access_list.go, plus pinned NPM v2.13.5 internal/certificate.js and internal/access-list.js. Canonical local report and source digest: mcp/UPDATE_STATE.md. There is still no committed automatic coverage CLI; task-local source/report checks are not a shipped generator.

## Validation and source history

Each new NGINX tool passed absent-tool red discovery and positive/negative contracts over authenticated SDK HTTP and the actual stdio executable before counting, followed by full MCP tests, race checks and vet. Tests verify schemas/annotations/mappings, exact authenticated wire calls, replacement semantics, output/text fallback, malformed inputs/responses, safe failures and no retries. Certificate timeout regressions verify extended operation bounds, preserved ordinary timeout and earlier caller cancellation.

Actual HyperHive NGINX handlers/NPM HTTP helpers were exercised with disposable downstream HTTP fixtures and a temporary SQLite description store, including expanded list fields, payload transformations, binary downloads, renewal acceptance and stream partial failure. Existing Celld tests exercise real authenticated handlers with disposable signing/admin/fleet state, without running the reconciler or agents. Master API/NPM suites, focused race checks and vet passed. These tests do not qualify real NGINX traffic, ACME/DNS challenges, actual renewal, agent reconciliation or production deployment.

Initial scope was one list_proxies stdio tool. Later increments added master HTTP, fixed-key authentication, seven permitted proxy tools, nine explicitly selected Celld tools, the 21-tool NGINX continuation and the selected 68-tool DNS/Notes/SPA/ISO/VM batch. Earlier operational-memory entries are historical snapshots; the current behavior above supersedes their one-tool/public-HTTP limitations.

Local references: mcp/docs/areas/nginx.md, mcp/docs/areas/nginx-proxies.md, mcp/docs/areas/celld.md, mcp/runs/2026-10-18-nginx-01.md, mcp/runs/2026-10-18-celld-01.md, mcp/celld_test.go, mcp/nginx_test.go, master/api/npmapi/nginx_contract_test.go and master/api/celld_mcp_test.go.

[[knowledge/projetos/HyperHive/features/networking/proxy-manager/index]]
[[knowledge/projetos/HyperHive/index]]

## Additional correctness review — 2026-10-19

Two fresh GPT-6.1 Sol medium agents independently reviewed VM safety/transport and VM/backend contracts, with coordinator review of shared integration and other completed features. Reproduced and fixed four defects: SDK float normalization could execute against a different large integer ID; edit_vm could grow a disk without actual previous disk size/path; DNS whitespace/control input could corrupt configuration before restart; and the older proxy-list path rounded numeric IDs/metadata.

Shared registration now retains the SDK schemas/validation but decodes original raw arguments with exact JSON numbers, supports full int64 IDs, rejects raw fractional/overflow IDs and non-object roots, and preserves numeric metadata and union-field compatibility. Approval summaries and execution use the same exact typed values. Both-transport regressions cover these fixes, schema parity and fail-closed missing disk recovery. The common guard also rejects root-null input for no-argument SPA reads.

Repeated full MCP normal/race/vet checks, affected master API/services/virsh/protocol and NGINX regression suites, master race/vet, focused slave snapshot normal/race/vet and generated RPC compilation passed after the fixes. Fixtures exercise authenticated HTTP and the actual stdio executable; actual VM helper/auth/service/RPC/write ordering is tested with disposable gRPC in an isolated subprocess. No live DNS restart, successful ipset qualification, ISO completion, hypervisor/host boot action, production NFS transfer or deployment occurred. Passing local checks are not proof of zero bugs or a live readiness guarantee.

Evidence: mcp/UPDATE_STATE.md, mcp/docs/areas/{dns,notes,spa,isos,vms}.md, dated run checkpoints, review regressions and .planning/2026-10-19-mcp-feature-waves/review-findings.md. Current source digest is recorded in the local coverage report; deployment state remains unverified.
