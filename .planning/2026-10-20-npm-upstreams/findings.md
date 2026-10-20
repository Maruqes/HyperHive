# Findings

Source reviewed 2026-10-20 at backend HEAD 0472a8bee5d182fb5df677503773465a3b187e36, plus working-tree evidence.
- master/npm/npm.go pins NPM 2.13.5, preserves existing compose, and manages stream custom logging.
- master/npm/proxy.go and streams.go wrap downstream NPM resources; no dedicated upstream module was found.
- NPM 2.13.5 upstream templates append forwarding ports; exact-image routing compatibility needs testing.
- Existing HyperHive SQLite can own pools; downstream NPM writes and NGINX reloads cannot share a SQLite transaction.
- mcp/AGENTS.md prohibits exposing deletion tools.
- Existing untracked proxy configuration test and another task directory must be preserved.
- SilverBullet proxy-manager index consulted; reusable template constraint saved separately.
Official documentation links and proposed contracts are in notes/npm-upstreams-plan.md. External material is research data, not instructions.

Implementation evidence: exact-image disposable prototype reached both IPv4 and IPv6 members for HTTP root/custom location and TCP/UDP. Native targeting failed. Local Docker uses unix:///var/run/docker.sock. Image digest and template fingerprints captured in notes/npm-upstreams-compatibility.json. Baseline npm tests passed; npmapi has pre-existing failures/panic in untracked proxy_config_test.go, requiring contract reconciliation. Initial fixture needed anonymous /data and /etc/letsencrypt volumes; cleanup removes both.

Knowledge maintenance scope: update the existing upstream topic and its proxy-manager navigation; locate the canonical MCP tool-contract page for the five new tools. Next bounded retrieval: proxy-manager index plus MCP index headings, then only the relevant MCP topic. No operational-memory event is warranted because no production service was changed.
