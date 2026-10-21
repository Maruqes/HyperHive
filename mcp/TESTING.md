# MCP test map

The MCP module is one Go package per directory. The root contract tests share
package-level fixtures and expected registries, so keep them beside the code
they exercise. Split independent implementation packages (`internal/...` and
`cmd/hyperhive-mcp`) keep their tests in the corresponding package directory.

## Root package

| Files | Responsibility |
| --- | --- |
| `auth_test.go`, `server_test.go`, `transport_test.go` | Authentication boundary, authenticated HTTP and picker transport compatibility |
| `celld_test.go` | Celld tool contracts and complete registry assertion |
| `dns_test.go`, `dns_metadata_test.go` | DNS API contracts and optional alias metadata |
| `isos_test.go` | ISO listing and asynchronous download contracts |
| `nginx_test.go`, `nginx_cases_test.go`, `proxy_mutations_test.go` | NGINX batch cases and proxy mutations |
| `notes_test.go` | Notes API contracts |
| `spa_test.go` | SPA read contracts |
| `upstreams_test.go` | Upstream tools and pooled consumer input contracts |
| `selected_registry_test.go` | Independently selected tool set used by the registry assertion |
| `vms_test.go`, `vms_writes_test.go`, `vms_more_test.go` | VM reads and mutation contracts/fixtures |
| `vms_host_test.go`, `vms_increment_test.go`, `vms_isos_test.go`, `vms_ssh_increment_test.go` | Host tuning, authorized VM additions, ISO removal and SSH recovery contracts |
| `vms_safety_test.go`, `vms_transfers_test.go`, `safety_integration_test.go` | Approval, recovery records, transfer integrity and cross-transport safety |
| `review_dns_test.go`, `review_proxies_test.go`, `review_vm_contracts_test.go`, `review_vm_safety_test.go` | Focused regression/review checks for earlier contract findings |

## Other package tests

- `internal/client/*_test.go` covers HTTP client and endpoint behavior.
- `internal/tools/arguments_review_test.go` covers argument decoding regressions.
- `internal/vmsafety/safety_test.go` covers approval and private recovery storage.
- `cmd/hyperhive-mcp/main_test.go` covers the actual stdio executable and CLI validation.

## Running checks

Run the full module suite from `mcp/` with `go test ./...`. Run a focused
contract with its test name, for example `go test . -run '^TestVMWriteContracts$'`.
Names and paths in this map describe source organization; `go test ./...` is the
authoritative package discovery mechanism.
