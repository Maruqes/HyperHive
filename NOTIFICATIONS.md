# Install and set up Android notifications

This guide sets up Firebase test notifications in the HyperHive Android app. You need an Android phone with Google Play services and access to the machine running the HyperHive backend.

**Android Firebase settings appear only in the Android app.** The website keeps its separate browser notification controls.

## What you need

Create one Firebase project and download two different files from it:

| File | Where to put it |
| --- | --- |
| `google-services.json` | Import it inside the Android app |
| Service-account JSON (the downloaded private key) | Keep it on the backend server only |

These files must belong to the same Firebase project. The service-account file is secret: never put it in the app, Git, a public folder or a chat.

## 1. Create your Firebase app

1. Open [Firebase Console](https://console.firebase.google.com/) and create a project. Google Analytics is optional.
2. Open **Project settings → General → Your apps** and add an **Android** app.
3. Enter this package name exactly: `com.anonymous.hyperhive`.
4. Register the app and download **google-services.json**. Transfer this file to your phone.
5. Under **Project settings → Cloud Messaging**, enable **Firebase Cloud Messaging API (V1)**. Follow the Google Cloud link if it needs enabling there.
6. Under **Project settings → Service accounts → Firebase Admin SDK**, click **Generate new private key**. Download this second JSON file for the backend.

## 2. Set up the backend

1. Update and rebuild the HyperHive backend with the Firebase notification changes.
2. Copy the **service-account JSON** to the machine running that backend. For example, save it as `/etc/hyperhive/firebase-service-account.json`.
3. Allow only the backend service's user to read the file. Do not place it in a web-served directory.
4. Add this setting to the environment used to start the backend:

   ```dotenv
   GOOGLE_APPLICATION_CREDENTIALS=/etc/hyperhive/firebase-service-account.json
   ```

5. Restart the backend.

Use the actual file path on the backend server. A file on your computer or phone is not automatically available to the server. For Docker, mount the file read-only and use its path **inside the container**.

If you use HyperHive's `.env` file, put the setting in the `.env` in the backend's working directory. If you use systemd or Docker, set it in that service's configuration. Setting it in another terminal does not update a running backend.

You do not upload this key through the app. The backend reads it directly from the configured path.

## 3. Install and configure the Android app

1. Install the current **standalone Android APK** supplied with your HyperHive build. It contains the app code and does not need a development server. The current test APK is ARM64 and test-signed; use a compatible phone and trusted download.
2. Open the app, select your HyperHive backend URL and sign in.
3. Open **Settings → Notifications**.
4. Tap **Import google-services.json** and select the Android file from step 1.
5. Tap **Enable notifications** and allow Android's notification permission.
6. Tap **Check Firebase connection**. This checks the setup without sending a notification.
7. Wait for **Android client: Working** and **Backend credentials: Working** in green.
8. Set up the critical sound as described below, then tap **Test info**, **Test warning**, **Test error**, or **Test critical**. Keep the settings screen open for the first test. All four tests share a 30-second cooldown.

When the app confirms **Device receipt: Working**, it received the matching foreground message. **Accepted by Firebase** alone means Firebase accepted the send request; it does not confirm that the phone received it. A background notification can be displayed without a foreground receipt confirmation. Neither result confirms that the sound played.

### Allow and choose the critical notification sound

1. In Android **Settings → Apps → HyperHive → Notifications**, allow notifications. On Samsung, if **Notification categories** is missing, first enable **Settings → Notifications → Advanced settings → Manage notification categories for each app**.
2. Open **Notification categories → HyperHive test critical**, enable the category and select **Alert**. Enable vibration and pop-up display if desired. Also enable the legacy **HyperHive test notifications** category: the current token check requires it even for the newer test types.
3. A newly created critical category uses HyperHive's bundled distinct sound. If the category existed before the app update, Android retains its previous sound. To select the new sound, copy [`hyperhive_critical.wav`](../HyperHiveFront/HyperHive/modules/firebase-notifications/android/src/main/res/raw/hyperhive_critical.wav) to the phone's **Internal storage → Notifications** folder, then open the critical category's **Sound** setting and select the copied sound under **Custom**. The exact menu names can vary by phone.
4. Turn on the phone's sound mode and notification volume. Silent or vibrate mode, Do Not Disturb, channel settings and lock-screen privacy can affect the result. Test the sound with **Test critical**.

Android keeps existing channel settings across app updates; clearing app data or reinstalling can erase your configuration. See the [detailed Firebase setup guide](notes/firebase-android-setup.md) for USB copy commands and troubleshooting.

To hear the tone on this computer, open the WAV file above in an audio player, or run:

```sh
paplay /home/marques/projects/HyperHiveFront/HyperHive/modules/firebase-notifications/android/src/main/res/raw/hyperhive_critical.wav
```

### Test types

| Button | Default Android behavior |
| --- | --- |
| **Test info** | Quiet; no sound or vibration |
| **Test warning** | Quiet; no sound or vibration |
| **Test error** | System notification sound and vibration |
| **Test critical** | Distinct bundled sound and vibration; high importance can show a pop-up |

These are defaults. Android and the user's settings control the actual presentation.

### Enter the Android settings manually

Instead of importing the file, fill in these four fields and tap **Save manual configuration**:

| App field | Value in `google-services.json` |
| --- | --- |
| Project ID | `project_info.project_id` |
| Sender ID / project number | `project_info.project_number` |
| Firebase Android App ID | `client_info.mobilesdk_app_id` in the client for `com.anonymous.hyperhive` |
| Android API key | `api_key[0].current_key` in that same client |

Do not enter the service-account private key in these fields. If changing Firebase projects, disable notifications first, save the new settings, then force-stop and reopen the app before enabling them again.

## If something fails

| What you see | What to do |
| --- | --- |
| Missing `GOOGLE_APPLICATION_CREDENTIALS` | Set it in the actual backend service's environment and restart that backend. |
| Wrong credential file | The phone uses `google-services.json`; the backend uses the separate service-account private key JSON. |
| File cannot be read | Check the path, container mount and permissions for the backend service's user. |
| Project or sender mismatch | Use Android settings and a service account from the same Firebase project. |
| API disabled or permission denied | Enable FCM API V1. The service account needs FCM sending permission; see the detailed guide below. |
| Connection check returns 404 | Update the backend to match the APK. |
| Connection check says notifications are blocked | Enable app notifications and the legacy **HyperHive test notifications** category, then check again. |
| Critical test uses the old sound | Copy the bundled WAV to the phone and select it in the critical category as described above. |
| Android client fails | Check internet access, Google Play services and the four Android values. |
| Firebase accepts it, but nothing arrives | Keep the app open and check connectivity, Android notification permissions, battery restrictions and Do Not Disturb. Reopen the app after force-stopping it. |
| Receipt works but no banner appears | Check Android's notification channel, sound and Do Not Disturb settings. |

## What this currently does

This is a manual, single-device notification test. It does not subscribe the phone to automatic HyperHive alerts or send broadcasts. Browser Web Push remains a separate feature.

### Using notifications elsewhere in HyperHive

Backend services currently publish system events with `nots.SendGlobalNotification(title, body, relativeURL, critical)`. This writes notification history and dispatches browser Web Push; critical events also produce a websocket notification. Slaves can send through the master's `SendNotifications` service. These paths do not currently send Firebase notifications to Android devices.

The authenticated `POST /notification/firebase/test` endpoint is for fixed test messages to a supplied device token. It accepts `token`, `projectId` and optional `severity` (`info`, `warn`, `error`, or `critical`); omitting severity keeps the legacy test behavior. `POST /notification/firebase/check` validates without sending. The test endpoint cannot be used to send arbitrary event content. Production Android alerts need device registration and event routing to be designed and implemented first; the existing global `critical` boolean alone is not a reliable severity mapping.

For service permissions, API-key restrictions, deployment details and deeper troubleshooting, see [the detailed Firebase setup guide](notes/firebase-android-setup.md).
