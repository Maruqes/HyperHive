# Progress

## Completed implementation
- Latest NPM 2.16.0 image and pristine templates captured; adapter accepts the actual Docker config ID, not the registry manifest digest.
- Kept one desired release setting in npm.go. Added read-only compiled manifest for updater validation.
- User explicitly chose deleting old pools and pooled proxy hosts/streams; runtime supports latest image only.
- Updater requires explicit --reset-upstreams and the saved pre-upgrade master binary. It backs up directories, consistent HyperHive SQLite, Compose and old executable before reset. Old image is retained locally.
- Reset removes owned adapter assets/includes/generations and pooled NPM consumers; direct resources stay. Rollback restores old binary and all data/configuration, retaining newer state separately.
- Updated upstream/proxy settings preservation and default normalization for new NPM fields.
- Ignored private backups and rollback artifacts in Git.

## Validation
- Seven updater unit tests passed, including explicit reset scope, pending-journal reset, custom-location deletion, direct-resource and user-hook preservation, and full-state rollback.
- Real current-image routing fixture passed all seven HTTP/six stream methods, affinity/distribution, IPv4/IPv6, HTTP/custom/TCP/UDP, TLS SNI/WebSockets, service-token auth, drift/failure recovery and recreation.
- Disposable real-Docker reset upgrade and rollback passed; final direct HTTP/TCP probes passed with reset and full rollback.
- go test ./npm, certificate handler tests and go vet ./npm passed.
- Master builds with libvirt_dlopen here. Normal build lacks local libvirt headers; project build settings were not changed.
- One existing MCP elicitation test failure reproduced on untouched HEAD in a temporary source copy; no unrelated MCP code changed.

## Documentation
- Updated repository upgrade/rollback notes and compatibility evidence.
- Updated and reread canonical SilverBullet upstream compatibility and NPM bootstrap/upgrade pages.

## Limits
- No remote host deployment or real ACME issuance/renewal performed.
- Latest NPM bundles OpenResty 1.31.1.1; no assertion that every October NGINX advisory is patched.
