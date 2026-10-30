# Notifications rework plan

Date: 2026-10-30. Status: implemented 2026-10-30 (local build and tests only; not deployed). Rules are global, not per user; finished operations push at low priority.

## Problem

Notifications fire from about 60 call sites in master and slave, with no severity, no dedup and no policy. All of them go through `nots.SendGlobalNotification(title, body, url, critical)` in `master/nots/nots.go` (slaves reach it via the `ExtraService.SendNotifications` gRPC), which saves history, sends Web Push to every subscription and, when `critical`, broadcasts a websocket `Notification` that opens a modal in the frontend.

### Noise sources

| Source | Problem |
|---|---|
| `master/info/info.go` `LoopNots` (every 10 min) | Stateless. Checks every CPU core separately (16 hot cores = 16 notifications every 10 min). Memory and disk warn at ≥70% and repeat forever. |
| `master/services/btrfs.go`, `master/services/nfs.go` | Success messages ("Add disk done", "ISO download done") are sent with `critical=true`, so they open a modal and send a critical push. |
| ISO download | Reported by both slave (`slave/nfs/nfs.go` started/succeeded/failed) and master (done/failed): 3–4 notifications per download. |
| Slave bootstrap (`slave/main.go`) | "X connected" and "Ensure VirtIO ISO" fire on every reconnect or re-exec. |
| SMART (`slave/smartdisk/helper.go`, every 6 h) and btrfs device stats (`slave/btrfs/stats.go`) | The same problem is sent again on every run, for every disk. |
| Firebase (`master/nots/firebase.go`) | Test sender only. The backend stores no Android device tokens, so real events never reach Android. |

Bug found: `CheckBtrfsLoop` in `slave/btrfs/stats.go` runs only once (no `for` loop).

## Phase 1: one interface (`master/nots/`)

```go
type Severity int // Info, Warning, Error, Critical

type Notification struct {
    Event    string   // stable key, e.g. "host.cpu.temperature", "btrfs.balance"
    Severity Severity
    Title, Body, URL string
    Machine  string
    DedupKey string   // e.g. "host.cpu.temperature:node1"
}

func Notify(ctx context.Context, n Notification) error
```

- `Notify` runs the policy check, then dedup/cooldown, then saves history in SQLite, then sends to each channel in parallel, each with its own timeout.
- Channel interface `Send(ctx, Notification) error`, implemented by `WebsocketChannel`, `WebPushChannel` and `FCMChannel`.
- Proto `extra.Notification`: add `severity`, `event` and `dedup_key` (fields 5–7). Keep `critical` so older slaves still work (`critical=true` maps to Error, otherwise Info). The slave gets `extra.Notify(...)` to match.
- `SendGlobalNotification` becomes a deprecated wrapper until every call site has moved.

## Phase 2: Android delivery

- New `firebase_devices` table with register/unregister endpoints (authenticated group). The app sends its token after login and on token refresh.
- `FCMChannel` maps severity to the existing Android channels (info/warn/error/critical, with the critical sound). It removes tokens that FCM reports as `UNREGISTERED`.

## Phase 3: alert state engine

- Metric alerts change state (ok → warning → critical) instead of being sent on every check. A notification goes out when an alert starts, a "resolved" message on recovery, and a reminder only after a long cooldown (e.g. 6 h).
- One alert per host, not per core: "node1 CPU 88°C (max core)".
- A dedup window for repeated events with the same `DedupKey` (SMART, btrfs device errors).
- State is in memory (process lifetime, mutex-protected). A master restart may send one alert again; that is acceptable.

## Phase 4: what notifies (proposed policy)

| Event | Severity | Push (web + Android) | Websocket |
|---|---|---|---|
| Slave lost connection | Critical | yes | modal |
| btrfs device errors, SMART failing, mount failed | Critical | yes, deduped | modal |
| CPU ≥85°C, disk ≥90% or ≥55°C, memory ≥95% (sustained) | Error | yes, when the alert starts | toast |
| Backup / migration / btrfs op / ISO / VM creation **failed** | Error | yes | toast |
| Warning thresholds (CPU ≥78°C, disk ≥80%) | Warning | no (history only) | toast |
| Op **succeeded** (backup, ISO, balance, scrub, migration) | Info | no | toast |
| "Started" messages, slave connected, VirtIO ISO ensured | none | no | — (log only) |
| Duplicate VM detected | Error | yes, deduped | toast |

Also remove the duplicate ISO download notifications (keep the master-side ones) and replace the Portuguese test notification text.

## Phase 5: user settings

- `notification_rules` table: event or category, enabled, minimum severity for push, optional quiet hours.
- Settings screen in the frontend, plus REST and MCP access to the rules.

## Phase 6: verification and docs

- Tests for the policy, dedup/state engine and each channel (with fakes).
- `go build -tags libvirt_dlopen ./...` and `go test -tags libvirt_dlopen ./...` in `api/`, `master/` and `slave/`, plus the frontend tests.
- Fix the `CheckBtrfsLoop` bug.
- Update SilverBullet `knowledge/projects/HyperHive/features/notifications/*`, the HTTP route inventory and the gRPC inventory.

## Open questions

1. Are the thresholds right for the hardware?
2. Should successful long operations (backup, migration) push to the phone, or only show in history?
3. Global settings, or per user?
4. Phase 5 settings UI now, or after the noise is fixed with hard-coded defaults?
