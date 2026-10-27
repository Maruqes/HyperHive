# Dashboard D01–D02

## Changes

- `HyperHiveFront/HyperHive/app/dashboard.tsx`: replaced the gauge and segmented hover chart with a compact aggregate and labeled volume rows. Each row shows used, free and total; accessibility labels expose exact byte counts. Removed the storage Update button and NFS implementation copy. Existing inclusion, deduplication, calculations, pull refresh and five-second polling remain unchanged.
- `HyperHiveFront/HyperHive/tests/dashboard-storage.test.js`: synthetic fixture verifies filtered system mounts, Btrfs deduplication, exact accessibility values, automatic refresh with changed disk data, and failed-snapshot empty presentation.

## Evidence

- `npx jest --config tests/jest.support.config.cjs --testMatch '<rootDir>/tests/dashboard-storage.test.js' --runInBand --silent`: 2 passed.
- `npx tsc --noEmit --pretty false`: seven existing errors in `components/ui/heading/index.web.tsx`; no dashboard errors.
- `git diff --check -- app/dashboard.tsx tests/dashboard-storage.test.js`: passed.
- Backend trace: `services/hyperhive.ts:getDiskInfo` calls `/info/disk/{machine}`; `master/api/info.go:getDiskSummary` delegates to `master/services/info.go:GetDiskSummary`, which requests the connected agent; `slave/info/disk.go:GetDiskSummary` consolidates disk inventory, usage and IO. No backend changes.

## Limits

No screenshots were captured. On the requested browser retry, the in-app browser returned `net::ERR_CONNECTION_REFUSED` for `http://127.0.0.1:8081`; a safe isolated synthetic browser fixture was not available without a build or disrupting shared tools. Desktop, 375px, intermediate, light/dark and keyboard visual checks remain for coordinator review. No native-device, live-resource, production or export verification was performed. The existing failed-node snapshot handling presents no disk data rather than a distinct per-node error.
