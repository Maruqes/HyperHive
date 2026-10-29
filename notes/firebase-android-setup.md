# Set up Firebase for HyperHive Android notifications

Use this guide for `com.anonymous.hyperhive` and the HyperHive backend. The phone and backend need **two different configurations from the same Firebase project**.

| Configuration | Where it goes | Purpose |
| --- | --- | --- |
| Android `google-services.json`, or its four client fields | Phone → Settings → Notifications | Registers this installation with Firebase |
| Service-account JSON containing `type: service_account`, `client_email` and `private_key` | Backend host only | Authorizes the backend to send messages |

`/home/marques/Downloads/google-services.json` was inspected on 2026-10-28: it is an Android client file for `com.anonymous.hyperhive`, not a backend credential. Copying this file to a server does not enable server authentication. Do not put a service-account private key on the phone or paste it into chat.

## 1. Create the Firebase project and Android app

1. Open [Firebase Console](https://console.firebase.google.com/), sign in and choose **Create a project**. Google Analytics is optional for this notification test.
2. Open the project → **Project settings → General**.
3. Under **Your apps**, add an **Android** app.
4. Enter the package exactly: `com.anonymous.hyperhive`. Package names are case-sensitive. An app nickname is optional.
5. Register the app and download `google-services.json`. HyperHive imports this file at runtime; do not add Firebase values or the file to the APK build.
6. Keep the project selected for all remaining steps.

Official reference: [Add Firebase to an Android project](https://firebase.google.com/docs/android/setup).

## 2. Prepare the four Android client fields

Either import `google-services.json` on the phone or enter these fields manually in Settings:

| HyperHive input | Location inside `google-services.json` |
| --- | --- |
| Project ID | `project_info.project_id` |
| Sender ID / project number | `project_info.project_number` |
| Firebase Android App ID | Matching client's `client_info.mobilesdk_app_id` |
| Android API key | Matching client's `api_key[0].current_key` |

Select the client whose `client_info.android_client_info.package_name` is `com.anonymous.hyperhive`. The Android App ID usually starts with `1:<sender-id>:android:`. The API key is **not** a service-account private key.

If API/application restrictions are enabled, ensure this Android app, its signing-certificate SHA-1 and the required Firebase APIs are allowed. Obtain the installed APK's certificate fingerprint using Android `apksigner verify --print-certs <apk>`; a production-signed APK will have a different certificate from the local test APK. Do not disable restrictions globally as a workaround. See [Firebase API keys](https://firebase.google.com/docs/projects/api-keys).

## 3. Enable the FCM server API and create server credentials

1. In Firebase **Project settings → Cloud Messaging**, ensure **Firebase Cloud Messaging API (V1)** is enabled. Follow its Google Cloud link if needed and enable `fcm.googleapis.com` for this project. The legacy server key is not used.
2. In **Project settings → Service accounts → Firebase Admin SDK**, choose **Generate new private key** and download the service-account JSON.
3. Keep that JSON on the backend host, outside source control and web-served folders. Its `project_id` must match the phone's Project ID.
4. The service account needs `cloudmessaging.messages.create`; the **Firebase Cloud Messaging API Admin** IAM role grants FCM sending permission. Avoid broad Owner permissions for this purpose. If organization policy prevents key creation, use your administrator's approved credential provisioning process rather than weakening the policy.

Official references: [Generate server service-account credentials](https://firebase.google.com/docs/admin/setup#initialize_the_sdk_in_non-google_environments), [FCM API and permissions](https://firebase.google.com/docs/reference/fcm/rest/v1/projects.messages/send).

## 4. Configure the process that actually runs HyperHive

Install the service-account file on the machine/container running the backend behind `https://512hyperhive.maruqes.local/api`. A path on your laptop is not automatically visible inside that backend or its container.

Example path: `/etc/hyperhive/firebase-service-account.json`. The backend service's user must be able to read it; keep permissions restrictive (for example mode `0600`, owned by that service's user). For a container, mount the file read-only and use its **container path** in the environment variable.

Set:

```dotenv
GOOGLE_APPLICATION_CREDENTIALS=/etc/hyperhive/firebase-service-account.json
```

For a manual launch, export this variable in the same shell that starts the backend. For HyperHive's existing `.env` loading, put it in the `.env` in the backend's working directory. For systemd, add it to the real service's `Environment=` or configured `EnvironmentFile=` and restart that service. Exporting a variable in an unrelated terminal does not change an already-running service's environment.

Deploy/rebuild the updated HyperHive backend and restart it after initially adding/changing the environment variable. This update reloads the credential file on each check/send while reusing OAuth tokens when contents are unchanged. Replacing the file at the configured path therefore does not require another restart. Replace credentials atomically to avoid reading a partial write. Every backend replica behind the URL must have the same intended project configuration.

No credential-upload API is provided. Uploading a JSON file through an unrelated backend page does not configure this sender.

## 5. Verify on the phone

1. Install the updated **standalone** APK, which embeds JavaScript and does not require Metro. The local ARM64 APK is `/home/marques/projects/HyperHiveFront/HyperHive/android/app/build/outputs/apk/release/app-release.apk`. It is signed with the local debug key, not a production key. An in-place update requires the installed app to have the same package and signing certificate. If the installer reports a signature mismatch, stop and preserve the existing app data; uninstalling can erase configuration.
2. Open HyperHive and sign in to the intended, updated backend. Deploy the backend supporting typed tests before using the four buttons.
3. Open HyperHive **Settings → Notifications**, import the Android client file or fill the four client fields and press **Save manual configuration**. To change projects, disable notifications first, save the new configuration, then force-stop and reopen the app.
4. Press **Enable notifications** and allow Android's notification permission. Also check **Android Settings → Apps → HyperHive → Notifications → Allow notifications** if the permission was previously denied.
5. Press **Check Firebase connection**. This fetches a current device token and uses Firebase's `validate_only` request; it sends **no push**. Look for green **Android client: Working** and **Backend credentials: Working**. A loaded JSON file alone is not labelled Working.
6. Set up the critical channel sound as described below, turn the phone's sound mode on, and check that notification volume is audible. Silent/vibrate modes, Do Not Disturb, app permission, channel settings and lock-screen privacy still apply. A channel can request a pop-up; the phone decides whether to display it.
7. In HyperHive, press **Test info**, **Test warning**, **Test error**, or **Test critical**. Info and warning are quiet by default; error uses the system notification sound; a newly created critical channel defaults to the distinct bundled sound and vibration. The four buttons share a 30-second per-user cooldown. Wait for the countdown before another send.
8. **Accepted by Firebase** means the provider accepted the request. Green **Device receipt: Working** confirms that the app received the matching foreground message; it does not confirm that a sound played. A background notification can be displayed by Android without that foreground receipt. Try foreground, background and locked-screen delivery and listen on the phone.

### Set the critical sound on Android

On a fresh installation, opening and enabling HyperHive creates the critical channel with the bundled tone as its default; no separate sound file is needed. Check that the channel and app notifications are enabled. If it is blocked, enable it yourself in Android settings. A previously blocked legacy test channel can cause the new channels to start blocked; HyperHive preserves that choice.

An existing `hyperhive_test_critical` channel keeps its previous sound when HyperHive is updated. Android lets the user change that sound in system settings; reinstalling or clearing app data is not necessary and can erase configuration. See [Android notification channels](https://developer.android.com/develop/ui/compose/notifications/channels).

Current implementation limitation: acquiring a Firebase token for the connection check or a test also requires the legacy **HyperHive test notifications** channel (`hyperhive_test_v2`) to be enabled. If Android reports that notifications are blocked even though the critical category is enabled, enable the legacy category too, then check the connection again.

To make the bundled tone available in the system sound picker for an existing channel, copy `hyperhive_critical.wav` from `HyperHiveFront/HyperHive/modules/firebase-notifications/android/src/main/res/raw/` to the phone's **Internal storage → Notifications** folder. Give it a recognizable name such as `HyperHive-critical.wav`; choose a different name if a file with that name already exists. Use USB file transfer or another file-copy method. With an authorized USB-debugging connection, the equivalent commands on this workstation are:

```sh
adb push /home/marques/projects/HyperHiveFront/HyperHive/modules/firebase-notifications/android/src/main/res/raw/hyperhive_critical.wav /sdcard/Notifications/HyperHive-critical.wav
adb shell am broadcast -a android.intent.action.MEDIA_SCANNER_SCAN_FILE -d file:///sdcard/Notifications/HyperHive-critical.wav
```

On Samsung Galaxy phones:

1. If **Notification categories** is hidden, open **Android Settings → Notifications → Advanced settings** and enable **Manage notification categories for each app**.
2. Open **Settings → Apps → HyperHive → Notifications**, enable **Allow notifications**, then open **Notification categories → HyperHive test critical**. HyperHive's **Open Android app settings** link can also take you to the app settings when a channel or permission is blocked.
3. Enable the category and select **Alert**. Tap **Sound**, select **HyperHive-critical** under **Custom**, and return to the category. Enable **Vibrate** and **Show as pop-up** if desired; check the lock-screen setting separately. Do not change the sound for the error channel unless you want to.
4. Confirm the category shows **Sound: HyperHive-critical**. If the file is missing from the picker, check that it is in the Notifications folder and scan or reopen the sound picker. On other Android phones, the path and labels may differ; look under the app's notification channels or categories.

These steps were checked on the connected Galaxy A35 with Android 16: the updated APK was installed in place and the existing critical category showed **HyperHive-critical** with vibration enabled. This confirms the setting, not that a notification played audibly. Samsung's [notification category guide](https://www.samsung.com/us/support/answer/ANS10002521/) explains where to find these controls.

### Listen to the critical tone on this computer

The tone is a short WAV file at `/home/marques/projects/HyperHiveFront/HyperHive/modules/firebase-notifications/android/src/main/res/raw/hyperhive_critical.wav`. Open it in an audio player, or run:

```sh
paplay /home/marques/projects/HyperHiveFront/HyperHive/modules/firebase-notifications/android/src/main/res/raw/hyperhive_critical.wav
```

Turn up the computer's output volume and choose the correct speakers or headphones. This previews the file; it does not prove the phone will play a notification.

## Troubleshooting

| Result | What to check |
| --- | --- |
| `GOOGLE_APPLICATION_CREDENTIALS` missing | Configure the environment of the actual running backend service/container, then restart it |
| Android client file supplied to backend | Download a separate service-account key; do not use `google-services.json` as the server credential |
| File missing/unreadable | Backend filesystem/mount path and service-user permissions |
| Project/sender mismatch | Phone client project, sender number and server service-account project must match; disable/reconfigure/restart phone if replacing its project |
| FCM API disabled or 403 | Enable `fcm.googleapis.com` and grant FCM send permission to the service account |
| Unregistered token | Disable notifications, enable again and rerun Check connection |
| Android client registration failure | Google Play services, internet access, correct App ID/sender/API key and key restrictions |
| Check endpoint is 404 | Backend code is older than the APK; deploy the matching backend update |
| Typed test says the backend needs updating | Deploy the backend with typed test support; older versions reject the severity field |
| Accepted but no foreground receipt | Keep Settings open; check phone connectivity to Google, VPN/firewall, battery restrictions and Play services. Do not assume backend acceptance equals delivery |
| Receipt confirmed but no banner | Check Android app/channel notification settings, Do Not Disturb, sound and lock-screen rules; these can suppress banners. An explicitly blocked old test channel remains respected |
| Critical notification uses the old sound | Existing Android channels retain their sound. Copy the tone to Notifications and select **HyperHive-critical** in that channel's Android settings |
| Critical category enabled but connection check says notifications are blocked | Enable the legacy **HyperHive test notifications** category too; the current native token check requires it |
| No sound | Check sound mode, notification volume, Do Not Disturb, app permission and the critical category's **Alert** and **Sound** settings. Info and warning are quiet by default |
| Intermittent credential errors | Check every backend instance/replica and the phone's selected backend URL; a load balancer may route to differently configured processes |
| Nothing after force-stop | Reopen the app. Android force-stop prevents FCM delivery until the app is opened again |

Relevant official explanations: [Android FCM handling](https://firebase.google.com/docs/cloud-messaging/android/receive-messages), [Delivery priority and Doze](https://firebase.google.com/docs/cloud-messaging/android-message-priority), [FCM errors](https://firebase.google.com/docs/cloud-messaging/error-codes).

## Scope of verification

The local client file's type and package name were verified without displaying its API key. The backend URL answered HTTPS with 401 without a session; that proves reachability, not Firebase readiness. Automated checks and APK builds are reported separately. No successful live send can be claimed until the actual backend has a valid service-account credential and the phone confirms receipt.

## Local validation of this update — 2026-10-28

- The supplied Android client and new service-account file were inspected without displaying their API/private keys; their projects match.
- A real Google OAuth exchange succeeded and an FCM `validate_only` request reached the API, which rejected the deliberately invalid placeholder token. This sent no notification and does not prove real-device delivery or final token authorization.
- Go API/sender tests, 12 frontend tests, four Kotlin parser tests, backend compilation, Android standalone build and web export passed. TypeScript still reports the unrelated existing `app/dashboard.tsx:822` Icon `strokeWidth` error.
- Updated standalone test APK: `/home/marques/projects/HyperHiveFront/HyperHive/android/app/build/outputs/apk/release/HyperHive-firebase-status-arm64.apk`. Its bundled JavaScript and signature against the previous standalone APK were verified.
- Backend deployment and a check using the actual phone's current token remain separate steps. Do not replace the backend service with a locally built binary without checking the server's operating-system compatibility and current service configuration.
