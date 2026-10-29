# Plan: Settings notification type tests

Generated: 2026-10-29. Scope: manual Firebase notification tests on the configured Android device only. This is the implementation plan for the current phase; `notification-behavior-plan.md` remains the broader future proposal.

## Outcome

Settings → Notifications offers four buttons: **Test info**, **Test warning**, **Test error**, and **Test critical**. Each sends one real Firebase message to the current device, clearly labelled as a test. Info is deliberately sendable here even though routine info push would normally be off.

Reuse existing configuration, enable/disable, connection validation, authentication, request cancellation, cooldown and receipt diagnostics. No database migration, token registry, infrastructure event publication, global broadcast, notification-center redesign or incident lifecycle is needed.

## Requested phone behavior

| Type | FCM delivery priority | Android channel importance | Default sound/vibration | Presentation |
| --- | --- | --- | --- | --- |
| `info` | Normal | Low | Neither | Quiet notification |
| `warn` | Normal | Low | Neither | Quiet warning notification |
| `error` | High | Default | Sound and vibration enabled | Notification with sound when permitted; no heads-up requested |
| `critical` | High | High | Distinct notification sound and vibration enabled | Heads-up when permitted; lock-screen notification |

Use four stable, separate channel IDs so Android exposes independent controls. Use the system notification sound initially; critical can use a distinct bundled sound only if an existing suitable asset exists, otherwise use the default and distinguish it by channel importance and vibration. No new sound dependency or asset-generation work is required.

In sound mode, error/critical request sound and vibration. In vibrate mode, they follow Android's vibration behavior without forcing sound. In silent mode, they remain quiet. All types respect DND, notification volume, app permission, channel overrides and lock-screen privacy settings. Device/OEM settings determine final behavior; do not promise screen wake or heads-up display. Label Settings descriptions as defaults.

This phase does not request DND policy access, override volume, use alarm audio, open full-screen activities or force the app open while locked. Those remain separate optional capabilities. Tapping a notification opens the app normally; protected content still requires authentication. Use generic test text on the lock screen.

## Increment 1: Typed single-device test endpoint

Files: `master/api/firebase_notifications.go`, `master/nots/firebase.go`, and their existing `_test.go` files.

- Add optional `severity` to `POST /notification/firebase/test`; accept exactly `info`, `warn`, `error`, `critical`. Missing severity retains the current legacy test behavior/channel so installed older clients keep working. Reject unknown or invalid values before contacting Firebase.
- Keep `/notification/firebase/check` a credential/token validation operation with no delivery and its existing request shape. Avoid accidentally accepting severity there through the shared handler.
- Update sender interface, live sender, test doubles and all callers together. Keep fixed server-owned test content; the request cannot choose arbitrary text, destination URLs, channel IDs or recipients beyond the existing token/project contract.
- Include type in fixed title/body, selected Android channel ID and FCM data for native foreground handling. Apply the table's transport priority. Keep notification payload support for Android background system display.
- Retain per-user 30-second send cooldown across all four types; expose retry timing for the Settings countdown. No automatic sending/retries or rate-limit bypass.

Demo/validation: inspect fake-transport payloads for each type and legacy sends; assert invalid severity never dispatches, dry-run never sends, and the same user cannot bypass cooldown by changing severity. Retain project mismatch, bounded input, authentication and sanitized-provider-error tests.

## Increment 2: Android channels and consistent display

Files under frontend `HyperHive/modules/firebase-notifications/android/src/main/java/expo/modules/firebasenotifications/`: `RuntimeFirebase.kt`, `TestMessagingService.kt`, `FirebaseNotificationsModule.kt`; update the existing Kotlin tests. Update shared status types and Android service bindings if their shape changes.

- Create all four channels on activation/startup before a background test can arrive; preserve the legacy channel. Existing channel choices must not be deleted/recreated or reset on launch.
- Preserve the user's existing disabled notification choice when introducing channels; do not use new channel IDs to work around a previously blocked legacy channel. Explain channel-specific settings where needed.
- Resolve validated severity/channel consistently for foreground native rendering and backend-generated background notifications. Unknown native payloads fall back safely to the legacy test behavior, never critical.
- Use matching pre-Android-8 notification priority/sound/vibration defaults where supported. On Android 8+, channels own the presentation.
- Report enabled/blocked state for each test channel rather than letting one blocked channel disable every type. Keep app-wide permission and enabled state as shared prerequisites.
- Add a normal notification tap intent to open the app, without full-screen or lock-screen bypass flags. Ensure one visible notification per message; retain message-ID receipt tracking without equating receipt with audible display.

Demo/validation: small Kotlin checks cover mapping, safe fallback and channel defaults. Android build verifies native integration. Physical-device checks below validate actual display; unit tests cannot prove OS behavior.

