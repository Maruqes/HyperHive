from pathlib import Path
import json, html, shutil

root = Path(__file__).parent
source = root.parent.parent / '.planning/2026-10-27-hyperhive-ui-approval-audit/screenshots'
shutil.copytree(source, root / 'screenshots', dirs_exist_ok=True)
sections = []
def page(name, route, shot, items, note='', pins=()):
    sections.append(dict(name=name, route=route, shot=shot, items=[dict(zip(('id','priority','effort','kind','title','detail'),i)) for i in items],note=note,pins=pins))

page('Shared shell and design language','All pages','01-vms.jpg',[
('G01','P1','M','Observed','Reserve space below the setup banner','Docker and Notes headings touch the banner; use one shared content offset and consistent top spacing on every route.'),
('G02','P2','M','Optional','Turn the permanent setup strip into a setup notice','Offer a direct setup destination, explain the affected feature, and allow dismissal after acknowledgement. Preserve the underlying setup requirement.'),
('G03','P2','M','Observed','Align page gutters and headers','Standardize title, subtitle, action row and content start. Current left gutters vary substantially between DNS, VMs, Support, Logs and Docker.'),
('G04','P2','S','Observed','Use one primary button treatment','Teal and white both mean primary today. Keep the current teal brand and define consistent primary, secondary, quiet and danger variants.'),
('G05','P2','S','Observed','Unify filters and status badges','NFS selected filters, XML counters and Nginx chips use different treatments. Use a consistent selected state and restrained status tokens.'),
('G06','P1','M','Observed','Give every control an accessible name','NFS, backups, DNS, VPN, Docker and Nginx contain unnamed buttons in the accessibility tree; several inputs are named only Input Field. Include the resource name in repeated actions.'),
('G07','P1','M','Observed','Distinguish pending data from zero','Dashboard, Docker, Updates and Nginx initially show zeros or empty content before data arrives. Use unknown placeholders/skeletons until the first response.'),
('G08','P2','M','Optional','Make secondary text and badges easier to read','Several metadata lines and dark chips appear faint. Measure text at 4.5:1 and essential controls at 3:1 before changing tokens; this audit does not certify contrast ratios.'),
('G09','P2','M','Optional','Add searchable navigation','The expanded menu requires long scrolling. Add a small navigation search, preserving the existing resize and collapse behavior.'),
('G10','P2','M','Optional','Use explicit resource names and consistent terminology','Prefer Host over Slave in interface copy, VM disks over VMs Disks, and Kubernetes over K8 Cluster. Keep API identifiers unchanged.'),
('G11','P1','M','Verify first','Check shared modal keyboard behavior','Audit dialog semantics, named close controls, Escape, focus containment and focus return. The observed trees expose backgrounds and unnamed close buttons; a full screen-reader test remains needed.'),
('G12','P2','M','Optional','Standardize dates and technical values','Use one date/time format with timezone for schedules, tabular numbers, and monospace for addresses/paths. Preserve exact technical strings and allow copying full values.'),
('G13','P2','M','Verify first','Complete phone, zoom and keyboard checks','Check 375px, tablet, landscape, 200% zoom, reduced motion, light theme and keyboard focus before rolling out shared changes. These are validation work, not confirmed defects.'),
], 'Keep the current dark/teal direction, Lucide icons and existing components. No font or dependency replacement is needed.', [('G02',65,2),('G03',22,9),('G09',10,65)])
page('Cluster Dashboard','/dashboard','02-dashboard.jpg',[
('D01','P2','M','Observed','Replace redundant storage graphics with one clear comparison','The gauge, total bar and volume strip repeat capacity information. Keep a compact total, then show labeled per-volume used/free bars with host and mount names.'),
('D02','P2','S','Observed','Replace implementation copy with operator copy','Replace Uses the same NFS calculation with a plain description of included mounts. Label the Update button Refresh storage so its scope is clear.'),
('D03','P2','M','Optional','Surface attention items before normal telemetry','Add a compact attention summary linking to high storage usage, unhealthy disks and stale hosts, using already available data. Requires agreed thresholds; no new monitoring backend assumed.'),
],pins=[('D01',58,51),('D02',49,32)])
page('Host details','/dashboard/[machine]','35-host-details.jpg',[
('H01','P2','M','Observed','Move tuning controls behind a distinct section','Host Controls occupies a large area before disk/network/history diagnostics. Put monitoring first and keep service tuning in an expandable Advanced controls section.'),
('H02','P2','S','Observed','Make hugepage state understandable','Group configured versus active capacity together and explain what zero active capacity means; avoid requiring operators to reconcile Disabled with Active: 2M (0 B). Verify backend meanings first.'),
('H03','P2','M','Optional','Reduce repeated gauges and tall empty panels','Use compact metrics and align host-control content to its actual height. Keep per-core detail available, with a clear expanded state.'),
], 'The warning toast in this screenshot belongs to the development runtime; it is not evidence of a production warning.', [('H01',39,35),('H02',82,58)])
page('Btrfs RAIDs','/btrfs-raids','03-btrfs.jpg',[
('BR01','P1','M','Verify first','Verify eligibility of devices shown as free disks','The list shows /dev/zram0 as a free SSD. Check whether it is actually eligible for array creation; if not, exclude it or explain why it cannot be used. This is not proof that creation accepts it.'),
('BR02','P2','S','Observed','Make capacity and identity easier to scan','Add a small labeled usage bar, align used/total/free values, and make UUID secondary but copyable. Preserve the existing balanced density.'),
],pins=[('BR01',34,68),('BR02',44,39)])
page('Automatic mounts','/btrfs-automatic-mounts','04-auto-mounts.jpg',[
('AM01','P2','S','Observed','Clarify removal scope','Rename Remove to Remove rule and explain that removing the startup rule differs from unmounting the active filesystem. Verify existing behavior before final copy.'),
('AM02','P3','S','Optional','Trim repeated row metadata','Keep mount path, array and compression aligned; move repeated host/UUID detail to a disclosure with copy support.'),
])
page('Smart Disk','/smartdisk','05-smartdisk.jpg',[
('SD01','P1','M','Observed','Explain Caution beside the disk','The caution disk shows zero reallocated and pending sectors, so the reason is unclear. Show the actual triggering attribute and a View health details action; do not infer failure from the badge alone.'),
('SD02','P2','S','Observed','Clarify reallocation and action hierarchy','Details is repeated as a primary teal action while Reallocation is unexplained. Keep Details neutral and explain potentially intensive maintenance before its confirmation.'),
('SD03','P2','M','Optional','Add health filtering and next-run context','Allow attention-only filtering, and show the next scheduled test with timezone. Next-run display may require derived scheduling logic or an API field.'),
],pins=[('SD01',88,58),('SD02',37,68)])
page('NFS mounts','/mounts','06-nfs.jpg',[
('NF01','P2','M','Optional','Offer compact capacity rows','Replace or supplement large gauges with name, host, path, used/free and VM connection counts in aligned rows. This is a density choice; retain cards if preferred.'),
('NF02','P1','S','Observed','Separate unmount from routine actions','Unmount sits directly beside Connect and Details on every card. Move it to a clearly labeled danger area or menu and explain affected consumers at confirmation.'),
('NF03','P2','S','Observed','Keep full paths inspectable','Long export paths wrap inside narrow cards. Provide full-value disclosure and a labeled copy button without expanding every row.'),
],pins=[('NF01',61,52),('NF02',94,61)])
page('ISO library','/isos','07-isos.jpg',[
('IS01','P2','S','Observed','Explain unavailable sizes once','Every visible size says Not reported. Use an unknown marker with a column-level explanation, or show sizes only when supplied; do not imply zero bytes.'),
('IS02','P3','M','Optional','Add storage and host filters','Search already works across the library; optional dropdown filters make storage placement and host availability easier to browse as inventory grows.'),
], 'The current row layout and explicit host availability are good foundations; a full redesign is unnecessary.')
page('Virtual Machines','/vms','01-vms.jpg',[
('VM01','P2','M','Optional','Explain the two RAM measurements','Rename RAM and Host RAM (real) only after confirming their semantics, and add short help explaining why they differ. Keep both readings available.'),
('VM02','P2','S','Observed','Make power actions recognizable before clicking','The actions have accessible names but no visible labels. Add keyboard-accessible tooltips or short labels for pause, restart, shutdown and details; retain frequently used actions.'),
('VM03','P3','M','Optional','Offer a compact row view for larger fleets','Add an optional view with host grouping, state, CPU/RAM/disk, primary IP and actions. Preserve the current cards, colored borders and compact host allocation summaries.'),
],pins=[('VM01',30,53),('VM02',32,71)])
page('VM creation','New VM dialog','36-new-vm.jpg',[
('VC01','P2','M','Observed','Group basic and advanced options','Group identity/host, resources, storage and network. Put XML template and specialized flags in Advanced while keeping selected values visible in a review summary.'),
('VC02','P2','S','Observed','Replace API-facing form language','Use Host, NFS share and Installation ISO. Remove Sends template_id only when a template is selected and explain the actual effect of selecting a template.'),
('VC03','P1','M','Observed','Associate labels and expose preset buttons correctly','Inputs appear as Input Field and RAM/disk presets as containers in the accessibility tree. Give inputs meaningful names and presets button roles with selected state.'),
('VC04','P2','M','Optional','Show host and storage capacity before creation','Once host and NFS are selected, show available capacity and explain overcommit warnings. Reuse allocation semantics and avoid silently changing creation rules.'),
], 'Form opened and cancelled without submission. Validation, live creation and failure recovery were not exercised.', [('VC02',54,61),('VC03',60,35),('VC01',33,68)])
page('VM details','VM details dialog','37-vm-details.jpg',[
('VD01','P2','S','Observed','Allow full path and per-interface IP inspection','Disk Path is truncated and all IPs share one line. Wrap or disclose the full path with copy, and list addresses individually with interface context when available.'),
('VD02','P3','L','Optional','Give VM details a dedicated URL','Make details shareable and preserve browser Back, keeping a lightweight modal for quick inspection if useful. This changes navigation and needs route/state work.'),
],pins=[('VD01',48,46)])
page('VM disks','/vms-disks','08-vm-disks.jpg',[
('DK01','P1','M','Verify first','Clarify what Used measures for raw disks','The attached raw disk reads 100%. Verify whether this is host allocation rather than guest filesystem usage and label it accordingly to avoid a false full-disk interpretation.'),
('DK02','P2','S','Observed','Show attachment and path more clearly','Give attached VM its own labeled field, disclose/copy the full path, and separate deletion from Grow. Keep attached-resource safety in the existing backend.'),
])
page('XML templates','/vms-xml-templates','09-xml.jpg',[
('XT01','P2','S','Observed','Remove repeated counts and character totals','The same one-template count appears in multiple summaries; character counts dominate the page. Keep total/matching count once and show size only in detail.'),
('XT02','P2','M','Observed','Make XML preview readable on demand','Use a collapsible formatted code preview instead of a flattened truncated line. Preserve placeholder case exactly and make placeholder behavior available in help.'),
])
page('GPUs','/gpus','10-gpus.jpg',[
('GP01','P2','S','Observed','Use a compact host selector toolbar','The host selector spans the page while Refresh and counters occupy another row. Match the compact toolbar used by other storage pages.'),
('GP02','P2','S','Optional','Explain the empty inventory state','Explain whether no devices were detected versus none eligible for passthrough, and provide Retry and a host-switch suggestion only where the API can distinguish those cases.'),
], 'Only the empty inventory for the selected host was verified; populated GPU assignment is outside this pass.')
page('Backups','/backups','11-backups.jpg',[
('BK01','P1','S','Observed','Name restore and delete actions by backup','Icon-only actions are unnamed in the observed tree. Include VM and backup date in accessible names, and provide tooltips with Restore, Download and Delete.'),
('BK02','P2','M','Observed','Use meaningful storage and backup-state names','Replace NFS #1 with its share name where known. Clarify Working / Live versus completed backup status after checking the underlying field semantics.'),
('BK03','P2','M','Optional','Add sorting, filters and collapsible VM groups','Keep the approved table layout; add latest-first sorting controls, date/status filters and group collapse for long backup histories.'),
])
page('Automatic backups','/autobackups','12-auto-backups.jpg',[
('AB01','P2','M','Observed','Replace average frequency with next-run information','5 d average frequency mixes daily and weekly schedules without helping the operator. Show the next scheduled window per VM and the applicable timezone.'),
('AB02','P2','S','Observed','Keep schedule identity and storage explicit','Show weekly day, actual NFS share name, retention and last result. Keep the existing aligned rows and neutral borders.'),
], 'Next-run accuracy depends on scheduler semantics; this needs verification before implementation.')
page('WireGuard VPN','/wireguard','13-wireguard.jpg',[
('WG01','P2','M','Optional','Prioritize peer connection information','Offer search and, if the API provides them, latest handshake and traffic. Move full public keys into a copy/disclosure control so peer identity stays prominent.'),
('WG02','P1','S','Observed','Label copy and remove controls','Use Copy public key for [peer] and Remove [peer] labels. Explain what Connect locally does before it starts a local connection workflow.'),
])
page('DNS','/dns','14-dns.jpg',[
('DN01','P2','M','Optional','Add alias search and sorting','Filter by alias, address and VM; keep the row layout and allow copying addresses.'),
('DN02','P2','S','Observed','Make optional metadata lighter','Two mostly empty columns consume space. Offer compact default rows with SSH username and VM relationship in details, retaining editable values.'),
])
page('Kubernetes cluster','/k8s-cluster','15-k8s.jpg',[
('K801','P2','S','Observed','Explain the connection address choice','Show which interface or network each address belongs to when available and explain where the downloaded connection file will be usable.'),
('K802','P2','S','Observed','Prioritize node state above download setup','Put node status first and keep the connection-file action compact. Avoid allocating a full section to an empty disconnected group.'),
])
page('celld Fleets','/celld-fleet','16-celld.jpg',[
('CF01','P2','S','Observed','Separate no resources from healthy resources','Healthy beside zero fleets/buckets can be read as a workload verdict. Label service reachability separately and show No fleets / No buckets as inventory state.'),
('CF02','P2','S','Observed','Make the empty state actionable','Place Create fleet in the empty-state panel as well as the header, and explain SILO in plain language. Move deleted-fleet recovery to a secondary menu.'),
])
page('Single Packet Authorization','/spa','17-spa.jpg',[
('SP01','P1','M','Observed','Fix browser autofill and port input semantics','The browser filled the Port field with an account email on navigation. Use an explicitly labeled numeric port input and appropriate autocomplete semantics; verify password-manager behavior without submitting a port.'),
('SP02','P2','M','Optional','Make access scope and duration explicit','Explain what access is opened, how long it lasts and how it is revoked before Create port. Show actual configured behavior; do not invent an expiry field unsupported by the backend.'),
], 'Screenshot captures initial loading and browser autofill, not a configured-port result. No port was created.', [('SP01',43,28)])
page('Updates','/updates','18-updates.jpg',[
('UP01','P1','M','Verify first','Show update impact before applying','Review the existing confirmation flow for host, package count, automatic reboot and expected interruption. Add missing impact information only after inspecting the current confirmation.'),
('UP03','P2','M','Optional','Use compact package rows','Thirty-eight updates each occupy a large card. Use aligned package, installed version, available version and action columns; explain unknown installed versions instead of a bare arrow.'),
('UP02','P2','S','Observed','Make refresh and pending availability explicit','Name the refresh control and distinguish checking from no pending packages. Preserve the existing Last checked timestamp and keep automatic interval labels hidden.'),
])
page('Logs','/logs','19-logs.jpg',[
('LG01','P1','M','Observed','Use semantic controls for filtering and reloading','Reload, Select sources and Register Notifications appear as text/containers in the observed tree. Make them keyboard-operable buttons/selects with proper names and state.'),
('LG02','P2','M','Observed','Simplify the filter model','Level (API) overlaps INFO/DEBUG switches and accepts arbitrary text. Offer one understandable severity model, with advanced source/limit controls in a disclosure.'),
('LG03','P2','M','Optional','Give empty results a recovery path','Differentiate loading, no matching logs, no collected logs and failed query. Offer Clear filters or Retry as applicable and put notification setup in secondary actions.'),
])
page('HH Support','/support','20-support.jpg',[
('SU01','P1','M','Observed','Prevent widgets shrinking below usable sizes','The saved layout shows a very narrow text widget and a narrow CPU chart surrounded by gaps. Enforce minimum usable widget dimensions and provide a deliberate Fit layout action without silently overwriting saved positions.'),
('SU02','P2','M','Observed','Explain Live, Offline and Healthy separately','The screen shows all three at once. Label connection to telemetry service, App heartbeat and last evaluated health as distinct signals with timestamps.'),
('SU03','P2','M','Optional','Offer alignment and layout recovery tools','Add Snap to grid / Align / Reset layout in edit mode, preserving the free placement model. Any reset must preview its effect and preserve a recovery path.'),
('SU04','P2','S','Observed','Make empty widgets concise and useful','The narrow waiting widget wraps a long paragraph word by word. Use a short no-data message with expected stream name and a details disclosure.'),
], 'The displayed layout is user-configurable; sparse placement may be intentional. Proposals must preserve existing layouts unless explicitly reset.', [('SU01',56,43),('SU02',42,18),('SU04',35,35)])
page('404 hosts','/404','21-404.jpg',[
('N401','P2','S','Observed','Present intentional 404 behavior without alarm styling','Every correctly configured rule shows a red warning-style 404 Not Found. Use a neutral Returns 404 behavior label; reserve red for actual failures.'),
('N402','P2','M','Optional','Use compact domain rules','Offer rows with domain, TLS policy, enabled state and actions. Remove repeated ACTIVE / SSL ENABLED chips when the same information is already expressed.'),
])
page('Certificates','/certificates','22-certificates.jpg',[
('CE01','P2','M','Optional','Make expiry actionable','Show Expires in [days] beside an unambiguous date, with expiry sorting and due-soon filtering. Reuse existing certificate data.'),
('CE02','P2','S','Verify first','Match certificate actions to provider capabilities','The custom certificate also shows Renew. Verify what that action does and use Replace or Renew according to the actual supported operation.'),
])
page('Access lists','/access-lists','23-access-lists.jpg',[
('AL01','P2','S','Optional','Explain the two kinds of access rules in the empty state','Offer concise guidance on HTTP authentication versus client allow/deny rules, then one Create access list action.'),
('AL02','P2','M','Verify first','Show policy effects in create/edit review','Before save, summarize allow/deny ordering, authentication combination and affected proxies. No populated list or save flow was tested in this audit.'),
], 'Only the empty inventory was inspected; the second item is a proposed verification task, not a confirmed bug.')
page('Proxy hosts','/proxy','24-proxy.jpg',[
('PX01','P2','M','Observed','Separate setup from everyday proxy management','Setup FrontEnd Page competes with Add Proxy Host in the same toolbar. Put setup status in a compact distinct notice, preserving its existing operation.'),
('PX02','P2','M','Optional','Offer searchable compact proxy rows','Add free-text search alongside domain grouping, and align domain, destination, TLS and enabled state. Keep operational actions available.'),
('PX03','P1','S','Observed','Reduce the visual dominance of delete','Bright filled red Delete appears on every proxy. Use a quiet danger affordance or labeled menu and retain resource-specific confirmation.'),
])
page('Upstreams','/upstreams','25-upstreams.jpg',[
('US01','P2','S','Optional','Offer purpose-specific empty-state actions','Give Create HTTP pool and Create TCP/UDP pool choices with a brief consumer explanation, instead of requiring an extra type decision after Add pool.'),
('US02','P2','M','Verify first','Show usage and capability restrictions on populated pools','Review whether pools show consumers and supported backend capabilities before editing/removal. Empty inventory prevents visual verification of that state.'),
])
page('Redirections','/redirection','26-redirection.jpg',[
('RD01','P1','S','Observed','Make redirect behavior readable','The 302 TEMPORARY and PRESERVE PATH chips are faint. Show clear normal-text labels for code, path behavior and destination.'),
('RD02','P2','S','Optional','Show source-to-destination mapping directly','Use Domain → destination, with copy/open controls and an explicit protocol. Preserve configured destinations exactly.'),
])
page('TCP/UDP streams','/streams','27-streams.jpg',[
('TS01','P2','S','Optional','Align listener and destination fields','Use rows with listener port, target host/port, TCP/UDP, TLS and enabled state. Keep description visible and technical values copyable.'),
('TS02','P2','S','Observed','Use protocol-appropriate security copy','For a TCP stream, No SSL is informational. Show TLS off neutrally; explain exposure in details instead of implying all non-HTTP streams require HTTPS.'),
])
page('Docker images','/docker/images','28-images.jpg',[
('DI01','P1','M','Observed','Contain overflowing image metadata','Long label chips run beyond the content area. Show label count and expand to a wrapping key/value table with full-value copy; preserve case and exact strings.'),
('DI02','P2','M','Optional','Add image search and usage context','Filter by repository/tag and show in-use references when available. Separate tag and digest so long hashes do not dominate identity; do not offer unverified cleanup savings.'),
],pins=[('G01',34,5),('DI01',78,73)])
page('Docker containers','/docker/containers','29-containers.jpg',[
('DC01','P1','M','Observed','Separate routine controls from forceful actions','Every container exposes Logs, Update, Restart, Pause, Stop, Kill and Remove together. Keep common actions visible and move Kill/Remove to a labeled danger menu with impact confirmation.'),
('DC02','P2','M','Optional','Make container rows scannable','Show name, image, textual state/health and ports first; collapse mount lists and restart metadata into details. Add search and state filters.'),
])
page('Docker volumes','/docker/volumes','30-volumes.jpg',[
('DV01','P1','M','Verify first','Disambiguate displayed capacity','Multiple different volumes show the same 33.9 GB / 220.6 GB pair. Verify whether this is backing-filesystem usage, then label it accordingly instead of implying per-volume size.'),
('DV02','P2','M','Optional','Prioritize volume identity and consumers','Shorten anonymous hashes visually with full copy/disclosure and show attached containers when available. Distinguish an unused volume only when reference data supports it.'),
])
page('Docker networks','/docker/networks','31-networks.jpg',[
('DW01','P2','S','Observed','Collapse verbose network driver options','Keep network, driver, subnet and scope prominent. Expand full options as a key/value table without uppercasing technical values.'),
('DW02','P1','M','Verify first','Explain built-in and in-use deletion restrictions','Built-in bridge/host/none networks display remove controls. Verify server restrictions and disable or explain unavailable actions, retaining backend validation.'),
])
page('Docker Git','/docker/git','32-git.jpg',[
('GT01','P2','S','Observed','Give the empty state a clear next step','Replace plain No repositories configured with a compact explanation and Clone stack action, including accepted repository/compose expectations.'),
('GT02','P2','M','Verify first','Review deployment state presentation','For populated stacks, check branch/ref, last result, update progress and error recovery. The current host has no repositories, so no such defect is claimed.'),
])
page('Notes','/notes','33-notes.jpg',[
('NT01','P2','S','Observed','Use consistent interface language','Rename Ver to View. Preserve user-authored Portuguese note titles and content exactly.'),
('NT02','P2','M','Observed','Make note previews more useful','Render a readable markdown excerpt rather than raw markdown markers, reduce forced empty card height, and provide search as notes grow.'),
])
page('Settings','Settings dialog','34-settings.jpg',[
('SE01','P1','S','Observed','Name the close icon and theme options clearly','The top close button has no accessible name and radio descriptions are light/dark/system. Use Close settings and the visible human-readable option labels.'),
('SE02','P3','S','Optional','Make the settings dialog proportional to its content','The dialog contains only theme preferences but uses a large shell. Use a compact layout while retaining readable descriptions and the explicit Close action.'),
], 'No theme preference was changed. Light appearance was not validated.')

