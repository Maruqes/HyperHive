# Proxy-host tool contracts

Reviewed source: `master/api/npmapi/proxy.go` (`SetupProxyAPI`, handlers and setup
helpers), `master/npm/proxy.go` (`Proxy`, `Location`, `StringOrInt`, NPM helpers),
`master/api/login.go` and `mcp/auth.go`. The current verified tool set and
endpoint coverage are recorded in [UPDATE_STATE.md](../../UPDATE_STATE.md).

All tools use HyperHive's authenticated API and its managed-proxy restrictions.
HTTP clients supply the fixed MCP Bearer key; stdio uses environment configuration.
Successful mutations return `{"success":true}` after the API's empty HTTP 200.
This confirms acceptance, not the resulting resource state or an ID. Inspect
`list_proxies` after a mutation when those values are needed. HTTP status is
preserved in sanitized errors, without backend bodies. No mutation retries.

## Create and update

`create_proxy` requires the complete writable configuration below. `update_proxy`
requires the same configuration plus a positive `id`. Updates replace all settings;
there is no implicit read/merge/write. Explicit false/zero values are preserved.
Omitting any setting, even a boolean, fails before contacting HyperHive. Unknown
fields are rejected except arbitrary JSON keys inside `meta`.

```json
{
  "domain_names": ["app.example.test"],
  "forward_scheme": "http",
  "forward_host": "127.0.0.1",
  "forward_port": 8080,
  "caching_enabled": false,
  "block_exploits": true,
  "allow_websocket_upgrade": true,
  "access_list_id": "0",
  "certificate_id": 0,
  "meta": {},
  "advanced_config": "",
  "locations": [],
  "http2_support": false,
  "hsts_enabled": false,
  "hsts_subdomains": false,
  "ssl_forced": false
}
```

Domains and direct forwarding hosts must be nonempty. Schemes are http/https; forwarding
ports are 1–65535. Each location requires `path` starting with `/`,
`forward_scheme`, `forward_host`, `forward_port` with the same bounds.
Certificate IDs are nonnegative. Access-list IDs accept a nonnegative decimal
string or safe JSON integer; zero means no access list.

`enabled` is ignored by the API's NPM create/edit helpers and is deliberately
absent from their writable schema. Use the dedicated enable/disable actions.
Create does not accept an ID; the API discards the created ID and returns no body.
Update checks the existing proxy by immutable ID before applying changes, so changing its domain cannot bypass managed-frontend restrictions. Missing IDs/domains are rejected, and omitted REST advanced configuration is preserved. MCP continues to require its complete set of existing configuration fields.

## Enable and disable

Positive proxy `id` is required: `{"id":42}`. Both operations use POST with JSON.
The backend forbids disabling managed frontend proxies (403); enabling is allowed.
No deletion tool, dispatcher, stub or indirect cleanup operation is exposed.

## Frontend setup

`setup_frontend_proxy` requires `{"domain":"admin.example.test","id":0}`.
Here `id` is a certificate ID, not a proxy ID. Zero selects no certificate;
positive values force TLS. The API creates or replaces a managed proxy matched
by domain, forwarding to frontend port 8079 and the configured API_PORT for
`/api`, `/novnc` and `/guest_api`. This can replace an existing proxy configuration.

`setup_secure_frontend_proxy` requires publicDomain, publicCertificateId,
privateDomain, privateCertificateId with the API's exact camelCase spelling.
Certificate IDs are nonnegative; each domain is required. It writes a private
managed proxy with the API locations, then a public managed proxy that allows
`/guest_api` and returns 403 for its root. Existing managed proxy IDs are reused.
It does not require TLS when certificate IDs are zero, despite its secure name.
A failure after the private write can leave that write applied: there is no
rollback or automatic retry. Review `list_proxies` before deciding how to recover.

Both setup operations preserve the server-owned long timeouts, no body-size limit,
no buffering and safe client-IP header configuration. These are proxy settings,
not proof that a domain resolves or a certificate works in production.

## Verification

Run `go test ./...`, `go test -race ./...`, `go vet ./...` from `mcp/`.
SDK contract tests exercise every enabled mutation over authenticated HTTP and
the real stdio executable against disposable HTTP fixtures: exact method/path,
API prefix, headers, all body fields, invalid input, empty success and safe
400/401/403/404/409/500/503 failures without retries. Shared client tests cover
limits, cancellation, timeout and redirect rejection. No live backend validation
or deployment is implied. Actual-handler tests in `master/api/npmapi/proxy_contract_test.go` additionally
exercise all six mutations against disposable NPM state, including managed
restrictions, certificate/port settings, existing proxy reuse and partial setup
failure. The full affected master API/NPM package suites, focused authentication/port
race checks and vet also pass.
See the [batch checkpoint](../../runs/2026-10-18-proxies-01.md) for evidence.

## Integer precision review — 2026-10-19

Proxy list IDs and numeric metadata are decoded without float conversion and
emitted as exact raw structured/text JSON. Shared tool registration also decodes
original request numbers after SDK schema validation, preserving target IDs and
metadata; numeric access-list IDs retain their documented safe-integer bounds.
SDK clients that decode arbitrary structured objects as floats may need the
exact text fallback. Regression: `TestReviewProxyListRetainsExactIntegerIDs`.

## Reusable pool targets

Root proxies and custom locations accept optional upstream_id. Use a positive compatible HTTP pool ID with empty direct host/zero port, null plus a valid direct target to detach, or omit on update to preserve the binding. Pool reads clear private target names. Location advanced_config is optional and preserved. REST edits preserve omitted configuration fields; MCP retains its complete-configuration contract for existing required settings. See [upstream tools](upstreams.md) for capabilities, revisions and recovery.
