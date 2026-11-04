# 05. NVMe self-test results are never parsed

| | |
| --- | --- |
| **Severity** | Medium |
| **Feature** | Disk health forecasting (the parser itself is older) |
| **Modules** | `slave`, with a small mirror in `master` |
| **Evidence** | Reproduced by `TestAuditNVMeSelfTestLogIsParsed`. JSON shape confirmed in upstream `nvmeprint.cpp` and in the smartctl 7.5 binary. Not run on a real NVMe disk. |
| **User decision** | None |

## Summary

The slave reads NVMe self-test logs using JSON keys that smartctl does not emit, so the list is always empty. A failed NVMe self-test therefore never lowers the score or raises an alert, although `docs/disk-health.md:59` says it does.

## What is impacted

- **Scoring:** the 30-point "Latest self-test failed" rule can never fire on NVMe.
- **Alerts:** no alert for a failed NVMe self-test.
- **Self-test progress:** `GetSelfTestProgress` cannot see a running NVMe test through parsed data, so the SMART page may show it as idle.
- **`SmartDiskInfo.self_tests`:** always empty for NVMe, including in the SMART API.

There are at least two NVMe disks in the live fleet, one on each host.

## How it is impacted

1. A scheduled or manual self-test runs on an NVMe disk and fails.
2. smartctl reports it under `nvme_self_test_log.table[]`.
3. `smartctlResponse` decodes `nvme_self_test_log` into a struct that expects `data[]`. There is no `data` key, so the slice stays empty and Go raises no error.
4. `extractSelfTests` returns nothing for the disk.
5. `sampleFromSmart` sees no tests, so `selfTestFailed` stays false.
6. The disk stays `Good`.

## Root cause

`slave/smartdisk/smartdisk.go:735-747`:

```go
type nvmeSelfTestLog struct {
    Data []nvmeSelfTestEntry `json:"data"`
}

type nvmeSelfTestEntry struct {
    Desc          string `json:"desc"`
    LifeTimeHours int64  `json:"life_time_hours"`
    Remaining     int64  `json:"remaining"`
    Status        struct {
        String string `json:"string"`
        Passed bool   `json:"passed"`
    } `json:"status"`
}
```

What smartctl actually writes (`print_self_test_log()` in `nvmeprint.cpp`):

```json
{
  "nvme_self_test_log": {
    "nsid": -1,
    "current_self_test_operation": { "value": 0, "string": "No self-test in progress" },
    "table": [
      {
        "self_test_code": { "value": 2, "string": "Extended" },
        "self_test_result": { "value": 7, "string": "Completed: failed segments" },
        "power_on_hours": 1200
      }
    ]
  }
}
```

While a test runs, `current_self_test_operation.value` is non-zero and a `current_self_test_completion_percent` key is added beside it. `desc`, `life_time_hours`, `remaining` and `status` do not appear in that function. `life_time_hours` is not in the smartctl 7.5 binary at all.

The `smartctl` man page confirms that `-a` on NVMe includes `-l selftest`. Older smartctl versions may behave differently, and the versions on the hosts were not checked.

## Evidence

```
parsed self tests: []
failed NVMe self-test not parsed: got 0 entries
```

## How to start fixing

1. Replace the structs:

   ```go
   type nvmeSelfTestLog struct {
       CurrentOperation struct {
           Value int64 `json:"value"`
       } `json:"current_self_test_operation"`
       CompletionPercent int64               `json:"current_self_test_completion_percent"`
       Table             []nvmeSelfTestEntry `json:"table"`
   }

   type nvmeSelfTestEntry struct {
       Code struct {
           String string `json:"string"`
       } `json:"self_test_code"`
       Result struct {
           Value  int64  `json:"value"`
           String string `json:"string"`
       } `json:"self_test_result"`
       PowerOnHours int64 `json:"power_on_hours"`
   }
   ```

2. Update `extractSelfTests` (`smartdisk.go:348`): `Type` from `Code.String`, `Status` from `Result.String`, `Passed` as `Result.Value == 0`, `LifetimeHours` from `PowerOnHours`, `RemainingPercent` 0.

3. For a running test, prepend an entry when `CurrentOperation.Value != 0`, with status `"in progress"` and remaining `100 - CompletionPercent`. That keeps the `self_tests[0]` contract the master relies on, and makes `GetSelfTestProgress` work.

4. Fix the `assessHealth` override (see the first trap).

5. Add slave tests for: passed; failed; aborted; running; empty table.

## Traps

- **`assessHealth` will start flagging healthy NVMe disks.** The override at `smartdisk.go:556-565` marks a healthy disk `warning` if **any** entry has `!Passed && RemainingPercent == 0`. NVMe entries have never reached this code. After the fix, one old `Aborted` entry in the 20-slot log turns a healthy disk into `warning`. Use `selfTestFailed(test.Status, test.RemainingPercent)` instead of `!test.Passed`, and consider checking only the latest entry. ATA aborted entries have the same issue today, so this fix helps them too.
- **Use `.value`, never `.string`, for the current operation.** The idle text is `"No self-test in progress"`, which contains "in progress". `containsInProgress` and the neutral list in `selfTestFailed` would both read it as running.
- **Check the failure strings against the neutral list.** Upstream results are: `Completed without error`, `Aborted: …`, `Fatal or unknown test error`, `Completed: unknown failed segment`, `Completed: failed segments`, `Unknown result (0x…)`. Today's lists classify these correctly, including treating `Unknown result` as failed. Add table tests so this stays true.
- **Two copies of the logic exist:** `selfTestFailed` in the slave and `SelfTestFailed` in `master/diskhealth/score.go:148`. Keep them in step.
- **A disk may have no log.** The NVMe self-test log is optional, so a missing `nvme_self_test_log` must leave `resp.NVMeSelfTestLog` nil and return no entries.
- **First poll after the upgrade may alert** if a disk already has a failed test on record. That is correct, but tell the user.

## Verifying on real hardware

The audit could not run smartctl on an NVMe disk (root needed, no passwordless sudo). Ask the user to run this on a host, or to authorize it:

```bash
sudo smartctl -a -j /dev/nvme0n1 | jq '.nvme_self_test_log'
```

Also record `smartctl --version` for each host. If a host's output has no `nvme_self_test_log`, the fix has no effect there, and the docs should say so.

## Done when

- `TestAuditNVMeSelfTestLogIsParsed` passes.
- New tests cover the cases in step 5 and confirm an old `Aborted` entry leaves a healthy disk healthy.
- `docs/disk-health.md:59` is accurate, including any version limit.
- Slaves are upgraded. This fix has no effect until they are.