items = [i for s in sections for i in s['items']]
ids = [i['id'] for i in items]
assert len(ids) == len(set(ids))
assert all((root/'screenshots'/s['shot']).is_file() for s in sections)
assert all(p[0] in ids for s in sections for p in s['pins'])
(root/'proposals.json').write_text(json.dumps(sections,indent=2))

intro = '''Review of the local HyperHive desktop web UI on 27 October 2026. Proposals only: no application implementation or infrastructure operations were performed. Select individual items and send the resulting IDs in chat; selecting a checkbox does not authorize or apply a change. Nothing is preselected.

Evidence: real browser screenshots and accessibility-tree observations, plus stored design preferences. Observed means the current view supports the finding; Optional means a design choice; Verify first means behavior/data needs inspection before any fix. Loading views do not prove a persistent failure. P1 = address first, P2 = usability improvement, P3 = optional expansion. S/M/L are relative implementation scope, not time estimates.

Preserve: VM status-colored borders, host-grouped cards and compact allocation summaries; neutral VM companion borders; row-based backups; automatic refresh without interval labels. No blanket density change is approved. Reuse the current typography, teal palette, Lucide and house components.

Limits: desktop dark/system appearance only. No exhaustive keyboard/screen-reader, phone, zoom, light-theme, error-state or populated-state certification. Login/logout, noVNC, GoAccess, StreamInfo and unused demo routes were not exercised. No create/save/delete/power/update/permission action was submitted. Screenshots include private infrastructure names and paths; keep this report local.
'''
md=['# HyperHive UI proposal catalogue',intro,f'{len(items)} individual proposals across {len(sections)} review sections. All unchecked.']
for s in sections:
    md += [f"\n## {s['name']} — {s['route']}",f"![Current {s['name']}](screenshots/{s['shot']})",s['note']]
    for i in s['items']:
        md += [f"- [ ] **{i['id']} · {i['priority']} · {i['effort']} · {i['kind']} — {i['title']}**\n\n  {i['detail']}"]
