# Plan: Android Firebase setup and test notification

Generated: 2026-10-28. Complexity: medium. Status: implemented locally; debug APK and automated checks passed; real-device acceptance pending.

## Implementation amendment — 2026-10-28

The user confirmed `com.anonymous.hyperhive` as the stable Android package. The user cannot connect a physical device and requested continued development with runtime client-file import. Implementation therefore proceeds without claiming the Sprint 1 physical-device gate has passed; those checks remain required before release. See `notes/android-firebase-notifications.md` for build, runtime import, server provisioning and validation instructions.

## Objective and agreed scope

Ship one Android build containing the Firebase Messaging SDK, with no bundled `google-services.json`, Firebase project values, or server credentials. In Settings, the user imports their Android Firebase client configuration, activates notifications, and presses a small **Send test notification** button. Only that explicit action sends a push, only to the requesting device. Firebase sender credentials are configured on the HyperHive backend.

Android only. No automatic infrastructure alerts, topics, broadcasts, scheduling, notification history integration, iOS work, or live switching between projects. One active Firebase project per app installation; changing it requires disabling notifications and restarting the app.

## Inspected starting points

- Frontend: `/home/marques/projects/HyperHiveFront/HyperHive`, Expo 57 / React Native 0.86.2. No Firebase or notification dependency is declared.
- `components/navigation/AppSidebar.tsx` already contains the Settings dialog and notification controls. Native registration currently opens the browser registration page.
- `app.json` and `plugins/withAndroidUserCertificates.js` demonstrate the existing Expo config-plugin approach. No local native modules directory was found. Confirm the stable Android application ID before implementation; app.json does not currently declare one.
- Backend: `master/api/nots.go` owns notification-history routes; `master/api/api.go` contains browser push registration and a test handler that broadcasts. `master/nots/nots.go` implements Web Push. Do not reuse the broadcasting test action for this feature.
- Reuse `services/api-client.ts`, authentication, UI components, document picker, and existing API error handling. Preserve unrelated working-tree changes and existing browser push behavior.

## Sprint 1: Prove runtime initialization in a real Android build

Goal: demonstrate FCM token creation with configuration supplied after installation. This is a feasibility gate before building the full Settings flow.

### 1.1 Add the minimum native foundation

Locations: frontend `modules/firebase-notifications/` (new local Expo module), `plugins/withFirebaseNotifications.js` if needed, `app.json`, package manifests.

Use a local Kotlin Expo module wrapping the Android Firebase Messaging SDK directly. Add only required native dependencies and lifecycle integration. This avoids adding React Native Firebase plus custom startup overrides for a test-only feature. Pin compatible versions during implementation. Register a Messaging service, notification icon/channel, Android notification permission, and native application-start initialization from saved settings. Use Expo lifecycle hooks where sufficient; use a reproducible config plugin for generated manifest/build changes. Do not hand-edit disposable prebuild output.

Expose only configuration import/save, activation/disable, status, and current token operations. Store validated client configuration in app-private native storage accessible before JavaScript starts. Do not initialize Firebase when unconfigured. Initialize the default app with explicit FirebaseOptions, including sender ID; keep auto token initialization off until activated. Guard against repeated initialization.

Validation: build and install a development APK, not Expo Go. Confirm clean prebuild/build works without a Google Services file or Gradle processing plugin. Inspect the resulting resources for accidental project values. Fresh launch must not crash or register with Firebase before setup.

### 1.2 Prove configured startup and delivery prerequisites

Depends on 1.1. Load runtime configuration, obtain a token, restart the process, and verify initialization restores before background Messaging uses it. Use a real Play Services Android device. Confirm foreground handling can display a system notification through the same native notification channel. Reuse the SDK's background notification display path and avoid duplicate display.

Gate: proceed only after token retrieval and cold-start initialization work without bundled configuration. If the SDK or Expo lifecycle requires a different initialization hook, resolve it here and document evidence; do not silently introduce build-time project values.

## Sprint 2: Add a narrowly scoped backend sender

Goal: an authenticated request sends a fixed test notification to one supplied device token, with no persistent device registry required for this first version.

### 2.1 Configure server-only Firebase authentication

Locations: `master/nots/firebase.go` (new), existing backend startup/config integration, `master/go.mod` and `go.sum` only if necessary.

Use the existing Go OAuth2 dependency and FCM HTTP v1 with a fixed Google endpoint. Load service-account credentials from a server-side file through `GOOGLE_APPLICATION_CREDENTIALS`; use the project's established configuration/startup pattern. Keep keys outside source control, mobile storage, API responses, and logs. Expose no credential-upload endpoint. Missing credentials must leave the rest of HyperHive usable and return a clear unavailable response for testing.

