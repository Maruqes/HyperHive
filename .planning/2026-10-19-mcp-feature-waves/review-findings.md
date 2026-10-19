# Additional correctness review — 2026-10-19

Scope: completed local MCP registry and selected backend recovery helpers. Two fresh GPT-6.1 Sol medium reviewers inspected VM safety/transport and VM/backend contracts; coordinator reviewed shared integration and DNS/Notes/SPA/ISO/previous-tool behavior. User explicitly authorized documentation updates. No live hosts or deployment.

## Reproduced and fixed

1. **Wrong-target integer mutation:** SDK schema/default handling converted integer arguments through float64. A request/approval for VM backup schedule 9007199254740993 could execute against 9007199254740992. Shared registration now retains the original input schema and SDK validation, but decodes original arguments with exact numbers before application handlers. Supports full int64 IDs, rejects raw fractions/overflow/non-object roots, and preserves numeric metadata and legacy union fields. Reproduced and regression-tested through HTTP and actual stdio, including MaxInt64, Notes IDs and ISO NFS-share body. Tool schemas remain equivalent. No approval flags introduced.
2. **Missing VM disk-resize recovery:** edit_vm could retain no actual previous disk size/path because inventory tolerates disk-inspection errors. Positive disk growth now requires a positive integral old size and a path matching the primary file-backed disk in secure inactive XML. Missing/inconsistent recovery blocks the write; CPU-only diskless edits remain supported. Invalid cases reproduced through both transports; valid retained state verified.
3. **Malformed DNS configuration writes:** backend trims copies for validation but writes original strings unescaped. Surrounding whitespace/control characters could reach the managed config before restart failed. Added write-only validation in client and tool; GET keeps existing trim behavior. HTTP/stdio/direct-client regressions prove invalid values make zero writes and valid multidomain aliases remain supported.
4. **Proxy response integer precision:** the older list_proxies client and typed SDK output rounded large identifiers/metadata. Client now decodes with UseNumber and registry emits raw structured/text JSON. Both-transport regression preserves exact large values; existing response contracts and null-list normalization retained.

The common original-object validation also closes no-input SPA root-null acceptance. No new tools, endpoint coverage or deletion permissions were added. The absence of further findings is a bounded source/local-fixture review result, not proof of zero bugs or live qualification.

## Verification

Focused reproducer tests failed before the relevant fix and passed afterward. Final full normal/race/vet gates and final source digest will be recorded in review-acceptance.json, run checkpoints and the updated source manifest. No production DNS restart, firewall action, guest/host change or file transfer qualification occurred.