(root/'review.md').write_text('\n\n'.join(md))

esc=html.escape
parts=[]
for n,s in enumerate(sections):
    cards=''.join(f'''<label class="proposal" data-priority="{i['priority']}" data-kind="{esc(i['kind'])}"><input type="checkbox" value="{i['id']}"><span><span class="meta"><b>{i['id']}</b><em class="{i['priority']}">{i['priority']}</em><em>{i['effort']}</em><em>{esc(i['kind'])}</em></span><strong>{esc(i['title'])}</strong><span class="detail">{esc(i['detail'])}</span></span></label>''' for i in s['items'])
    pins=''.join(f'<a class="pin" style="left:{x}%;top:{y}%" href="#item-{pid}" title="Proposal {pid}">{pid}</a>' for pid,x,y in s['pins'])
    for i in s['items']: cards=cards.replace(f'value="{i["id"]}"',f'id="item-{i["id"]}" value="{i["id"]}"')
    parts.append(f'''<details class="section" id="section-{n}" {'open' if n==0 else ''}><summary><span>{esc(s['name'])}<small>{esc(s['route'])}</small></span><b>{len(s['items'])} ideas</b></summary><div class="sectionbody"><div class="evidence"><div class="shot"><a href="screenshots/{s['shot']}" target="_blank" aria-label="Open full screenshot of {esc(s['name'])}"><img loading="lazy" src="screenshots/{s['shot']}" alt="Current {esc(s['name'])} screen"></a>{pins}</div><p>Current interface · click image for full size. Pins point to proposal locations.</p><p>{esc(s['note'])}</p></div><div>{cards}</div></div></details>''')
