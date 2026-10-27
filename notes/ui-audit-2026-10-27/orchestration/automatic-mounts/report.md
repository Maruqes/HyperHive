# Automatic mounts — coordinator handoff

AM01 and AM02 implemented in `HyperHiveFront/HyperHive/app/btrfs-automatic-mounts.tsx`. Rows omit repeated machine and UUID metadata; Details exposes selectable mount point, machine, UUID and compression. Remove rule opens a confirmation identifying the selected rule and explaining that removal does not unmount. Cancellation sends no request; failure leaves the confirmation open for retry; successful deletion retains the existing ID payload and refresh.

Backend evidence: `master/api/btrfs.go` removeAutomaticMount decodes positive ID and calls `BTRFSService.RemoveAutomaticMount`; `master/services/btrfs.go` calls `db.DeleteBtrfs` and rejects zero affected rows; `master/db/btrfs.go` deletes the row by ID. `AutoMountRaid` reads remaining rows for the machine and calls MountRaid. SilverBullet `knowledge/projects/HyperHive/features/storage/btrfs/automatic-mounts` already records the same distinction; no update needed. No live operations performed.

Files: page above; `tests/automatic-mounts-dialog.test.js`; `tests/jest.automatic-mounts.config.cjs`. No shared files or backend files changed.

Validation: `./node_modules/.bin/jest --config tests/jest.automatic-mounts.config.cjs --runInBand` passed 4 tests covering details, cancel, correct ID, deletion failure/retry, loading, empty and load failure/refresh states. `npx tsc --noEmit --pretty false` reported seven existing `components/ui/heading/index.web.tsx` WebkitBoxOrient errors, no page errors. `git diff --check` passed. No full export/build was run.

Coordinator action: `tests/storage-pages.test.js` has a pre-existing automatic-mount test that expects UUID in the row and immediate deletion; update that shared test to the new confirmation flow when integrating. It was left untouched under group ownership rules.

Browser transport recovered on retry, but the loopback Expo preview did not open port 18847 during startup; the browser returned `net::ERR_CONNECTION_REFUSED`. The disposable fixture and preview were stopped. Desktop, 375px, intermediate widths, keyboard navigation and light/dark visual checks remain unverified; no screenshots were produced. Native hardware and live infrastructure were not tested.
