# HyperHive MCP

Use HyperHive through named MCP tools. The master serves **Streamable HTTP** on
its API listener; standalone stdio shares the same 118-tool registry.

## API coverage

Last checked: **2026-10-20**, locally. Counts measure supported API endpoints.

| Scope | Supported / total | Coverage |
| --- | ---: | ---: |
| HyperHive API | 118 / 322 | **36.65%** |
| NGINX | 33 / 43 | 76.74% |
| Celld | 9 / 17 | 52.94% |
| DNS aliases | 3 / 4 | 75.00% |
| Notes | 5 / 6 | 83.33% |
| SPA management | 2 / 4 | 50.00% |
| ISOs | 2 / 3 | 66.67% |
| VMs, templates and disks | 64 / 82 | 78.05% |

Area rows are subsets, not additional endpoints. DNS, Notes and ISOs cover all
permitted operations. SPA creation is excluded for deletion effects. The explicitly
authorized VM increment adds creation, autostart/live flag changes,
automatic backup schedule create/update/enable, SSH key injection, and
`remove_vm_isos` (all CD-ROM devices, retaining ISO files). Schedules
can delete old backups under retention. Other deletion capabilities remain
excluded. The inventory includes six upstream endpoints and two internal recovery
helpers; the recovery helpers earn no direct tool credit. See [full state](UPDATE_STATE.md) for all mappings,
exclusions, validation and missing endpoints.

VM tools publish low/medium/high/very high risk. Medium+ requires explicit
per-invocation user approval in the harness picker. Before settings changes,
private retained records save actual old XML/settings; failed snapshots or
persistence block the operation. Configure MCP_VM_BACKUP_DIR or use the process
user's cache default. No automatic rollback is provided. SSH injection requires a
shut-off VM and additionally saves the primary disk file with a private size/hash
manifest before customization; other settings records are not disk backups.
Both updated master and slave snapshot support are required for protected VM
changes. See [VM contracts](docs/areas/vms.md) and [connection setup](CONNECTING.md).

Feature contracts: [upstreams](docs/areas/upstreams.md), [NGINX](docs/areas/nginx.md), [Celld](docs/areas/celld.md),
[DNS](docs/areas/dns.md), [Notes](docs/areas/notes.md), [SPA](docs/areas/spa.md),
[ISOs](docs/areas/isos.md), [VMs](docs/areas/vms.md).

DNS listing now includes optional `ssh_username` and `related_vm` metadata.
`dns_alias_add` accepts both fields and still returns `{success:true}`; omitted
or empty metadata clears stored values. See the [DNS contract](docs/areas/dns.md)
for examples and backend compatibility.

## Connect

Generate a key with `openssl rand -hex 32`, add it to master/.env, then restart
the updated master:

```dotenv
API_PORT=9595
MCP_TOKEN=<your-generated-key>
```

Connect to `http://<host>:9595/mcp` using `Authorization: Bearer <MCP_TOKEN>`.
The key also grants administrator REST access. No login session is needed.
See [CONNECTING.md](CONNECTING.md) for transport, picker and backup configuration.

## Standalone and development

Requires Go 1.25+. From mcp/:

```sh
go build -o bin/hyperhive-mcp ./cmd/hyperhive-mcp
go test ./...
```

Launch the binary by absolute path with HYPERHIVE_API_URL and MCP_TOKEN in its
environment; it does not load .env. Follow [AGENTS.md](AGENTS.md) and
[IMPLEMENT_TOOLS.md](IMPLEMENT_TOOLS.md) for selected implementation batches.
No production deployment or live VM qualification is claimed by these checks.
