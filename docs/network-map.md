# Network map

Status document for the unified HyperHive network flow graph (idea 5). Read this first when resuming work, and update it in place at the end of each milestone.

**Current status:** M1 implemented and verified locally, not deployed. **Next step:** review M1, then M2 when asked.

## Goal

One read-only, web-only graph that traces flow through HyperHive: domain, proxy/stream, then host, bridge, VM, Docker container, Kubernetes service or celld fleet, plus WireGuard. Nothing is persisted.

## Design

- One master endpoint, `GET /network/graph`, builds the graph on demand.
- Response: `{generatedAt, nodes:[{id,type,label,host,status,details}], edges:[{source,target,type,match}], warnings:[]}`. New sources add node types and a collector, not a new shape.
- A failed slave or source adds a warning and returns a partial graph.
- Targets match by DNS alias, then hostname lookup, then IP/port. Unmatched targets become an external/unknown node. Each edge records its match method.
- Traffic is live only: the API returns cumulative counters and a timestamp, and the client computes rates.
- The page polls every 10 seconds, pauses while the tab is hidden, and refreshes on `VMInfo` / `MigrateVm` websocket events.
- Frontend: React Flow (`@xyflow/react`) and `elkjs` in a `.web.tsx` file. The link is hidden on Android.
- MCP: read-only `network_graph_get`.

## Implementation notes (M1)

- Code: `master/services/network_graph.go`, `master/api/network_graph.go`, `mcp/internal/tools/network_graph.go`. Frontend repository: `app/network-map.tsx`, `components/network/`, `services/network-graph.ts`, `utils/network-graph.ts`.
- Node types: `host`, `interface`, `bridge`, `vm`, `domain`, `proxy`, `redirection`, `stream`, `upstream`, `wireguard`, `wireguard_peer`, `unknown`.
- Match values: `dns_alias`, `hostname`, `ip`, `loopback`, `domain`, `upstream`, `none`. Structural edges have an empty match.
- Upstream pools were added to the sources: the NPM readers blank the target of a pool-bound proxy or stream, so the pool is the only route.
- Loopback targets mean the master host, because NPM uses host networking. The master host comes from `k8s.IsMasterSlaveCtx`. If it cannot be detected, the target stays unknown.
- An address that matches more than one node stays unknown and adds a warning, rather than guessing. Default NAT gateways repeat across hosts.
- `veth*`, `vnet*` and loopback interfaces get no node, but their addresses still match.
- Warnings carry only the source name and failure kind. Raw errors go to the log, because they can quote upstream response bodies.
- Details are copied field by field. VNC passwords, `advanced_config`, `meta` and WireGuard keys never enter the graph.
- `services.GetAllVms` now passes its context to the slave RPC, so a hung slave fails on time and other hosts' VMs are kept.
- The page loads once and has a Refresh button. Polling and websocket refresh arrive with M5.

## Milestones

Work one at a time. Each milestone is shippable and is reviewed before the next one starts.

### M1: Foundation

- [x] Master collectors, pure graph builder and `GET /network/graph`
- [x] Hosts, interfaces, bridges, VMs, proxies, redirections, streams, DNS aliases, WireGuard
- [x] MCP `network_graph_get`
- [x] Web page with host groups, search, filters and side panel
- [x] Web-only sidebar link and native message

**Done when:** a domain can be followed to its VM or unknown node on the web page, and a failed source shows a warning.

### M2: celld

- [ ] Collector from existing fleet data, with no new RPC
- [ ] Fleet, per-host node and SILO nodes, with port matching
- [ ] Layer toggle

**Done when:** fleets appear on their hosts, and proxy or stream targets on celld ports link to them.

### M3: Docker

- [ ] Read-only slave RPC for containers, networks, IPs and published ports
- [ ] No container env, and no labels that may hold secrets
- [ ] Container nodes and target matching

**Done when:** containers appear under each host, and a host without Docker gives a warning rather than an error.

### M4: Kubernetes

- [ ] Confirm kubectl and kubeconfig availability on agent nodes
- [ ] Read-only RPC for nodes, Services, Ingress and ports, never Secrets
- [ ] De-duplicated cluster with Ingress, Service and NodePort edges

**Done when:** a domain can be followed into a cluster Service.

### M5: Live traffic and all NICs

- [ ] `GetVMInterfaceStats` RPC and master wrapper
- [ ] VM NIC nodes, plus host and stream counters
- [ ] Polling, websocket refresh, status colors and edge thickness
- [ ] Counter resets give no rate, and older slaves give a warning

**Done when:** edge thickness follows current traffic while the page is open.

### M6: Polish

- [ ] Collapsible groups, flow-path highlight and layer presets
- [ ] Large graph performance check
- [ ] Final docs and SilverBullet update

**Done when:** clicking a domain highlights its full route, and a large graph stays usable.

## Known limits

- Dead slaves leave the connection list and no registry exists. Offline hosts can only come from machines with NFS shares, so a dead slave without shares has no node.
- Flow is inferred from configuration, not captured from packets. An edge shows a configured route, not proof of traffic.
- Each VM shows one NIC, from `Vm.network`, until M5.
- `Vm.network` is a libvirt network name, so it links to a host interface only on an exact name match. Otherwise the bridge hangs off the host.
- Targets match by address only in M1. Ports appear in node details and start to matter with M2 and M3.
- ELK runs on the main thread. Large graphs are checked in M6.

## Verification log

Record per milestone what was verified locally and what was verified live.

**M1, 2026-11-04, local only.** Nothing was deployed and nothing was checked against live infrastructure.

- Master: builder, matcher, timeout and failure-warning unit tests pass, also under `-race`. A route test confirms every source failing still returns 200 with warnings.
- MCP: `go test ./...` and `go vet` pass, covering HTTP and stdio discovery, the authenticated call, invalid responses, unknown arguments and body suppression.
- Frontend: unit tests pass. The Metro web and Android bundles build, and the Android bundle contains no `xyflow` or ELK code.
- Browser: a production web build ran against a local mock serving real `BuildNetworkGraph` output. Verified: a domain traced through a redirection and proxy to its VM, the warning banner, search, host and type filters, the side panel, reopening it, "Open page", and a failed refresh keeping the last graph.
- Not verified: real slaves, NPM, WireGuard or DNS lookups; the native message on a device; keyboard activation of nodes.
- Existing failures unrelated to M1, identical on a clean checkout of HEAD: `master/virsh` tests do not build because `VMXMLHasDiskPath` is undefined, `master/api/npmapi` MCP contract tests fail on `tool_approval` because their test client lacks elicitation, and two `tests/sidebar.test.js` cases fail.

## Not included

Stored traffic history, manual link overrides, saved node positions, master-to-slave WireGuard links, pods, celld request-flow tracing, packet capture and a mobile graph.
