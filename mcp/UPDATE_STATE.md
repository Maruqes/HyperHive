# Update MCP state

## Refresh procedure

Use this file when asked to refresh MCP compatibility:

> Review the current HyperHive API and registered MCP tools. Recalculate endpoint
> coverage, replace the report below, update the README coverage summary, and
> report the percentage and changes. Follow AGENTS.md and keep everything in English.

1. Inspect `master/api/api.go` and the setup functions it actually registers,
   including `master/api/npmapi/`. List unique HTTP method/path pairs with full
   nested prefixes. Expand route constants and loops; preserve registered aliases.
2. Review MCP registration in `internal/tools`, the shared client, API handlers,
   request/response types and relevant tests. Map tools to endpoints. Review any
   contract changes before retaining implemented status.
3. Mark endpoints implemented, missing, partial, stale, disabled or excluded.
   Only implemented endpoints with enabled tools and passing contract tests count.
   Keep exclusions in the stated denominator; explain scope changes explicitly.
   NEVER implement or recommend a deletion tool. On refresh, classify all HTTP
   DELETE endpoints and other operations with deletion behavior as excluded by
   user policy. Keep them in raw totals with zero credit, exclude them from
   eligible tool queues, and never treat "all tools" or "100%" as an exception.
4. Calculate `implemented / total * 100` for every leaf area and the overall API.
   Sum counts for NGINX and other parent areas. Use N/A for zero endpoints.
5. Run `go test ./...` from `mcp/`; run additional checks only when relevant.
   Record failures and unverified behavior honestly. Do not contact production.
6. Replace the report and endpoint inventory below with the date, source revision
   and content digest, totals, limitations and validation. Update the README's
   date, percentages, counts and available-tool summary in the same change.
7. Return the new overall percentage, area changes, missing capabilities and checks.
   Keep AGENTS.md general. Do not add tools or change runtime behavior during a refresh.

This is an agent workflow and source-reviewed report, not an executable command
or background updater. Reinspect the working tree on each refresh; the date and
Git revision alone do not prove freshness. There is no committed coverage CLI yet.

## Latest report

Checked: **2026-10-18**, from local source. No live deployment check.

- Source revision: `ee9fb23a4d298056e3ec0a354d63b9bf8180155f` plus working-tree changes.
- Source content SHA-256: `747af02f9727d7c3db39548e2178c7a5ae82babb0e69c68049e4df8c466bc81c`.
- Digest input: non-test `master/api/**/*.go`, `master/npm/proxy.go`, all
  `mcp/**/*.go`, `mcp/go.mod` and `mcp/go.sum`; sort repository-relative paths,
  then hash `path + NUL + file bytes + NUL` for each file.
- Inventory: 314 unique explicit HTTP method/path pairs in the master API setup.
  Includes UI/static routes, WebSocket upgrades and aliases as unsupported routes.
  The `/api` middleware prefix is an alias, not a second copy of every endpoint.
- Outside this API scope: the `/mcp` transport multiplexer and the separate pprof
  debug listener. Neither is a HyperHive capability endpoint mapped to a tool.
- Independent task-local AST enumeration matched all 314 report routes (zero differences); this was source inspection, not a committed CLI or runtime route walk.
- Special registrations checked: Celld's start/stop/restart loop, GoAccess's
  `/ws_goaccess` constant and nested `/virsh/xmltemplates` routes.
- Status: 7 implemented, 270 missing, 37 excluded by deletion policy; no partial, stale or disabled endpoints. Exclusions retain zero raw coverage credit. All DELETE routes and four explicit non-DELETE removal/wipe routes are excluded; other missing endpoints require effect/eligibility review before implementation.
- Validation: `go test ./...`, `go test -race ./...` and `go vet ./...` passed from `mcp/`, including each enabled mutation through authenticated HTTP and the real stdio executable with disposable API fixtures. The full affected master API/NPM package suites, focused race checks and vet also passed, including actual HyperHive handlers and disposable NPM fixtures. No production behavior is claimed.

| Scope | Implemented / total | Missing | Coverage |
| --- | ---: | ---: | ---: |
| **HyperHive API** | 7 / 314 | 270 | 2.23% |
| NGINX subtotal | 7 / 37 | 24 | 18.92% |

NGINX is a subset of the overall API. The following leaf areas partition the total. Missing counts omit policy exclusions; the inventory below identifies each exclusion:

| Area | Implemented / total | Missing | Coverage | Route source |
| --- | ---: | ---: | ---: | --- |
| Guest access and SPA | 0 / 12 | 11 | 0.00% | [GUESTS.go](../master/api/GUESTS.go) |
| Core, login and notifications | 0 / 12 | 11 | 0.00% | [api.go](../master/api/api.go) |
| BTRFS | 0 / 22 | 19 | 0.00% | [btrfs.go](../master/api/btrfs.go) |
| Celld | 0 / 17 | 16 | 0.00% | [celld.go](../master/api/celld.go) |
| DNS aliases | 0 / 4 | 3 | 0.00% | [dnsmasq.go](../master/api/dnsmasq.go) |
| Docker | 0 / 26 | 21 | 0.00% | [docker.go](../master/api/docker.go) |
| Host maintenance | 0 / 4 | 4 | 0.00% | [extra.go](../master/api/extra.go) |
| GoAccess | 0 / 3 | 3 | 0.00% | [goaccess.go](../master/api/goaccess.go) |
| Host information | 0 / 15 | 15 | 0.00% | [info.go](../master/api/info.go) |
| ISOs | 0 / 3 | 2 | 0.00% | [isos.go](../master/api/isos.go) |
| Kubernetes | 0 / 3 | 3 | 0.00% | [k8s.go](../master/api/k8s.go) |
| Logs | 0 / 1 | 1 | 0.00% | [logs.go](../master/api/logs.go) |
| NFS | 0 / 5 | 4 | 0.00% | [nfs.go](../master/api/nfs.go) |
| Notes | 0 / 6 | 5 | 0.00% | [notes.go](../master/api/notes.go) |
| Notification history | 0 / 1 | 1 | 0.00% | [nots.go](../master/api/nots.go) |
| noVNC | 0 / 3 | 3 | 0.00% | [novnc.go](../master/api/novnc.go) |
| NGINX / 404 hosts | 0 / 6 | 5 | 0.00% | [404hosts.go](../master/api/npmapi/404hosts.go) |
| NGINX / Access lists | 0 / 4 | 3 | 0.00% | [access_list.go](../master/api/npmapi/access_list.go) |
| NGINX / Certificates | 0 / 7 | 6 | 0.00% | [certs.go](../master/api/npmapi/certs.go) |
| NGINX / Proxies | 7 / 8 | 0 | 87.50% | [proxy.go](../master/api/npmapi/proxy.go) |
| NGINX / Redirects | 0 / 6 | 5 | 0.00% | [redirection.go](../master/api/npmapi/redirection.go) |
| NGINX / Streams | 0 / 6 | 5 | 0.00% | [streams.go](../master/api/npmapi/streams.go) |
| PCI / GPU | 0 / 6 | 6 | 0.00% | [pci.go](../master/api/pci.go) |
| Protocol connections | 0 / 1 | 1 | 0.00% | [protocol.go](../master/api/protocol.go) |
| SMART disks | 0 / 14 | 12 | 0.00% | [smartdisk.go](../master/api/smartdisk.go) |
| Stream observability | 0 / 35 | 33 | 0.00% | [streamInfo.go](../master/api/streamInfo.go) |
| VMs and host tuning | 0 / 71 | 62 | 0.00% | [virsh.go](../master/api/virsh.go) |
| VM XML templates | 0 / 5 | 4 | 0.00% | [virsh_xml_templates.go](../master/api/virsh_xml_templates.go) |
| VM disks | 0 / 4 | 3 | 0.00% | [vm_disk.go](../master/api/vm_disk.go) |
| WireGuard | 0 / 4 | 3 | 0.00% | [wireguard.go](../master/api/wireguard.go) |