Validate credential project identity and use bounded requests, OAuth token reuse, and sanitized errors. Never accept an arbitrary send URL or private key from the mobile request.

### 2.2 Add POST /notification/firebase/test

Locations: `master/api/nots.go`, authenticated route registration, `master/api/nots_test.go`, sender tests.

Depends on 2.1. Accept only a bounded device token and client project ID. Require normal authentication and reject project mismatch before sending. Send a fixed English title/body and Android channel ID to that token only. Apply a small per-user test cooldown using an existing limiter if available. Do not call SendGlobalNotification, save history, or add this sender to automatic event paths.

Return a message ID and an explicit **accepted by Firebase** result; do not claim delivery. Handle invalid tokens, disabled FCM API, credential failures, timeout, and rate limits without leaking secrets. Do not automatically retry an ambiguous send, which could duplicate the notification.

Validation: fake the outbound FCM transport; assert authentication, size limits, project mismatch, single-token targeting, fixed content, cooldown, and sanitized provider failures. No live Firebase credentials in tests.

## Sprint 3: Complete the Settings flow

Goal: import configuration, activate, and send a visible test from the installed app.

### 3.1 Add Android-only Firebase controls

Locations: `components/navigation/AppSidebar.tsx`, a small extracted Settings section if needed, `services/firebase-notifications.android.ts` with a safe non-Android counterpart, and focused tests.

Depends on Sprint 1. Use the installed document picker to import google-services.json at runtime. Validate JSON size/shape and choose the client whose Android package name matches the installed app; reject missing or ambiguous matches. Extract only app ID, project ID, API key, and sender ID. Never accept service-account JSON. Show the selected project and provide **Enable notifications** and **Disable** controls. Request Android 13+ permission on activation and report blocked permissions/channel settings with a route to system settings.

Disable clears the enabled flag and deletes the FCM token where possible; report failures accurately. For project replacement, save pending configuration and require a full app restart before initializing the new default app. Until restart, block test sends; do not mix the old token and new project.

Keep browser registration and history controls unchanged. No native Firebase import may break web or iOS builds.

### 3.2 Add the small Send test notification button

Depends on 2.2 and 3.1. Obtain the current token immediately before sending; use the existing authenticated API client. Disable the button while unconfigured, disabled, blocked, awaiting restart, signed out, or sending. On success show **Test accepted by Firebase**. On failure show a concise actionable message. The fixed test must appear as an Android system notification even while Settings is open.

No database token registration is needed yet: fetching the current token for each test avoids storing a future-use subscription system. Logout/backend changes cancel in-flight work and clear transient token references; future tests require the current session and matching backend project.

Validation: test parser failures and package selection, Settings readiness/error states, double-tap prevention, and correct API payload. Verify Android-only controls and native imports do not regress web.

## End-to-end acceptance and release

- Same APK configures successfully on two clean installations using different compatible Firebase projects, each with matching backend credentials.
- No setup or activation action sends a push. One button press sends one fixed message to the pressing device; another device receives none.
- Verify foreground, background, and process-recreated delivery. For background checks, press Test and immediately background the app; use a controlled test harness if needed to make timing repeatable. Android force-stop requires reopening and is not a supported delivery state.
- Verify permission denial, channel disabled, no network, missing credentials, project mismatch, malformed import, restart, configuration replacement, logout, and backend change.
- Run focused Go and frontend tests, TypeScript/web checks, clean Android prebuild/build, and physical-device checks. Inspect artifacts and logs for credentials/project values baked into the build.
- Document APK build/install, runtime import, server credential provisioning, and test steps. Release only after the feasibility gate and device delivery checks pass.

## Risks and rollback

Runtime Firebase initialization is supported by the SDK, but its interaction with Expo startup/background handling must be proven in Sprint 1. Firebase console app registration must match the APK package; API-key restrictions and backend project credentials must also match. FCM acceptance does not guarantee device delivery.

Rollback removes the Android Settings controls, local module/plugin, and new backend route/sender together, preserving browser push and notification history. Clear saved runtime Firebase settings/token during rollback if the native module remains installed. No database migration is planned.

## Official references

- Runtime options: https://firebase.google.com/docs/projects/multiprojects
- Android initialization: https://firebase.google.com/docs/reference/android/com/google/firebase/FirebaseApp
- Android message handling: https://firebase.google.com/docs/cloud-messaging/android/receive
- Server authentication: https://firebase.google.com/docs/cloud-messaging/auth-server
- Native Expo modules: https://docs.expo.dev/modules/overview/

References reviewed on 2026-10-28. Context7 is not available in this session; official documentation was consulted directly. This document is a plan, not evidence of successful build or delivery.
