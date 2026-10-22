# HyperHive UI Reworks: Grill / Discovery Notes
Date: 2026-10-22 · Goal: Extract reusable guidelines from the ISO and NFS mount page reworks through a fast interview.

## Summary / key decisions
- User describes the ISO and NFS mount page reworks as a "devops kind style" and wants guidelines for future reworks.
- Prioritize operational clarity: scannable data, explicit status and direct actions.
- Choose density according to the amount of data on the page; ask the user during each rework rather than imposing a universal density.

- Use a common shell: title, status, primary action, filters and content. Choose the content layout per page.

- Keep the main page action in the header and frequent item actions visible on rows/cards; put secondary actions in an overflow menu.

- Show explicit status text with restrained color/icons. Distinguish loading, empty, failed and stale/unknown states; expose details when attention is needed.

- Use restrained surfaces and borders, color for status/actions, normal text for labels, and monospace for technical values such as paths, IDs and addresses.

## Q&A log

### Q1 — Meaning of DevOps style
- Asked: What should DevOps style primarily mean for future HyperHive reworks?
- Captured: "Operation clarity:dense, scannable data, explicit status and direct actions, but it should depend on the ammounts of data the page shows, so ask the user".
- Decision: Operational clarity is the priority. Density is page-specific and must be discussed with the user.
- Flags: None.

### Q2 — Shared page structure
- Asked: How much structure should future pages share?
- Captured: "Common shell; page-specific content layout (Recommended)".
- Decision: Reuse the common shell (title, status, primary action, filters and content); adapt the content layout to the page.
- Flags: None.

### Q3 — Action placement
- Asked: How should pages expose actions?
- Captured: "Main + frequent actions visible; secondary in menu (Recommended)".
- Decision: Main page action in the header; frequent item actions visible on rows/cards; secondary actions in an overflow menu.
- Flags: None.

### Q4 — Operational status
- Asked: How should operational state appear?
- Captured: "Explicit status; details when attention is needed (Recommended)".
- Decision: Explicit status text with restrained color/icons; distinguish loading, empty, failed and stale/unknown states. Show details when attention is needed.
- Flags: None.

### Q5 — Visual rules
- Asked: Which visual rule should carry across reworks?
- Captured: "Restrained UI; monospace for technical values (Recommended)".
- Decision: Restrained surfaces and borders; color for status/actions; normal text for labels; monospace for technical values such as paths, IDs and addresses.
- Flags: None.

### Q6 — Completeness
- Asked: Anything missing before consolidation?
- Captured: "Baseline is enough—consolidate it (Recommended)".
- Decision: Consolidate the five agreed rules without expanding scope.
- Flags: None.

## Open flags (pending input)
None. Baseline approved for consolidation. Curated local result: [UI rework guidelines](ui-rework-guidelines.md).