### Available capabilities

Both authenticated HTTP and stdio share the same registrations. `list_proxies`
returns `{"proxies":[...]}`; mutations return `{"success":true}` after an empty
successful API response. Complete replacement input is required for create/edit.
See [proxy contracts](docs/areas/nginx-proxies.md) for inputs and limitations.

| Tool | Endpoint |
| --- | --- |
| `list_proxies` | `GET /proxy/list` |
| `create_proxy` | `POST /proxy/create` |
| `update_proxy` | `PUT /proxy/edit` |
| `enable_proxy` | `POST /proxy/enable` |
| `disable_proxy` | `POST /proxy/disable` |
| `setup_frontend_proxy` | `POST /proxy/setupFrontEnd` |
| `setup_secure_frontend_proxy` | `POST /proxy/setupFrontEndSecure` |

Evidence: [registration](internal/tools/proxies.go), [mutations](internal/tools/proxy_mutations.go),
[client](internal/client/proxies.go), [API](../master/api/npmapi/proxy.go),
[HTTP/stdio contracts](proxy_mutations_test.go), and existing list/client tests.
Deletion is excluded by user policy. Raw proxies retain all eight endpoints;
eligible proxies exclude only DELETE /proxy/delete (seven endpoints).

### Endpoint inventory and remaining work

Each row is one method/path pair. Source links point to route registrations and
handlers. `implemented` maps to the tools above; every `missing` row has no mapped
MCP tool. Only permitted operations may receive a named tool, reviewed contract,
implementation and tests. Inventory visibility is not authorization to implement
deletion: deletion tools are forbidden by user policy regardless of row status.
UI/static and streaming routes may need a documented alternative or intentional
exclusion after review; they remain in the denominator until then.

