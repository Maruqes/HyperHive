# HH Support API and frontend: Grill / Discovery Notes

Date: 2026-10-26 · Goal: Extract and settle the requirements for the shared observability API and HH frontend before writing an implementation plan.

Implementation authorized on 2026-10-26 and completed locally. Open technical flags below are resolved by defaults in the [implementation contract](../../docs/designs/hh-support.md); see [setup and operational boundaries](../../docs/hh-support.md). The interview remains the record of accepted product requirements. Automations and VM agents remain deferred.

## Summary / key decisions

Confirmed user requirements:
- HH Support runs as a separate service with its own database and uses the HH UI.
- The proxy must route `/api/support` and its subpaths to HH Support while preserving existing `/api` backend routing. Route precedence, prefix handling and WebSocket upgrades require implementation review.
- One HTTP ingestion interface usable by both custom applications and VM agents.
- The monitored identity is a generic App; HH Support does not need to know whether its sender is a VM, service or another runtime.
- Creating an App issues an App ID and secret used to submit data to that App. Which senders share an App is determined by their configuration, not automatically inferred from infrastructure.
- A mandatory App-to-VM hierarchy is not accepted. Senders may include an optional instance ID: named instances are individually visible; senders without one report directly to the App.
- Any monitored instance going offline makes its parent App offline, even if other instances remain healthy.
- Heartbeat expiry is configurable per App and shared by its instances, defaulting to two minutes without a heartbeat received by HH. The sender's timestamp does not determine the liveness deadline.
- When an authenticated request includes `instance_id`, HH tracks and monitors that instance under the App. Without `instance_id`, data belongs directly to the App and HH does not infer an instance. There is no instance retirement lifecycle or pre-registration requirement.
- Receive observability data, health checks, errors/severity and analytics; exact event contracts remain open.
- Use typed, named data streams described by Apps (type, unit, label). HH offers compatible widgets that users can choose and move; heartbeat monitoring remains built in. Payloads and the widget catalog remain open.
- Initial stream types: number (numeric measurements), text (string values), boolean (true/false), log (timestamped text with severity), and event (named occurrences with structured fields). Heartbeats remain built in rather than requiring a user-defined stream.
- Connectivity and explicit reported health are separate. An App can be online but unhealthy, for example when it reports a database failure while heartbeats continue. Logs and ordinary stream values do not automatically change health.
- Senders declare stream definitions through the API (name, type, unit, label); HH discovers them automatically. Users configure the corresponding widgets in the UI rather than pre-creating streams there.
- Each App has one shared dashboard layout persisted on the server. All HH users see that same layout; personal layouts and personal overrides are outside the initial scope.
- Initial dashboard widgets: current-value cards, line charts, gauges, text/boolean indicators, a live log viewer with severity filtering, and an event list. All widgets are movable and resizable.
- Stream history retention is configurable per App with a seven-day default for the proof of concept. Automatically delete expired history; retained data remains available after frontend refresh or reconnect.
- Keep the initial architecture simple for a few Apps; do not introduce large-scale infrastructure or require detailed capacity estimates. Logs may contain thousands or hundreds of thousands of entries (no per-second rate specified). Use ordinary indexed storage, bounded requests and paginated history rather than loading all records into the frontend.
- Dashboard streams combine samples across instances into one shared sequence. Separate displayed values require separate stream names supplied by the sender, rather than per-widget instance selection. Numeric charts show the shared sample sequence; current-value widgets show the latest received value, without automatic sums or averages.
- WebSockets stream live data and state changes from HH Support to the frontend. Do not use periodic frontend polling (for example every five seconds) for live updates. Apps and agents submit telemetry over HTTP.
- Register monitored Apps and create tabs and tags in the HH frontend.
- Tabs organize Apps, for example VMs and Local Apps. Each App belongs to exactly one tab; multiple-tab membership is not supported.
- Tags are simple labels, for example Go and marques2673sv, rather than structured key/value pairs. They categorize Apps across tabs and support investigation of shared failures.
- Plan three areas sequentially: API/frontend first, HH Automations second, VM agents third.
- Later automations should trigger scripts from conditions such as service downtime duration or CPU usage. Script language remains undecided.
- Checkpoint every interview answer here before asking the next question.

## Q&A log

### Q1 — Deployment and routing boundary
- Asked: Should HH Support run inside the existing backend or as a separate service?
- Captured: "Separate service with separate db using HH UI but our proxy should be ready for it... now /api goes to our backend we sould make that /api/support goes to the new service".
- Decision: Separate HH Support backend and database; integrate its frontend into HH. Reserve `/api/support` for the new service.
- Evidence: Initial source search found leading `/api` prefix handling in `master/api/api.go:253`; exact proxy ownership and forwarding behavior remain to be inspected. No routing changes made.
- Flags: Define authentication/trust boundary, exact upstream prefix, route precedence and WebSocket forwarding during technical planning.

