# GP01 and GP02

Frontend files: `app/gpus.tsx`, `services/pci.ts`, `tests/gpus.test.js`, `tests/pci-gpu-inventory.test.js`, `tests/jest.gpus.config.cjs`. Backend files: `master/api/pci.go`, `master/api/pci_gpu_inventory_test.go`.

The host selector, refresh button and counts share a wrapping toolbar. Counts are withheld while the selected host is loading or has failed. Loading, failed requests, no selected host and a successful empty response have distinct presentations; empty and error states point to Refresh and host selection. A request generation guard prevents an earlier host's response from overwriting the current host. The existing selected-machine hook and actions remain in use.

The PCI service rejects malformed responses, while accepting arrays and objects containing a GPU array. `listHostGpus` has only the GPU page as a production caller. The HTTP endpoint now returns `{ "gpus": [] }` for empty inventory; previously Go's `omitempty` omitted the field. A nil backend response returns 502. An older backend returning `{}` will now show an error until updated alongside the frontend.

Backend evidence: `master/api/pci.go` calls `ListHostGPUs`; `slave/pci/pci.go` includes devices classified `IsGPU`, while a separate `ListHostGPUsWithIOMMU` applies an IOMMU filter. The API supplies no empty-result reason, so none is asserted.

Validation: `npx jest --config tests/jest.gpus.config.cjs --runInBand` passed 10 tests covering synthetic populated and empty results, malformed responses, loading, error, retry, and out-of-order host switching. `go test ./api -run '^TestWriteHostGPUs$' -count=1` passed, covering empty, populated and nil responses. Both repositories' `git diff --check` passed for changed tracked files. Earlier `npx tsc --noEmit --pretty false` reported errors in other files, with no `app/gpus.tsx` diagnostics. No full export was run.

Screenshots: none. On retry, the in-app browser became available but `http://127.0.0.1:8081/gpus` returned `net::ERR_CONNECTION_REFUSED`; no existing synthetic GPU fixture server was available. Desktop, 375px, intermediate widths, keyboard navigation, and light/dark appearance require integrated fixture verification. No native device or live resource operations were tested.