#### Guest access and SPA

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /guest_api/guest_page/{vm_name}` | missing | [GUESTS.go:839](../master/api/GUESTS.go#L839) |
| `POST /guest_api/guest_vm` | missing | [GUESTS.go:840](../master/api/GUESTS.go#L840) |
| `GET /guest_api/novnc/ws` | missing | [GUESTS.go:846](../master/api/GUESTS.go#L846) |
| `GET /guest_api/novnc/sprite/{vm_name}` | missing | [GUESTS.go:847](../master/api/GUESTS.go#L847) |
| `GET /guest_api/novnc/*` | missing | [GUESTS.go:848](../master/api/GUESTS.go#L848) |
| `POST /guest_api/spa/allow-ip` | missing | [GUESTS.go:852](../master/api/GUESTS.go#L852) |
| `POST /guest_api/spa/allow` | missing | [GUESTS.go:853](../master/api/GUESTS.go#L853) |
| `GET /guest_api/spa/pageallow/{port}` | missing | [GUESTS.go:854](../master/api/GUESTS.go#L854) |
| `POST /spa/` | missing | [GUESTS.go:861](../master/api/GUESTS.go#L861) |
| `GET /spa/` | missing | [GUESTS.go:862](../master/api/GUESTS.go#L862) |
| `GET /spa/allow/{port}` | missing | [GUESTS.go:863](../master/api/GUESTS.go#L863) |
| `DELETE /spa/{port}` | excluded (deletion policy) | [GUESTS.go:864](../master/api/GUESTS.go#L864) |

#### Core, login and notifications

| Endpoint | Status | Route source |
| --- | --- | --- |
| `POST /login` | missing | [api.go:278](../master/api/api.go#L278) |
| `GET /nots/register` | missing | [api.go:283](../master/api/api.go#L283) |
| `GET /static/notification-icon.png` | missing | [api.go:284](../master/api/api.go#L284) |
| `GET /icon.png` | missing | [api.go:288](../master/api/api.go#L288) |
| `GET /new-slave-count` | missing | [api.go:300](../master/api/api.go#L300) |
| `GET /notification/public-key` | missing | [api.go:304](../master/api/api.go#L304) |
| `POST /notification/subscribe` | missing | [api.go:305](../master/api/api.go#L305) |
| `GET /notification/test` | missing | [api.go:306](../master/api/api.go#L306) |
| `DELETE /notification/subscriptions` | excluded (deletion policy) | [api.go:307](../master/api/api.go#L307) |
| `GET /sw.js` | missing | [api.go:309](../master/api/api.go#L309) |
| `GET /protected` | missing | [api.go:313](../master/api/api.go#L313) |
| `GET /ws` | missing | [api.go:315](../master/api/api.go#L315) |

#### BTRFS

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /btrfs/getFreeDisks/{machine_name}` | missing | [btrfs.go:654](../master/api/btrfs.go#L654) |
| `GET /btrfs/getAllDuckingDisks/{machine_name}` | missing | [btrfs.go:655](../master/api/btrfs.go#L655) |
| `GET /btrfs/getraids/{machine_name}` | missing | [btrfs.go:656](../master/api/btrfs.go#L656) |
| `POST /btrfs/createraid/{machine_name}` | missing | [btrfs.go:657](../master/api/btrfs.go#L657) |
| `DELETE /btrfs/removeraid/{machine_name}` | excluded (deletion policy) | [btrfs.go:658](../master/api/btrfs.go#L658) |
| `POST /btrfs/mount_raid/{machine_name}` | missing | [btrfs.go:660](../master/api/btrfs.go#L660) |
| `POST /btrfs/umount_raid/{machine_name}` | missing | [btrfs.go:661](../master/api/btrfs.go#L661) |
| `GET /btrfs/automatic_mount/{machine_name}` | missing | [btrfs.go:664](../master/api/btrfs.go#L664) |
| `POST /btrfs/automatic_mount` | missing | [btrfs.go:665](../master/api/btrfs.go#L665) |
| `DELETE /btrfs/automatic_mount` | excluded (deletion policy) | [btrfs.go:666](../master/api/btrfs.go#L666) |
| `POST /btrfs/add_diskraid/{machine_name}` | missing | [btrfs.go:668](../master/api/btrfs.go#L668) |
| `POST /btrfs/remove_diskraid/{machine_name}` | excluded (deletion policy) | [btrfs.go:669](../master/api/btrfs.go#L669) |
| `POST /btrfs/replace_diskraid/{machine_name}` | missing | [btrfs.go:670](../master/api/btrfs.go#L670) |
| `POST /btrfs/change_raid_level/{machine_name}` | missing | [btrfs.go:672](../master/api/btrfs.go#L672) |
| `POST /btrfs/balance_raid/{machine_name}` | missing | [btrfs.go:673](../master/api/btrfs.go#L673) |
| `POST /btrfs/pause_balance/{machine_name}` | missing | [btrfs.go:674](../master/api/btrfs.go#L674) |
| `POST /btrfs/resume_balance/{machine_name}` | missing | [btrfs.go:675](../master/api/btrfs.go#L675) |
| `POST /btrfs/cancel_balance/{machine_name}` | missing | [btrfs.go:676](../master/api/btrfs.go#L676) |
| `POST /btrfs/defragment_raid/{machine_name}` | missing | [btrfs.go:677](../master/api/btrfs.go#L677) |
| `POST /btrfs/scrub_raid/{machine_name}` | missing | [btrfs.go:678](../master/api/btrfs.go#L678) |
| `GET /btrfs/scrub_stats/{machine_name}` | missing | [btrfs.go:679](../master/api/btrfs.go#L679) |
| `GET /btrfs/raid_status/{machine_name}` | missing | [btrfs.go:682](../master/api/btrfs.go#L682) |

#### Celld

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /celld/health` | missing | [celld.go:14](../master/api/celld.go#L14) |
| `GET /celld/fleets` | missing | [celld.go:15](../master/api/celld.go#L15) |
| `POST /celld/fleets` | missing | [celld.go:16](../master/api/celld.go#L16) |
| `GET /celld/fleets/{id}` | missing | [celld.go:17](../master/api/celld.go#L17) |
| `PATCH /celld/fleets/{id}` | missing | [celld.go:18](../master/api/celld.go#L18) |
| `DELETE /celld/fleets/{id}` | excluded (deletion policy) | [celld.go:19](../master/api/celld.go#L19) |
| `POST /celld/fleets/{id}/start` | missing | [celld.go:21](../master/api/celld.go#L21) |
| `POST /celld/fleets/{id}/stop` | missing | [celld.go:21](../master/api/celld.go#L21) |
| `POST /celld/fleets/{id}/restart` | missing | [celld.go:21](../master/api/celld.go#L21) |
| `GET /celld/fleets/{id}/credentials` | missing | [celld.go:23](../master/api/celld.go#L23) |
| `GET /celld/fleets/{id}/observability` | missing | [celld.go:24](../master/api/celld.go#L24) |
| `GET /celld/fleets/{id}/logs/stream` | missing | [celld.go:25](../master/api/celld.go#L25) |
| `GET /celld/fleets/{id}/events/stream` | missing | [celld.go:26](../master/api/celld.go#L26) |
| `GET /celld/fleets/{id}/logs` | missing | [celld.go:27](../master/api/celld.go#L27) |
| `GET /celld/fleets/{id}/events` | missing | [celld.go:28](../master/api/celld.go#L28) |
| `GET /celld/fleets/{id}/traces/{trace_id}` | missing | [celld.go:29](../master/api/celld.go#L29) |
| `GET /celld/fleets/{id}/metrics` | missing | [celld.go:30](../master/api/celld.go#L30) |

#### DNS aliases

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /dnsmasq/alias/all` | missing | [dnsmasq.go:96](../master/api/dnsmasq.go#L96) |
| `GET /dnsmasq/alias/get` | missing | [dnsmasq.go:97](../master/api/dnsmasq.go#L97) |
| `POST /dnsmasq/alias/add` | missing | [dnsmasq.go:98](../master/api/dnsmasq.go#L98) |
| `DELETE /dnsmasq/alias/remove` | excluded (deletion policy) | [dnsmasq.go:99](../master/api/dnsmasq.go#L99) |

#### Docker

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /docker/images/{machineName}` | missing | [docker.go:716](../master/api/docker.go#L716) |
| `POST /docker/images/download/{machineName}` | missing | [docker.go:717](../master/api/docker.go#L717) |
| `DELETE /docker/images/remove/{machineName}` | excluded (deletion policy) | [docker.go:718](../master/api/docker.go#L718) |
| `GET /docker/containers/{machineName}` | missing | [docker.go:722](../master/api/docker.go#L722) |
| `POST /docker/containers/create/{machineName}` | missing | [docker.go:723](../master/api/docker.go#L723) |
| `DELETE /docker/containers/remove/{machineName}` | excluded (deletion policy) | [docker.go:724](../master/api/docker.go#L724) |
| `POST /docker/containers/stop/{machineName}` | missing | [docker.go:725](../master/api/docker.go#L725) |
| `POST /docker/containers/start/{machineName}` | missing | [docker.go:726](../master/api/docker.go#L726) |
| `POST /docker/containers/restart/{machineName}` | missing | [docker.go:727](../master/api/docker.go#L727) |
| `POST /docker/containers/pause/{machineName}` | missing | [docker.go:728](../master/api/docker.go#L728) |
| `POST /docker/containers/unpause/{machineName}` | missing | [docker.go:729](../master/api/docker.go#L729) |
| `POST /docker/containers/kill/{machineName}` | missing | [docker.go:730](../master/api/docker.go#L730) |
| `POST /docker/containers/logs/{machineName}` | missing | [docker.go:731](../master/api/docker.go#L731) |
| `POST /docker/containers/update/{machineName}` | missing | [docker.go:732](../master/api/docker.go#L732) |
| `POST /docker/containers/rename/{machineName}` | missing | [docker.go:733](../master/api/docker.go#L733) |
| `POST /docker/containers/exec/{machineName}` | missing | [docker.go:734](../master/api/docker.go#L734) |
| `GET /docker/volumes/{machineName}` | missing | [docker.go:738](../master/api/docker.go#L738) |
| `POST /docker/volumes/create/{machineName}` | missing | [docker.go:739](../master/api/docker.go#L739) |
| `DELETE /docker/volumes/remove/{machineName}` | excluded (deletion policy) | [docker.go:740](../master/api/docker.go#L740) |
| `GET /docker/networks/{machineName}` | missing | [docker.go:744](../master/api/docker.go#L744) |
| `POST /docker/networks/create/{machineName}` | missing | [docker.go:745](../master/api/docker.go#L745) |
| `DELETE /docker/networks/remove/{machineName}` | excluded (deletion policy) | [docker.go:746](../master/api/docker.go#L746) |
| `GET /docker/git/{machineName}` | missing | [docker.go:750](../master/api/docker.go#L750) |
| `POST /docker/git/clone/{machineName}` | missing | [docker.go:751](../master/api/docker.go#L751) |
| `DELETE /docker/git/remove/{machineName}` | excluded (deletion policy) | [docker.go:752](../master/api/docker.go#L752) |
| `POST /docker/git/update/{machineName}` | missing | [docker.go:753](../master/api/docker.go#L753) |

#### Host maintenance

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /extra/getUpdates/{machineName}` | missing | [extra.go:81](../master/api/extra.go#L81) |
| `POST /extra/performUpdate/{machineName}` | missing | [extra.go:82](../master/api/extra.go#L82) |
| `POST /extra/shutdown/{machineName}` | missing | [extra.go:83](../master/api/extra.go#L83) |
| `POST /extra/restart/{machineName}` | missing | [extra.go:84](../master/api/extra.go#L84) |

#### GoAccess

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /goaccess` | missing | [goaccess.go:623](../master/api/goaccess.go#L623) |
| `GET /ws_goaccess` | missing | [goaccess.go:624](../master/api/goaccess.go#L624) |
| `GET /goaccess/status` | missing | [goaccess.go:625](../master/api/goaccess.go#L625) |

#### Host information

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /info/cpu/{machine_name}` | missing | [info.go:773](../master/api/info.go#L773) |
| `GET /info/mem/{machine_name}` | missing | [info.go:774](../master/api/info.go#L774) |
| `GET /info/disk/{machine_name}` | missing | [info.go:775](../master/api/info.go#L775) |
| `GET /info/network/{machine_name}` | missing | [info.go:776](../master/api/info.go#L776) |
| `GET /info/processes/{machine_name}` | missing | [info.go:777](../master/api/info.go#L777) |
| `GET /info/processes/{machine_name}/{pid}` | missing | [info.go:778](../master/api/info.go#L778) |
| `POST /info/processes/{machine_name}/{pid}/kill` | missing | [info.go:779](../master/api/info.go#L779) |
| `POST /info/processes/{machine_name}/{pid}/terminate` | missing | [info.go:780](../master/api/info.go#L780) |
| `GET /info/history/cpu/{machine_name}` | missing | [info.go:781](../master/api/info.go#L781) |
| `GET /info/history/mem/{machine_name}` | missing | [info.go:782](../master/api/info.go#L782) |
| `GET /info/history/disk/{machine_name}` | missing | [info.go:783](../master/api/info.go#L783) |
| `GET /info/history/network/{machine_name}` | missing | [info.go:784](../master/api/info.go#L784) |
| `POST /info/stress-cpu/{machine_name}` | missing | [info.go:785](../master/api/info.go#L785) |
| `POST /info/test-ram/{machine_name}` | missing | [info.go:786](../master/api/info.go#L786) |
| `GET /info/time-since/{machine_name}` | missing | [info.go:788](../master/api/info.go#L788) |

#### ISOs

| Endpoint | Status | Route source |
| --- | --- | --- |
| `POST /isos/download` | missing | [isos.go:144](../master/api/isos.go#L144) |
| `GET /isos/` | missing | [isos.go:145](../master/api/isos.go#L145) |
| `DELETE /isos/{id}` | excluded (deletion policy) | [isos.go:146](../master/api/isos.go#L146) |

#### Kubernetes

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /k8s/tls-sans` | missing | [k8s.go:67](../master/api/k8s.go#L67) |
| `GET /k8s/cluster/status` | missing | [k8s.go:68](../master/api/k8s.go#L68) |
| `POST /k8s/connection-file` | missing | [k8s.go:69](../master/api/k8s.go#L69) |

#### Logs

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /logs/list` | missing | [logs.go:53](../master/api/logs.go#L53) |

#### NFS

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /nfs/list` | missing | [nfs.go:166](../master/api/nfs.go#L166) |
| `POST /nfs/create` | missing | [nfs.go:167](../master/api/nfs.go#L167) |
| `DELETE /nfs/delete` | excluded (deletion policy) | [nfs.go:168](../master/api/nfs.go#L168) |
| `POST /nfs/remount/{id}` | missing | [nfs.go:169](../master/api/nfs.go#L169) |
| `POST /nfs/contents/{machine}` | missing | [nfs.go:170](../master/api/nfs.go#L170) |

#### Notes

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /notes/` | missing | [notes.go:140](../master/api/notes.go#L140) |
| `GET /notes/{id}` | missing | [notes.go:141](../master/api/notes.go#L141) |
| `POST /notes/` | missing | [notes.go:142](../master/api/notes.go#L142) |
| `PUT /notes/{id}` | missing | [notes.go:143](../master/api/notes.go#L143) |
| `PATCH /notes/{id}` | missing | [notes.go:144](../master/api/notes.go#L144) |
| `DELETE /notes/{id}` | excluded (deletion policy) | [notes.go:145](../master/api/notes.go#L145) |

#### Notification history

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /nots/` | missing | [nots.go:45](../master/api/nots.go#L45) |

#### noVNC

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /novnc/ws` | missing | [novnc.go:225](../master/api/novnc.go#L225) |
| `GET /novnc/sprite/{vm_name}` | missing | [novnc.go:226](../master/api/novnc.go#L226) |
| `GET /novnc/*` | missing | [novnc.go:227](../master/api/novnc.go#L227) |

#### NGINX / 404 hosts

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /404/list` | missing | [404hosts.go:126](../master/api/npmapi/404hosts.go#L126) |
| `POST /404/create` | missing | [404hosts.go:127](../master/api/npmapi/404hosts.go#L127) |
| `PUT /404/edit` | missing | [404hosts.go:128](../master/api/npmapi/404hosts.go#L128) |
| `DELETE /404/delete` | excluded (deletion policy) | [404hosts.go:129](../master/api/npmapi/404hosts.go#L129) |
| `POST /404/disable` | missing | [404hosts.go:130](../master/api/npmapi/404hosts.go#L130) |
| `POST /404/enable` | missing | [404hosts.go:131](../master/api/npmapi/404hosts.go#L131) |

#### NGINX / Access lists

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /access-lists/list` | missing | [access_list.go:82](../master/api/npmapi/access_list.go#L82) |
| `POST /access-lists/create` | missing | [access_list.go:83](../master/api/npmapi/access_list.go#L83) |
| `PUT /access-lists/edit` | missing | [access_list.go:84](../master/api/npmapi/access_list.go#L84) |
| `DELETE /access-lists/delete` | excluded (deletion policy) | [access_list.go:85](../master/api/npmapi/access_list.go#L85) |

#### NGINX / Certificates

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /certs/dns-providers` | missing | [certs.go:265](../master/api/npmapi/certs.go#L265) |
| `GET /certs/list` | missing | [certs.go:266](../master/api/npmapi/certs.go#L266) |
| `POST /certs/create` | missing | [certs.go:267](../master/api/npmapi/certs.go#L267) |
| `POST /certs/create-lets-encrypt` | missing | [certs.go:268](../master/api/npmapi/certs.go#L268) |
| `POST /certs/download` | missing | [certs.go:269](../master/api/npmapi/certs.go#L269) |
| `POST /certs/renew` | missing | [certs.go:270](../master/api/npmapi/certs.go#L270) |
| `DELETE /certs/delete` | excluded (deletion policy) | [certs.go:271](../master/api/npmapi/certs.go#L271) |

#### NGINX / Proxies

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /proxy/list` | implemented | [proxy.go:364](../master/api/npmapi/proxy.go#L364) |
| `POST /proxy/create` | implemented | [proxy.go:365](../master/api/npmapi/proxy.go#L365) |
| `PUT /proxy/edit` | implemented | [proxy.go:366](../master/api/npmapi/proxy.go#L366) |
| `DELETE /proxy/delete` | excluded (deletion policy) | [proxy.go:367](../master/api/npmapi/proxy.go#L367) |
| `POST /proxy/disable` | implemented | [proxy.go:368](../master/api/npmapi/proxy.go#L368) |
| `POST /proxy/enable` | implemented | [proxy.go:369](../master/api/npmapi/proxy.go#L369) |
| `POST /proxy/setupFrontEnd` | implemented | [proxy.go:371](../master/api/npmapi/proxy.go#L371) |
| `POST /proxy/setupFrontEndSecure` | implemented | [proxy.go:372](../master/api/npmapi/proxy.go#L372) |

#### NGINX / Redirects

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /redirection/list` | missing | [redirection.go:126](../master/api/npmapi/redirection.go#L126) |
| `POST /redirection/create` | missing | [redirection.go:127](../master/api/npmapi/redirection.go#L127) |
| `PUT /redirection/edit` | missing | [redirection.go:128](../master/api/npmapi/redirection.go#L128) |
| `DELETE /redirection/delete` | excluded (deletion policy) | [redirection.go:129](../master/api/npmapi/redirection.go#L129) |
| `POST /redirection/disable` | missing | [redirection.go:130](../master/api/npmapi/redirection.go#L130) |
| `POST /redirection/enable` | missing | [redirection.go:131](../master/api/npmapi/redirection.go#L131) |

#### NGINX / Streams

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /stream/list` | missing | [streams.go:153](../master/api/npmapi/streams.go#L153) |
| `POST /stream/create` | missing | [streams.go:154](../master/api/npmapi/streams.go#L154) |
| `PUT /stream/edit` | missing | [streams.go:155](../master/api/npmapi/streams.go#L155) |
| `DELETE /stream/delete` | excluded (deletion policy) | [streams.go:156](../master/api/npmapi/streams.go#L156) |
| `POST /stream/disable` | missing | [streams.go:157](../master/api/npmapi/streams.go#L157) |
| `POST /stream/enable` | missing | [streams.go:158](../master/api/npmapi/streams.go#L158) |

#### PCI / GPU

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /pci/host/{machine_name}` | missing | [pci.go:177](../master/api/pci.go#L177) |
| `GET /pci/host/iommu/{machine_name}` | missing | [pci.go:178](../master/api/pci.go#L178) |
| `GET /pci/vm/{machine_name}/{vm_name}` | missing | [pci.go:179](../master/api/pci.go#L179) |
| `POST /pci/attach/{machine_name}` | missing | [pci.go:180](../master/api/pci.go#L180) |
| `POST /pci/detach/{machine_name}` | missing | [pci.go:181](../master/api/pci.go#L181) |
| `POST /pci/return/{machine_name}` | missing | [pci.go:182](../master/api/pci.go#L182) |

#### Protocol connections

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /protocol/list` | missing | [protocol.go:22](../master/api/protocol.go#L22) |

#### SMART disks

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /smartdisk/{machine_name}` | missing | [smartdisk.go:455](../master/api/smartdisk.go#L455) |
| `POST /smartdisk/{machine_name}/self-test` | missing | [smartdisk.go:456](../master/api/smartdisk.go#L456) |
| `GET /smartdisk/{machine_name}/self-test/progress` | missing | [smartdisk.go:457](../master/api/smartdisk.go#L457) |
| `POST /smartdisk/{machine_name}/self-test/cancel` | missing | [smartdisk.go:458](../master/api/smartdisk.go#L458) |
| `POST /smartdisk/{machine_name}/schedules/` | missing | [smartdisk.go:461](../master/api/smartdisk.go#L461) |
| `GET /smartdisk/{machine_name}/schedules/` | missing | [smartdisk.go:462](../master/api/smartdisk.go#L462) |
| `PUT /smartdisk/{machine_name}/schedules/{id}/` | missing | [smartdisk.go:464](../master/api/smartdisk.go#L464) |
| `DELETE /smartdisk/{machine_name}/schedules/{id}/` | excluded (deletion policy) | [smartdisk.go:465](../master/api/smartdisk.go#L465) |
| `PUT /smartdisk/{machine_name}/schedules/{id}/enable` | missing | [smartdisk.go:466](../master/api/smartdisk.go#L466) |
| `POST /smartdisk/{machine_name}/realloc/full-wipe` | excluded (deletion policy) | [smartdisk.go:470](../master/api/smartdisk.go#L470) |
| `POST /smartdisk/{machine_name}/realloc/non-destructive` | missing | [smartdisk.go:471](../master/api/smartdisk.go#L471) |
| `GET /smartdisk/{machine_name}/realloc/status` | missing | [smartdisk.go:472](../master/api/smartdisk.go#L472) |
| `GET /smartdisk/{machine_name}/realloc/` | missing | [smartdisk.go:473](../master/api/smartdisk.go#L473) |
| `POST /smartdisk/{machine_name}/realloc/cancel` | missing | [smartdisk.go:474](../master/api/smartdisk.go#L474) |

#### Stream observability

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /streamInfo/` | missing | [streamInfo.go:2204](../master/api/streamInfo.go#L2204) |
| `DELETE /streamInfo/logs` | excluded (deletion policy) | [streamInfo.go:2208](../master/api/streamInfo.go#L2208) |
| `GET /streamInfo/ip-aliases` | missing | [streamInfo.go:2209](../master/api/streamInfo.go#L2209) |
| `POST /streamInfo/ip-aliases` | missing | [streamInfo.go:2210](../master/api/streamInfo.go#L2210) |
| `DELETE /streamInfo/ip-aliases` | excluded (deletion policy) | [streamInfo.go:2211](../master/api/streamInfo.go#L2211) |
| `GET /streamInfo/streamInfo.css` | missing | [streamInfo.go:2218](../master/api/streamInfo.go#L2218) |
| `GET /streamInfo/streamInfo-core.js` | missing | [streamInfo.go:2219](../master/api/streamInfo.go#L2219) |
| `GET /streamInfo/streamInfo-overview.js` | missing | [streamInfo.go:2220](../master/api/streamInfo.go#L2220) |
| `GET /streamInfo/streamInfo-tabs.js` | missing | [streamInfo.go:2221](../master/api/streamInfo.go#L2221) |
| `GET /streamInfo/streamInfo-profiles.js` | missing | [streamInfo.go:2222](../master/api/streamInfo.go#L2222) |
| `GET /streamInfo/streamInfo-interactions.js` | missing | [streamInfo.go:2223](../master/api/streamInfo.go#L2223) |
| `GET /streamInfo/data` | missing | [streamInfo.go:2226](../master/api/streamInfo.go#L2226) |
| `GET /streamInfo/analytics` | missing | [streamInfo.go:2229](../master/api/streamInfo.go#L2229) |
| `GET /streamInfo/intel` | missing | [streamInfo.go:2230](../master/api/streamInfo.go#L2230) |
| `GET /streamInfo/active-connections` | missing | [streamInfo.go:2231](../master/api/streamInfo.go#L2231) |
| `GET /streamInfo/summary` | missing | [streamInfo.go:2232](../master/api/streamInfo.go#L2232) |
| `GET /streamInfo/traffic/by-ip` | missing | [streamInfo.go:2235](../master/api/streamInfo.go#L2235) |
| `GET /streamInfo/traffic/by-country` | missing | [streamInfo.go:2236](../master/api/streamInfo.go#L2236) |
| `GET /streamInfo/traffic/by-port` | missing | [streamInfo.go:2237](../master/api/streamInfo.go#L2237) |
| `GET /streamInfo/traffic/by-protocol` | missing | [streamInfo.go:2238](../master/api/streamInfo.go#L2238) |
| `GET /streamInfo/traffic/by-ip-port` | missing | [streamInfo.go:2239](../master/api/streamInfo.go#L2239) |
| `GET /streamInfo/traffic/by-country-port` | missing | [streamInfo.go:2240](../master/api/streamInfo.go#L2240) |
| `GET /streamInfo/country/{country}` | missing | [streamInfo.go:2241](../master/api/streamInfo.go#L2241) |
| `GET /streamInfo/server/{upstream}/activity` | missing | [streamInfo.go:2242](../master/api/streamInfo.go#L2242) |
| `GET /streamInfo/stats/daily` | missing | [streamInfo.go:2245](../master/api/streamInfo.go#L2245) |
| `GET /streamInfo/stats/hourly` | missing | [streamInfo.go:2246](../master/api/streamInfo.go#L2246) |
| `GET /streamInfo/timeline` | missing | [streamInfo.go:2247](../master/api/streamInfo.go#L2247) |
| `GET /streamInfo/top/talkers` | missing | [streamInfo.go:2250](../master/api/streamInfo.go#L2250) |
| `GET /streamInfo/top/countries` | missing | [streamInfo.go:2251](../master/api/streamInfo.go#L2251) |
| `GET /streamInfo/top/ports` | missing | [streamInfo.go:2252](../master/api/streamInfo.go#L2252) |
| `GET /streamInfo/upstream` | missing | [streamInfo.go:2255](../master/api/streamInfo.go#L2255) |
| `GET /streamInfo/failed` | missing | [streamInfo.go:2256](../master/api/streamInfo.go#L2256) |
| `GET /streamInfo/flow` | missing | [streamInfo.go:2259](../master/api/streamInfo.go#L2259) |
| `GET /streamInfo/daterange` | missing | [streamInfo.go:2260](../master/api/streamInfo.go#L2260) |
| `GET /streamInfo/anomalies` | missing | [streamInfo.go:2261](../master/api/streamInfo.go#L2261) |

#### VMs and host tuning

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /virsh/getcpudisablefeatures` | missing | [virsh.go:2507](../master/api/virsh.go#L2507) |
| `GET /virsh/getallvms` | missing | [virsh.go:2508](../master/api/virsh.go#L2508) |
| `POST /virsh/createvm` | missing | [virsh.go:2509](../master/api/virsh.go#L2509) |
| `POST /virsh/migratevm/{vm_name}` | missing | [virsh.go:2511](../master/api/virsh.go#L2511) |
| `POST /virsh/updatecpuxml/{vm_name}` | missing | [virsh.go:2512](../master/api/virsh.go#L2512) |
| `GET /virsh/cpuxml/{vm_name}` | missing | [virsh.go:2513](../master/api/virsh.go#L2513) |
| `POST /virsh/updatevmxml/{vm_name}` | missing | [virsh.go:2514](../master/api/virsh.go#L2514) |
| `GET /virsh/vmxml/{vm_name}` | missing | [virsh.go:2515](../master/api/virsh.go#L2515) |
| `DELETE /virsh/deletevm/{vm_name}` | excluded (deletion policy) | [virsh.go:2518](../master/api/virsh.go#L2518) |
| `POST /virsh/startvm/{vm_name}` | missing | [virsh.go:2519](../master/api/virsh.go#L2519) |
| `POST /virsh/shutdownvm/{vm_name}` | missing | [virsh.go:2520](../master/api/virsh.go#L2520) |
| `POST /virsh/forceshutdownvm/{vm_name}` | missing | [virsh.go:2521](../master/api/virsh.go#L2521) |
| `POST /virsh/restartvm/{vm_name}` | missing | [virsh.go:2522](../master/api/virsh.go#L2522) |
| `POST /virsh/editvm/{vm_name}` | missing | [virsh.go:2523](../master/api/virsh.go#L2523) |
| `POST /virsh/pausevm/{vm_name}` | missing | [virsh.go:2524](../master/api/virsh.go#L2524) |
| `POST /virsh/resumevm/{vm_name}` | missing | [virsh.go:2525](../master/api/virsh.go#L2525) |
| `GET /virsh/getvmbyname/{vm_name}` | missing | [virsh.go:2526](../master/api/virsh.go#L2526) |
| `POST /virsh/removeiso/{vm_name}` | excluded (deletion policy) | [virsh.go:2527](../master/api/virsh.go#L2527) |
| `POST /virsh/vm_disk/{vm_name}` | missing | [virsh.go:2528](../master/api/virsh.go#L2528) |
| `GET /virsh/vm_disk/{vm_name}` | missing | [virsh.go:2529](../master/api/virsh.go#L2529) |
| `DELETE /virsh/vm_disk/{vm_name}/{vm_disk_id}` | excluded (deletion policy) | [virsh.go:2530](../master/api/virsh.go#L2530) |
| `POST /virsh/change_vm_network/{vm_name}` | missing | [virsh.go:2532](../master/api/virsh.go#L2532) |
| `POST /virsh/change_vnc_password/{vm_name}` | missing | [virsh.go:2533](../master/api/virsh.go#L2533) |
| `POST /virsh/add_ssh_key/{vm_name}` | missing | [virsh.go:2534](../master/api/virsh.go#L2534) |
| `POST /virsh/novncvideo/add/{vm_name}` | missing | [virsh.go:2535](../master/api/virsh.go#L2535) |
| `POST /virsh/novncvideo/remove/{vm_name}` | excluded (deletion policy) | [virsh.go:2536](../master/api/virsh.go#L2536) |
| `GET /virsh/novncvideo/{vm_name}` | missing | [virsh.go:2537](../master/api/virsh.go#L2537) |
| `GET /virsh/ballooning/{vm_name}` | missing | [virsh.go:2538](../master/api/virsh.go#L2538) |
| `POST /virsh/ballooning/{vm_name}` | missing | [virsh.go:2539](../master/api/virsh.go#L2539) |
| `GET /virsh/hugepages/{vm_name}` | missing | [virsh.go:2540](../master/api/virsh.go#L2540) |
| `POST /virsh/hugepages/{vm_name}` | missing | [virsh.go:2541](../master/api/virsh.go#L2541) |
| `GET /virsh/machine_types/{machine_name}` | missing | [virsh.go:2542](../master/api/virsh.go#L2542) |
| `POST /virsh/machine_type/{vm_name}` | missing | [virsh.go:2543](../master/api/virsh.go#L2543) |
| `GET /virsh/kvm/{vm_name}` | missing | [virsh.go:2544](../master/api/virsh.go#L2544) |
| `POST /virsh/kvm/{vm_name}` | missing | [virsh.go:2545](../master/api/virsh.go#L2545) |
| `GET /virsh/hyperv/{vm_name}` | missing | [virsh.go:2546](../master/api/virsh.go#L2546) |
| `POST /virsh/hyperv/{vm_name}` | missing | [virsh.go:2547](../master/api/virsh.go#L2547) |
| `POST /virsh/cpupinning/{vm_name}` | missing | [virsh.go:2550](../master/api/virsh.go#L2550) |
| `DELETE /virsh/cpupinning/{vm_name}` | excluded (deletion policy) | [virsh.go:2551](../master/api/virsh.go#L2551) |
| `GET /virsh/cpupinning/{vm_name}` | missing | [virsh.go:2552](../master/api/virsh.go#L2552) |
| `GET /virsh/cputopology/{machine_name}` | missing | [virsh.go:2553](../master/api/virsh.go#L2553) |
| `GET /virsh/tunedadm/{machine_name}` | missing | [virsh.go:2554](../master/api/virsh.go#L2554) |
| `POST /virsh/tunedadm/{machine_name}` | missing | [virsh.go:2555](../master/api/virsh.go#L2555) |
| `GET /virsh/irqbalance/{machine_name}` | missing | [virsh.go:2556](../master/api/virsh.go#L2556) |
| `POST /virsh/irqbalance/{machine_name}` | missing | [virsh.go:2557](../master/api/virsh.go#L2557) |
| `GET /virsh/coreisolation/{machine_name}` | missing | [virsh.go:2558](../master/api/virsh.go#L2558) |
| `POST /virsh/coreisolation/{machine_name}` | missing | [virsh.go:2559](../master/api/virsh.go#L2559) |
| `PUT /virsh/coreisolation/{machine_name}` | missing | [virsh.go:2560](../master/api/virsh.go#L2560) |
| `DELETE /virsh/coreisolation/{machine_name}` | excluded (deletion policy) | [virsh.go:2561](../master/api/virsh.go#L2561) |
| `GET /virsh/hosthugepages/{machine_name}` | missing | [virsh.go:2562](../master/api/virsh.go#L2562) |
| `POST /virsh/hosthugepages/{machine_name}` | missing | [virsh.go:2563](../master/api/virsh.go#L2563) |
| `PUT /virsh/hosthugepages/{machine_name}` | missing | [virsh.go:2564](../master/api/virsh.go#L2564) |
| `DELETE /virsh/hosthugepages/{machine_name}` | excluded (deletion policy) | [virsh.go:2565](../master/api/virsh.go#L2565) |
| `POST /virsh/moveDisk/{vm_name}/{dest_nfs}` | missing | [virsh.go:2568](../master/api/virsh.go#L2568) |
| `POST /virsh/coldMigrate/{vm_name}/{dest_machine_name}` | missing | [virsh.go:2569](../master/api/virsh.go#L2569) |
| `POST /virsh/cloneVM/{vm_name}/{dest_nfs}/{dest_machine_name}` | missing | [virsh.go:2570](../master/api/virsh.go#L2570) |
| `GET /virsh/export/{vm_name}` | missing | [virsh.go:2573](../master/api/virsh.go#L2573) |
| `PUT /virsh/import` | missing | [virsh.go:2574](../master/api/virsh.go#L2574) |
| `POST /virsh/backup/{vm_name}/{nfs_id}` | missing | [virsh.go:2577](../master/api/virsh.go#L2577) |
| `GET /virsh/backups` | missing | [virsh.go:2578](../master/api/virsh.go#L2578) |
| `GET /virsh/downloadbackup/{backup_id}` | missing | [virsh.go:2579](../master/api/virsh.go#L2579) |
| `POST /virsh/useBackup/{backup_id}` | missing | [virsh.go:2580](../master/api/virsh.go#L2580) |
| `DELETE /virsh/deleteBackup/{backup_id}` | excluded (deletion policy) | [virsh.go:2581](../master/api/virsh.go#L2581) |
| `POST /virsh/autostart/{vm_name}` | missing | [virsh.go:2583](../master/api/virsh.go#L2583) |
| `POST /virsh/vmlive/{vm_name}` | missing | [virsh.go:2584](../master/api/virsh.go#L2584) |
| `POST /virsh/autobak` | missing | [virsh.go:2587](../master/api/virsh.go#L2587) |
| `GET /virsh/autobak` | missing | [virsh.go:2588](../master/api/virsh.go#L2588) |
| `PUT /virsh/autobak/{id}` | missing | [virsh.go:2589](../master/api/virsh.go#L2589) |
| `DELETE /virsh/autobak/{id}` | excluded (deletion policy) | [virsh.go:2590](../master/api/virsh.go#L2590) |
| `PATCH /virsh/autopak/enable/{id}` | missing | [virsh.go:2591](../master/api/virsh.go#L2591) |
| `PATCH /virsh/autopak/disable/{id}` | missing | [virsh.go:2592](../master/api/virsh.go#L2592) |

#### VM XML templates

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /virsh/xmltemplates/` | missing | [virsh_xml_templates.go:22](../master/api/virsh_xml_templates.go#L22) |
| `GET /virsh/xmltemplates/{id}` | missing | [virsh_xml_templates.go:23](../master/api/virsh_xml_templates.go#L23) |
| `POST /virsh/xmltemplates/` | missing | [virsh_xml_templates.go:24](../master/api/virsh_xml_templates.go#L24) |
| `PUT /virsh/xmltemplates/{id}` | missing | [virsh_xml_templates.go:25](../master/api/virsh_xml_templates.go#L25) |
| `DELETE /virsh/xmltemplates/{id}` | excluded (deletion policy) | [virsh_xml_templates.go:26](../master/api/virsh_xml_templates.go#L26) |

#### VM disks

| Endpoint | Status | Route source |
| --- | --- | --- |
| `GET /vm-disk/list` | missing | [vm_disk.go:103](../master/api/vm_disk.go#L103) |
| `POST /vm-disk/create` | missing | [vm_disk.go:104](../master/api/vm_disk.go#L104) |
| `DELETE /vm-disk/{id}` | excluded (deletion policy) | [vm_disk.go:105](../master/api/vm_disk.go#L105) |
| `POST /vm-disk/{id}/grow` | missing | [vm_disk.go:106](../master/api/vm_disk.go#L106) |

#### WireGuard

| Endpoint | Status | Route source |
| --- | --- | --- |
| `POST /wireguard/createvpn` | missing | [wireguard.go:203](../master/api/wireguard.go#L203) |
| `POST /wireguard/newPeer` | missing | [wireguard.go:204](../master/api/wireguard.go#L204) |
| `GET /wireguard/` | missing | [wireguard.go:205](../master/api/wireguard.go#L205) |
| `DELETE /wireguard/{id}` | excluded (deletion policy) | [wireguard.go:206](../master/api/wireguard.go#L206) |