### Q2 — Generic App identity and sender configuration
- Asked: Should the same application on three VMs appear as one App with monitored instances or as three Apps?
- Captured: "we dont know what a \"VM\" is.. we just know apps or instances"; "it should depend on how is configures"; "when creating an app it should give iis an app id and a secret... thatw e use to give info to that certain app".
- Decision: Keep App identity independent of infrastructure. Issue an App ID and secret at creation; senders target that App through configured credentials. Do not force a grouping model based on VMs.
- Flags: Optional instance identity was resolved in Q3; offline aggregation was resolved in Q4.

### Q3 — Optional instance identity
- Asked: Can senders sharing an App ID and secret include an optional instance ID to distinguish metrics and heartbeats?
- Captured: "Allow optional instance ID (Recommended)".
- Decision: Accept an optional instance ID. Named instances retain individually visible telemetry and heartbeats; omitted IDs report directly to the App.
- Flags: Offline aggregation was resolved in Q4. Q7 clarifies that instance identity comes solely from optional `instance_id`, without a retirement lifecycle.

### Q4 — App health when an instance is offline
- Asked: How should one offline instance affect overall App health?
- Captured: "Any instance offline makes the App offline".
- Decision: Any monitored instance that is offline makes the parent App offline. A healthy sibling does not keep the App healthy or merely degraded.
- Flags: Heartbeat expiry was resolved in Q6; Q7 replaces the instance lifecycle proposed in Q5.

### Q5 — Instance discovery and expected membership
- Asked: How should an instance become expected so its disappearance can mark the App offline?
- Captured: "First heartbeat registers it; explicit retirement (Recommended)".
- Superseded: The initial picker answer accepted first-heartbeat registration and explicit retirement. Q7 explicitly removes retirement and makes instance discovery depend on optional `instance_id` in incoming requests.
- Flags: Do not carry the superseded retirement lifecycle into the implementation plan.

### Q6 — Heartbeat timeout
- Asked: How should HH decide that heartbeats have stopped?
- Captured: "Configurable per App; default 2 minutes (Recommended)".
- Decision: Set the heartbeat timeout per App, shared by its instances, with a two-minute default measured from server receipt of the last heartbeat.
- Flags: A 30-second sending interval was illustrative, not an accepted agent default. Startup state, recovery and delayed/replayed heartbeat handling remain to be specified.

### Q7 — Correction: no instance retirement concept
- Asked: What should happen if a retired instance reports again?
- Captured: "there is not a concept of retired instances"; "there is an app with app id/secret optionally the api can recieve \"instance_id\". if it does it should keep an eye on it if it doesnt it does not even know it exists".
- Decision: Remove the retirement lifecycle entirely. An authenticated request with `instance_id` identifies an instance to track; a request without it identifies only the App. No infrastructure inference or instance pre-registration is required.
- Supersedes: Q5's retirement model and any pending retirement/reactivation questions. Q3 optional identity, Q4 offline aggregation and Q6 timeout remain accepted.

### Q8 — Tab membership
- Asked: Should each App belong to one tab or be allowed in several tabs?
- Captured: "an app should only be able to be on one tab".
- Decision: Each App belongs to exactly one tab. Tags provide categories that can span tabs.

### Q9 — Tag format
- Asked: Should tags be simple labels or structured key/value pairs?
- Captured: "Simple labels (Recommended)".
- Decision: Use simple labels such as `Go` and `marques2673sv`. Do not require key/value structure.

### Q10 — Realtime frontend delivery
- Asked: What should WebSockets support in the API/frontend phase?
- Captured: "the websockets should be used to stream the data to the frontend... i dont want anything like (update each 5 secs) i want realtime updates".
- Decision: Push data and state changes to the frontend through WebSockets as they occur. Do not implement interval-based polling for live updates. Keep sender ingestion on HTTP; WebSocket ingestion or commands were not requested.
- Flags: Initial snapshot/history loading, subscriptions, reconnect reconciliation and backpressure -> technical planning.

### Q11 — Data types and customizable UI exploration
- Asked: Which data types should the first API/frontend version accept?
- Captured: "we need to think of that"; "lets brainstorm a litlle bit on this to get to a conclusion"; "create like types of data for example (stream fo text, number, etc etc) and i can construct the UI move pieces and things like that?".
- User example: An App reports logs, CPU and RAM; asks whether typed data can drive a reactive, user-arranged UI.
- Status: No fixed five-type ingestion model accepted. Explore typed named streams and configurable dashboard widgets before deciding the API contract.
- Flags: Determine stream definitions, supported value types, widget mappings, layout ownership and schema evolution. Assistant examples remain proposals until accepted.

### Q12 — Typed streams and customizable dashboard
- Asked: Which dashboard direction fits: hybrid typed streams with movable widgets and built-in health, entirely custom, or fixed generated layout?
- Captured: "Typed streams + movable widgets + built-in health (Recommended)".
- Decision: Apps describe named streams with metadata such as type, unit and label; HH offers compatible widgets and users choose and move them. Keep heartbeat monitoring built in.
- Flags: Stream definition workflow, exact types, layouts, widget configuration and update compatibility remain open. No example schema has been approved.

