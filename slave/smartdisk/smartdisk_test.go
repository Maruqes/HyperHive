package smartdisk

import "testing"

func TestSelfTestFailed(t *testing.T) {
	cases := []struct {
		name      string
		status    string
		remaining int64
		want      bool
	}{
		{"completed without error", "Completed without error", 0, false},
		{"completed successfully", "Completed successfully", 0, false},
		{"completed read failure", "Completed: read failure", 0, true},
		{"completed read element failure", "The previous self-test completed having the read element of the test failed.", 0, true},
		{"not supported", "Self-test not supported", 0, false},
		{"aborted", "Aborted by host", 0, false},
		{"empty status", "", 0, false},
		{"still running", "Completed: read failure", 60, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := selfTestFailed(tc.status, tc.remaining); got != tc.want {
				t.Fatalf("selfTestFailed(%q, %d) = %v, want %v", tc.status, tc.remaining, got, tc.want)
			}
		})
	}
}

func TestSummarizeSMARTSmartStatus(t *testing.T) {
	cases := []struct {
		name   string
		status *smartStatus
		want   bool
	}{
		{"absent verdict is not a failure", nil, true},
		{"failed verdict", &smartStatus{Passed: false}, false},
		{"passed verdict", &smartStatus{Passed: true}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := summarizeSMART("/dev/sda", &smartctlResponse{SmartStatus: tc.status})
			if info.SmartPassed != tc.want {
				t.Fatalf("SmartPassed = %v, want %v", info.SmartPassed, tc.want)
			}
		})
	}
}

func TestExtractSelfTestsATACurrentFailure(t *testing.T) {
	resp := &smartctlResponse{ATASMARTData: &ataSmartData{}}
	resp.ATASMARTData.SelfTest.Status.String = "Completed: read failure"
	resp.ATASMARTData.SelfTest.Status.RemainingPercent = 0

	tests := extractSelfTests(resp)
	if len(tests) != 1 {
		t.Fatalf("got %d self-test entries, want 1", len(tests))
	}
	if tests[0].Passed {
		t.Fatalf("completed read failure marked as passed: %+v", tests[0])
	}
}

func TestExtractSelfTestsATACurrentSuccess(t *testing.T) {
	resp := &smartctlResponse{ATASMARTData: &ataSmartData{}}
	resp.ATASMARTData.SelfTest.Status.String = "Completed without error"
	resp.ATASMARTData.SelfTest.Status.RemainingPercent = 0

	tests := extractSelfTests(resp)
	if len(tests) != 1 {
		t.Fatalf("got %d self-test entries, want 1", len(tests))
	}
	if !tests[0].Passed {
		t.Fatalf("successful self-test marked as failed: %+v", tests[0])
	}
}
