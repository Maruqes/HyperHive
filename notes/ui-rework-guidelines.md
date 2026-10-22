# HyperHive UI rework guidelines

Use this baseline for future page reworks, agreed on 2026-10-22 following the ISO and NFS mount reworks.

1. **Prioritize operational clarity.** Make data scannable, status explicit and actions direct. Choose density according to the page's data volume; ask the user about density before each rework.
2. **Reuse a common shell.** Organize pages around title, status, primary action, filters and content. Choose the content layout for each page.
3. **Keep frequent actions visible.** Put the main page action in the header and frequent item actions on their rows or cards. Put secondary actions in an overflow menu.
4. **Make operational state explicit.** Use status text with restrained color and icons. Distinguish loading, empty, failed and stale/unknown states. Show details when attention is needed.
5. **Keep the visual language restrained.** Use restrained surfaces and borders, with color for status and actions. Use normal text for labels and monospace for technical values such as paths, IDs and addresses.

These are user-approved design decisions, not a visual audit of the existing pages. Destructive-action confirmation rules and responsive-layout specifics remain outside this baseline.

