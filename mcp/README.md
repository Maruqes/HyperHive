# HyperHive MCP server

For client commands, see the short [Codex and Claude Code connection guide](CONNECTING.md).

Go server using the official MCP Go SDK v1.8.0. HyperHive master automatically
serves MCP over Streamable HTTP; a standalone stdio command is also available.
The only registered tool is **`list_proxies`**, a read-only call to HyperHive's
authenticated **`GET /proxy/list`** endpoint. Authentication and permissions
remain in the existing HyperHive API.

## Automatic HTTP endpoint

Build and start HyperHive master normally. MCP is compiled into the master and
registered outside the API login middleware on the existing listener:

```text
http://<hyperhive-host>:9595/mcp
```

Port **9595** is the default. Set `API_PORT` in `master/.env` or the master
environment to change the shared API/MCP listener port (valid range 1–65535).
For example:

```dotenv
API_PORT=9595
MCP_TOKEN=<your-fixed-random-key>
```

With `API_PORT=10595`, connect to `http://<hyperhive-host>:10595/mcp`. The loopback
backend URL and newly generated frontend proxy targets use the same port.
When changing an existing installation's port, update its client URLs, existing
reverse-proxy targets and `MAIN_LINK` if it points directly to that listener.
An empty/unset port uses 9595; an invalid port stops startup with a clear error.

The listener binds on all interfaces; no separate MCP process, binary or port is
required. HTTP MCP requires `Authorization: Bearer <MCP_TOKEN>` on every request.
Missing/incorrect credentials return 401; an unset/invalid server key disables
MCP with 503. Query parameters and cookies do not authenticate MCP.

Generate your fixed key with `openssl rand -hex 32`, put it in the master's `.env`
and configure the same key in your MCP client. The key remains valid until you
change it and restart the master. No HyperHive login session, email or password
needs to be stored for MCP.

The normal REST API accepts either its existing login session or the fixed
`MCP_TOKEN` in a Bearer header. The fixed key therefore grants administrator-level
API access, while MCP exposes only `list_proxies`. Normal cookie/query session
authentication remains unchanged; the fixed MCP key is header-only.

The tool calls `http://127.0.0.1:<API_PORT>/proxy/list` with that fixed key. After
validating it, the master creates an NPM-compatible RS256 JWT lasting two minutes
for the first active administrator in the locally managed NPM installation.
It reads `npm-data/keys.json` and `npm-data/database.sqlite` in read-only mode;
no users, passwords or NPM state are changed. The fixed key is never forwarded to
NPM. This bridge follows the pinned NPM 2.13.5 token/SQLite contract; an unavailable
key/database or no active administrator fails with HTTP 503. NPM still applies
its authorization rules when handling the short-lived credential.

This is stateless Streamable HTTP with JSON responses and SDK request-size and
localhost protections. Authenticated GET `/mcp` returns 405 because calls use
POST; without the key it returns 401. There is no legacy SSE endpoint or OAuth
login flow. The existing optional API prefix also accepts `/api/mcp`.

## Standalone stdio alternative

Requires Go 1.25 or newer. From this directory:

```sh
go build -o bin/hyperhive-mcp ./cmd/hyperhive-mcp
```

Set these environment variables in your MCP client's server configuration:

| Variable | Meaning |
| --- | --- |
| `HYPERHIVE_API_URL` | HyperHive API origin, optionally including `/api` or a reverse-proxy prefix. A trailing slash is accepted. |
| `MCP_TOKEN` | Fixed MCP key, matching the master configuration. |

Point the MCP client at the **absolute path** to `bin/hyperhive-mcp`, optionally
with the argument `serve`. Both no arguments and `serve` start the same server.
`--help` prints usage to stderr without requiring credentials.

Generic MCP client configuration (replace the path and environment values):

```json
{
  "mcpServers": {
    "hyperhive": {
      "command": "/absolute/path/to/HyperHive/mcp/bin/hyperhive-mcp",
      "args": ["serve"],
      "env": {
        "HYPERHIVE_API_URL": "https://hyperhive.example.test/api",
        "MCP_TOKEN": "<your-fixed-random-key>"
      }
    }
  }
}
```

The URL must target **HyperHive**, not NGINX Proxy Manager. For example, an API
base ending in `/api` results in `/api/proxy/list`; a base without it results in
`/proxy/list`. Preserve any reverse-proxy prefix in the base URL. URL credentials,
query strings and fragments are rejected. The server reads environment variables
directly; it does not load `.env` files. Store real tokens in your client's local
secret configuration rather than committed files.

## Tool contract

Call `list_proxies` with `{}`. There are no filters, credentials, pagination or
other tool arguments. Unknown arguments are rejected before an HTTP request.

The result contains structured content and a JSON text fallback:

```json
{
  "proxies": [
    {
      "id": 42,
      "domain_names": ["example.test"],
      "forward_scheme": "http",
      "forward_host": "app.internal",
      "forward_port": 8080,
      "enabled": true
    }
  ]
}
```

This example is abbreviated. Objects preserve every field returned by the API,
including TLS flags, advanced configuration, access list IDs (strings or
numbers), metadata and nested locations. No IDs or properties are invented.
An API response of `null` or `[]` becomes `{"proxies":[]}`. An empty body,
malformed JSON or a non-list response is an error.

Requests have a 30-second timeout, honor MCP cancellation and are not retried.
Responses are limited to 16 MiB. Redirects are not followed: configure the final
API URL. API failures become MCP tool errors retaining the HTTP status; raw error
bodies and transport URLs are omitted to avoid exposing tokens or backend
configuration. Stdout is reserved for MCP protocol traffic; startup diagnostics
go to stderr in standalone mode. The embedded HTTP handler shares the master's
listener and logging. The master creates short-lived downstream credentials automatically.

## Verify and extend

```sh
go test ./...
go test -race ./...
go vet ./...
```

Tests use disposable HTTP fixtures and an SDK client launching the actual server
binary over stdio. They cover initialization, exactly one advertised tool,
schemas, invocation, unknown arguments, API authentication, URL prefixes,
response fields, empty lists, errors, redirect refusal, cancellation and timeout.
HTTP tests verify Bearer authentication, refusal of missing/wrong keys, sole-tool
discovery and calls, and the static-key-to-NPM credential bridge. A master route test verifies
MCP and REST authentication, including normal sessions, the fixed key, and a
successful tool invocation through the loopback API on a nondefault port. No production
API access is required or claimed.

Source contracts: `../master/api/npmapi/proxy.go` (`SetupProxyAPI`,
`listProxies`), `../master/api/login.go` (authentication), and
`../master/npm/proxy.go` (`Proxy`, `Location`, `GetAllProxys`).

Register future tools explicitly in `internal/tools`, sharing `internal/client`.
The broader plan is in `../notes/hyperhive-mcp-implementation-plan.md`. API
inventory generation, automated endpoint coverage, router extraction, mutations,
and CI are deferred. There is no coverage CLI or generated report in this
increment, and no full proxy-management coverage is claimed.
