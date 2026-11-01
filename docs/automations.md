# HH Automations

HH Automations are user-written JavaScript/TypeScript scripts that run on the
master host as **root** with Node. They start from a manual RUN or from a
"Condition to run" expression over HH Support data.

Read this before changing `support/internal/*/automations*.go`,
`master/automations/`, `master/{api,services,db}/automations.go` or the
automation MCP tools.

## Split of responsibilities

| Concern | Owner | Storage |
| --- | --- | --- |
| Automation config (name, condition, enable, cooldown, for, timeout, rate cap) | HH Support | Support SQLite `automations` |
| Trigger state (armed, true-since, last fire, hourly fires, last error) | HH Support | Support SQLite `automations.state` |
| Global pause | HH Support | Support SQLite `automation_settings` |
| Script files, shared `package.json`/`node_modules`, SDK | master | `$HH_AUTOMATIONS_DIR` (default `/opt/hyperhive/automations`) |
| Runs, output, retention, notifications | master | master SQLite `automation_runs` |

The master is the only public facade: the frontend and MCP call `/automations/*`
on the master. The master reads/writes config through its Support client
(`SUPPORT_URL` + `SUPPORT_PROXY_SECRET`). Support calls the master back on
`/support-internal/automations/*` with the same secret in `X-HH-Support-Key`
(`SUPPORT_MASTER_URL`, default `http://127.0.0.1:<API_PORT>`).

## Condition to run

