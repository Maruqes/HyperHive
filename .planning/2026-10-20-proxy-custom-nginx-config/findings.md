# Findings
- master/npm/proxy.go forwards host advanced_config unchanged for create/update and decodes it for list.
- Both backend npm.Location and MCP client.Location omit advanced_config, dropping location snippets.
- editProxy uses submitted DomainNames[0] for management checks; missing domains panic and changed domains bypass restrictions.
- Existing SDK fixtures exercise HTTP and real stdio; master fixture exercises MCP through actual handlers into disposable NPM HTTP server.
- SilverBullet canonical proxy behavior page is stale regarding existing tests and edit behavior; MCP Contracts and limits is the canonical MCP contract.
- Official NPM component schema confirms optional location advanced_config strings: https://raw.githubusercontent.com/NginxProxyManager/nginx-proxy-manager/develop/backend/schema/components/proxy-host-object.json (reviewed 2026-10-20).
- Further SilverBullet retrieval is limited to destination metadata/parent verification needed for the two canonical updates; initial behavior and MCP section evidence is sufficient for implementation.
