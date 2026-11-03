# HyperHive feature ideas: Grill / Discovery Notes
Date: 2026-11-03 · Status: **ready idea** (not an implementation plan) · Goal: turn five raw feature ideas into one ready IDEA each: what and why, scope, boundaries and open points.

> Mirror of the SilverBullet page `ideas/HyperHive/feature-ideas-2026-11`. The Q&A log below is the source for every decision (Qn references).

## Summary / key decisions

- **Scope:** five ideas. "VM metrics history" was dropped at the user's request; no idea depends on a per-VM metrics history (Q1).
- **Recommended build order (smallest first):** 1 Snapshot timeline → 2 Disk health → 5 Network graph → 3 Power and cost → 4 Infrastructure as code (Q29).
- **Cross-cutting rules:** MCP gets read-only tools only; nothing that overwrites or deletes data goes through MCP. Every new background loop is master-owned and stoppable. Estimates are labelled as estimates in the UI.

## The ideas

### 1. Snapshot timeline
- **What:** a per-VM timeline on the VM page that shows its existing automatic and manual backups (date, manual/automatic, NFS share), where the user picks one to **revert in place** (Q2, Q5, Q6).
- **Why:** backups already exist but can only be restored as a new VM; getting a VM back to a known-good disk takes too many steps.
- **Behavior:** revert overwrites the VM's primary disk with the chosen backup. The VM must be off; a running VM is offered "shut down and revert". Before overwriting, HH takes a normal manual backup of the current disk on the chosen point's NFS share as a safety point (Q3, Q7). Restore-as-new-VM stays as today.
- **Boundaries:** points are full backup copies only; no libvirt/qcow2 snapshots (Q2). Disk only; domain XML/config is never captured or reverted (Q4). No "snapshot before risky change" hook (Q5). No new tables, no compare; anything not stored (for example size) is read live or omitted (Q6). No MCP revert tool (Q7).
- **Failure semantics:** if the safety backup fails, the revert does not start. If the copy over the disk fails, the safety backup is the recovery point and the user is notified.

### 2. Disk health forecasting
- **What:** a SMART history per physical disk with a 0–100 health score, a plain-language reason, trend charts and alerts before failure (Q8–Q12).
- **Why:** SMART is only checked as a current snapshot today; slowly worsening disks go unnoticed, and the existing alert repeats without dedup.
- **Collection:** the master polls the existing `GetSmartInfo` RPC every 6 h for every disk on every connected host and stores the key attributes in master SQLite, keyed by **disk serial**. No proto change (Q8).
- **Retention:** kept for life. A serial unseen for 90 days becomes "retired" with history kept; manual purge from the UI (Q9).
- **Scoring:** weighted rules on critical counters (pending, reallocated, offline-uncorrectable, media errors, CRC) plus their growth over 7 and 30 days; SSD/NVMe remaining life by linear extrapolation of `percentage_used`. No ML (Q10).
- **Alerts:** only on change: the score drops into a worse band (good → watch → at risk → critical) or a critical counter increases, one notification per change via `nots.Notify`. The slave's repeating 6-h SMART alerts (`StartSmartTestChecker`) are retired (Q11).
- **UI and MCP:** score badge per disk in the SMART/host view, per-disk chart page (score, temperature, critical counters, wear), retired-disks list. Disks without SMART show "no SMART data" and are never scored. Read-only MCP "list disk health" and "get disk health history"; purge is UI-only (Q12).
- **Open for the plan:** exact weights and band thresholds.

### 5. Visual network graph
- **What:** a new "Network map" page showing hosts, interfaces, bridges, WireGuard links, VMs, proxies, streams and DNS names, with live status and live traffic (Q25–Q27).
- **Why:** the topology now lives across libvirt, NPM, DNS, DHCP and WireGuard; one view answers "what reaches this VM, and how".
- **Data:** built on demand by one master endpoint; nothing is stored. The graph refreshes while open from existing websocket events plus a slow poll (Q27).
- **Edges:** proxy/stream → VM is inferred by IP. The NPM forward host is resolved (DNS aliases, then hostname) and matched against VM IPs (DHCP leases, guest agent) and host IPs. Unmatched targets become an "external / unknown" node, and each edge records how it was matched (Q26).
- **Traffic:** live only, nothing stored. Host interfaces use existing host data, VM NICs use on-demand libvirt counter deltas, streams use existing stream stats (Q25).
- **UI and MCP:** grouped by host; layer toggles (physical / VMs / services / DNS); filters by host, VM or service; search. A node opens a side panel (IP, MAC, ports, status, match method) with "open page". Read-only MCP "get network graph" returns JSON (Q27).

