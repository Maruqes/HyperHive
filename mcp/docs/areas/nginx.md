# NGINX MCP contracts

The shared registry exposes named HyperHive API operations over authenticated
Streamable HTTP and executable stdio. See [current coverage](../../UPDATE_STATE.md)
and [batch evidence](../../runs/2026-10-18-nginx-01.md) for enabled/verified state.
The 2026-10-18 continuation completes every remaining permitted NGINX operation.
This page describes verified local contracts, not production deployment.

## Scope and policy exclusions

Existing proxy tools are documented in [proxy contracts](nginx-proxies.md).
The completed continuation adds these 21 verified tools:

| Tool | API endpoint |
| --- | --- |
| `list_certificates` | `GET /certs/list` |
| `list_dns_providers` | `GET /certs/dns-providers` |
| `download_certificate` | `POST /certs/download` |
| `renew_certificate` | `POST /certs/renew` |
| `list_access_lists` | `GET /access-lists/list` |
| `create_access_list` | `POST /access-lists/create` |
| `list_streams` | `GET /stream/list` |
| `create_stream` | `POST /stream/create` |
| `update_stream` | `PUT /stream/edit` |
| `enable_stream` | `POST /stream/enable` |
| `disable_stream` | `POST /stream/disable` |
| `list_redirects` | `GET /redirection/list` |
| `create_redirect` | `POST /redirection/create` |
| `update_redirect` | `PUT /redirection/edit` |
| `enable_redirect` | `POST /redirection/enable` |
| `disable_redirect` | `POST /redirection/disable` |
| `list_404_hosts` | `GET /404/list` |
| `create_404_host` | `POST /404/create` |
| `update_404_host` | `PUT /404/edit` |
| `enable_404_host` | `POST /404/enable` |
| `disable_404_host` | `POST /404/disable` |

All six NGINX HTTP DELETE endpoints remain excluded. Three additional endpoints
are excluded by the existing no-deletion policy after reviewing actual effects:

- `POST /certs/create`: HyperHive's CreateCertDetails invokes
  rollbackCreatedCertificate/ DeleteCert when upload or confirmation fails.
- `POST /certs/create-lets-encrypt`: pinned NPM inserts a certificate row, then
  hard-deletes it if issuance fails.
- `PUT /access-lists/edit`: pinned NPM deletes authentication items and client
  rules while replacing them, including removing entries omitted from the request.

No partial workaround, disabled deletion tool or generic dispatcher is provided.
The previous explicit Celld deletion exception is limited to Celld. These nine
NGINX exclusions remain visible in the raw 37-endpoint denominator with zero credit.
The eligible denominator is 28, including the seven existing proxy endpoints.

