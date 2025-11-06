## Setting Up GeoIP for GoAccess

The “Visitor Hostnames and IPs” panel in GoAccess only shows country/city information when a MaxMind GeoIP database (`*.mmdb`) is available. This project auto-fetches that database when you provide a MaxMind license key. Follow this guide to obtain the key, configure it, and verify that geolocation works.

### 1. Create a MaxMind Account
- Visit [https://www.maxmind.com](https://www.maxmind.com) and click **Create Account**.
- Choose the **GeoLite2 Free** plan and fill out your details.
- Confirm your email and accept the terms of use. MaxMind’s license requires every download to be tied to a user, which is why we cannot redistribute the file ourselves.

### 2. Generate a License Key
1. Sign in to the MaxMind portal and go to **My Account → Manage License Keys**.
2. Click **Generate New License Key**.
3. Give it a descriptive name (e.g., `hyperhive-goaccess`) and answer **Yes** to “Will this key be used for GeoIP Update?”
4. Copy the key shown (it is only displayed once). You can revoke or regenerate it later if needed.

### 3. Pick the Edition (City or Country)
- **GeoLite2-City** (default) includes country, city, and latitude/longitude.
- **GeoLite2-Country** has only the country but produces a smaller file.
- Any edition available from MaxMind works; just provide the correct edition ID (e.g., `GeoLite2-City`, `GeoLite2-Country`).

### 4. Update the `.env`
Edit your `.env` file and set:

```env
GOACCESS_GEOIP_LICENSE_KEY=put_your_key_here
# optional; leave GeoLite2-City to keep the default
GOACCESS_GEOIP_EDITION=GeoLite2-City
```

The license key is mandatory for the built-in downloader. Only set `GOACCESS_GEOIP_DB` if you host the `.mmdb` yourself (for example, on a private mirror); otherwise leave it blank so the downloader manages everything.

### 5. Where Files Are Stored
- The service automatically creates `./geoipdb/` (already in `.gitignore`) and caches the downloaded `.mmdb` there.
- You do not need to copy anything into that directory; it exists solely so the downloader has a place to keep the latest database.

### 6. Automatic Downloads in Action
With the key in place, start the binary normally (`go run main.go` or however you run the service). When the GoAccess report is requested it:
1. Uses `GOACCESS_GEOIP_DB` if you explicitly pointed to a file.
2. Otherwise, looks for a cached `.mmdb` inside `./geoipdb/`.
3. If nothing is cached yet—or if the cached file is older than seven days—it contacts the MaxMind API with your license key, downloads the configured edition, and stores it under `./geoipdb/` for future runs.

### 7. Verifying Everything Works
- Hit `GET /goaccess` (or the equivalent UI flow) to regenerate the report.
- Open `npm-data/stats/goaccess.html` in a browser and confirm the country column is populated.
- If something fails, inspect the service logs: common issues are invalid license keys, typos in the edition ID, or firewalls blocking `download.maxmind.com`.

### 8. Keeping the Database Fresh
- MaxMind recommends pulling a new copy every week; the service automatically refreshes any cached file older than seven days as long as the license key is configured.
- To force an immediate refresh, delete the cached `.mmdb` in `./geoipdb/` (or bump the edition) and trigger the report again—the downloader will fetch a new file right away.
- External automations can still schedule calls to `/goaccess` if you want tighter control or monitoring, but manual cleanup is no longer required.

### 9. License Key Hygiene
- Treat `GOACCESS_GEOIP_LICENSE_KEY` as a secret. Never commit `.env` files that include the key.
- In production, prefer injecting the key via environment variables or a secret manager (Vault, AWS Secrets Manager, etc.).

Follow these steps and the GeoIP data in GoAccess will stay current without forcing each end-user to sign up for their own MaxMind account.
