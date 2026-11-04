package smartdisk

// Audit proofs for .problems/. Run them with .problems/proofs/run.sh, which adds
// this file to the package through `go test -overlay`.
//
// Each test asserts the CORRECT behaviour: FAIL means the bug is still present.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Problem 05: JSON shape from smartmontools nvmeprint.cpp print_self_test_log().
func TestAuditNVMeSelfTestLogIsParsed(t *testing.T) {
	raw := `{"nvme_self_test_log":{"nsid":-1,
		"current_self_test_operation":{"value":0,"string":"No self-test in progress"},
		"table":[{"self_test_code":{"value":2,"string":"Extended"},
		          "self_test_result":{"value":7,"string":"Completed: failed segments"},
		          "power_on_hours":1200,"segment":3}]}}`
	var resp smartctlResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	tests := extractSelfTests(&resp)
	t.Logf("parsed self tests: %+v", tests)
	if len(tests) != 1 {
		t.Fatalf("failed NVMe self-test not parsed: got %d entries", len(tests))
	}
}

// Problem 07: a non-zero exit (bit 6 = error log has entries) with valid JSON.
// This checks the proto field on purpose: the Go error must keep the JSON, because
// parseRemainingFromError reads it.
func TestAuditSmartctlErrorIsNotTheWholeJSON(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s' '{\"model_name\":\"X\",\"power_on_time\":{\"hours\":10}}'\nexit 64\n"
	if err := os.WriteFile(filepath.Join(dir, "smartctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	info, err := GetSmartInfoContext(context.Background(), "/dev/sda")
	if info == nil || err == nil {
		t.Fatalf("expected info plus error, got info=%v err=%v", info, err)
	}
	got := toProtoSmartDiskInfo(info, err).GetSmartctlError()
	t.Logf("smartctl_error = %s", got)
	if strings.Contains(got, "model_name") {
		t.Errorf("smartctl_error carries the full smartctl JSON document")
	}
}
