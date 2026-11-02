# HH Automations — multiple conditions with AND / OR

Date: 2026-11-02. Status: implemented 2026-11-02 (uncommitted). One deviation: `L && !(R)` reads back as the AND NOT joiner, not AND + row NOT; condition examples live in `automation-config-form.tsx` EXAMPLES, not `automation-examples.ts`.

## Goal

The "Run when" editor shows one condition input. Users want several conditions,
each on its own row, joined by AND / OR, added with an "Add condition" button.

## Feasibility (verified 2026-11-02)

The stored format stays one expr string; no backend, DB, proto or MCP change.

Checked with a throwaway test against `support/internal/domain.EvaluateCondition`
(fake Apps UPS and NAS):

| Condition | Result | Deps |
| --- | --- | --- |
| `(app('UPS').battery < 20) && (app('NAS').health == 'bad')` | true | UPS, NAS |
| `(app('UPS').battery > 50) \|\| (app('NAS').health == 'bad')` | true | UPS, NAS |
| `(app('UPS').battery > 50) && (app('NAS').health == 'bad')` | false | UPS (short-circuit) |
| `(app('UPS').missing > 1) \|\| (app('NAS').health == 'bad')` | false + error | UPS |
| `(app('NAS').health == 'bad') \|\| (app('UPS').missing > 1)` | true | NAS |

- Multiple Apps, `&&`, `||` and parentheses work today.
- Short-circuit deps are sound: the result can only change when an App that was
  read changes, and deps are recomputed on every evaluation
  (`support/internal/service/automations.go:279`, `:307-308`).
- Caveat: any error makes the whole condition false, and it is order dependent
  (rows 4 vs 5). Kept as-is in v1; the UI explains it (see step 5). A lenient
  "errored row = false" mode is a possible later backend change.

Frontend building blocks already exist in `HyperHiveFront/HyperHive`:
`tokenize()` is exported from `utils/condition-assist.ts`, `ConditionInput`
takes `value`/`onChange`, and the form treats `draft.condition` as a plain string.

## Operators (verified 2026-11-02 against `EvaluateCondition`)

Between two rows (joiner), chosen from a small dropdown:

| UI label | Meaning | Emitted expr |
| --- | --- | --- |
| AND | both true | `L && R` |
| OR | at least one true | `L \|\| R` |
| AND NOT | left true, right false | `L && !R` |
| OR NOT | left true or right false | `L \|\| !R` |
| XOR | exactly one true | `L != R` |

Per row: a **NOT** toggle that negates that row (`!(c)`).
Whole group: a **"None of / not all"** style toggle is covered by NOT on rows
plus the joiners (NAND = `!(a && b)`, NOR = `!(a || b)`); offered as a
"Negate result" switch that wraps the full expression in `!( … )`.

All seven forms (`&& !`, `|| !`, `!=` on booleans, `!( && )`, `!( || )`,
`and not`, leading `!`) compiled and returned the expected values with the
expected deps.

## Design

- Model: `rows: { id, text, not }[]`, `ops: ("and" | "or" | "andNot" | "orNot" | "xor")[]`
  (one between each pair) and `negate: boolean`.
- Evaluation order: **left to right, as shown on screen**. expr precedence
  differs per operator (`!=` binds tighter than `&&`, `&&` tighter than `||`),
  so the builder adds explicit parentheses: `((c1 && c2) || c3) != c4`. This
  makes mixed operators predictable without the user knowing precedence.
- Each row is emitted as `(text)`, or `!(text)` with NOT. One plain row with no
  NOT and no negate is emitted unwrapped, so existing automations round-trip
  unchanged.
- A read-only "Full condition" preview under the rows shows the real string.
- Parse (on load / when the saved condition changes): tokenize with `tokenize()`
  and recognise exactly the shape the builder emits (left-nested parentheses,
  the five joiners, optional `!` per row and around the whole). Anything else
  falls back to one row holding the whole text, so hand-written conditions keep
  working and are never rewritten unless the user edits.
- Round-trip rule: `build(parse(s))` must equal `s` or the editor uses the
  single-row fallback, so saving without edits never changes the stored text
  and the dirty check stays correct.

## Steps

1. **Pure helpers** — new `utils/condition-group.ts`:
   `parseConditionGroup(text) -> { rows, ops } | null` and
   `buildConditionGroup(rows, ops) -> string`, using `tokenize()`.
2. **Unit tests** — `utils/condition-group.test.ts` (jest-expo is configured;
   no tests exist yet, run with `npx jest utils/condition-group --watchAll=false`):
   single row, each of the five joiners, row NOT, negate-all, mixed joiners
   (left-to-right parentheses), strings containing `&&`/`)`, hand-written
   `a && b || c` fallback, empty rows dropped, round-trip identity.
3. **Component** — new `components/automations/condition-group.tsx`:
   rows of the existing `ConditionInput`; between rows a small joiner dropdown
   (AND / OR / AND NOT / OR NOT / XOR); a NOT chip on each row; a "Negate
   result" switch; per-row remove (hidden when only one row); "Add condition" button
   below; full-condition preview. Variables panel: show it once (for the focused
   row) instead of per row — add an optional `onFocus` to `ConditionInput` and
   insert snippets into the focused row.
4. **Wire into the form** — `components/automations/automation-config-form.tsx:144`:
   replace `ConditionInput` with `ConditionGroup value/onChange` on
   `draft.condition`. Test button, dirty check and save stay string-based.
   Examples: clicking one replaces all rows (parsed). Add two-App AND and XOR examples in
   `automation-examples.ts`.
5. **Error hint** — under the preview when there are 2+ rows:
   "If any row can't be read (missing stream, offline sender), the whole
   condition is false."
6. **Verify** — build the web app, open Automations in the browser pane, create
   two/three-row conditions with each joiner, NOT and negate, press Test, save, reload, confirm rows
   reappear; open an old single condition and confirm it is unchanged.
7. **Backend test (HyperHive)** — add the multi-App cases above to
   `support/internal/domain/conditions_test.go` so the contract is pinned.
8. **Docs** — update the HH Automations SilverBullet page (condition editor
   section) and `docs/` if it describes the editor.

## Out of scope (v1)

- Nested groups (`(a || b) && c` as UI groups) — fallback shows them as one row.
- Per-row true/false status (needs structured storage in HH Support).
- Lenient error handling for OR.
