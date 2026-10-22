# NPM adapter upgrade

## Goal
Update the adapter and NPM to the latest stable release with one desired-version setting and a verified rollback. User explicitly approved deleting current pools and their pooled proxy hosts/streams. Keep direct-target resources. No Go/dependency upgrades or remote deployment.

## Phases
1. Complete: latest NPM is 2.16.0; captured actual image identity, pristine templates and bundled OpenResty build.
2. Complete: updated exact-image adapter and startup. No legacy runtime support, per user preference.
3. Complete: explicit reset upgrade, private state/database/Compose/previous-master backup and rollback.
4. Complete: final isolated routing/lifecycle and reset/rollback regression runs. Unit/vet/certificate adapter tests pass. Full npmapi suite has a confirmed baseline MCP elicitation failure.
5. Complete: repository docs and canonical knowledge updated and verified.

## Next Step
No local implementation work remains. The user can run the documented reset upgrade on the master host and test real traffic/certificate renewal.

## Decisions
- User authorized deleting pooled proxy hosts and streams as well as pools; --reset-upstreams makes that destructive scope explicit.
- Save the previous master binary before rebuilding, since rollback restores old data and must also restore compatible application code.
- The desired image remains the single Go image constant. Exact config ID is a separate compatibility check, not a registry manifest digest.

## Errors and resolutions
- Initial go test ran at repo root without a module; reran from master.
- Local normal master build lacks libvirt headers; libvirt_dlopen build passed without changing project defaults.
- Existing npmapi MCP test fails because its client lacks elicitation; reproduced on untouched HEAD in a disposable source copy.
- Found and fixed preexisting use of image config IDs as registry manifest digests in fixture/generated Compose.
- Final checks must preserve the common include's permitted trailing-LF normalization.