### 3. Power and cost per VM
- **What:** energy (kWh) and cost (EUR) per host, per VM and in total, plus estimated power-schedule savings (Q13–Q18).
- **Why:** it shows what each VM and host costs to run and what the power schedule saves.
- **Power source:** a pluggable per-host setting: RAPL (labelled "CPU+RAM estimate"), IPMI/DCMI, or a smart plug over a local HTTP API. v1 ships RAPL plus a manual "fixed watts" fallback; IPMI and plugs come later (Q13).
- **VM split:** every minute the slave reads host watts and each VM's libvirt CPU-time delta in the same pass. Idle watts go to a "host" bucket; the rest is split by CPU share. Only Wh per VM per hour is stored, never raw usage (Q14).
- **Idle baseline:** auto-learned as p5 of host watts over 7 days, recomputed daily; the user can pin a fixed value per host, and the UI shows which is active (Q15).
- **Price:** a single flat EUR/kWh for the whole cluster (Q16). A price change applies from now on.
- **Savings (estimated):** off hours in the schedule window × the host's average on-watts over its last 7 days × price, per day and month, per host and in total (Q17).
- **Retention, UI, MCP:** hourly Wh per host and per VM kept 2 years; VM rows are keyed by name and purged when the VM is deleted. A cluster energy dashboard (today, month, year; per-host and per-VM tables; savings card) plus a "kWh / EUR this month" line on each VM and host page. Read-only MCP "get energy usage"; settings are UI-only (Q18).
- **Open for the plan:** host power storage granularity (hourly Wh + hourly minimum watts suggested); a VM that moves host mid-hour.

### 4. Infrastructure as code (export + import)
- **What:** export the cluster config to one YAML file, then import it with a plan (dry-run diff) and apply (Q19–Q24).
- **Why:** disaster rebuild, cloning to another master, and versioning config in git.
- **Content:** VMs (definitions), proxies, redirects, 404 hosts, streams, upstreams, DNS aliases, access lists, automations (script files inline), backup schedules and notes. Schema `hyperhive/v1`; JSON accepted on import (Q24).
- **Apply model:** additive. Creates missing and updates changed resources; anything absent from the file is listed as "unmanaged" and left alone; **import never deletes** (Q19).
- **Matching:** natural keys. VM = name; proxy / redirect / 404 = first domain; stream = incoming port + protocol; access list / automation / DNS alias = name; backup schedule = VM name; note = title. References use the same keys, and clashes block the plan (Q21).
- **VMs:** the existing disk is used if present; otherwise a fresh empty disk of the recorded size is created, and the plan states this clearly so data can then be restored from backups (Q20).
- **Secrets:** never exported. The file holds named placeholders, and the plan lists missing secrets, which the user fills in before apply (otherwise the resource is skipped). Let's Encrypt certs are re-requested and custom certs re-uploaded. The file is safe for git (Q22).
- **Partial failure:** dependency order (access lists/upstreams → DNS → proxies/redirects/streams → VMs → schedules/automations/notes). On failure, apply continues and skips dependants; no rollback. The result is a per-resource report (created / updated / unchanged / failed / skipped), and re-running is idempotent (Q23).
- **Surface:** REST + UI (export download; upload → plan → fill secrets → apply). CLI and a Terraform/OpenTofu provider come later. Read-only MCP "export config" and "plan import"; apply is UI/API only (Q24).

## Raw input (user, 2026-11-03)

1. **Snapshot timeline** — visual per-VM timeline (date, size, manual/automatic), filter/compare; one-click revert/restore to a point in time; "snapshot before risky change" on edit VM / XML update / disk grow; backend: timeline listing, restore point, pre-change hook. "Mostly built on the existing backup system."
2. **Disk health forecasting** — periodically store SMART attributes (reallocated/pending sectors, CRC errors, wear, temperature, power-on hours); detect worsening trends, estimate risk/remaining life; notify before failure; per-disk history charts and health score.
3. **Power and cost per VM** — host power from RAPL, IPMI or smart plugs; split host power across VMs by usage share ("needs metrics history" — that idea is now dropped, see Q1); configurable EUR/kWh; dashboard per VM/host/total; show power-schedule savings.
4. **Infrastructure as code (export + import)** — export VMs, proxies, redirects, streams, DNS, access lists, automations, backup schedules, notes to YAML/JSON; import with dry-run diff ("plan") then apply; idempotent; clear partial-failure handling; use cases: disaster rebuild, clone to another master, config in git; no plain-text secrets; later CLI or Terraform/OpenTofu provider.
5. **Visual network graph** — nodes: hosts, interfaces, bridges, WireGuard links, VMs, proxies/services, DNS names, streams; edges interface → bridge → VM NIC → service/proxy → domain; click for details and jump; live status colors, optional traffic, filters.

