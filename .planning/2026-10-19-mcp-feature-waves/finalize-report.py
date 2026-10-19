"""Task-local source-reviewed report assembly, not a shipped coverage CLI."""
import hashlib
import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PLAN = Path(__file__).resolve().parent
state = json.loads((PLAN / "coverage-state.json").read_text())
acceptance = json.loads((PLAN / "acceptance.json").read_text())
assert acceptance and all(acceptance.values()), acceptance
rows = state["inventory"]
tools = json.loads((PLAN / "discovery.json").read_text())["tools"]
assert len(rows) == 316 and len(tools) == 105
pairs = {(t["_meta"]["hyperhive/endpoint"]["method"], t["_meta"]["hyperhive/endpoint"]["path"]) for t in tools}
assert len(pairs) == 105
assert pairs == {(r["method"], r["path"]) for r in rows if r["status"] == "implemented"}

def count(group, status):
    return sum(r["status"].startswith(status) for r in group)

def percent(supported, total):
    return "N/A" if not total else f"{100 * supported / total:.2f}%"

areas = list(dict.fromkeys(r["area"] for r in rows))
old = (ROOT / "mcp/UPDATE_STATE.md").read_text()
area_sources = {}
for area in areas:
    match = re.search(r"^\| " + re.escape(area) + r" \|.*\| (\[[^\n]+) \|$", old, re.M)
    assert match, area
    area_sources[area] = match[1]

# Resolve current source lines rather than applying cumulative offsets.
virsh_lines = (ROOT / "master/api/virsh.go").read_text().splitlines()
for row in rows:
    if row["source"].startswith("[virsh.go:"):
        suffix = row["path"].removeprefix("/virsh")
        needle = f'r.{row["method"].title()}("{suffix}"'
        matches = [i + 1 for i, line in enumerate(virsh_lines) if needle in line]
        assert len(matches) == 1, (row, matches)
        number = matches[0]
        row["source"] = f"[virsh.go:{number}](../master/api/virsh.go#L{number})"

files = subprocess.check_output(["rg", "--files", "master", "slave", "api", "mcp"], cwd=ROOT, text=True).splitlines()
sources = sorted(p for p in files if (
    (p.startswith("mcp/") and (p.endswith(".go") or p in ("mcp/go.mod", "mcp/go.sum")))
    or (not p.startswith("mcp/") and ((p.endswith(".go") and not p.endswith("_test.go")) or p.endswith(".proto") or p in ("master/go.mod", "master/go.sum", "slave/go.mod", "slave/go.sum", "api/go.mod", "api/go.sum")))
))
digest = hashlib.sha256()
for path in sources:
    digest.update(path.encode() + b"\0" + (ROOT / path).read_bytes() + b"\0")
source_hash = digest.hexdigest()
(PLAN / "source-digest.json").write_text(json.dumps({"sha256": source_hash, "paths": sources}, indent=2) + "\n")
revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()

