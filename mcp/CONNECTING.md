# Connect to HyperHive MCP

HyperHive starts MCP with the master at `http://<host>:9595/mcp` using
**Streamable HTTP**. HTTP and standalone stdio expose the same named tools.
See [current coverage](README.md) and the linked feature contracts for available
operations, exclusions and complete inputs.
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

### VM approval and recovery records

Every VM, disk, template and host-tuning tool publishes `hyperhive/risk` as
`low`, `medium`, `high` or `very high`. Medium and higher require an explicit
`approve` choice in the client's MCP form picker for each invocation. Tool
arguments cannot supply approval. Decline, cancellation, unsupported picker,
expired continuation or changed arguments block the operation.

Protocol 2026-07-28 clients use the input-required/multi-round-trip form flow on
both transports. Older clients use legacy form elicitation; HTTP retains a
session for these clients, with a 20-minute idle timeout. The fixed Bearer key
is required on every HTTP request, including picker replies. Clients without
form support cannot execute protected operations. Client UI support must be
verified with the actual harness; server tests use SDK clients with disposable
backends. Modern HTTP remains stateless. GET without a legacy session returns
405 as before.

If a VM call reports an approval error but no picker appeared, that error does
not establish that the user declined. The MCP client may have canceled the form,
returned no decision, or been unable to complete elicitation. Current errors
identify the response category without exposing response payloads. Reconnect or
use a client that displays MCP form elicitation and retry the exact invocation;
permission typed in chat does not satisfy the server's per-call form gate.
A tool being listed does not establish that its approval UI is supported.

For Codex desktop, an effective `approval_policy = "never"` suppresses approval
prompts. Use an interactive permission policy that permits MCP elicitation, or a
granular policy with `mcp_elicitations = true`. For example, this configuration
allows MCP forms while leaving the other approval prompt categories disabled:

```toml
approval_policy = { granular = { sandbox_approval = false, rules = false, mcp_elicitations = true, request_permissions = false, skill_approval = false } }
```

Apply it in the appropriate Codex configuration or active permission profile,
then retry after the effective policy refreshes. Start a new chat if the existing
chat retains the old policy. A desktop-selected permission profile can override
file defaults. This enables a human prompt; it does not
automatically approve a VM operation. Official reference:
https://learn.chatgpt.com/docs/config-file/config-reference

Before settings changes, MCP saves the old domain XML and relevant prior state
on the filesystem of the MCP process. Configure an absolute private directory:

```dotenv
MCP_VM_BACKUP_DIR=/var/lib/hyperhive/mcp-vm-backups
```

Apply this environment variable to the master for embedded HTTP, or to the
standalone process for stdio. It is never a tool argument. The default is the
process user's cache directory under `hyperhive/mcp/vm-backups`. Directories
must be private (0700); records are unique 0600 `before.json` files. MCP retains
them without automatic cleanup and returns their location. Snapshot read,
validation or persistence failure blocks the write. Records may contain secrets;
keep them private. They preserve configuration, not disk contents, and do not
provide automatic rollback or atomicity against changes made outside this MCP
registry. See [VM contracts](docs/areas/vms.md) for operation-specific limits.

### Authentication and transport

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
