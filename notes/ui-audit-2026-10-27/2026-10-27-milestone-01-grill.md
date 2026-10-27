# Milestone 01 scope: Grill / Discovery Notes
Date: 2026-10-27 · Goal: Implement the tickets in Milestone 01 of the UI improvements plan.

## Summary / key decisions
- User requested: take tickets out of `hyperhive-ui-improvements-plan.md`, starting with "Milestone 01", using grill-me and ponytail.
- Milestone 01 contains H03, G01, VM02, NT01, NT02, SE01 and SE02.
- User clarified: "do the tickets inside milestone 01". Implement the milestone; retain its tickets in the plan.
- Start with H03 and follow the plan's ticket-by-ticket review checkpoints.

- H03: compact numeric summaries; per-core detail stays visible. User requests smaller Host Controls and correction of the chart event-handler warning before acceptance.

## Q&A log

### Q1 — Meaning of “take out”
- Asked: Remove the milestone, choose removals, or extract it for separate work?
- Captured: "do the tickets inside milestone 01".
- Decision: Implement H03, G01, VM02, NT01, NT02, SE01 and SE02 in the documented order.

### Q2 — H03 density
- Asked: Compact summaries with visible per-core detail, or collapsed per-core detail?
- Captured: "Compact summaries; per-core detail stays visible (Recommended)".
- Decision: Replace circular summary gauges with numeric metrics, retain the visible core section and existing Show more/less disclosure, and avoid stretching host controls.

### Q3 — H03 review correction
- Asked: Accept H03 and continue, or adjust before continuing?
- Captured: User reports "Unknown event handler property `onTouchPinch`" at `ZoomVoronoiContainer`, and "Host Controls are to bigg to much empty space".
- Decision: H03 is not accepted. Remove the unused zoom interaction causing the warning and compress Host Controls into compact full-width rows while preserving operation handlers and status/error text.

### Q4 — Corrected H03 acceptance
- Asked: Accept revised H03 and continue to G01?
- Captured: "Accept revised H03 and continue to G01 (Recommended)".
- Decision: H03 accepted on 2026-10-27; implement G01 next. Keep Docker-specific verification deferred until milestone 11.

### Q5 — G01 acceptance
- Asked: Accept G01 and continue to VM02?
- Captured: "Accept G01 and continue to VM02 (Recommended)".
- Decision: G01 accepted on 2026-10-27; implement VM02 next. Docker-specific verification remains deferred to milestone 11.

### Q6 — Continue after VM02
- Captured: "continue".
- Decision: Proceed to NT01 as explicitly instructed; VM02 review remains pending rather than being recorded as accepted.

### Q7 — VM02 and NT01 review
- Asked: Accept the labeled VM controls and the Notes View button, then continue to NT02?
- Captured: "accept but dont continue".
- Decision: VM02 and NT01 accepted on 2026-10-27; stop before NT02.

## Open flags (pending input)
- Notes preview density/search and settings details → inspect existing UI before asking further questions.
