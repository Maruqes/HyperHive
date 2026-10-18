# HyperHive MCP

Use HyperHive from an AI client through named MCP tools. MCP starts with the
master over **Streamable HTTP**; standalone stdio is also available.

## API coverage

Last checked: **2026-10-18**. Coverage means API endpoints supported by MCP tools.

| Scope | Supported / total | Coverage |
| --- | ---: | ---: |
| HyperHive API | 7 / 314 | **2.23%** |
| NGINX (all features) | 7 / 37 | **18.92%** |
| Proxy management (raw) | 7 / 8 | **87.50%** |
| Proxy management (eligible) | 7 / 7 | **100.00%** |

NGINX and proxy rows are subsets of the API total. Available tools:
`list_proxies`, `create_proxy`, `update_proxy`, `enable_proxy`, `disable_proxy`, `setup_frontend_proxy`, `setup_secure_frontend_proxy`.
Deletion is forbidden and excluded from eligible coverage; it remains in raw totals.
See [proxy contracts](docs/areas/nginx-proxies.md) for complete inputs and setup side effects.

See [UPDATE_STATE.md](UPDATE_STATE.md) for the full report and refresh procedure.

## Connect

Generate a key with `openssl rand -hex 32`, add it to `master/.env`, then restart
the master:

```dotenv
API_PORT=9595
MCP_TOKEN=<your-generated-key>
```

Connect to `http://<host>:9595/mcp` using
`Authorization: Bearer <MCP_TOKEN>`. Use your configured port if different.
The key also grants administrator access to the REST API; keep it private.
No HyperHive login session is needed.

See [CONNECTING.md](CONNECTING.md) for Codex and Claude Code setup and troubleshooting.

## Standalone stdio

Requires Go 1.25+. From `mcp/`:

```sh
go build -o bin/hyperhive-mcp ./cmd/hyperhive-mcp
```

Configure the client to launch the binary by its absolute path, with
`HYPERHIVE_API_URL` pointing to HyperHive and `MCP_TOKEN` matching the master.
The binary reads environment variables directly, without loading `.env`.

## Development

Run `go test ./...` from `mcp/`. Follow [AGENTS.md](AGENTS.md) when adding tools,
then refresh [UPDATE_STATE.md](UPDATE_STATE.md) and this coverage summary.
For a batch of new tools, point the agent to [IMPLEMENT_TOOLS.md](IMPLEMENT_TOOLS.md).
