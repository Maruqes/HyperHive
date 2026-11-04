# 07. `smartctl_error` stores the whole smartctl JSON document

| | |
| --- | --- |
| **Severity** | Low today, grows with fleet age |
| **Feature** | Disk health forecasting |
| **Modules** | `slave`, `master` |
| **Evidence** | Reproduced by `TestAuditSmartctlErrorIsNotTheWholeJSON` with a fake `smartctl`. **Not seen live:** all 7 current disks exit 0. |
| **User decision** | None |

## Summary

When smartctl exits non-zero, the slave's error text includes the entire JSON output. That text is sent as `smartctl_error`, saved with every sample, and returned by the list API and MCP.

## What is impacted

- **`disk_health.db`:** every sample for an affected disk stores a full smartctl document. At 4 samples a day and 90 days of raw retention, that is about 360 copies per disk.
- **`GET /diskhealth`:** `latest.smartctlError` carries the document for each affected disk, on every SMART page load.
- **MCP `list_disk_health`:** same, and it lands in an agent's context. A few affected disks could make the tool output very large.
- **`GET /diskhealth/{serial}`:** `disk.latest` carries it too. Chart points do not.

Nothing in the frontend displays this field. It is normalized in `services/smartdisk.ts:94` and `services/diskhealth.ts:50`, but no screen reads it.

## How it is impacted

smartctl's exit status is a bitmask, and several bits describe history, not an error in this read (from the man page):

| Bit | Value | Meaning |
| --- | --- | --- |
| 3 | 8 | SMART status check returned "DISK FAILING" |
| 4 | 16 | Prefail attributes at or below threshold |
| 5 | 32 | Some attributes were at or below threshold in the past |
| 6 | 64 | The device error log contains records of errors |
| 7 | 128 | The self-test log contains records of errors (ATA) |

Bits 5 and 6 can stay set for the rest of a drive's life. One old logged error means full JSON stored on every poll from then on. That is why this is harmless on today's clean disks and gets worse as they age.

The path:

1. `runSmartctlJSON` builds the error at `slave/smartdisk/smartdisk.go:202`.
2. `toProtoSmartDiskInfo` copies it at `slave/smartdisk/smartdiskGRPC.go:149`.
3. `sampleFromSmart` copies it at `master/services/disk_health.go:275`.
4. It is stored in `disk_health_samples.smartctl_error` (`master/db/disk_health.go:185`).
5. It is returned through `DiskHealthSample` (`master/db/disk_health.go:95`, `mcp/internal/client/disk_health.go:34`).

## Root cause

`slave/smartdisk/smartdisk.go:202`:

```go
return &resp, fmt.Errorf("smartctl reported issues for %s: %w: %s", device, err, strings.TrimSpace(string(output)))
```

## Evidence

The fake smartctl prints JSON and exits 64:

```
smartctl_error = smartctl reported issues for /dev/sda: exit status 64: {"model_name":"X","power_on_time":{"hours":10}}
```

## How to start fixing

**Do not change line 202** (see the first trap). Trim when the value leaves the slave, and again on the master.

1. **Slave:** in `toProtoSmartDiskInfo` (`smartdiskGRPC.go:149`), replace `smartctlErr.Error()` with a short summary. Use `errors.As` to get the `*exec.ExitError`, which works because line 202 wraps with `%w`, then decode the bits into readable text.

   ```
   smartctl exit 64: device error log contains records
   ```

   If it is not an `ExitError`, fall back to the text capped at about 512 bytes.

2. **Master:** in `sampleFromSmart` (`disk_health.go:275`), cap the stored string at about 512 bytes as well. Older slaves keep sending the full JSON until they are upgraded, and the docs expect mixed versions during rollout.

3. **Existing rows:** if any non-empty values exist at deployment, shorten them once with `UPDATE disk_health_samples SET smartctl_error = substr(smartctl_error, 1, 512) WHERE length(smartctl_error) > 512`. Space comes back only after a `VACUUM`, and none is needed for the 7 current disks.

## Traps

- **The slave's self-test progress reads the JSON out of this error.** `GetSelfTestProgress` calls `parseRemainingFromError(err)` (`smartdiskGRPC.go:281`), which searches the error text for a percentage. Its own comment says the error includes the JSON. Trim at line 202 and that fallback silently stops working. This is why the fix belongs at the proto boundary, and why the proof checks the proto field and not the Go error.
- **`GetSmartInfoContext` returns both a value and an error here.** A non-zero exit with valid JSON is normal. Do not turn it into a failure.
- **`smartctl_error` also feeds the SMART API.** Check nothing depends on the JSON. The audit found no reader, but search again before changing it.
- **Keep the field name.** It is `string smartctl_error = 49` in the proto and `smartctlError` in JSON. Change the content, not the contract.

## Done when

- `TestAuditSmartctlErrorIsNotTheWholeJSON` passes.
- A new slave test confirms `parseRemainingFromError` still finds a percentage in the full error.
- A new master test confirms an oversized value from an old slave is capped.
- `docs/disk-health.md` polling step 4 still reads correctly.
