For the current credential setup, manual input fields, connection diagnostics and receipt checks, use [Firebase Android setup](firebase-android-setup.md). The guide below includes the original build history; later validation and workflow changes are documented in that setup guide.

# Android Firebase test notifications

This feature adds a manual, single-device Android push test. It does not publish infrastructure events or store device tokens in the database.

## Build and install

For direct installation on a phone, use a **standalone APK with bundled JavaScript**. The earlier `app-debug.apk` depends on Metro and can remain on the splash screen when the development server is unreachable. It is for connected development.

Build the standalone ARM64 test APK after prebuild:

```sh
cd /home/marques/projects/HyperHiveFront/HyperHive/android
NODE_ENV=production JAVA_HOME=/opt/android-studio/jbr ANDROID_HOME=/home/marques/Android/Sdk ./gradlew :app:assembleRelease --max-workers=1 -PreactNativeArchitectures=arm64-v8a
```

Output: `android/app/build/outputs/apk/release/app-release.apk` within the frontend project. This local test artifact uses the generated debug signing certificate, matching the previous debug APK, and is not a production-signed release. It must contain `assets/index.android.bundle`; do not substitute the development APK when a standalone install is needed. Runtime Firebase import remains unchanged.

### Connected development APK

Use `/home/marques/projects/HyperHiveFront/HyperHive`. The application ID is `com.anonymous.hyperhive`; register that exact Android package in each Firebase project. The local Expo module pins Firebase Messaging 26.0.1 and is automatically discovered under `modules/`.

