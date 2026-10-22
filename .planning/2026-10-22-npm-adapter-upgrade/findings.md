# Findings

- GitHub latest stable is NPM 2.16.0 as of 2026-10-22. Desired tag is defined only in master/npm/npm.go.
- Docker config ID is sha256:5c80aaf5e26c1f45a6ce9f58ad00edceabb717d68236ff5b06d6f4d1f1271742. Registry manifest digest is sha256:4393e642e233e5efd5adeb7f10918d4aaea71c9ed875b94190273fc8e87f9594; these identifiers serve different purposes.
- The pulled image bundles OpenResty 1.31.1.1. This task verifies functionality, not that every October NGINX advisory is patched.
- Stream template adds reuseport and per-stream logs. Common proxy include adds forwarded scheme/proto variables. HTTP host/location template bodies are unchanged.
- New proxy settings (trust_forwarded_proto and per-location access_list_id) need preservation across older-client edits and defaults normalized when comparing old journals.
- User approved removal of existing pools and all pooled proxy hosts/streams, including hosts with pooled custom locations; direct targets stay.
- Reset works only after NPM stops and a complete backup exists. It removes owned includes/assets/generations, soft-deletes pooled NPM resources, removes their generated files and clears HyperHive pool state.
- Rollback restores image, both NPM directories, base/override Compose, HyperHive SQLite and the saved previous master binary; newer directories/database files remain separate.
- Existing MCP contract test failure reproduced on untouched HEAD; no unrelated MCP changes made.