Expressions use [expr](https://expr-lang.org) and must return a boolean.

- `app('db')` — an App by name (case-insensitive) or ID. Fields: every stream
  name with its latest value, plus `health` (`healthy|degraded|unhealthy|unknown`),
  `connectivity` (`online|offline|unknown`), `health_message` and `streams`
  (map of the same stream values, for stream names that clash with the fields above).
- `app('db', 'instance-1')` — the same for one instance.
- Stream names that are not identifiers: `app('db')['disk.used']`.
- Example: `app('db').cpu >= 95 || app('web').connectivity == 'offline'`.

A stream value is **stale** when the participant (instance or direct sender)
that produced it is offline; stale and missing values are absent. Any
evaluation error (unknown App, missing value, non-boolean result) evaluates to
**false** and is shown as `state.last_error`.

Conditions are evaluated after every ingest that touches a referenced App and
every second (heartbeat expiry, `for` timers, cooldown expiry).

### Firing rules

- Edge-triggered: fires when the condition is true and the automation is
  *armed*; firing disarms it; it re-arms when the condition is false (or errors).
- `for_seconds`: the condition must stay true that long first.
- `cooldown_seconds`: minimum gap after the previous fire; a true edge during
  cooldown fires when the cooldown ends if it is still true.
- `max_runs_per_hour`: when a condition fire would exceed it, Support disables
  the automation (`state.disabled_reason`) and sends an `automation.disabled`
  notification.
- Global pause stops condition fires only; manual runs still work.
- One concurrent run per automation: the master rejects a second fire with 409.
- New automations start **disabled**.
- Enabling, resuming from pause or changing the condition takes a *baseline*:
  the next evaluation only records the current value, so a condition that is
  already true does not fire until it goes false and true again.

## Run environment

`node --experimental-strip-types $DIR/.hh/runtime.mjs scripts/<id>/<entry>`,
cwd `scripts/<id>`, as root, in its own process group with a parent-death
signal. The entry is `main.ts`, else `main.js`, else `main.mjs`.

Globals available to scripts (typings in `GET /automations/sdk/hh.d.ts`):

- `ctx` — `{ runId, automationId, automationName, trigger: 'manual'|'condition', condition, snapshot }`.
- `hh` — generated client for the master REST API: `hh.request(method, path, { query, body })`
  plus one function per master route, e.g. `hh.api.virsh.getGetallvms()`. Path
  parameters become arguments. A per-run token is injected and revoked when the run ends.
- `log.info|warn|error(...args)` — writes to the run output.
- A default-exported function is awaited: `export default async function ({ ctx, hh, log }) {}`.

Scripts need Node.js ≥ 22.6 (TypeScript type stripping; no enums or
namespaces). Exit code 0 is success. The default timeout is 600 s (per automation,
`timeout_seconds`); on timeout the whole process group is killed. Output
(stdout+stderr) is capped at 1 MiB per run. Failure, timeout and kill send an
`automation.failed` notification. On master restart, `running` runs become
`interrupted` and their surviving process groups are killed.

Shared dependencies: one `package.json` and `node_modules` in `$HH_AUTOMATIONS_DIR`;
`POST /automations/package/install` runs `npm install` as a run with
`trigger: "npm-install"`. Node and npm are installed with `dnf` when missing.

## HTTP API (master, authenticated)

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/automations` | `{ paused, node: { available, version }, automations: [Automation] }` |
| POST | `/automations` | Create `{ name, description?, condition?, cooldown_seconds?, for_seconds?, timeout_seconds?, max_runs_per_hour? }` → 201 Automation (disabled, with a starter `main.ts`) |
| GET | `/automations/{id}` | Automation |
| PUT | `/automations/{id}` | Replace config `{ name, description, enabled, condition, cooldown_seconds, for_seconds, timeout_seconds, max_runs_per_hour }` → Automation |
| DELETE | `/automations/{id}` | Kill its run, delete config, files and runs (UI only, never MCP) |
| POST | `/automations/{id}/run` | Manual run → 202 `{ run_id }`; 409 when already running |
| POST | `/automations/evaluate` | `{ condition }` → `{ value, error, snapshot }` without firing |
| GET | `/automations/catalog` | `{ apps: [{ id, name, connectivity, health, instances: [id], streams: [{ name, type, unit?, label?, value? }] }] }` — what a condition can read, for editor autocomplete; `value` is the current App-wide value, absent when missing or stale |
| GET | `/automations/settings` | `{ paused, retention_days, retention_count }` (defaults 30 days, 200 runs per automation) |
| PUT | `/automations/settings` | Same body; `paused` stops condition fires |
| GET | `/automations/{id}/files` | `{ files: [{ path, size, modified_at }] }` |
| GET | `/automations/{id}/files/content?path=` | `{ path, content }` (UTF-8, ≤ 1 MiB) |
| PUT | `/automations/{id}/files/content?path=` | `{ content }` → `{ path, size, modified_at }` |
| DELETE | `/automations/{id}/files/content?path=` | Delete one file (UI only) |
| POST | `/automations/{id}/files/rename` | `{ from, to }` → `{ path, size, modified_at }`; never overwrites (409 if `to` exists), creates parent folders (UI only) |
| GET | `/automations/package` | `{ content }` of the shared `package.json` |
| PUT | `/automations/package` | `{ content }` (must be a JSON object) |
| POST | `/automations/package/install` | 202 `{ run_id }` |
| GET | `/automations/sdk/hh.d.ts` | TypeScript declarations for Monaco (`text/plain`) |
| GET | `/automations/runs?automation_id=&limit=` | `{ runs: [RunSummary] }` newest first (limit ≤ 200, default 50) |
| GET | `/automations/runs/{run_id}` | RunSummary + `output` |
| POST | `/automations/runs/{run_id}/kill` | 202; kills the process group |

```jsonc
// Automation
{
  "id": "…", "name": "…", "description": "", "enabled": false,
  "condition": "app('db').cpu >= 95",
  "cooldown_seconds": 0, "for_seconds": 0, "timeout_seconds": 600, "max_runs_per_hour": 10,
  "created_at": "RFC3339", "updated_at": "RFC3339",
  "state": {
    "condition_value": true, "last_evaluated_at": "RFC3339|null", "last_error": "",
    "armed": true, "true_since": "RFC3339|null", "last_fired_at": "RFC3339|null",
    "last_fire_error": "", "disabled_reason": "", "snapshot": { "db.cpu": 97 }
  },
  "running_run_id": "…|null",
  "last_run": "RunSummary|null"
}
// RunSummary
{
  "id": "…", "automation_id": "…", "automation_name": "…",
  "trigger": "manual|condition|npm-install", "condition": "", "snapshot": {},
  "status": "running|succeeded|failed|timeout|killed|interrupted",
  "exit_code": 0, "started_at": "RFC3339", "ended_at": "RFC3339|null",
  "output_truncated": false, "error": ""
}
```

Live updates on the master `/ws` websocket:

- `{ "type": "AutomationRun", "data": "<RunSummary JSON>", "extra": "<run_id>" }` on start and end.
- `{ "type": "AutomationLog", "data": "<output chunk>", "extra": "<run_id>" }` while running (batched ~250 ms, stops after the 1 MiB cap).

Errors are plain text (`http.Error`) with 400 validation, 404 unknown, 409
conflict (already running), 502/503 when Support is unreachable.

## MCP

Tools (area `automations`): low `list_automations`, `get_automation`,
`evaluate_automation_condition`, `list_automation_runs`, `get_automation_run`,
`list_automation_files`, `read_automation_file`, `read_automation_package`;
medium `create_automation` (always disabled); high `update_automation`
(partial update, prior config saved), `write_automation_file` (prior content
saved), `write_automation_package`, `install_automation_packages`; very high
`run_automation`. There are no delete or kill tools; settings and SDK typings
are UI-only.

## Frontend

HyperHiveFront `app/automations.tsx`. In the sidebar, "HH Automations" is the
first child of HH Support and expands into one entry per automation (store
`hooks/useAutomationNavigation.ts`). With nothing selected the page shows an
overview grid and the global settings. Selecting an automation opens a workspace:
on web at 1100 px or wider it is a full-height IDE (configuration inspector on the
left, file explorer and Monaco editor filling the rest, resizable Output/Runs panel
at the bottom); narrower screens and native stack the same parts in one column.
The "Run when" field (`components/automations/condition-input.tsx`, logic in
`utils/condition-assist.ts`) loads `/automations/catalog` (refreshed on focus after
30 s) and suggests, as you type, App names, instances, fields and streams (with type
and current value), comparisons that fit the type (number, text, boolean, the
`health`/`connectivity` values) and enum values inside quotes. Stream names that are
not identifiers or clash with a field are inserted as `['name']` / `.streams['name']`.
Web keys: ↑/↓, Enter or Tab, Esc. A "Variables" panel lists and filters every App's
fields and streams and inserts one at the cursor. Live hints flag unknown Apps,
instances or streams (with a "did you mean"), comparisons with the wrong literal type,
invalid enum values and streams with no current value. They are advisory; the server
still decides.
Unsaved config or code edits prompt before switching automations or leaving the
page. Monaco is web-only, loads from cdn.jsdelivr.net and follows the app theme.
The list polls every 5 s and follows `AutomationRun`/`AutomationLog` websocket
messages.

## Limits

- Scripts run as root with full host access; there is no sandbox, secrets
  store, cron/webhook/master-event trigger or code revision history.
- Run output is stored when the run ends; a master crash loses the output of
  in-flight runs (they become `interrupted`).
- Support restart keeps trigger state (persisted on change) but not the last
  evaluation snapshot. The in-memory latest-value cache is rebuilt from samples.
