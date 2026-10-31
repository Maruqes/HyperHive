package extra

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakePowerCall struct {
	stdin string
	env   []string
	argv  string
}

type fakePowerHost struct {
	calls   []fakePowerCall
	outputs map[string]string // argv prefix -> output
	fail    map[string]error  // argv prefix -> error
	missing map[string]bool   // binaries absent from PATH
}

func installFakePowerHost(t *testing.T, now time.Time) *fakePowerHost {
	t.Helper()
	h := &fakePowerHost{outputs: map[string]string{}, fail: map[string]error{}, missing: map[string]bool{}}
	prevRunner, prevLook, prevNow := powerCmdRunner, powerLookPath, powerNow
	powerCmdRunner = func(_ context.Context, _ time.Duration, stdin string, env []string, name string, args ...string) (string, error) {
		argv := strings.Join(append([]string{name}, args...), " ")
		h.calls = append(h.calls, fakePowerCall{stdin: stdin, env: env, argv: argv})
		for prefix, err := range h.fail {
			if strings.HasPrefix(argv, prefix) {
				return "", err
			}
		}
		for prefix, out := range h.outputs {
			if strings.HasPrefix(argv, prefix) {
				return out, nil
			}
		}
		return "", nil
	}
	powerLookPath = func(name string) (string, error) {
		if h.missing[name] {
			return "", errors.New("not found")
		}
		return "/usr/sbin/" + name, nil
	}
	powerNow = func() time.Time { return now }
	t.Cleanup(func() { powerCmdRunner, powerLookPath, powerNow = prevRunner, prevLook, prevNow })
	return h
}

func (h *fakePowerHost) argvs() []string {
	out := make([]string, len(h.calls))
	for i, c := range h.calls {
		out[i] = c.argv
	}
	return out
}

func TestApplyPowerScheduleEnabledReplacesJobs(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	h := installFakePowerHost(t, now)
	h.outputs["atq"] = "7\tSat Oct 31 23:00:00 2026 h root\n9\tSun Nov  1 23:00:00 2026 h root\n"
	h.outputs["at "] = "warning: commands will be executed using /bin/sh\njob 12 at Sat Oct 31 23:00:00 2026"

	stop := time.Date(2026, 10, 31, 23, 0, 0, 0, time.UTC)
	wake := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	res, err := ApplyPowerSchedule(context.Background(), true, stop, wake)
	if err != nil {
		t.Fatalf("ApplyPowerSchedule: %v", err)
	}
	if res.JobID != "12" || !res.Stop.Equal(stop) || !res.Wake.Equal(wake) {
		t.Fatalf("unexpected result %+v", res)
	}

	want := []string{
		"systemctl enable --now atd",
		"atq -q h",
		"atrm 7 9",
		"at -q h -t 202610312300.00",
	}
	if got := h.argvs(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	last := h.calls[len(h.calls)-1]
	if last.stdin != "/usr/sbin/rtcwake -u -m off -t 1793520000\n" {
		t.Fatalf("at script = %q", last.stdin)
	}
	if len(last.env) != 1 || last.env[0] != "TZ=UTC" {
		t.Fatalf("at env = %q", last.env)
	}
}

func TestApplyPowerScheduleInstallsAtWhenMissing(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	h := installFakePowerHost(t, now)
	h.missing["at"] = true
	h.outputs["at "] = "job 1 at Sat Oct 31 23:00:00 2026"

	_, err := ApplyPowerSchedule(context.Background(), true, now.Add(time.Hour), now.Add(10*time.Hour))
	if err != nil {
		t.Fatalf("ApplyPowerSchedule: %v", err)
	}
	if got := h.argvs(); got[0] != "dnf -y install at" {
		t.Fatalf("first command = %q, want dnf install", got[0])
	}
}

func TestApplyPowerScheduleValidation(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		stop, wake time.Time
	}{
		{"stop in past", now.Add(-time.Minute), now.Add(time.Hour)},
		{"stop is now", now, now.Add(time.Hour)},
		{"wake before stop", now.Add(2 * time.Hour), now.Add(time.Hour)},
		{"wake equals stop", now.Add(time.Hour), now.Add(time.Hour)},
		{"wake too far", now.Add(time.Hour), now.Add(31 * 24 * time.Hour)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := installFakePowerHost(t, now)
			if _, err := ApplyPowerSchedule(context.Background(), true, tc.stop, tc.wake); err == nil {
				t.Fatal("expected validation error")
			}
			if len(h.calls) != 0 {
				t.Fatalf("validation failure ran commands: %q", h.argvs())
			}
		})
	}
}

func TestApplyPowerScheduleQueueFailure(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	h := installFakePowerHost(t, now)
	h.fail["at "] = errors.New("atd down")

	if _, err := ApplyPowerSchedule(context.Background(), true, now.Add(time.Hour), now.Add(2*time.Hour)); err == nil {
		t.Fatal("expected queue error")
	}
}

func TestApplyPowerScheduleUnexpectedAtOutput(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	h := installFakePowerHost(t, now)
	h.outputs["at "] = "garbage"

	if _, err := ApplyPowerSchedule(context.Background(), true, now.Add(time.Hour), now.Add(2*time.Hour)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestApplyPowerScheduleDisabled(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	h := installFakePowerHost(t, now)
	h.outputs["atq"] = "4\tSat Oct 31 23:00:00 2026 h root\n"

	res, err := ApplyPowerSchedule(context.Background(), false, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if res.JobID != "" {
		t.Fatalf("disabled result has job id %q", res.JobID)
	}
	want := "atq -q h|atrm 4|rtcwake -m disable"
	if got := strings.Join(h.argvs(), "|"); got != want {
		t.Fatalf("commands = %q, want %q", got, want)
	}
}

func TestApplyPowerScheduleDisabledWithoutAt(t *testing.T) {
	h := installFakePowerHost(t, time.Now())
	h.missing["atq"] = true

	if _, err := ApplyPowerSchedule(context.Background(), false, time.Time{}, time.Time{}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if got := strings.Join(h.argvs(), "|"); got != "rtcwake -m disable" {
		t.Fatalf("commands = %q", got)
	}
}

func TestParseAtqJobIDs(t *testing.T) {
	got := parseAtqJobIDs("3\tMon Nov  2 23:00:00 2026 h root\n\nnot-a-job line\n15\tTue Nov  3 23:00:00 2026 h root")
	if strings.Join(got, ",") != "3,15" {
		t.Fatalf("ids = %q", got)
	}
}
