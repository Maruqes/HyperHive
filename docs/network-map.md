# Network map

Status document for the unified HyperHive network flow graph (idea 5). Read this first when resuming work, and update it in place at the end of each milestone.

**Current status:** M1 and live traffic (M5, brought forward) implemented and verified locally, not deployed. **Next step:** deploy master and slaves and check real rates, then M2 when asked.

## Goal

One read-only, web-only graph that traces flow through HyperHive: domain, proxy/stream, then host, bridge, VM, Docker container, Kubernetes service or celld fleet, plus WireGuard. Nothing is persisted.

## Design

- One master endpoint, `GET /network/graph`, builds the graph on demand.
- Response: `{generatedAt, nodes:[{id,type,label,host,status,details}], edges:[{source,target,type,match}], warnings:[]}`. New sources add node types and a collector, not a new shape.
- A failed slave or source adds a warning and returns a partial graph.
- Targets match by DNS alias, then hostname lookup, then IP/port. Unmatched targets become an external/unknown node. Each edge records its match method.
- Traffic is live only and nothing is stored. `GET /network/traffic` returns bytes per second, computed on the master. See [Live traffic](#live-traffic).
- The page polls traffic every 2 seconds and pauses while the tab is hidden. The graph itself loads once and on Refresh.
- Frontend: React Flow (`@xyflow/react`) in a `.web.tsx` file, with a deterministic board layout in `utils/network-graph.ts`. The link is hidden on Android.
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
- The page loads the graph once and has a Refresh button.

## Live traffic

Each measurement point reports what it can really see, so a number is never inferred from configuration.

| Shown on | Source | Meaning |
| --- | --- | --- |
| Proxy, stream and pool lines | Kernel TCP sockets on the master host, read with `netlink.SocketDiagTCPInfo` | Bytes between NPM and that target `ip:port`. `→` towards the target, `←` back. |
| VM cards | New slave RPC `GetVMInterfaceStats`, one libvirt call per host | All NICs of the VM, seen from the guest. `↑` sent, `↓` received. |
| WireGuard peer cards | `wgctrl` on the master | Seen from the peer. |
| Host interfaces, in the machine side panel | Existing `GetNetworkSummary` | Seen from the host. |

- `flows` in the graph lists the `ip:port` endpoints behind each route. The traffic response is keyed by endpoint and node ID, so polling never touches NPM, DNS or the graph builder.
- Only outbound sockets count: a socket sharing its local port with a listener was accepted, and counting it would add every client and both ends of a loopback hop.
- Rates are computed on the master because socket totals fall whenever a connection closes. The sampler keeps the last sample per socket, so a closed socket lowers nothing and a new one counts in full.
- No rate is reported for a first sample, a baseline over 15 seconds old, a lower counter (VM restart), or sockets after a failed or interrupted socket dump. Requests within a second share one sample.
- Traffic stays out of the board state: changing the board resets the view. Lines read it in the edge memo and cards through a context.
- Idle lines stay plain. Busy lines thicken on a log scale and draw above the boxes. A failed poll clears all rates, because old numbers would read as current.

## Board layout

The API graph stays complete; `networkBoard` turns it into a view where each thing appears once.

- Columns run left to right: entry points, then pools and external targets, then machines.
- Entry column boxes: Proxied domains, Redirects, Streams, DNS aliases, WireGuard. Middle column boxes: Upstream pools, External / unknown. New sources add a box, for example Docker inside a machine in M3.
- Containment replaces lines: a VM sits in its network box, and a network box in its machine box. A machine box is the host itself, and the WireGuard box is the server.
- A proxy or redirection card shows its domains and target, so proxied domains get no card of their own. Only unrouted DNS aliases do, and an alias with no match appears once, under External.
- Interfaces are listed on the machine header and in its side panel, not as cards.
- Only `forwards`, `resolves` and `redirects` edges are drawn. A dashed line is an unmatched target. Match methods are in the side panel.
- Hovering or selecting anything lights its routes and fades the rest. This covers the M6 flow-path highlight for direct use.
- Search walks forward along routes only, and back along routes and containment, so reaching a host does not pull in everything on it.
- The view fits the width and starts at the top; scrolling pans. `elkjs` was removed: hierarchical auto-layout was the source of the disorder, and a fixed layout also stays still between M5 polls.

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

- [x] `GetVMInterfaceStats` RPC and master wrapper
- [x] Per-connection rates on lines, and VM, host interface and WireGuard counters
- [x] Polling and edge thickness
- [x] Counter resets give no rate, and older slaves give a warning
- [ ] VM NIC nodes, one per NIC (rates are summed per VM for now)
- [ ] Graph polling, websocket refresh and status colors

**Done when:** edge thickness follows current traffic while the page is open.

### M6: Polish

- [ ] Collapsible groups and layer presets (route highlight landed early, in the M1 board)
- [ ] Large graph performance check
- [ ] Final docs and SilverBullet update

**Done when:** clicking a domain highlights its full route, and a large graph stays usable.

## Known limits

- Dead slaves leave the connection list and no registry exists. Offline hosts can only come from machines with NFS shares, so a dead slave without shares has no node.
- Flow is inferred from configuration, not captured from packets. An edge shows a configured route, not proof of traffic.
- Each VM shows one network, from `Vm.network`. Its traffic rate covers all NICs.
- A socket does not say which proxy opened it, so routes to the same target `ip:port` show the same number, marked `shared`.
- Line rates cover TCP only. UDP streams show no rate, and neither do redirections, which carry no traffic to a target.
- A connection that opens and closes between two polls is missed, and the last bytes before a close are lost. Long transfers, which dominate the numbers, are measured. Many short requests read low.
- Line rates are bytes between NPM and the target, not the client side, and TLS or compression can make those differ. Traffic that reaches a VM without NPM appears on its card only.
- Rates are throughput only: there are no running totals, history or MCP tool yet.
- `Vm.network` is a libvirt network name, so it links to a host interface only on an exact name match. Otherwise the bridge hangs off the host.
- Targets match by address only in M1. Ports appear in node details and start to matter with M2 and M3.
- Tall boards scroll rather than fit on one screen. Large graphs are checked in M6.
- A line to a VM can pass behind middle-column boxes until it is highlighted.

## Verification log

Record per milestone what was verified locally and what was verified live.

**M1, 2026-11-04, local only.** Nothing was deployed and nothing was checked against live infrastructure.

- Master: builder, matcher, timeout and failure-warning unit tests pass, also under `-race`. A route test confirms every source failing still returns 200 with warnings.
- MCP: `go test ./...` and `go vet` pass, covering HTTP and stdio discovery, the authenticated call, invalid responses, unknown arguments and body suppression.
- Frontend: unit tests pass. The Metro web and Android bundles build, and the Android bundle contains no `xyflow` code, rechecked after the board rework.
- Browser: a production web build ran against a local mock serving real `BuildNetworkGraph` output. Verified: a domain traced through a redirection and proxy to its VM, the warning banner, search, host filter, the side panel, reopening it, "Open page", and a failed refresh keeping the last graph.
- Board rework, same mock with 77 nodes on four machines: divisions, nesting, route highlight on hover, route-only search, division toggles, bounded panning and the machine side panel.
- Not verified: real slaves, NPM, WireGuard or DNS lookups; the native message on a device; keyboard activation of nodes.
- Existing failures unrelated to M1, identical on a clean checkout of HEAD: `master/virsh` tests do not build because `VMXMLHasDiskPath` is undefined, `master/api/npmapi` MCP contract tests fail on `tool_approval` because their test client lacks elicitation, and two `tests/sidebar.test.js` cases fail.

**Live traffic, 2026-11-04, local only.** Nothing was deployed and nothing was checked against live infrastructure.

- Master: rate, socket filter, flow and WireGuard unit tests pass, also under `-race`. The route test covers `/network/traffic` with every source failing.
- Real kernel: the sampler measured a local TCP transfer at 10.36 MB/s against about 10.4 MB/s sent, on one socket, with the accepting side excluded.
- Slave: NIC summing is tested. The libvirt call itself was not run against a hypervisor.
- Frontend: unit tests pass and the web bundle builds.
- Browser: a production web build against a local mock with changing rates. Verified: rates on lines and cards update while the viewport stays put, `shared` labels, line thickness, the VM side panel, no polls while hidden, and a failed poll clearing rates and recovering.
- Not verified: real NPM sockets, libvirt counters, an older slave's warning, or the host side panel in a browser (its rates are unit-tested).

## Not included

Stored traffic history, manual link overrides, saved node positions, master-to-slave WireGuard links, pods, celld request-flow tracing, packet capture and a mobile graph.
