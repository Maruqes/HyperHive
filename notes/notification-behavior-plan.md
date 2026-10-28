# Notification behavior proposal

Date: 2026-10-28. Status: proposed; the user confirmed coverage of both in-app notifications and Firebase push with severity-based defaults. The specific policies below await product agreement. No runtime changes are included.

## Model and defaults

Keep urgency (`severity`) separate from what happened (`eventType`, such as `backup.failed`) and outcome (`success`, `failure`, `recovered`, where applicable). Use four severity values; debug belongs in logs.

| Severity | Meaning and example | In-app behavior | Push default |
| --- | --- | --- | --- |
| `info` | Routine change or success: backup completed | History; brief toast for the initiating user's completed action | Off; optional normal-priority silent delivery |
| `warn` | Degraded or approaching a limit: sustained high disk usage | History and warning indicator; no modal | On, normal delivery priority, silent channel |
| `error` | Operation failed or a resource is unavailable: scheduled backup failed | History, error indicator and actionable detail; no blocking modal | On, high delivery priority, channel with sound |
| `critical` | Immediate intervention needed to protect data or restore essential service: storage pool unavailable | Prominent persistent incident banner and history, with affected resource and next action | On, high delivery priority, heads-up channel with sound/vibration |

All push behavior is subject to permission, OS settings, channel settings and connectivity. Severity cannot guarantee delivery or override Do Not Disturb. Never force navigation, play looping alarms or open full-screen UI. Show a text label and icon as well as color.

Example classifications are contextual: one failed backup is normally error; losing every usable backup can be critical. High CPU alone is not critical. Resource thresholds must include duration and recovery hysteresis; retain current thresholds until separately reviewed.

## Incident behavior

- Persist notifications independently of push availability. Deliver only to users authorized for the affected backend/resource; the current global broadcast cannot be assumed suitable for device routing.
- Identify an incident by backend, resource and rule/event type. Repeated samples update last-seen time and occurrence count without creating another notification or push.
- Notify immediately on escalation. Acknowledge means a user has taken ownership; it silences reminders, not new severity escalation. Read/unread and resolution remain separate.
- Proposed default: one reminder after 30 minutes for an unacknowledged, still-critical incident; no endless reminders. Errors and warnings do not repeat automatically. Snooze suppresses reminders for the chosen interval, while escalation still alerts.
- Resolve only after confirmed recovery. Send one silent recovery push if an opening/escalation push was eligible for that recipient and push remains enabled; otherwise update in-app history only. Do not label provider acceptance as confirmed receipt.
- On startup/reconnect, fetch current incident state and history instead of replaying every old push. Short disconnects and flapping measurements need rule-specific debounce.
- Clicking opens the correct backend/resource after authentication and authorization. A deleted or inaccessible resource gets an explanatory fallback. An OS dismissal does not acknowledge or resolve the incident.
- Foreground delivery updates the center/banner once; prevent duplicate local and system notifications. Routine activity must not interrupt active work.
- Allow per-user push toggles for severity and optional quiet hours. Respect critical opt-out too; any quiet-hours exception for critical requires an explicit setting. Keep sensitive details out of lock-screen previews by default.

## Current implementation constraints

Source inspection found `critical bool` in `master/db/nots.go` and `master/nots/nots.go`. Both successful and failed Btrfs operations can pass true. `SendGlobalNotification` persists history, broadcasts critical websocket events, and attempts Web Push for all subscriptions regardless of severity. Save failures currently do not stop delivery.

Firebase in `master/nots/firebase.go` exposes test/check delivery to a supplied token. `notes/android-firebase-notifications.md` explicitly describes a single-device test without infrastructure publication or persisted tokens. The native `TestMessagingService.kt` uses the test channel. Production device registration, recipient routing and real event delivery are prerequisites, not already-completed features.

## Implementation increments

1. **Typed contract and compatibility.** Add validated severity/event type to persistence and API/RPC/push contracts; update `master/db/nots.go`, `master/nots/nots.go`, `master/extra/extra.go`, `api/proto/extra.proto`, generated bindings and slave callers. Review every notification producer in `master/services`, `master/info` and slaves. Retain deprecated `critical` compatibility for older clients. Preserve old history as legacy urgency: the boolean cannot reliably reconstruct severity. Demo: new levels round-trip and legacy clients/history remain readable. Test validation, migration and representative producer classifications.
2. **Production routing and delivery.** Extend the Firebase test foundation with authenticated device registration/token refresh/removal, backend/project/account binding, authorized recipients, preferences and a persisted delivery queue with bounded retry and invalid-token cleanup. Keep test sends isolated. Apply the same policy to Web Push and Firebase; add stable event IDs, timestamps, expiry, resource targets and severity-specific Android channels. Demo: authorized devices receive one permitted event; disabled, stale or cross-backend registrations do not. Test routing, persistence failure, retries, expiry and deduplication. Provider acceptance is not receipt.
3. **In-app presentation.** Update frontend notification history, `components/SocketNotificationModalListener.tsx`, websocket payload handling, Firebase services/native module and `master/static/sw.js`. Identify history consumers before implementation. Replace routine notification modals with toasts/center updates and add a persistent critical banner. Demo: all severities render with accessible labels; foreground/background clicks reach the authorized target without duplicates. Verify on a physical Android device with blocked channels, logout, backend changes and offline recovery.
4. **Incident lifecycle.** Add stable incident IDs and persisted occurrence/state transitions plus per-user read, acknowledgement and snooze state. Add deduplication, recovery confirmation, escalation and the single critical reminder. Demo: repeated samples produce one incident; escalation alerts; reading does not resolve; recovery closes it. Test restart safety, flapping, concurrent updates and reminder cancellation.

Roll out production push only after recipient isolation and device lifecycle checks pass. Keep the existing test endpoint and legacy contract during migration; disable new dispatch if rollback is needed without deleting notification history. Do not enable repeated reminders before lifecycle state is durable.

## Platform references and validation boundary

- https://firebase.google.com/docs/cloud-messaging/android-message-priority — FCM normal/high controls delivery timing; high priority is for time-sensitive visible notifications.
- https://developer.android.com/develop/ui/compose/notifications/channels — Android channels control interruption; users can change settings, and an existing channel's importance cannot be changed programmatically.

Source review and policy design only. No tests, builds, deployments or live delivery checks were run for this proposal.