Effect evidence: local `master/npm/certs.go`, `master/npm/cert_upload.go`,
`master/npm/access_list.go`; pinned NPM v2.13.5
[certificate implementation](https://raw.githubusercontent.com/NginxProxyManager/nginx-proxy-manager/v2.13.5/backend/internal/certificate.js)
and [access-list implementation](https://raw.githubusercontent.com/NginxProxyManager/nginx-proxy-manager/v2.13.5/backend/internal/access-list.js).
DNS-provider response shape and renewal behavior are also checked against the
[pinned certificate routes](https://raw.githubusercontent.com/NginxProxyManager/nginx-proxy-manager/v2.13.5/backend/routes/nginx/certificates.js).
These are source references, not checks against a live NPM installation.

## Common behavior

API URL and MCP_TOKEN come from configuration, never tool arguments. Tools call
HyperHive's authenticated API; they do not bypass handlers with direct NPM or
database access. The existing fixed-key/session authentication bridge is unchanged.

Lists take `{}` and return `{ "items": [...] }` plus an identical JSON text
fallback. All fields returned by HyperHive are preserved, including nested
metadata, null fields, owner/items/clients expansions and descriptions. Large integers are decoded without float conversion and retained in structured JSON and text; a client’s own decoder may still apply floating-point semantics. A null
root array becomes an empty array; null or non-object list entries and malformed,
empty or wrong-shaped JSON are rejected. Sensitive values follow the API's
masking/disclosure behavior; the MCP layer does not invent extra fields.

Enable/disable, download and renew require a positive integer `id` from the
corresponding list tool. Unknown fields, incorrect types, invalid bounds and
explicit null inputs are rejected before API dispatch. Create/update inputs
require complete writable configuration, retaining false, zero, empty strings
and empty arrays. Updates replace configuration; they do not read/merge/write.

Ordinary mutations acknowledge empty successful API responses as
`{ "success": true }`. This is API acceptance, not a fabricated created ID,
resource snapshot or a claim that configuration is already serving traffic.
Downstream NPM remains authoritative for conflicts, permissions and validation.
No mutation is automatically retried. On a failure or timeout, inspect the
corresponding list before deciding whether a user-initiated retry is appropriate.

Ordinary requests retain the shared 30-second timeout. All operations honor
caller cancellation, refuse redirects and bound API response bodies to 16 MiB.
Errors report HTTP status without forwarding backend bodies, credential-bearing
URLs or transport details. Shared transport tests exercise those boundaries.

## Certificates

List returns complete certificate objects as emitted by HyperHive, including
certificate metadata and the API's timestamp field names. DNS providers return
id/name/credentials template objects, including any additional API fields.
Credential-format templates do not represent configured provider secrets.

Download uses `{ "id": 42 }` and returns:

```json
{
  "data_base64": "UEs...",
  "content_type": "application/zip",
  "content_disposition": "attachment; filename=\"certificate-42\""
}
```

Decode data_base64 to recover the exact archive bytes. This can contain private
keys and must be treated as sensitive. No output file is written. Empty successful
binary responses remain empty data; the adapter does not assume every success is
JSON. Headers are copied from the API. Download has a three-minute timeout to
cover the API's two-minute downstream limit and is annotated read-only/idempotent
despite using POST.

Renew accepts the same ID input, preserves empty success, and uses an eleven-minute
client timeout to cover the API helper's ten-minute downstream operation. Earlier
caller deadlines still apply. HyperHive/NPM decide whether the certificate's
provider is renewable. Cancellation/timeout cannot establish whether the downstream
renewal completed; no retry or completion state is invented.

## Access lists

Creation requires name, satisfy_any, pass_auth, items and clients. Each item has
username/password, each client has directive/address. Supply allow or deny
for directives; addresses may include CIDR, IP or the API-supported `all` value.
Both arrays can be empty. Passwords are sensitive. Read-only IDs, timestamps,
owner fields and metadata ignored by the API's write helper are not writable
arguments. The helper strips these fields and returns no created ID.
Listing preserves the raw expanded API response and backend password masking.
Editing is excluded because its required effects remove stored entries.

## Streams

Create/update require incoming_port, forwarding_host, forwarding_port,
tcp_forwarding, udp_forwarding, certificate_id, meta and description. Both ports
must be in 1–65535, the host nonempty and certificate_id nonnegative (0 means no
certificate). Meta requires dns_provider_credentials, letsencrypt_agree and
dns_challenge. Update additionally requires positive id. Enabled state is changed
only by the dedicated actions; the create/edit helper ignores enabled.

Description is explicit, including an empty string for blank text. HyperHive
writes NPM first and the SQLite description second. If the description write
fails, the API can return HTTP 500 while the NPM change remains applied. The MCP
adapter does not retry or attempt deletion/rollback. Listing includes descriptions
from HyperHive, rather than querying NPM directly.

## Redirects and 404 hosts

Both require domain_names, certificate_id, meta, advanced_config, http2_support,
hsts_enabled, hsts_subdomains and ssl_forced. Domain arrays must be nonempty with
nonempty members, and certificate_id nonnegative. Meta requires letsencrypt_agree
and dns_challenge. Empty advanced_config and explicit false flags are preserved.
Update additionally requires positive id; enabled uses separate actions.

Redirects also require forward_scheme (http, https or $scheme),
forward_domain_name, forward_http_code, preserve_path and block_exploits. Status
can be an integer or decimal string; HyperHive's StringOrInt type normalizes it
before forwarding to NPM. The MCP input supports both forms, and NPM validates
its permitted redirect status/configuration combinations.

## Verification and limitations

Each verified tool has an independently specified fixture exercised through SDK
clients on authenticated HTTP and the real stdio executable. Tests verify
name/schema/annotations/endpoint metadata, exact method/path/body/authentication,
success and text fallback, explicit false/zero/empty values, missing/null/unknown
inputs, safe HTTP failures and no retries. Lists also exercise malformed JSON,
wrong shapes and empty/null arrays; downloads verify binary round-trip and headers.

The NGINX actual-handler test uses all actual HyperHive NGINX route handlers and
NPM HTTP helpers with disposable downstream HTTP fixtures and a temporary SQLite
description store. It verifies transformed payloads, raw expanded lists, binary
certificate transfer, downstream renewal acceptance and stream partial failure
without retries or deletion. It does not run NGINX, ACME, certificate issuance,
real renewal, DNS challenges or any production service. Check the batch checkpoint
for the checks completed during this run and its final outcome.

## Reusable upstream pools

Five additional [upstream tools](upstreams.md) manage/read pool definitions and capabilities. Proxy/location/stream tools accept optional bindings while preserving direct clients. Confirmed creates are completed during recovery, without deletion compensation; explicit upstream deletion remains excluded. The [release guide](../../../notes/npm-upstreams.md) and disposable exact-image tests cover NGINX traffic separately from the historical HTTP-handler fixtures above.