### Q13 — Stream definition workflow
- Asked: Should streams be declared by senders through the API, pre-created in the UI, or supported through both paths?
- Captured: "Sender declares streams through API (Recommended)".
- Decision: Senders declare named streams and their type/unit/label metadata through the API. HH discovers these streams automatically; widget configuration happens in the UI.
- Flags: Exact initial stream types, definition updates and payload contracts remain to be decided.

### Q14 — Initial stream types
- Asked: Which stream types should the first version support?
- Captured: "All five: number, text, boolean, log, event (Recommended)".
- Decision: Support number, text, boolean, log and event streams initially. Examples: CPU/RAM numbers, version text, boolean indicators, severity-bearing logs and structured deployment/signup events. Keep heartbeat monitoring built in.
- Flags: Define validation, numeric metric semantics, log severity values, structured-event limits and explicit health reporting during contract design.

### Q15 — Connectivity versus explicit health
- Asked: How should HH represent an App that continues sending heartbeats but reports a failure such as database unavailability?
- Captured: "Separate connectivity and explicit health (Recommended)".
- Decision: Support explicit built-in health reports independently of heartbeat-based connectivity. Display online-but-unhealthy when appropriate. Do not automatically derive health from logs or ordinary stream values.
- Flags: Health payload/state vocabulary, instance health aggregation, freshness and recovery semantics -> contract design and further discovery where needed.

### Q16 — Dashboard layout ownership
- Asked: Should App dashboard layouts be shared, personal per user, or a shared default with personal overrides?
- Captured: "One shared layout per App (Recommended)".
- Decision: Store one shared layout per App on the server, visible to all HH users. Do not include personal layouts or overrides in the first version.
- Flags: Layout editing permissions and concurrent-edit handling remain to be specified. Q17 resolves stream display across instances.

### Q17 — Combining stream data across instances
- Asked: How should a widget display the same CPU stream published by multiple instances?
- Captured: "combine values... if i want separate values i will send them in differente streams".
- Decision: Combine data at the named stream level across instances. Senders use different streams when separate values are wanted. Do not make instance selection the widget model.
- Flags: Numeric combination was resolved in Q18. Instance identity remains relevant to heartbeat monitoring.

### Q18 — Numeric stream combination semantics
- Asked: Should numeric values from multiple senders form a shared sample sequence, a sum, or an average?
- Captured: "Shared sample sequence; latest current value (Recommended)".
- Decision: Append samples from all senders to the same named stream. Charts show this shared sequence; current-value widgets use the latest received sample. Do not automatically sum or average across senders.
- Flags: Historical event-time ordering, retry deduplication and chart downsampling remain technical contract details; preserve the accepted latest-received current-value behavior.

### Q19 — Initial widget catalog
- Asked: Which widget set should the first dashboard builder include?
- Captured: "Full proposed widget set (Recommended)".
- Decision: Include current-value cards, line charts, gauges, text/boolean indicators, a live log viewer with severity filtering, and an event list. Support moving and resizing all widgets.
- Flags: Stream/widget compatibility, gauge ranges, chart time windows and history retention -> technical design and further discovery.

### Q20 — History retention
- Asked: How should HH retain stream history?
- Captured: "Per-App retention; default 7 days (Recommended)".
- Decision: Configure retention per App, defaulting to seven days for the proof of concept, with automatic deletion after expiry. Persist history for charts/logs across frontend refresh and reconnect.
- Flags: Retention cleanup must be bounded and indexed. How expired latest values are represented and whether operational records have separate retention remain technical design items.

### Q21 — Scope correction
- Decision: Remove the previously discussed delivery features from these planning documents. Continue discovery of telemetry ingestion, health display, streams and dashboard configuration only. Script execution remains a separate future planning session.
- Maintenance: Removed the superseded Q21 answer and related pending questions as requested by the user.

### Q22 — Simplicity and expected data volume
- Asked: What initial App count and data frequency should guide API/database sizing?
- Captured: "dont think of that make it as simple as normal dont need to option or expect large scale infra... (some apps, but logs can be thousands or hunders of thousands)".
- Decision: Prefer a straightforward architecture for a few Apps. Do not pursue large-scale infrastructure or further capacity-estimation questions. Account for substantial log history with indexed queries, bounded reads and pagination.
- Evidence limit: Thousands/hundreds of thousands describes possible log volume without a time interval; do not interpret it as an ingestion rate.

## Open flags (pending input)

- Proxy integration details and deployment configuration -> repository review.
- Startup/recovery health, tab management and tag management/filtering -> user.
- Ingestion authentication, batching, limits and delivery guarantees -> discovery and repository evidence.
- Health semantics and missing-data detection -> user and technical design.
- WebSocket authentication, subscriptions, reconnect reconciliation and backpressure -> technical planning.
- Frontend workflows, permissions and proof-of-concept acceptance criteria -> user.
- Requested “ponytail” skill was not found in the available skill catalog or local skill roots; planner is available and being used for planning structure.

## Next discovery branches

Deployment boundary; App/instance model; tabs/tags; ingestion and health contracts; WebSockets; frontend workflows; permissions; storage/retention and scale; automation handoff; acceptance criteria and implementation increments.
