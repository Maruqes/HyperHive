# Network map

Status document for the unified HyperHive network flow graph (idea 5). Read this first when resuming work, and update it in place at the end of each milestone.

**Current status:** all milestones, M1 to M6, are implemented and verified locally, not deployed. **Next step:** deploy master and slaves, then check real Docker, k3s, NPM and libvirt data.

## Goal

One read-only, web-only graph that traces flow through HyperHive: domain, proxy/stream, then host, bridge, VM, Docker container, Kubernetes service or celld fleet, plus WireGuard. Only traffic history and where pieces were moved to are persisted.

## Design

- One master endpoint, `GET /network/graph`, builds the graph on demand.
- Response: `{generatedAt, nodes:[{id,type,label,host,status,details}], edges:[{source,target,type,match}], flows:[{source,target,endpoints}], positions:{id:{x,y}}, warnings:[]}`. New sources add node types and a collector, not a new shape.
- A failed slave or source adds a warning and returns a partial graph.
- Targets match by DNS alias, then hostname lookup, then IP/port. Unmatched targets become an external/unknown node. Each edge records its match method.
- `GET /network/traffic` returns bytes per second, computed on the master. See [Live traffic](#live-traffic). `GET /network/traffic/history` returns saved rates. See [Traffic history](#traffic-history).
- The page polls traffic every 2 seconds and pauses while the tab is hidden. After a successful authenticated write (any 2xx request that is not GET, HEAD or OPTIONS), the master sends a `NetworkChanged` websocket message with no data, at most once per second and always after the last write.
- The page reloads the graph silently every 30 seconds while visible and 500 ms after a `NetworkChanged` message. Silent reloads pause during a drag or pending save, keep the last graph on failure, and never move the viewport. A refresh arriving mid-drag is skipped and picked up by the next trigger.
- The view fits only on first load, Refresh, and search, host or division changes.
- Frontend: React Flow (`@xyflow/react`) in a `.web.tsx` file, with a deterministic board layout in `utils/network-graph.ts`. The link is hidden on Android.
- MCP: read-only `network_graph_get`.

## Implementation notes (M1)

- Code: `master/services/network_graph.go`, `master/api/network_graph.go`, `mcp/internal/tools/network_graph.go`. Frontend repository: `app/network-map.tsx`, `components/network/`, `services/network-graph.ts`, `utils/network-graph.ts`.
- Node types: `host`, `interface`, `bridge`, `vm`, `vm_nic`, `domain`, `proxy`, `redirection`, `stream`, `upstream`, `wireguard`, `wireguard_peer`, `celld_fleet`, `celld_member`, `container`, `k8s_cluster`, `k8s_ingress`, `k8s_service`, `unknown`.
- Match values: `dns_alias`, `hostname`, `ip`, `loopback`, `domain`, `upstream`, `celld_port`, `docker_port`, `k8s_port`, `k8s_ingress`, `none`. Structural edges have an empty match.
- Upstream pools were added to the sources: the NPM readers blank the target of a pool-bound proxy or stream, so the pool is the only route.
- Loopback targets mean the master host, because NPM uses host networking. The master host comes from `k8s.IsMasterSlaveCtx`. If it cannot be detected, the target stays unknown.
- An address that matches more than one node stays unknown and adds a warning, rather than guessing. Default NAT gateways repeat across hosts.
- `veth*`, `vnet*` and loopback interfaces get no node, but their addresses still match.
- Warnings carry only the source name and failure kind. Raw errors go to the log, because they can quote upstream response bodies.
- Details are copied field by field. VNC passwords, `advanced_config`, `meta` and WireGuard keys never enter the graph.
- `services.GetAllVms` now passes its context to the slave RPC, so a hung slave fails on time and other hosts' VMs are kept.
- The page has a Refresh button. Automatic refresh is described in [Design](#design).

## celld (M2)

- Fleets come from `CelldFleet.List()`, which reads master state only. There is no new RPC, endpoint or proto.
- One `celld_fleet` card (`celld:<id>`) sits in the "Celld fleets" section of the middle column. A `celld_member` card (`celld:<id>:<host>`) sits inside each machine it runs on, outside the network boxes. Drawn `celld_member` lines join them; a `runs` edge ties the member to its host for search and is not drawn.
- A proxy or stream target links to the exact member when its resolved `ip:port` equals a member's HTTP or internal port, with match `celld_port`. Loopback counts for members on the master. The same address on another port stays on the host, and upstream pools still point at the pool.
- Status: `running` is up, `stopped` disabled, `error` and `degraded` down, and transitional states unknown. A member is up only when running and healthy. Stopped or failed fleets stay on the map, greyed out, and routes to them still link.
- Details are copied field by field: `Deploy`, `Storage`, `URLs` and raw `Error` text never enter the graph.
- A member with no IP gets a card but no port match.
- History: a member charts the endpoints of routes into it, and a fleet adds up its members. Only traffic through NPM is measured, so a fleet with no proxy or stream route has no chart.
- SILO nodes were dropped from M2.

## Docker (M3)

- Each slave reads its Docker Unix socket (`slave/docker/containers.go`) through the `DockerService.ListContainers` RPC. The graph code is `master/services/network_graph_docker.go`.
- A `container` card (`container:<host>:<name>`) has a `runs` edge from its host. Details are image, state, networks, ports and addresses.
- Env, commands and labels are never forwarded. Labels are read only to skip containers with a `celld.` label, because celld members have their own cards.
- Status: `running` is up, `exited` and `created` disabled, `dead` down, and anything else unknown.
- A proxy or stream target links to a container with match `docker_port` when it resolves to the container's host, loopback included, and its port is a published port. Other ports stay on the host, and upstream pools still point at the pool.
- A target on a container IP also links, but only for containers on the master host: only NPM's host can route to a Docker bridge address, and bridge IPs repeat across hosts.
- An unreachable socket gives the warning `host <name> docker unavailable`, as does an older slave.

## Kubernetes (M4)

- Agents have no kubeconfig, so the read runs only on the k3s server: the master calls `K8sService.GetClusterResources` on the host where `k8s.IsMasterSlaveCtx` is true, which also de-duplicates the cluster. This was confirmed from source (`slave/our_k8s/our_k8s.go`), not on live agents. The slave code is `slave/our_k8s/resources.go`, the graph code `master/services/network_graph_k8s.go`.
- One `kubectl get nodes,services,ingresses -A -o json` call with a 5 second timeout. Only listed fields are decoded, so annotations, labels, TLS secret names and Secrets never reach the proto message or the graph.
- One `k8s_cluster` card (`k8s:cluster`), plus `k8s_ingress` (`k8s:ingress:<ns>:<name>`) and `k8s_service` (`k8s:service:<ns>:<name>`) cards, joined to it by `k8s_member` edges. A `k8s_route` edge goes from an ingress to each service it targets.
- Status: the cluster is up when all nodes are ready, down when none are, otherwise unknown. Services and ingresses stay unknown, because no health is measured.
- `default/kubernetes` and ClusterIP services in `kube-system` are skipped unless an ingress targets them.
- A proxy or stream target on a service's cluster IP and port links to the service with match `k8s_port`. The same address on another port stays unknown.
- A target on a cluster node, loopback included, on a NodePort or LoadBalancer port links to the ingress whose rule host equals a proxy domain, with match `k8s_ingress`. Otherwise it links to the service with `k8s_port`. Streams have no domains. A domain on several ingresses, usually split by path, links to the service.
- A celld member or published Docker port on the same target wins. Other ports and hosts outside the cluster stay on the host, and upstream pools still point at the pool.
- A failed read or an older slave gives the warning `kubernetes unavailable`, or `kubernetes timed out`, and a partial graph.
- Limits: an ingress backend that names its port shows port 0. Only exact rule hosts match, not wildcards.

## Live traffic

Each measurement point reports what it can really see, so a number is never inferred from configuration.

| Shown on | Source | Meaning |
| --- | --- | --- |
| Proxy, stream and pool lines | Kernel TCP sockets on the master host, read with `netlink.SocketDiagTCPInfo` | Bytes between NPM and that target `ip:port`. `→` towards the target, `←` back. |
| VM cards | New slave RPC `GetVMInterfaceStats`, one libvirt call per host | All NICs of the VM, seen from the guest. `↑` sent, `↓` received. |
| VM NICs (`nic:<host>:<vm>:<mac>`) | Same RPC, device mapped to MAC from the domain XML | One NIC, seen from the guest. Missing for a VM whose XML could not be read, and for older slaves. |
| WireGuard peer cards | `wgctrl` on the master | Seen from the peer. |
| Host interfaces, in the machine side panel | Existing `GetNetworkSummary` | Seen from the host. |

- `flows` in the graph lists the `ip:port` endpoints behind each route. The traffic response is keyed by endpoint and node ID, so polling never touches NPM, DNS or the graph builder.
- Only outbound sockets count: a socket sharing its local port with a listener was accepted, and counting it would add every client and both ends of a loopback hop.
- Rates are computed on the master because socket totals fall whenever a connection closes. The sampler keeps the last sample per socket, so a closed socket lowers nothing and a new one counts in full.
- No rate is reported for a first sample, a baseline over 15 seconds old, a lower counter (VM restart), or sockets after a failed or interrupted socket dump. Requests within a second share one sample.
- Traffic stays out of the board state, so a poll never rebuilds the cards. Lines read it in the edge memo and cards through a context.
- Idle lines stay plain. Busy lines thicken on a log scale and draw above the boxes. Rate labels sit beside the source card, because a midpoint can land on a card in the next column. A failed poll clears all rates, because old numbers would read as current.

## Traffic history

Clicking a card or a line opens the side panel with a chart for 1d, 7d, 30d, 3m, 6m or 1y: average and peak rate per direction, plus totals for the range.

- Code: `master/db/network_history.go`, `master/services/network_history.go`. Frontend repository: `components/network/network-history.web.tsx`.
- `RunHistoryLoop` calls `Traffic` every 10 seconds, so sampling no longer depends on an open page. `Traffic` records every sample, so an open map adds detail to the same buckets.
- Data lives in `network_history.db`, next to `data.db` and outside its backups. If it cannot open, the master logs an error and runs without history. Deleting the file only loses charts.
- Every sample is added to all three tiers, so no rollup job exists. An hourly cleanup deletes by age.

| Bucket | Kept for | Ranges |
| --- | --- | --- |
| 1 minute | 25 hours | 1d |
| 15 minutes | 31 days | 7d, 30d (merged to 1 hour for the response) |
| 6 hours | 366 days | 3m, 6m, 1y |

- Each row holds bytes and peak rate per direction, under the same keys as live traffic: `ip:port` or a node ID. A changed IP or moved VM starts a new history, and deleted resources age out.
- Each sample also writes a row with an empty key, so a sampled idle bucket reads as zero and an unsampled one as a gap (null rates).
- `GET /network/traffic/history?range=7d&key=...&key=...` sums up to 64 keys. Peaks are summed per bucket, so across keys they are an upper bound.
- Proxies, streams and pools chart their outgoing endpoints; VMs, NICs, peers and interfaces their own key. Containers, ingresses and services chart the routes into them. A host offers a picker of its interfaces, because adding them would count bridged traffic twice.
- Limits: a bucket counts as fully sampled, so the newest one and those beside an outage read low. A failed slave or socket dump inside a sampled bucket reads as idle. Live traffic limits apply too: short sockets can be missed and UDP has no rate.

## Board layout

The API graph stays complete; `networkBoard` turns it into a view where each thing appears once.

- Columns run left to right: entry points, then pools and external targets, then machines.
- Entry column boxes: Proxied domains, Redirects, Streams, DNS aliases, WireGuard. Middle column boxes: Upstream pools, Celld fleets, Kubernetes, External / unknown. New sources add a box.
- Containment replaces lines: a VM sits in its network box, and a network box in its machine box. A machine box is the host itself, and the WireGuard box is the server.
- Each machine holds its networks, then celld members, then a Docker box. Stopped containers are greyed.
- The Kubernetes box is the cluster. It holds ingress cards, then service cards.
- Only secondary NICs get cards, inside their network box, because the VM card stands for the primary one. The VM side panel lists all NICs with live rates. Network counts stay VMs only.
- A proxy or redirection card shows its domains and target, so proxied domains get no card of their own. Only unrouted DNS aliases do, and an alias with no match appears once, under External.
- Interfaces are listed on the machine header and in its side panel, not as cards.
- Only `forwards`, `resolves`, `redirects`, `celld_member` and `k8s_route` edges are drawn. A dashed line is an unmatched target. Match methods are in the side panel.
- Hovering or selecting anything lights its routes and fades the rest. This covers the M6 flow-path highlight for direct use.
- Search walks forward along routes only, and back along routes and containment, so reaching a host does not pull in everything on it.
- The view fits the width and starts at the top; scrolling pans. `elkjs` was removed: hierarchical auto-layout was the source of the disorder, and a fixed layout also stays still between M5 polls.

## Collapsing and layers

- Every box can collapse to its header. The state is remembered per browser in `network-map-collapsed`, not on the master.
- Lines into a collapsed box move to the nearest visible box. Lines that become identical merge, and merged lines show no rate or history.
- Search or a host filter ignores collapsing, and the chevrons are disabled meanwhile.
- The Layers menu applies presets (All, Routes, Compute, VPN) and shows Custom when the toggles match none.

## Saved layout

Any card or box can be dragged, or moved with the arrow keys when selected. Positions live on the master, so every browser and user sees the same map.

- Code: `master/db/network_layout.go`, `master/services/network_layout.go`. Frontend repository: `networkPlacedBoard` in `utils/network-graph.ts`.
- Data lives in the `network_layout` table in `data.db`, so it is backed up, unlike traffic history. A failed read adds a `saved layout` warning and shows the default board.
- Pages get positions in `positions` of `GET /network/graph`. `PUT /network/layout` takes `{id:{x,y}}` and changes only those pieces, so two open pages cannot undo each other's other moves. `DELETE /network/layout` clears them, behind the Reset layout button.
- Only moved pieces are saved, by board node ID, so everything else keeps following the default board. A piece inside a box is stored relative to it and moves with it.
- Box sizes are not saved. A box always fits its pieces, growing and shrinking during a drag too, and a piece stays below its box header. React Flow's `expandParent` is not used, because it only ever grows a box.
- Saves wait 400 ms, so repeated arrow keys send one request. A failed save shows an error and is retried with the next move. Leaving the page sends any waiting save.
- A dragged piece snaps to the edges and centres of any other piece within 8 screen pixels, also across boxes, and dashed guide lines show the match. It skips the boxes around it and the pieces inside it. Arrow keys and multi-piece drags do not snap.
- Moving a piece does not reset the view. The canvas has no pan limit, and dragging near its edge pans it, so a piece can go anywhere.
- Limits: boxes do not push each other, so a box that grows can cover the one below it, and a new card can land on a moved one. A filtered view keeps saved positions, so it can leave gaps. Rows for deleted resources stay, so a recreated resource returns to its old spot. A VM on a new host is a new ID and starts at the default.
- The MCP client ignores `positions`: `network_graph_get` stays read-only, and no MCP tool writes the layout.

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

- [x] Collector from existing fleet data, with no new RPC
- [x] Fleet and per-host member nodes, with port matching (SILO dropped)
- [x] Layer toggle

**Done when:** fleets appear on their hosts, and proxy or stream targets on celld ports link to them.

### M3: Docker

- [x] Read-only slave RPC for containers, networks, IPs and published ports
- [x] No container env, and no labels that may hold secrets
- [x] Container nodes and target matching

**Done when:** containers appear under each host, and a host without Docker gives a warning rather than an error.

### M4: Kubernetes

- [x] Confirm kubectl and kubeconfig availability on agent nodes (from source: agents have no kubeconfig)
- [x] Read-only RPC for nodes, Services, Ingress and ports, never Secrets
- [x] De-duplicated cluster with Ingress, Service and NodePort edges

**Done when:** a domain can be followed into a cluster Service.

### M5: Live traffic and all NICs

- [x] `GetVMInterfaceStats` RPC and master wrapper
- [x] Per-connection rates on lines, and VM, host interface and WireGuard counters
- [x] Polling and edge thickness
- [x] Counter resets give no rate, and older slaves give a warning
- [x] VM NIC nodes, one per NIC, with per-NIC rates (backend; the VM keeps its summed rate)
- [x] Graph polling, websocket refresh and status colors

**Done when:** edge thickness follows current traffic while the page is open.

### M6: Polish

- [x] Collapsible groups and layer presets (route highlight landed early, in the M1 board)
- [x] Large graph performance check
- [x] Final docs and SilverBullet update

**Done when:** clicking a domain highlights its full route, and a large graph stays usable. Met locally, with the drag limit under [Known limits](#known-limits).

## Known limits

- Dead slaves leave the connection list and no registry exists. Offline hosts can only come from machines with NFS shares, so a dead slave without shares has no node.
- Flow is inferred from configuration, not captured from packets. An edge shows a configured route, not proof of traffic.
- A VM card sits under its primary network, from `Vm.network`, and its own rate covers all NICs. Every NIC has a `vm_nic` node with its own rate, and a secondary network gets a bridge. Older slaves send no NICs, so their VMs show one network only.
- A socket does not say which proxy opened it, so routes to the same target `ip:port` show the same number, marked `shared`.
- Line rates cover TCP only. UDP streams show no rate, and neither do redirections, which carry no traffic to a target.
- A connection that opens and closes between two polls is missed, and the last bytes before a close are lost. Long transfers, which dominate the numbers, are measured. Many short requests read low.
- Line rates are bytes between NPM and the target, not the client side, and TLS or compression can make those differ. Traffic that reaches a VM without NPM appears on its card only.
- Live rates are throughput only. Saved history and totals are in [Traffic history](#traffic-history); there is no MCP tool for either yet.
- `Vm.network` is a libvirt network name, so it links to a host interface only on an exact name match. Otherwise the bridge hangs off the host.
- Targets match by address, plus port for celld members, published Docker ports and Kubernetes services. Other ports appear in node details only.
- Tall boards scroll rather than fit on one screen. A 2000-node graph stays usable, but drag p95 is 41 to 55 ms, over the 33 ms target. See the [Verification log](#verification-log).
- A line to a VM can pass behind middle-column boxes until it is highlighted.
- Traffic routed through an ingress charts on the ingress, not its service.
- Upstream pool members and DNS aliases do not use Docker port or Kubernetes matching, so a pool member on a NodePort stays on the host.
- A domain matching several ingresses links to the service.
- A matching domain on any NodePort links to the ingress, even if that port is not the ingress controller's.
- A named ingress backend port reads as 0.
- Off-screen cards leave the DOM (`onlyRenderVisibleElements`), so they are not in the Tab order.

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

**M2 celld, 2026-11-04, local only.** Nothing was deployed and nothing was checked against live infrastructure.

- Master: member nodes and edges, proxy and stream matching on HTTP and internal ports through IP, alias, hostname and loopback, a non-celld port staying on the host, two fleets on one host, a stopped fleet, a member with no IP, and no `Deploy`, storage, URL or raw error data in the graph. `go test -tags libvirt_dlopen ./services/ ./api/` and the MCP tools tests pass.
- Frontend: unit tests pass, covering placement, the fleet section, drawn lines, the toggle and search.
- Browser: a production web build against a local mock with a two-machine fleet and a stopped fleet. Verified: member cards inside machines, greyed cards, the side panel with the `Celld port` match, and the toggle hiding cards and lines.
- Not verified: real fleets on live hosts, or the Celld source failing in a running master (the shared warning path is tested).

**Saved layout, 2026-11-04, local only.** Nothing was deployed and nothing was checked against live infrastructure.

- Master: database, validation and route tests pass, also under `-race`, against in-memory SQLite. The graph route test confirms saved positions come back in `GET /network/graph`.
- Frontend: unit tests pass. No new type errors.
- Browser: a production web build against a local mock that stores positions like the master. Verified: dragging a card, a box, a network inside a machine and a whole machine with its contents; lines following; boxes growing; positions surviving a reload; no side panel on drag and click still opening it; three arrow presses sending one save; a failed save showing an error and being retried; Reset layout.
- Box fitting and the infinite canvas, same mock with a celld fleet: during one drag down and back up, a box and its machine followed the card both ways and the view stayed put. Panning went 3,376 units past the old top edge. Click, arrow keys, reload and Reset were rechecked.
- Not verified: the real master and `data.db` end to end, two browsers at once, or a box dragged to a negative position in a browser (its bounds are unit-tested).

**M3 Docker backend, 2026-11-04, local only.** Nothing was deployed and nothing was checked against live infrastructure.

- Slave: parser tests cover name stripping, skipped celld containers, dropped labels and commands, a fake Docker socket, and a missing socket giving `available=false`.
- Master: container nodes, status mapping, published-port matching through IP and loopback, a non-published port staying on the host, container IPs matching on the master host only, and the unavailable warning.
- Not verified: a real Docker engine, an older slave, or the frontend cards.

**M4 Kubernetes backend, 2026-11-04, local only.** Nothing was deployed and nothing was checked against live infrastructure.

- Slave: the parser drops annotations, labels and TLS secret names on a fixture, invalid output is an error, and a non-server host gives `available=false`.
- Master: cluster, service and ingress nodes and edges, skipped system services, ingress, LoadBalancer, NodePort and cluster IP matching through IP and loopback, a stream on a NodePort, a host outside the cluster, Docker winning a shared port, no stray unknown node or warning, and the unavailable warning.
- Not verified: a real k3s cluster or `kubectl` output, an older slave, or the frontend cards.

**M5 VM NICs backend, 2026-11-04, local only.** Nothing was deployed and nothing was checked against live infrastructure.

- Slave: XML parsing of two NICs, `Vm.network` still the first named NIC, and device to MAC mapping are tested.
- Master: primary and secondary NICs on two networks, two NICs on one network with one primary, an older slave with no NICs, per-NIC rates with a counter reset giving no rate, and history samples under the NIC key.
- Not verified: real libvirt counters and XML, the XML failure path of `GetVMInterfaceStats` (needs a hypervisor), the poll cost of one XML read per VM, or the frontend cards.

**M5 change broadcast, 2026-11-04, local only.** Nothing was deployed and nothing was checked against live infrastructure.

- Master: `broadcastNetworkChanged` tests cover one message after a POST, a handler that writes nothing counting as 200, a burst sharing one message, a later write sending again, no message for a GET or failed POST, and `Flush` working through the wrapper.
- Not verified: a real websocket client receiving the message, or the frontend reload.

**M6 and browser check, 2026-11-04, local only.** Nothing was deployed and nothing was checked against live infrastructure.

- Setup: a production web build against a local mock, with a 108-node fixture and a 2000-node fixture (20 hosts, 600 VMs, 300 containers, 400 proxies, 100 streams, 100 services).
- Browser: Docker and Kubernetes boxes, a domain followed through an ingress to its service, the secondary NIC card and VM panel, chevrons by mouse and keyboard with no selection, panel or viewport move, collapse surviving a reload, lines moving to a collapsed box, the Layers menu with Custom, `NetworkChanged` updating a card with the viewport unchanged, an open panel surviving a refresh, and a drag undisturbed by one.
- Large graph, medians with `onlyRenderVisibleElements`: first render 1.7 s, hover 23 ms, traffic tick 33 ms, drag frame about 30 ms. Drag p95 is 41 to 55 ms, over the 33 ms target. Before the change: first render 2.0 s, hover 193 ms, traffic tick 238 ms, drag 443 ms.
- Timings use synthetic events in the built-in browser. After adding `onlyRenderVisibleElements`, only collapse, reload, hover and `NetworkChanged` were rerun.
- Not verified: a real Docker engine, k3s, libvirt NIC data, or a real master websocket with a real client.

## Not included

Manual link overrides, master-to-slave WireGuard links, pods, celld request-flow tracing, packet capture and a mobile graph.
