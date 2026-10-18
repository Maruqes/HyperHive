# HyperHive MCP

Use HyperHive from an AI client through named MCP tools. MCP starts with the
master over **Streamable HTTP**; standalone stdio is also available.

## API coverage

Last checked: **2026-10-18**. Coverage means API endpoints supported by MCP tools.

| Scope | Supported / total | Coverage |
| --- | ---: | ---: |
| HyperHive API | 1 / 314 | **0.32%** |
| NGINX (all features) | 1 / 37 | **2.70%** |
| Proxy management | 1 / 8 | **12.50%** |

NGINX and proxy rows are subsets of the API total. Only `list_proxies` is
available: it calls `GET /proxy/list` with `{}` and returns `{"proxies":[...]}`.
Create, update, delete, enable and disable operations are not available yet.

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