## Increment 3: Settings test buttons and feedback

Frontend files: `HyperHive/components/navigation/FirebaseNotificationSettings.android.tsx`, `HyperHive/services/firebase-notifications.ts`, `HyperHive/services/firebase-notifications.android.ts`, and `HyperHive/tests/firebase-notifications.test.js`. Keep the non-Android Settings fallback unchanged.

- Replace the single send button with four labelled buttons using existing house components. Show a short behavior description for each; no icon/color-only distinction.
- Reuse the existing send action with a severity argument. Keep client/backend validation prerequisites, busy protection, unmount/logout/backend-change cancellation and current-project checks.
- Show a shared cooldown countdown and disable test sends until it ends; connection validation keeps its separate cooldown. Honor backend 429/Retry-After rather than suggesting that failed or unknown-acceptance requests are safe to resend immediately.
- Name the selected type in acceptance, waiting and receipt feedback. A matching message ID confirms this app received that message, not that a sound played. Explain that background system display may not produce the foreground receipt callback.
- Explain blocked channels individually and provide the relevant Android settings link. Keep configuration/check controls usable when a type's channel is blocked; do not require an enabled legacy channel to test a new enabled channel.

Demo/validation: rendered tests verify the four payloads, readiness/blocked-channel behavior, shared cooldown, acceptance-versus-receipt wording and cancellation on logout/backend changes. Run existing focused Jest and Go tests plus TypeScript checking; report unrelated failures without expanding scope.

## Increment 4: Deliver an APK and collect user feedback

The implementing agent must produce an installable standalone APK with bundled JavaScript after the implementation and local checks pass. Follow the existing Android setup guide; do not deliver a development APK that requires Metro. Verify the embedded JavaScript bundle, application ID, target architecture and signing configuration, and explain whether the APK can update the user's installed build or requires a different installation procedure. Do not uninstall the user's app or erase configuration automatically.

Provide the user with a clickable absolute path to the APK, identify its build/signing limitations, and give short installation and test instructions. Ask the user to install it and report results for each of the four buttons, including phone model/Android version, foreground/background/locked state, sound/vibrate/silent/DND mode, and any unexpected sound, vibration, display or tap behavior. Request feedback explicitly; do not stop at a successful build or assume silence means acceptance.

Record reported results separately from agent-verified results. Fix reported issues within this scope, rerun affected checks, produce a replacement APK and request another test when needed. If the user cannot test immediately, report **APK delivered; user feedback pending**. Physical-device acceptance remains incomplete until the user reports results and any blocking issues are resolved.

Demo/validation: the standalone APK is accessible to the user, artifact checks pass, installation/test instructions are delivered, and user feedback is recorded or explicitly pending.

## Physical-device acceptance

After installing an APK with bundled JavaScript, check all four types in foreground and background, then with screen locked. Check sound, vibrate, silent and DND modes; confirm the actual results against channel settings rather than promising overrides. Verify channel-specific blocking, app-wide denial, tap-to-open, no duplicate notifications, and process restart. Test normal-priority delays under device idle; do not describe a delay as confirmed delivery failure.

Retain the setup guide's existing commands for Go/Jest, Android build and standalone APK inspection. Update `notes/firebase-android-setup.md` with the four buttons, cooldown and device results when implementation is complete. Do not claim delivery or sound verification without a physical device.

## Compatibility and rollout

Deploy the updated backend before the new client because old strict request parsing rejects a new severity field. An older backend should produce a clear update-required message; never silently send the wrong type. Keep the legacy endpoint behavior/channel for older apps. Rollback disables/removes the new test UI while retaining existing channels and configuration; no stored infrastructure data changes.

## References

- https://firebase.google.com/docs/cloud-messaging/android-message-priority — delivery priority is separate from display importance.
- https://developer.android.com/reference/android/app/NotificationChannel — user-controlled sound, vibration, importance and DND behavior.
- https://source.android.com/docs/core/permissions/fsi-limits — special full-screen access, outside this phase.

Implementation status: increments 1–3 are implemented locally. Increment 4 produced `/home/marques/projects/HyperHiveFront/HyperHive/android/app/build/outputs/apk/release/app-release.apk`; artifact checks confirmed the bundled JS, package ID and ARM64 target. The APK uses the local debug signing key, not a production key. Go, Jest and Android module tests passed on 2026-10-29. TypeScript checking reports the pre-existing unrelated `strokeWidth` error in `app/dashboard.tsx:822`. Physical-device checks and user feedback remain pending; no live Firebase delivery or sound behavior is claimed. Backend deployment is also pending and must precede use of typed buttons against the intended backend.
