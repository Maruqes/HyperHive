# Findings

2026-10-18: Read current mcp/AGENTS.md, IMPLEMENT_TOOLS.md, UPDATE_STATE.md, README, completed Celld checkpoint and current registration/client code. Existing working tree contains the completed nine Celld tools; preserve it.

Baseline: API 16/314 (5.10%); NGINX 7/37 (18.92%), with 24 reported missing endpoints and six explicit DELETE exclusions. Proxy management: 7/8 raw, 7/7 eligible. Baseline go test ./... in mcp passed.

Read all NGINX area routes/handlers and local NPM helper implementations. Manual POST /certs/create calls CreateCertDetails, which invokes rollbackCreatedCertificate after creation/upload/read failures; exclude the operation rather than weakening its contract. Other candidate contracts still require final per-tool effect review.

Stream create/edit also persist descriptions through db.SetResourceDescription (SQL upsert, not deletion), and can partially apply if the description write fails after NPM succeeds. Create/edit helpers ignore enabled and create IDs; dedicated actions control enabled state. API mutations generally return empty HTTP 200 and do not expose downstream created IDs.

Access lists return raw expanded owner/items/clients; mutation payloads sanitize IDs/metadata and forward name, flags, items and clients only. Certificate list preserves API metadata, DNS providers are raw JSON, download is binary, renewal is empty success, and Let's Encrypt creation can take up to ten minutes in the downstream helper (shared MCP client currently has a 30-second timeout).

Consulted SilverBullet infrastructure index, operational memory and MCP behavior note. The MCP note still describes one tool and needs a canonical refresh after this batch, including already-completed proxy/Celld state. User explicitly authorized updating SilverBullet afterward; no additional documentation confirmation is needed. No new infrastructure hosts accessed or identified.

User chose all remaining permitted NGINX tools. Final queue: 21. Three newly reviewed exclusions: manual certificate creation, Let's Encrypt creation (upstream hard-deletes on failure) and access-list editing (upstream deletes authentication/client rows). Sources and contract cards are in mcp/runs/2026-10-18-nginx-01.md. All nine NGINX exclusions retain raw coverage denominator credit of zero.

Pinned upstream source review confirmed access-list edit and Let's Encrypt create deletion effects. DNS-provider route returns an array of {id,name,credentials} template objects. Ordinary shared timeout remains 30 seconds; download gets three minutes and renewal eleven minutes using a cloned HTTP client, retaining caller cancellation, response limits and redirect refusal. No endpoint URL/token is accepted as an argument. Stream flags are forwarded without inventing an additional both-false restriction not established by the HyperHive contract. Two initially guessed upstream schema URLs returned 404; authoritative existing internal/route source references were used instead.

Final precision regression: the pre-fix NGINX list path rounded 9007199254740993 in arbitrary metadata, observed through both HTTP and stdio. Updated the shared list decoder to UseNumber and emit explicitly typed, raw structured/text JSON while retaining SDK input validation and output schema. All six NGINX list tools now exercise this regression. Actual HyperHive/NPM fixture validation passed all 21 tools before this final serializer correction; final suites will revalidate it.
