# Findings

- Baseline: one tool, list_proxies, proxy coverage 1/8, NGINX 1/37, API 1/314.
- Proxy API mutations return empty HTTP 200, never an ID. Deletion is excluded by user policy.
- editProxy reads domain_names[0]; MCP must reject empty domains before dispatch.
- Create/edit NPM helpers forward all configuration fields except id (routing only on edit) and enabled (ignored). Dedicated enable/disable actions change enabled state.
- access_list_id accepts string or number; metadata is arbitrary JSON; location fields are path/scheme/host/port.
- Setup frontend accepts domain/id. Secure setup accepts camelCase public/private domain and certificate IDs, writes private then public, and can partially apply without rollback.
- Consulted infrastructure index, host pages, operational memory and canonical MCP/proxy behavior notes. Notes are source history, not live state.
- No unfamiliar hosts requiring membership confirmation were discovered for access.

Expanded null-input checks found that SDK slice inference permits null locations. Added explicit validation requiring an array (empty [] is valid) to preserve the complete-input contract before backend writes. Empty metadata object and numeric access-list IDs are also exercised through both transports.