## What already exists (source/SilverBullet evidence, 2026-11-03)

- **VM backups** are full copies of the *primary disk* onto an NFS share, async, guest-agent freeze for running VMs; metadata in master SQLite `virsh_backups` (id, VM name, path, `nfsmount_id`, created_at, automatic). Restore (`UseBackup`, `/virsh/useBackup/{id}`) copies the backup to a new folder and cold-migrates it as a **new VM** with a new name; there is no in-place revert. No libvirt/qcow2 snapshot feature exists (`master/services/vm_snapshot.go` is a VM state read, not a disk snapshot). See SilverBullet `knowledge/projects/HyperHive/features/backups/vm/index`.
- **Host-level snapshots** exist: master tables `cpu_snapshots`, `mem_snapshots`, `disk_snapshots`, `network_snapshots` (machine_name, captured_at, JSON payload) in `master/db/info.go`. No per-VM history.
- **HH Support** is a separate telemetry service (own SQLite, number/text/boolean/log/event streams, widgets, 7-day default retention).
- **HH Automations** run scripts on HH Support conditions — possible alert/action path.
- **SMART**: `GetSmartInfo` (slave `smartctl -a -j`) returns 40+ attributes (`api/proto/smartdisk.proto` `SmartDiskInfo`) plus slave-computed `health_status`, `physical_problem_risk`, `recommended_action`. Slave `StartSmartTestChecker` alerts every 6 h without per-device dedup. Master schedules self-tests (`smartdisk_repeat`). No stored attribute history.
- **Power schedule** exists (`power_schedule_status`) — relevant to savings in idea 3.

## Q&A log

### Q1 — Scope reset
- Asked: the earlier session's answers were about a sixth idea, "VM metrics history"; keep it, fold it into power/cost, or drop it?
- Captured: user: "delete everything related to that 'VM metris history'". All metrics-history answers were removed. Discussion now follows the list order 1 → 5.
- Flags: idea 3 (power split across VMs) loses its planned data source → must be re-decided in idea 3.

### Q2 — Idea 1: what a timeline point is
- Asked: backups only, hybrid (backups + internal qcow2 snapshots), or real libvirt snapshots only?
- Captured: **Backups only.** Every timeline point is an existing-style full backup copy of the primary disk on NFS. No libvirt/qcow2 snapshots are introduced, so migration, move-disk, disk grow and the backup copy logic stay unchanged.
- Flags: none.

### Q3 — Idea 1: revert behavior
- Asked: in place with safety backup, in place without, or keep "restore as new VM" only?
- Captured: **In place + safety backup.** Same VM, same name. VM must be off (HH offers to shut it down). HH first backs up the current disk (a new timeline point), then copies the chosen backup over the primary disk. Restore-as-new-VM stays available as today.
- Flags: none.

### Q4 — Idea 1: what a point captures
- Asked: disk + domain XML (selectable revert), disk + XML always together, or disk only?
- Captured: **Disk only**, as today. Backups and reverts never touch the domain XML or VM config.
- Flags: none.

### Q5 — Idea 1: "snapshot before risky change"
- Asked: how a pre-change snapshot should run (chained job, backup first, fire both).
- Captured: user rejected the hook: "Backups are done automatically that already existe, there should bem a option o the ui for the user to see al of the previous snaoshots (backups) and select one". **No pre-change snapshot hook.** Idea 1 is a per-VM timeline UI over the existing automatic and manual backups, where the user picks one to revert to.
- Flags: none.

### Q6 — Idea 1: timeline entry content
- Asked: what each entry shows (size, NFS share, missing-file warning, filters) and whether to keep "compare".
- Captured: user: "Each time line entry is a backup... we do not need any new tables Notingm.. its just a way os representing the backups". **No new tables or columns.** The timeline is a presentation of the existing `virsh_backups` rows for one VM (date, manual/automatic, NFS share). **No compare.** Anything not already stored (for example size) is either read live or left out.
- Flags: none.

### Q7 — Idea 1: revert defaults
- Asked: lock (a) safety backup = normal manual backup on the same NFS share as the chosen point, (b) running VM → "shut down and revert" only, (c) no MCP revert tool?
- Captured: **All three locked.**
- Flags: none. Idea 1 complete.