style='''*{box-sizing:border-box}html{scroll-behavior:smooth}body{margin:0;background:#0c111b;color:#e7edf5;font:15px/1.55 system-ui,sans-serif}main{max-width:1500px;margin:auto;padding:28px}header{padding:22px 0 26px}h1{font-size:38px;margin:10px 0}h1+ p{max-width:850px;color:#b8c5d8}.eyebrow{font-size:12px;letter-spacing:2px;color:#66ded0}.stats{display:flex;gap:12px;flex-wrap:wrap}.stats span{background:#162233;padding:8px 14px;border-radius:8px}.intro{max-width:1100px;color:#b8c5d8}.intro p{margin:12px 0}.toolbar{position:sticky;top:0;z-index:4;background:#111c2def;padding:14px;display:flex;flex-wrap:wrap;gap:10px;border:1px solid #344359;border-radius:10px;backdrop-filter:blur(8px)}button,select,input[type=search]{font:inherit;color:#e7edf5;background:#1c2b40;border:1px solid #53647d;border-radius:6px;padding:9px 12px}button{cursor:pointer}button:hover{background:#2b405b}button:focus-visible,a:focus-visible,input:focus-visible,select:focus-visible,summary:focus-visible{outline:3px solid #67e8d6;outline-offset:3px}.primary{background:#1b756c}#search{flex:1;min-width:180px}#count{padding:10px}.section{margin:18px 0;border:1px solid #344359;border-radius:12px;background:#111b2a;overflow:clip}summary{display:flex;justify-content:space-between;align-items:center;gap:12px;padding:18px 22px;cursor:pointer;font-size:21px;font-weight:650}summary small{display:block;font-size:13px;color:#b6c4d7;font-weight:400}summary b{font-size:13px;color:#8ce5da}.sectionbody{border-top:1px solid #344359;display:grid;grid-template-columns:minmax(300px,1fr) minmax(350px,1fr);gap:24px;padding:22px}.evidence p{font-size:13px;color:#b6c4d7}.shot{position:relative}.shot img{display:block;width:100%;height:auto;border-radius:7px}.pin{position:absolute;transform:translate(-50%,-50%);border:2px solid #fff;color:#fff;background:#aa2539;text-decoration:none;font-size:11px;font-weight:750;border-radius:5px;padding:3px 5px;box-shadow:0 2px 10px #000}.proposal{display:flex;align-items:start;gap:12px;padding:16px;margin-bottom:12px;border:1px solid #344359;border-radius:8px;background:#152135;cursor:pointer}.proposal:has(input:checked){border-color:#62dfce;background:#163a39}.proposal input{margin-top:7px;width:21px;height:21px;flex-shrink:0;accent-color:#2dd4bf}.proposal strong{display:block;font-size:17px;margin:7px 0}.detail{display:block;color:#c4cfdf}.meta{display:flex;gap:7px;align-items:center;flex-wrap:wrap}.meta b{color:#8be8dc;font:600 13px ui-monospace,monospace}.meta em{font-size:11px;font-style:normal;background:#2b3b52;padding:2px 6px;border-radius:4px}.meta .P1{background:#62303a}.meta .P2{background:#3b4468}#approval{width:100%;min-height:90px;font:14px/1.6 ui-monospace,monospace;background:#09121f;color:#ddf8ee;padding:14px;border:1px solid #50617a;border-radius:8px}footer{margin:24px 0;color:#b6c4d7}.hidden{display:none!important}@media(max-width:850px){main{padding:14px}h1{font-size:29px}.sectionbody{grid-template-columns:1fr;padding:14px}summary{padding:14px;font-size:19px}.toolbar{position:static}}@media(prefers-reduced-motion:reduce){html{scroll-behavior:auto}}@media print{.toolbar,.controls{display:none}.sectionbody{display:block}details{break-inside:avoid}body{background:white;color:black}.proposal,.section{background:white;color:black}.detail,.intro,.evidence p{color:#222}.evidence img{max-height:500px;object-fit:contain}}'''
script='''const boxes=[...document.querySelectorAll('.proposal input')];const count=document.querySelector('#count');const approval=document.querySelector('#approval');
function update(){const selected=boxes.filter(b=>b.checked).map(b=>b.value);count.textContent=selected.length+' selected';approval.value=selected.length?'Approve only: '+selected.join(', ')+'. All other proposals remain unapproved.':'No proposals selected.';}
boxes.forEach(b=>b.addEventListener('change',update));
document.querySelector('#expand').onclick=()=>document.querySelectorAll('details').forEach(d=>d.open=true);
document.querySelector('#collapse').onclick=()=>document.querySelectorAll('details').forEach(d=>d.open=false);
document.querySelector('#clear').onclick=()=>{boxes.forEach(b=>b.checked=false);update();};
document.querySelector('#copy').onclick=()=>{approval.focus();approval.select();};
function filter(){const q=document.querySelector('#search').value.toLowerCase();const p=document.querySelector('#priority').value;document.querySelectorAll('.section').forEach(s=>{let shown=0;s.querySelectorAll('.proposal').forEach(c=>{const match=(!p||c.dataset.priority===p)&&(!q||(s.querySelector('summary').textContent+' '+c.textContent).toLowerCase().includes(q));c.classList.toggle('hidden',!match);if(match)shown++;});s.classList.toggle('hidden',!shown);if((q||p)&&shown)s.open=true;});}
document.querySelector('#search').oninput=filter;document.querySelector('#priority').onchange=filter;update();'''
introhtml=''.join('<p>'+esc(p)+'</p>' for p in intro.strip().split('\n\n'))
(root/'index.html').write_text(f'''<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>HyperHive · UI approval review</title><style>{style}</style></head><body><main><header><div class="eyebrow">HYPERHIVE / DESIGN REVIEW / 27 OCT 2026</div><h1>You choose what changes.</h1><p>A broad, screenshot-backed UI review. Practical fixes and bigger options, each independently selectable. This board makes no requests to HyperHive.</p><div class="stats"><span>{len(items)} proposals</span><span>{len(sections)} review sections</span><span>37 screenshots</span><span>0 product changes</span></div><div class="intro">{introhtml}</div></header><div class="toolbar"><input id="search" type="search" aria-label="Search proposals" placeholder="Search page, proposal ID or topic"><select id="priority" aria-label="Filter by priority"><option value="">All priorities</option><option>P1</option><option>P2</option><option>P3</option></select><button id="expand">Expand all</button><button id="collapse">Collapse all</button><span id="count" role="status" aria-live="polite"></span></div><div>{''.join(parts)}</div><footer><h2>Your approval list</h2><p>Tick only the proposals you want. Select the text below, copy it and send it in chat. The board does not send approvals or store your selections after reload.</p><textarea id="approval" aria-label="Prepared approval list" readonly></textarea><div class="controls"><button class="primary" id="copy">Select approval text</button> <button id="clear">Clear selections</button></div><p>Sources: browser views at localhost:8081; ui-ux-pro-max checklist and verified dashboard/accessibility searches; HyperHive UI rework guidelines in SilverBullet. Relative effort is indicative and behavior changes need implementation review.</p></footer></main><script>{script}</script></body></html>''')
assert (root/'index.html').read_text().count('type="checkbox"') == len(items)
assert all(i['priority'] in {'P1','P2','P3'} for i in items)
print(f'Created {len(items)} proposals in {len(sections)} sections; screenshot links and unique IDs verified.')
