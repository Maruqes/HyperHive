# HH Automations: Grill / Discovery Notes
Date: 2026-10-31 · Goal: Define HH Automations (custom code in HH Support, web IDE, automatic triggers, full HyperHive access).

## Summary / key decisions
HH Automations = user-written JS/TS scripts that HH Support triggers and the master executes as root.
- **Where/as whom:** node process on the master host, as **root** (Q1, Q2).
- **Who:** the single HH account (behind VPN), no extra re-auth (Q3). **MCP** may list/read/run/create/edit, never delete (Q14).
- **HH access:** generated TS SDK over master REST, covering the majority of endpoints, with an auto-injected short-lived token (Q4, Q5).
- **Triggers (MVP):** a RUN button plus a "Condition to run" expression over **HH Support data only** (app streams/samples, health, connectivity). Conditions can reference any app: `app('db').cpu >= 95` (Q6, Q8).
- **Firing:** edge-triggered false->true, re-arms on false, optional cooldown + "for" duration, one concurrent run per automation (Q7). Missing/stale values evaluate false and the error shows in the UI. Evaluated on ingest and on heartbeat expiry (Q16).
- **Split:** Support stores config, evaluates conditions and keeps trigger state, then calls a new master endpoint. Master spawns the process, stores scripts and run logs, and streams output (Q9).
- **IDE:** Monaco embedded in HyperHiveFront web only, with SDK typings, Run button and live logs (Q10).
- **Deps:** one shared package.json / node_modules (Q11).
- **Run:** `ctx` (trigger, condition, value snapshot, run id) + `hh` client + logger. 10 min default timeout (configurable), process-group kill, exit code = result (Q12).
- **Results:** run history (output capped ~1 MB, retention) + live logs + notification on failure/timeout (Q13).
- **Safety:** enable toggle (off by default), global pause, Kill, hourly rate cap with auto-disable + notify (Q15).
- **Out of MVP:** cron, master system events, webhooks, secrets store, code revision history, restart semantics (Q17).

## Q&A log

### Q1 — Execution location
- Asked: Where do automation scripts execute?
- Captured: **On the master host** (as a host process, raw host access), chosen over sandbox container or embedded interpreter.
- Flags: Implies scripts run with master-host privileges; isolation/user/identity must be settled next.

### Q2 — OS identity
- Asked: Which OS user do scripts run as?
- Captured: **Root** (same rights as master). Rejected unprivileged user and per-script toggle.
- Flags: Security risk accepted by user: a script can break master, DB, firewall. Authorization to create/edit scripts becomes equivalent to root on master -> needs strict auth decision.

### Q3 — Authorization
- Asked: Who may create/edit/run automations?
- Captured: "there is only 1 account in HH, and is protected by a vpn.. so the normal user should be able to do it". Normal logged-in session is enough; no re-auth.
- Flags: MCP/API token access to script editing not explicitly decided -> user (later).

### Q4 — HyperHive access from scripts
- Asked: How do scripts drive HyperHive?
- Captured: **SDK over REST**: client library generated from the same tool catalog as MCP, calling master REST with an auto-injected short-lived token. Same handlers/validation/logs as UI and MCP.
- Flags: SDK language(s) -> next question.

### Q5 — Script language
- Asked: Which language(s)?
- Captured: "js/ts with node (we should have the majoraty of endpoints mapped)". JS/TS on Node; the SDK should cover the majority of master REST endpoints (not just MCP tools).
- Flags: SDK generation source (MCP catalog vs OpenAPI/route table) to settle since coverage must exceed MCP.

### Q6 — Triggers
- Asked: Which triggers in v1?
- Captured: Manual **RUN button** + condition-based runs. "on HH suport we have variables on apps if i say the condition to run is (var>=95) it should run". A dedicated main field called **"Condition to run"**. MVP: conditions **only on HH Support data** (no cron, no master system events, no webhook yet).
- Code context: HH Support "variables" = app **streams** (name/type/unit) with ingested **samples** (JSON value, per app and optional instance), plus app/instance `health` and `connectivity` (heartbeat).
- Flags: cron / HH system events / webhooks deferred post-MVP.

### Q7 — Firing semantics
- Asked: How often does a sustained-true condition fire?
- Captured: **Edge-triggered + cooldown**: fire on false->true, re-arm only after false; optional cooldown and optional "for" duration; no concurrent runs of the same script.

### Q8 — Condition scope
- Asked: What can the condition reference?
- Captured: **Any app, qualified** (e.g. `app('db').cpu >= 95 || app('web').health == 'down'`). Cross-app conditions; automation is not bound to one app.
- Flags: Expression engine choice (safe evaluator, e.g. expr-lang) and dependency tracking (re-evaluate when any referenced app/stream changes) -> design. Instance addressing syntax -> design.

### Q9 — Component split
- Asked: Support is Docker, scripts run as root on master host; who does what?
- Captured: **Support decides, master runs.** Support stores automation config + evaluates conditions on ingest + keeps trigger state; on fire it calls a new master endpoint across the /api/support boundary. Master spawns node as root, records run log, streams output. Script files live on master host.
- Flags: Auth of Support->master "run" call (Support becomes able to trigger root code) -> design.

### Q10 — IDE
- Asked: How is the IDE delivered?
- Captured: **Monaco embedded** in HyperHiveFront web (hidden on Android/native): per-automation file tree, SDK TS types for autocomplete, Run button, live log pane, saves via master API. No code-server.

### Q11 — Dependencies
- Asked: npm dependency model?
- Captured: **Shared node_modules**: one global package.json for all automations (plus HH SDK).
- Flags: UI for editing the shared package.json + install trigger/log -> design. Upgrading a shared dep can break other automations (accepted).

### Q12 — Run context and limits
- Asked: What does a run receive / what limits?
- Captured: `ctx` (trigger type manual|condition, condition text, snapshot of referenced values, run id), preconfigured `hh` SDK client, logger. Per-run timeout (default 10 min, configurable per automation), kill whole process group on timeout, capture stdout/stderr, exit code = success/failure.

### Q13 — Results and failure handling
- Asked: What happens to results?
- Captured: Run history (trigger, start/end, exit code, output capped ~1 MB, retention by days/count), live logs over websocket, HH notification on failure/timeout via existing notification rules.

### Q14 — MCP access
- Asked: Should MCP get automation tools?
- Captured: **Full write via MCP**: list, read, run, create and edit scripts/conditions (AI agents effectively get root on master; accepted). Still no delete tools (MCP absolute rule).

### Q15 — Safety controls
- Asked: Runaway protection?
- Captured: Per-automation enable toggle (new automations start disabled), global "pause all", Kill button for running runs, rate cap of N condition-runs/hour per automation -> auto-disable + notify.

### Q16 — Missing/stale data
- Asked: Behavior when referenced app/stream is missing or stale?
- Captured: **Treat as false**, surface the evaluation error in UI. Outages are expressed explicitly (`app('db').connectivity == 'offline'`). Evaluate on sample ingest and on heartbeat expiry.

### Q17 — Completeness backstop
- Captured: "Done, wrap up". Secrets, code versions and master-restart behavior are not in MVP.

## Open flags (pending input)
- Master-restart behavior for in-flight runs (recommend: kill + mark interrupted) -> design
- Support->master trigger endpoint auth (Support can now cause root execution) -> design
- Shared package.json editing UI + install log -> design
- SDK generation source to reach "majority of endpoints" -> design
