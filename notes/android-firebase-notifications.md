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
4. Sign into the intended HyperHive backend and press Send test notification. Success means **Test accepted by Firebase**, not proven delivery.
5. If Android permissions or the notification channel are blocked, use Open Android app settings.

To replace a project, disable notifications first, import the new client file, then force-stop and reopen the app using Android app settings. Test sends are blocked while the running default Firebase app differs from the saved configuration. Normal startup restores saved settings through a private Android content provider before background services run. An unconfigured installation does not initialize Firebase.

Disable turns off auto initialization, persists the disabled flag and attempts token deletion. If deletion fails, the UI reports that failure; retry online. Already accepted/in-flight notifications can still arrive. Android force-stop prevents delivery until the app is reopened.

## Configure the backend

Provision a service-account JSON file on the backend host outside source control, readable only by its service account. Grant FCM send permission for the same project and enable the Firebase Cloud Messaging API. Set the backend service environment:

```sh
GOOGLE_APPLICATION_CREDENTIALS=/private/path/firebase-service-account.json
```

Restart the backend after changing the credential file/environment. Missing or invalid configuration leaves other functionality usable and returns an unavailable response for this feature. There is no credential-upload endpoint. Client configuration alone cannot authorize the backend to send.

`POST /notification/firebase/test` uses normal authentication. Its JSON body contains `token` (1–4096 characters) and `projectId`. It rejects unknown fields, oversized bodies and project mismatch; imposes a 30-second per-user cooldown; sends a fixed title/body/channel to one token; and returns `result` and `messageId`. Provider errors are sanitized, network requests are bounded, and no application retry is performed. Browser Web Push and notification history retain their existing behavior.

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

## Local verification — 2026-10-28

- Android prebuild and `:app:assembleDebug` succeeded without a Google Services file/plugin, using Android Studio Java 21. The debug APK is `/home/marques/projects/HyperHiveFront/HyperHive/android/app/build/outputs/apk/debug/app-debug.apk` (all four configured CPU architectures; requires Metro).
- APK inspection confirmed the package ID, absence of bundled `google-services.json`, absence of Firebase project configuration resources, and absence of parser-test project/key values in DEX files.
- Four Kotlin parser tests, eleven frontend tests, and the full Go API/notification package tests with `libvirt_dlopen` passed. Web export passed.
- TypeScript reports an existing error in `app/dashboard.tsx:822`: `strokeWidth` is not a supported `Icon` prop. The feature files produced no TypeScript errors.
- No live Firebase credential, token acquisition, cold-start initialization or device delivery was tested. No release or deployment was performed.

## Standalone startup fix — 2026-10-28

The user reported the development APK staying on its splash screen. Inspection confirmed that it contained no JavaScript bundle and required Metro. A standalone ARM64 test APK was built successfully at `/home/marques/projects/HyperHiveFront/HyperHive/android/app/build/outputs/apk/release/app-release.apk` (54,674,116 bytes). ZIP inspection verified a 10,812,268-byte embedded `assets/index.android.bundle`, ARM64 native libraries and no Google Services JSON. Package inspection confirmed `com.anonymous.hyperhive` with debugging disabled; signature verification matched the configured Android test key. The earlier development APK file was no longer present for direct certificate comparison. No phone startup or Firebase delivery test was performed.