head = old.split("## Latest report", 1)[0]
report = f"""## Latest report

Checked: **2026-10-19**, from local source and SDK discovery. No live deployment check.

- Source revision: `{revision}` plus working-tree changes, preserving the preceding NGINX/Celld implementation.
- Source content SHA-256: `{source_hash}`.
- Digest input: sorted repository-relative paths; non-test Go and protobuf sources under master/slave/api, all MCP Go sources, and those four modules' go.mod/go.sum. Hash `path + NUL + file bytes + NUL` for each. Expanded to include the VM/ISO/DNS/SPA/Notes service, slave and RPC contracts; the task checkpoint retains the exact path manifest.
- Inventory: **316** unique explicit API method/path pairs, including UI/static/WebSocket/alias routes. Previous inventory 314 is unchanged except two additive authenticated recovery helpers: `GET /virsh/snapshot/{{vm_name}}` and `GET /virsh/hostsnapshot/{{machine_name}}`. Existing route registrations and API-source changes were reviewed; no runtime router walker or general coverage CLI is claimed.
- `/api` remains a prefix alias, not duplicate coverage. `/mcp` transport/session lifecycle and the separate pprof listener remain outside capability scope. Special Celld action-loop, GoAccess constant and nested XML-template registrations remain included.
- Status: **105 implemented, 158 missing, 53 excluded**; no enabled partial, stale or disabled endpoint is counted as implemented. Missing includes one blocked selected SSH operation and two internal recovery helpers; helper calls earn no extra tool credit.
- Scope: DNS, Notes and ISOs all permitted operations; SPA management reads S1/S2 (S3 excluded after effect review); all safely supportable selected VM operations. No new deletion permission was granted. The previous narrow Celld fleet-delete exception remains unchanged.
- Validation: full MCP tests, race checks and vet passed after final review fixes. Affected master API/services/virsh/protocol suites, API/NPM regression suites, master race/vet, generated RPC compilation and focused slave snapshot tests/race/vet passed. Three new stdio fixtures initially read stderr before the child writer stopped; the original feature agents fixed ordering without removing assertions, then the full gates passed. Exact 105-tool discovery expectations passed.
- Additional correctness review: two fresh VM reviewers and coordinator inspection found and fixed SDK input integer rounding that could mutate a different ID, incomplete pre-growth VM disk metadata, unsafe DNS whitespace/control writes, and older proxy-list numeric precision. Exact original input decoding retains schemas and full int64 IDs; invalid fractions/overflow/non-object roots fail before execution. New both-transport regressions and complete repeated normal/race/vet gates passed; tool count and coverage are unchanged.
- Limits: SDK HTTP and actual stdio use disposable HTTP backends. Notes/SPA/ISO actual handlers use temporary SQLite; VM actual auth/HTTP/service/RPC backup-before-write is tested in an isolated subprocess with disposable gRPC. Slave readers/commands are injected. No real DNS restart, successful ipset read, ISO completion, VM/hypervisor action, host boot change, NFS transfer qualification or deployment was performed.

| Scope | Implemented / total | Missing | Coverage |
| --- | ---: | ---: | ---: |
| **HyperHive API** | 105 / 316 | 158 | 33.23% |
| NGINX subtotal | 28 / 37 | 0 | 75.68% |
| VM subtotal, including templates/disks/helpers | 56 / 82 | 3 | 68.29% |

Subtotals are subsets of the API. Leaf areas partition the total. Missing omits policy exclusions; both new internal helper routes remain missing for direct tool coverage. Eligible-only percentages are meaningful only for reviewed selected scopes, not the entire API's still-unclassified browser/stream routes.

| Area | Implemented / total | Missing | Coverage | Route source |
| --- | ---: | ---: | ---: | --- |
"""
for area in areas:
    group = [r for r in rows if r["area"] == area]
    report += f"| {area} | {count(group, 'implemented')} / {len(group)} | {count(group, 'missing')} | {percent(count(group, 'implemented'), len(group))} | {area_sources[area]} |\n"

report += """
### Available capabilities and safety

Both transports share 105 registrations: 28 NGINX, nine Celld, three DNS, five Notes, two SPA, two ISO and 56 VM tools. Feature contracts: [NGINX](docs/areas/nginx.md), [proxies](docs/areas/nginx-proxies.md), [Celld](docs/areas/celld.md), [DNS](docs/areas/dns.md), [Notes](docs/areas/notes.md), [SPA](docs/areas/spa.md), [ISOs](docs/areas/isos.md), [VMs](docs/areas/vms.md).

Every VM tool publishes low/medium/high/very high risk. Medium+ requires per-call explicit user form approval bound to complete arguments; stale/replayed/forged/changed continuations or unsupported/declined/canceled pickers cannot execute writes. Modern HTTP remains stateless; legacy HTTP retains authenticated sessions for form elicitation. Stdio supports both protocol generations.

VM writes capture actual previous settings and secure inactive/current XML; CPU pinning includes the exact prior pinning getter. Host boot setters additionally capture every grubby entry, default kernel and current command line. Persistence must succeed before dispatch. Private 0700 directories and 0600 retained before.json files live in MCP_VM_BACKUP_DIR, or the process user's cache under hyperhive/mcp/vm-backups. Paths are returned even after subsequent backend failure. Configuration records do not contain disk snapshots or automatic rollback; disk growth/guest RAM loss cannot be undone by them. Locking covers this registry, not external API/hypervisor changes or independent server processes. Updated master/slave snapshot support is required; older backends fail closed.

VM export and backup download stream full binary responses to private artifacts on the MCP server filesystem, with size/hash and a six-hour bound honoring earlier cancellation. Clients do not automatically download these files. ISO download, manual VM backup and clone return initiation/acceptance, never fabricated completion/job IDs. Ordinary JSON calls retain shared timeout, cancellation, response-size, redirect and safe-error protections; mutations are never automatically retried.

DNS/Notes/ISO permitted endpoint coverage is 3/3, 5/5 and 2/2. SPA has the two selected reads; POST creation is excluded because firewall replacement/rollback deletes rules and can destroy an existing allow set. VM original selection is 56/57 permitted (98.25%): SSH injection remains absent because actual previous guest files/ownership cannot be backed up. All 23 VM deletion exclusions and both internal helpers are visible below. No arbitrary API dispatcher or confirmation argument was added.

| Tool | Endpoint | VM risk |
| --- | --- | --- |
"""
for tool in sorted(tools, key=lambda t: t["name"]):
    meta = tool["_meta"]
    endpoint = meta["hyperhive/endpoint"]
    report += f"| `{tool['name']}` | `{endpoint['method']} {endpoint['path']}` | {meta.get('hyperhive/risk', '—')} |\n"