### Q8 — Idea 2: SMART collection and storage
- Asked: master polls and stores, slave pushes, or HH Support streams?
- Captured: **The master polls the existing `GetSmartInfo` RPC** for every disk on every connected host **every 6 h** and stores key attributes in a **master SQLite table keyed by disk serial** (not `/dev/sdX`). No proto change.
- Flags: none.

### Q9 — Idea 2: retention and disappeared disks
- Asked: keep for life, fixed window, or purge when the disk disappears?
- Captured: **Keep for life.** All samples are kept while the disk exists. A serial unseen for **90 days** becomes **"retired"** with its history kept (so a failed disk's curve stays visible). Manual purge from the UI.
- Flags: none.

### Q10 — Idea 2: forecasting method and health score
- Asked: rules + trends with a score, trends only, or a statistical model?
- Captured: **Rules + trends.** Score **0–100** from weighted rules on current critical counters (pending, reallocated, offline-uncorrectable, media errors, CRC) plus their growth over **7 and 30 days**. SSD/NVMe remaining life = **linear extrapolation of `percentage_used`**. Each score has a **plain-language reason**. No ML.
- Flags: exact weights and band thresholds → plan.

### Q11 — Idea 2: notifications
- Asked: master replaces the slave's repeating 6-h alerts, both deduped, or master adds and slave stays?
- Captured: **Master replaces slave.** Alerts fire only on change: the score drops into a worse band (**good → watch → at risk → critical**) or a critical counter increases. One notification per change via `nots.Notify` with a disk-health category. The slave's repeating 6-h SMART alerts (`StartSmartTestChecker`) are retired.
- Flags: none.

### Q12 — Idea 2: UI, no-SMART disks, MCP
- Asked: lock (a) score badge per disk in the existing SMART/host view + per-disk page with score/temperature/critical-counter/wear charts + "retired disks" list, (b) disks without SMART listed as "no SMART data" and never scored, (c) read-only MCP "list disk health" and "get disk health history", purge UI-only?
- Captured: **All three locked.**
- Flags: none. Idea 2 complete.

### Q13 — Idea 3: host power source
- Asked: pluggable with RAPL first, smart plugs only, or static estimate only?
- Captured: **Pluggable, RAPL first.** Per-host power source setting: **RAPL** (zero setup, shown as "CPU+RAM estimate"), **IPMI** (DCMI power reading), or **smart plug** over a local HTTP API (Shelly/Tasmota style). **v1 = RAPL + manual "fixed watts" fallback**; IPMI and plugs later.
- Context: no power reader exists in source today (no RAPL/IPMI/plug code).
- Flags: none.

### Q14 — Idea 3: splitting host power across VMs
- Asked: CPU share at sample time, split by allocation, or host level only?
- Captured: **CPU share at sample time.** Every minute the slave reads host watts and each VM's libvirt CPU-time delta in the same pass. **Idle watts → a "host" bucket**; the remainder is split by each VM's CPU share. **Only the result (Wh per VM per hour) is stored**, never raw VM usage, so this does not reintroduce a metrics history.
- Flags: how "idle watts" is determined (measured baseline vs. user-entered) → Q15. Open flag from Q1 resolved.

### Q15 — Idea 3: idle baseline
- Asked: auto-learned with override, user-entered only, or no idle bucket?
- Captured: **Auto, user can override.** Baseline = **p5 of host watts over the last 7 days**, recomputed daily. The user can pin a fixed value per host; the UI shows which one is active.
- Implication: needs a small host-level power record (for example per-host hourly Wh and hourly minimum watts). This is host power data, not a per-VM metrics history.
- Flags: exact host power storage granularity → plan.

### Q16 — Idea 3: tariff
- Asked: time-of-use periods, single flat price, or flat price per host?
- Captured: **Single flat price** (one EUR/kWh for the whole cluster).
- Flags: whether a price change re-prices past costs or applies from now on → plan (default: from now on).

### Q17 — Idea 3: power-schedule savings
- Asked: avg on-power × off hours, idle watts × off hours, or skip?
- Captured: **Avg on-power × off hours.** Per host: off hours in the schedule window × that host's **average watts over its last 7 days of "on" time** × price. Labelled **"estimated"**, shown per day and month, per host and in total.
- Flags: none.

### Q18 — Idea 3: retention, UI, MCP
- Asked: lock (a) hourly Wh per host and VM kept 2 years, VM rows keyed by name and purged on VM delete, (b) cluster energy dashboard (kWh/EUR today/month/year, per-host and per-VM tables, savings card) + "kWh / EUR this month" on each VM and host page, (c) read-only MCP "get energy usage", settings UI-only?
- Captured: **All three locked.**
- Flags: none. Idea 3 complete.

### Q19 — Idea 4: apply model
- Asked: additive (create + update), full declarative (delete what is missing), or create only?
- Captured: **Additive: create + update.** Apply creates missing resources and updates changed ones. Resources in HH but not in the file are left alone and listed as **"unmanaged"** in the plan. **Nothing is ever deleted** by import.
- Flags: none.

### Q20 — Idea 4: VMs in the export
- Asked: definition reusing an existing disk only, definition + empty disk, or no VMs in v1?
- Captured: **Definition + empty disk.** A VM entry is its definition (host, CPU/RAM, NIC, template, disk path and size, autostart, notes). On import, if the disk file is missing HH creates the VM with a **fresh empty disk** of the recorded size; if it exists, the existing disk is used.
- Implication: for disaster rebuild the plan should say clearly which VMs get an empty disk so the user can restore data from backups afterwards.
- Flags: none.

### Q21 — Idea 4: matching resources
- Asked: natural keys or HH-managed export ids?
- Captured: **Natural keys.** VM = name; proxy / redirect / 404 host = first domain; stream = incoming port + protocol; access list / automation / DNS alias = name; backup schedule = VM name; note = title. References between resources use these keys (for example proxy → access list by name). A key clash or ambiguity **blocks the plan**.
- Flags: none.

### Q22 — Idea 4: secrets
- Asked: placeholders never exported, encrypted bundle, or skip secret-bearing resources?
- Captured: **Placeholders, never exported.** The file holds named placeholders (for example `secret: dns/cloudflare`). The plan lists every missing secret; the user fills them in a form before apply, or that resource is skipped. Let's Encrypt certificates are re-requested; custom certificates must be re-uploaded. The export is safe to commit to git.
- Flags: none.

### Q23 — Idea 4: partial failure
- Asked: continue and skip dependants, stop at first failure, or all-or-nothing rollback?
- Captured: **Continue, skip dependants.** Apply order: access lists / upstreams → DNS → proxies / redirects / streams → VMs → backup schedules / automations / notes. Independent resources keep applying; dependants of a failed resource are skipped. **No rollback.** Result = per-resource report (created / updated / unchanged / failed / skipped). Re-running apply is idempotent and continues from the failure.
- Flags: none.

### Q24 — Idea 4: format, surface, automations, MCP
- Asked: lock (a) one YAML file, versioned schema `hyperhive/v1`, JSON accepted, (b) REST + UI flow (export download; upload → plan → fill secrets → apply), CLI and Terraform/OpenTofu later, (c) automations exported with script files inline, (d) read-only MCP "export config" and "plan import", apply UI/API only?
- Captured: **All four locked.**
- Flags: none. Idea 4 complete.

### Q25 — Idea 5: traffic on the graph
- Asked: live only (nothing stored), status only, or host edges only?
- Captured: **Live only, nothing stored.** While the graph is open, edge thickness shows current rates: host interfaces from existing host network data, VM NICs from **on-demand libvirt interface counter deltas**, streams from existing stream stats. No new persistence.
- Flags: none.

### Q26 — Idea 5: inferring proxy/stream → VM edges
- Asked: match by IP with unknown nodes, manual links, or match + manual override?
- Captured: **Match by IP, show unknowns.** Resolve the NPM forward host to an IP (DNS aliases first, then hostname), then match against VM IPs (DHCP leases, guest agent) and host IPs. Unmatched targets become an **"external / unknown"** node; nothing is hidden. Each edge records **how it was matched**.
- Flags: none.

### Q27 — Idea 5: build, page, interaction, MCP
- Asked: lock (a) built on demand by one master endpoint, nothing stored, refreshed by existing websocket events + slow poll, (b) new "Network map" page grouped by host with layer toggles (physical / VMs / services / DNS), filters (host / VM / service) and search, (c) node side panel (IP, MAC, ports, status, match method) with "open page", (d) read-only MCP "get network graph" (JSON)?
- Captured: **All four locked.**
- Flags: none. Idea 5 complete.

### Q28 — Completeness check
- Asked: anything not yet covered (priority, cross-idea dependencies, explicit out-of-scope)?
- Captured: **add a build priority**.
- Flags: none.

### Q29 — Build order
- Asked: smallest first, risk-reduction first, or list order?
- Captured: **Smallest first:** 1 Snapshot timeline → 2 Disk health → 5 Network graph → 3 Power and cost → 4 IaC.
- Flags: none. Grill complete.

## Open flags (pending input)

- Idea 2: score weights and band thresholds → implementation plan.
- Idea 3: host power storage granularity; VM moving host mid-hour → implementation plan.
