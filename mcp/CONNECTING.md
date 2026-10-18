# Connect to HyperHive MCP

HyperHive starts MCP with the master at `http://<host>:9595/mcp` using
**Streamable HTTP**. There is exactly one tool: **`list_proxies`**.
Connections require your fixed **`MCP_TOKEN`** in an Authorization Bearer header.

The examples use `vm.tools` as an example HyperHive hostname. Replace it with your
master's reachable hostname/IP. Use `127.0.0.1` only for a client on the master itself.

## Prepare the master

Generate your own fixed key:

```sh
openssl rand -hex 32
```

Store the generated value in the master's `.env`, then restart the updated master:

```dotenv
API_PORT=9595
MCP_TOKEN=<your-generated-key>
```

No login session, email or password is needed for MCP. `API_PORT` controls both
REST and MCP; if changed, use that port in the client URLs too. The fixed key also
authenticates normal API routes with administrator privileges.

## Codex CLI

On your client machine, set `MCP_TOKEN` to the same generated value, then register:

```sh
export MCP_TOKEN='<your-generated-key>'
codex mcp add hyperhive --url http://vm.tools:9595/mcp --bearer-token-env-var MCP_TOKEN
codex mcp list
```

Keep `MCP_TOKEN` available in the environment when launching Codex. Open a new
session and use `/mcp` to inspect the connection, then ask:
**"Use HyperHive to list my proxy hosts."**

## Claude Code

```sh
export MCP_TOKEN='<your-generated-key>'
claude mcp add --transport http hyperhive --scope user http://vm.tools:9595/mcp \
  --header "Authorization: Bearer $MCP_TOKEN"
claude mcp get hyperhive
```

This saves the header in your local user configuration and makes the connection
available across projects. Start Claude Code and use `/mcp` to inspect it, then
ask: **"Use HyperHive to list my proxy hosts."** Update the saved header if you
rotate the key. No OAuth/browser login is required.

To remove a connection:

```sh
codex mcp remove hyperhive
claude mcp remove hyperhive --scope user
```

## How it works and common errors

The client authenticates to `/mcp`, discovers `list_proxies` and calls it with
`{}`. The tool calls HyperHive's `/proxy/list` with the same fixed key. The master
validates it and automatically supplies a two-minute NPM credential internally,
using its existing local NPM state. Normal API login sessions still work.

- **401:** the client key is missing or incorrect. Check the Bearer header/key.
- **503 at /mcp:** the master has no valid `MCP_TOKEN`; set it and restart.
- **503 from the tool:** the local NPM signing key/database or active administrator is unavailable.
- **405 on authenticated GET /mcp:** expected; MCP uses POST, not a browser page.
- **Connection failure:** check that the updated master is running and the hostname/port is reachable.

See [server setup](README.md). Command references:
[Codex MCP](https://developers.openai.com/codex/mcp) and
[Claude Code MCP](https://code.claude.com/docs/en/mcp).