report += """
### Endpoint inventory and remaining work

One row is one method/path pair. Only registered, enabled, contract-reviewed and validated tools count as implemented. Exclusions remain in raw coverage with zero credit; all/all-remaining/100% does not authorize deletion. The existing Celld exception does not authorize deletion elsewhere. Selected SSH injection is blocked and unregistered; the two snapshot routes are internal prerequisites with zero direct coverage. Unselected browser/stream routes remain visible for later disposition.
"""
for area in areas:
    report += f"\n#### {area}\n\n| Endpoint | Status | Route source |\n| --- | --- | --- |\n"
    for row in (r for r in rows if r["area"] == area):
        report += f"| `{row['method']} {row['path']}` | {row['status']} | {row['source']} |\n"
(ROOT / "mcp/UPDATE_STATE.md").write_text(head + report)

readme = """# HyperHive MCP

Use HyperHive through named MCP tools. The master serves **Streamable HTTP** on
its API listener; standalone stdio shares the same 105-tool registry.

## API coverage

Last checked: **2026-10-19**, locally. Counts measure supported API endpoints.

| Scope | Supported / total | Coverage |
| --- | ---: | ---: |
| HyperHive API | 105 / 316 | **33.23%** |
| NGINX | 28 / 37 | 75.68% |
| Celld | 9 / 17 | 52.94% |
| DNS aliases | 3 / 4 | 75.00% |
| Notes | 5 / 6 | 83.33% |
| SPA management | 2 / 4 | 50.00% |
| ISOs | 2 / 3 | 66.67% |
| VMs, templates and disks | 56 / 82 | 68.29% |

Area rows are subsets, not additional endpoints. DNS, Notes and ISOs cover all
permitted operations. SPA creation is excluded for deletion effects. VM SSH
injection is blocked by unavailable prior guest-file state; other deletion
capabilities remain excluded. The prior explicit Celld fleet-delete exception
remains scoped to Celld. Two new recovery helpers increase raw inventory by two
and earn no extra tool credit. See [full state](UPDATE_STATE.md) for all mappings,
exclusions, validation and missing endpoints.

VM tools publish low/medium/high/very high risk. Medium+ requires explicit
per-invocation user approval in the harness picker. Before settings changes,
private retained records save actual old XML/settings; failed snapshots or
persistence block the operation. Configure MCP_VM_BACKUP_DIR or use the process
user's cache default. No automatic rollback or disk-content backup is implied.
Both updated master and slave snapshot support are required for protected VM
changes. See [VM contracts](docs/areas/vms.md) and [connection setup](CONNECTING.md).

Feature contracts: [NGINX](docs/areas/nginx.md), [Celld](docs/areas/celld.md),
[DNS](docs/areas/dns.md), [Notes](docs/areas/notes.md), [SPA](docs/areas/spa.md),
[ISOs](docs/areas/isos.md), [VMs](docs/areas/vms.md).

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
"""
(ROOT / "mcp/README.md").write_text(readme)
(PLAN / "coverage-state.json").write_text(json.dumps(state, indent=2) + "\n")
print(f"Reconciled 105 tools, 316 endpoints; digest {source_hash}")
