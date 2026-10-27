# DK02 — VM disks

Changed frontend `app/vms-disks.tsx` and added `tests/vm-disks.test.js`. Cards explicitly label the attached VM and show the complete selectable disk file path, with a copy button and failure feedback. Grow and Delete have separate visible labels; attached deletion explains the detach attempt in the existing confirmation.

Backend evidence: `master/services/vm_disk.go` returns distinct `disk_path` and `folder_path`; `Delete` checks the attachment, attempts detach and clears attachment before deleting. This change leaves backend checks and all mutation handlers intact. No real disks or hosts were accessed.

Validation: `npx jest --config tests/jest.support.config.cjs --testMatch '<rootDir>/tests/vm-disks.test.js' --runInBand` passed four synthetic tests covering file path and copy, clipboard failure, missing paths, attached deletion confirmation without mutation, and loading/empty/error states. `git diff --check` passed. No Expo export was run.

Limitations: the in-app browser retry for `http://127.0.0.1:8081/vms-disks` returned `net::ERR_CONNECTION_REFUSED`. Desktop, 375px, intermediate widths, keyboard and light/dark appearance have not been browser-verified; no screenshots were captured. Coordinator should verify these using the integrated fixture before acceptance. Native device and live API behavior were not tested.

TypeScript check completed with errors in shared `components/ui/heading/index.web.tsx`; no `vms-disks.tsx` diagnostics were reported. The check ran while other page owners were editing the shared checkout.
