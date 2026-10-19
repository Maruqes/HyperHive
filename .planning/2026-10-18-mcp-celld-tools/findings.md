# Findings
- Celld registers 17 unique routes; none currently mapped to MCP.
- Fleet writes return JSON HTTP 202, credentials/list HTTP 200; unhealthy health returns diagnostics at HTTP 503.
- Fleet edit may remove node data and is outside scope.
- Logs/events support filter queries, cursor pagination and NDJSON download with continuation/truncation headers.
- Read SilverBullet infrastructure index, operational memory and canonical MCP note. MCP note predates the seven current proxy tools; source is authoritative for this task. No remote access or new infrastructure candidates.

Logs tests found SDK v1.8.0 automatic object-output defaults decode/remarshal via float64, rounding nanosecond timestamps even after client UseNumber. A typed-input registration wrapper now emits the validated object as raw JSON with an explicit output schema and identical text fallback.
