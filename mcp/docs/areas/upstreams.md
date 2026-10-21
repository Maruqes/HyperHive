# Reusable upstream tools

Registration and safety: [shared policy and classifications](../tool-safety.md).

Both authenticated HTTP and stdio expose five named upstream tools. They use the shared HyperHive client, authentication, cancellation, response limits and safe errors; mutations are never retried. No upstream deletion tool, alias or generic dispatcher is exposed.

| Tool | REST endpoint | Output |
| --- | --- | --- |
| list_upstreams | GET /upstream/list | upstreams array |
| get_upstream | GET /upstream/{id} | upstream definition and references |
| get_upstream_capabilities | GET /upstream/capabilities | capabilities object, including unknown/recovery states |
| create_upstream | POST /upstream/create | upstream definition after activation |
| update_upstream | PUT /upstream/{id} | upstream definition after activation |

Create accepts name, loadbalancer_type, server_arr and optional context (http by default) / loadbalancer_options.hash_key. Members require IP literals, ports 1–65535 and validated settings_string. There are at most 128 members. Update adds positive immutable id and expected revision; stale revisions or external consumer drift return 409. Master remains authoritative for context, build-specific capabilities, parameter parsing, names and method compatibility. An unknown capability never enables mutation. Definitions and reference summaries preserve all API fields and wire integer values.

Proxy, custom-location and stream mutation inputs accept optional upstream_id. A positive ID selects a compatible pool, null explicitly detaches with valid direct fields, and omission on update preserves the binding. Complete MCP configurations use an empty direct host and zero port for pool targets. HTTP schemes remain explicit. Locations also preserve optional advanced_config. Managed frontend proxies are checked by ID and retain their restrictions. Existing direct payloads keep their contracts.

Master journals uncertain outcomes, retains working definitions on failed edits and reports 503/recovery state rather than retrying. A confirmed create whose response/local commit failed is completed during startup reconciliation; it is never deleted as creation rollback. Unknown creates without an observable operation marker remain blocked for manual inspection. List/capabilities expose recovery identifiers when applicable. A request that returned an error may subsequently be completed by recovery, so inspect state before issuing another create.

The exact-image adapter and protocol/recovery acceptance fixture are documented in [the release guide](../../../notes/npm-upstreams.md). Backend HTTPS SNI/verification is independent from public NPM certificates. UDP failover tests send independent datagrams and allow session-related loss; they do not promise replay or transparent failover of each packet.

Validation: SDK discovery/invocation on authenticated HTTP and the actual stdio executable, method/path/auth/body/false/zero/null/omission checks, safe failure responses, no retries, and exact large-integer response text. Master separately tests actual handlers, SQLite/coordinator behavior and disposable exact-image NGINX traffic. Production deployment and ACME issuance are outside these local checks.