```sh
npx expo prebuild --platform android --no-install
cd android
NODE_ENV=development JAVA_HOME=/opt/android-studio/jbr ANDROID_HOME=/home/marques/Android/Sdk CMAKE_BUILD_PARALLEL_LEVEL=2 ./gradlew :app:assembleDebug :firebase-notifications:testDebugUnitTest --max-workers=2
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

Use a supported JDK (the installed Android Studio runtime is Java 21); host Java 25 fails React Native CMake/Prefab configuration. A debug APK requires the Expo development server (`npx expo start`; use the development server connection offered by the installed app). For a standalone APK with a bundled JavaScript application, use a properly signed release build. Expo Go does not contain this module. Generated `android/` files are disposable: the native source and manifest live in `modules/firebase-notifications/android/`. No Google Services Gradle plugin or build-time Firebase client file is required.

## Configure after installation

1. Copy the Firebase Android **client** `google-services.json` onto the device. Do not copy server credentials.
2. Open Settings → Notifications → Import google-services.json. The app selects the unique client matching its installed package and keeps only app ID, project ID, API key and sender ID in private application storage. The temporary picker copy is deleted.
3. Press Enable notifications and allow Android notification permission. Importing and enabling do not send push messages.
4. Sign into the intended HyperHive backend and press Check Firebase connection. This validates credentials, project and device token without sending a notification.
5. Settings → Notifications offers **Test info**, **Test warning**, **Test error**, and **Test critical**. Each sends one fixed test message to this device. Info/warning channels are quiet by default; error uses default importance and the system sound; a newly created critical channel uses high importance and the bundled two-note sound. Android retains the sound of an existing channel, which can be changed in system settings. Sound/vibration mode, Do Not Disturb and lock-screen privacy control final presentation. If the older test channel was blocked, the new channels inherit that block. Tap a notification to open the app normally.
6. Test sends share a 30-second per-user cooldown across all four types. A Firebase acceptance is not proof of device receipt or sound; a foreground receipt confirms only that this app received that message. Background system display may not report a foreground receipt.
7. If a channel or app permission is blocked, use Open Android app settings. Configure each channel there; the UI reports blocked channels individually.

To replace a project, disable notifications first, import the new client file, then force-stop and reopen the app using Android app settings. Test sends are blocked while the running default Firebase app differs from the saved configuration. Normal startup restores saved settings through a private Android content provider before background services run. An unconfigured installation does not initialize Firebase.

Disable turns off auto initialization, persists the disabled flag and attempts token deletion. If deletion fails, the UI reports that failure; retry online. Already accepted/in-flight notifications can still arrive. Android force-stop prevents delivery until the app is reopened.

## Configure the backend

Provision a service-account JSON file on the backend host outside source control, readable only by its service account. Grant FCM send permission for the same project and enable the Firebase Cloud Messaging API. Set the backend service environment:

```sh
GOOGLE_APPLICATION_CREDENTIALS=/private/path/firebase-service-account.json
```

Restart the backend after initially setting or changing the credential environment variable. Replacing the file at that configured path is reloaded on later requests. Missing or invalid configuration leaves other functionality usable and returns an unavailable response for this feature. There is no credential-upload endpoint. Client configuration alone cannot authorize the backend to send.

`POST /notification/firebase/test` uses normal authentication. Its JSON body contains `token` (1–4096 characters), `projectId`, and optional `severity` (`info`, `warn`, `error`, `critical`). Omitting severity retains the legacy test channel and content. Typed sends include fixed server-owned copy, a severity channel ID and severity data for foreground rendering. Info/warn use normal FCM priority; error/critical use high. The endpoint rejects unknown fields, oversized bodies, invalid severity and project mismatch; imposes a shared 30-second per-user cooldown and returns `Retry-After`; sends only to the supplied device token; and returns `result` and `messageId`. `/notification/firebase/check` remains delivery-free and rejects severity. Provider errors are sanitized, network requests are bounded, and no application retry is performed. Browser Web Push and notification history retain their existing behavior.

Deploy the updated backend before the typed-test client. An older strict backend rejects the added field; Settings reports that it needs a backend update instead of silently sending the legacy type.

Logout or a backend change aborts an in-flight client request and ignores any pending token result. A send already accepted by Firebase cannot be recalled.

## Validation and release boundary

Focused backend tests use a fake outbound transport and no live credentials:

```sh
cd /home/marques/projects/HyperHive/master
GOMAXPROCS=2 go test -p 1 -tags libvirt_dlopen ./api ./nots
cd /home/marques/projects/HyperHiveFront/HyperHive
npx jest --config tests/jest.firebase.config.cjs --runInBand
npx tsc --noEmit
npx expo export --platform web --output-dir /tmp/hyperhive-firebase-web --max-workers 1
```

Physical-device checks remain required before release: clean install without setup; runtime token acquisition; process restart; foreground/background delivery; two installations using different projects; permission denial; blocked channel; offline activation/deletion/testing; backend mismatch; credential errors; project replacement; logout/backend changes. The user requested implementation to continue without an attached device on 2026-10-28. No successful real-device token generation or delivery is claimed.

## Severity test implementation — 2026-10-29

Settings provides four typed test sends with one shared cooldown. Android creates stable independent `hyperhive_test_info`, `hyperhive_test_warn`, `hyperhive_test_error`, and `hyperhive_test_critical` channels while retaining `hyperhive_test_v2`. Existing blocked legacy test channels propagate the block to the new channels. User overrides on existing channels are not reset. Notifications open the app through its normal launch intent. UI wording describes defaults and does not claim that device receipt proves sound or display behavior.

On 2026-10-29 the user requested a distinct critical sound. New critical channels use the bundled `hyperhive_critical.wav` notification tone; existing channels keep their Android-managed sound. Firebase includes the sound resource name in critical payloads for older Android background display. On the connected Galaxy A35 (Android 16), the tone was added to the Notifications folder and selected in the existing critical channel. The APK was installed in place and Android still reported that selected sound and enabled vibration afterward. No live push or audible result was verified; the phone appeared to be in silent or vibrate mode.

Local checks: Go API/notification tests, focused Jest tests and the Android notification module unit tests pass. TypeScript checking remains blocked by the existing unrelated `strokeWidth` error in `app/dashboard.tsx:822`. Physical-device behavior and user feedback remain pending; report the phone model/Android version, app state, sound/vibrate/silent/DND mode and each type's observed sound, vibration, display and tap behavior before treating device acceptance as complete.

## Local verification — 2026-10-28

- Android prebuild and `:app:assembleDebug` succeeded without a Google Services file/plugin, using Android Studio Java 21. The debug APK is `/home/marques/projects/HyperHiveFront/HyperHive/android/app/build/outputs/apk/debug/app-debug.apk` (all four configured CPU architectures; requires Metro).
- APK inspection confirmed the package ID, absence of bundled `google-services.json`, absence of Firebase project configuration resources, and absence of parser-test project/key values in DEX files.
- Four Kotlin parser tests, eleven frontend tests, and the full Go API/notification package tests with `libvirt_dlopen` passed. Web export passed.
- TypeScript reports an existing error in `app/dashboard.tsx:822`: `strokeWidth` is not a supported `Icon` prop. The feature files produced no TypeScript errors.
- No live Firebase credential, token acquisition, cold-start initialization or device delivery was tested. No release or deployment was performed.

## Standalone startup fix — 2026-10-28

The user reported the development APK staying on its splash screen. Inspection confirmed that it contained no JavaScript bundle and required Metro. A standalone ARM64 test APK was built successfully at `/home/marques/projects/HyperHiveFront/HyperHive/android/app/build/outputs/apk/release/app-release.apk` (54,674,116 bytes). ZIP inspection verified a 10,812,268-byte embedded `assets/index.android.bundle`, ARM64 native libraries and no Google Services JSON. Package inspection confirmed `com.anonymous.hyperhive` with debugging disabled; signature verification matched the configured Android test key. The earlier development APK file was no longer present for direct certificate comparison. No phone startup or Firebase delivery test was performed.

## Typed notification tests APK — 2026-10-29

The severity-test standalone ARM64 APK is at `/home/marques/projects/HyperHiveFront/HyperHive/android/app/build/outputs/apk/release/app-release.apk`. Build completed with `:app:assembleRelease`; APK inspection confirmed package `com.anonymous.hyperhive`, version code 1, target SDK 36, ARM64-only native libraries, a bundled 10,750,908-byte `assets/index.android.bundle`, and the critical sound resource. SHA-256: `7c3206155ba5a03f91a106907b6881f89c3c5bad2aa28751d6cd8719ab30d057`.

The APK is signed with the local Android debug certificate (SHA-256 fingerprint `fac61745dc0903786fb9ede62a962b399f7348f0bb6f899b8332667591033b9c`), not a production signing key. It can update an installation only when the package and signing certificate match; a Play/production-signed app will reject it as an incompatible update. Do not uninstall to work around a signature mismatch because that may erase local configuration. Backend deployment is not verified; deploy the backend that accepts typed severity before using the new buttons. Physical-device tests and user feedback are pending.
