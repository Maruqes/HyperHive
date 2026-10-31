package extra

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Maruqes/512SvMan/logger"
)

// powerScheduleQueue is the at(1) queue that holds only HH power-off jobs, so
// re-applying or disabling never touches other users' jobs.
const powerScheduleQueue = "h"

// maxPowerScheduleAhead bounds the wake epoch; the RTC alarm only reaches about a month ahead.
const maxPowerScheduleAhead = 30 * 24 * time.Hour

const (
	powerCmdTimeout     = 15 * time.Second
	powerInstallTimeout = 5 * time.Minute
)

// powerCmdRunner runs one command and returns its combined output. stdin may be empty.
// It is a variable so tests can replace host commands.
var powerCmdRunner = func(ctx context.Context, timeout time.Duration, stdin string, env []string, name string, args ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	output := strings.TrimSpace(out.String())
	if err != nil {
		if ctxErr := cmdCtx.Err(); ctxErr != nil {
			err = errors.Join(err, ctxErr)
		}
		if output != "" {
			return output, fmt.Errorf("%s: %w: %s", name, err, output)
		}
		return output, fmt.Errorf("%s: %w", name, err)
	}
	return output, nil
}

// powerLookPath resolves binaries; replaced in tests.
var powerLookPath = exec.LookPath

var powerNow = time.Now

var atJobIDPattern = regexp.MustCompile(`job (\d+) at`)

// PowerScheduleResult is what was applied; JobID is empty when disabled.
type PowerScheduleResult struct {
	JobID string
	Stop  time.Time
	Wake  time.Time
}

// ApplyPowerSchedule replaces this host's HH power-off job.
//
// Enabled: validate, ensure at/atd, remove previous HH jobs, then queue
// `rtcwake -u -m off -t <wake>` at stop. If queueing fails after removal the host
// has no HH job and simply stays on, which is the safe state.
// Disabled: remove HH jobs and clear the RTC alarm.
func ApplyPowerSchedule(ctx context.Context, enabled bool, stop, wake time.Time) (PowerScheduleResult, error) {
	if !enabled {
		return PowerScheduleResult{}, disablePowerSchedule(ctx)
	}

	if err := validatePowerWindow(powerNow(), stop, wake); err != nil {
		return PowerScheduleResult{}, err
	}

	if err := ensureAtTooling(ctx); err != nil {
		return PowerScheduleResult{}, err
	}
	rtcwake, err := powerLookPath("rtcwake")
	if err != nil {
		return PowerScheduleResult{}, fmt.Errorf("power schedule: rtcwake not found: %w", err)
	}

	if err := removePowerScheduleJobs(ctx); err != nil {
		return PowerScheduleResult{}, err
	}

	jobID, err := queuePowerOff(ctx, rtcwake, stop, wake)
	if err != nil {
		return PowerScheduleResult{}, err
	}
	logger.Infof("power schedule: job %s powers off at %s, wakes at %s", jobID, stop.UTC().Format(time.RFC3339), wake.UTC().Format(time.RFC3339))
	return PowerScheduleResult{JobID: jobID, Stop: stop, Wake: wake}, nil
}

func validatePowerWindow(now, stop, wake time.Time) error {
	if !stop.After(now) {
		return fmt.Errorf("power schedule: stop time %s is not in the future", stop.UTC().Format(time.RFC3339))
	}
	if !wake.After(stop) {
		return fmt.Errorf("power schedule: wake time must be after stop time")
	}
	if wake.Sub(now) > maxPowerScheduleAhead {
		return fmt.Errorf("power schedule: wake time is more than %s ahead", maxPowerScheduleAhead)
	}
	return nil
}

func disablePowerSchedule(ctx context.Context) error {
	// Without at installed there can be no HH job; don't install packages just to disable.
	if _, err := powerLookPath("atq"); err == nil {
		if err := removePowerScheduleJobs(ctx); err != nil {
			return err
		}
	}
	if _, err := powerCmdRunner(ctx, powerCmdTimeout, "", nil, "rtcwake", "-m", "disable"); err != nil {
		return fmt.Errorf("power schedule: clear RTC alarm: %w", err)
	}
	logger.Info("power schedule: disabled, HH jobs removed and RTC alarm cleared")
	return nil
}

func ensureAtTooling(ctx context.Context) error {
	if _, err := powerLookPath("at"); err != nil {
		logger.Info("power schedule: installing at")
		if _, err := powerCmdRunner(ctx, powerInstallTimeout, "", nil, "dnf", "-y", "install", "at"); err != nil {
			return fmt.Errorf("power schedule: install at: %w", err)
		}
	}
	if _, err := powerCmdRunner(ctx, powerCmdTimeout, "", nil, "systemctl", "enable", "--now", "atd"); err != nil {
		return fmt.Errorf("power schedule: enable atd: %w", err)
	}
	return nil
}

func removePowerScheduleJobs(ctx context.Context) error {
	out, err := powerCmdRunner(ctx, powerCmdTimeout, "", nil, "atq", "-q", powerScheduleQueue)
	if err != nil {
		return fmt.Errorf("power schedule: list jobs: %w", err)
	}
	ids := parseAtqJobIDs(out)
	if len(ids) == 0 {
		return nil
	}
	if _, err := powerCmdRunner(ctx, powerCmdTimeout, "", nil, "atrm", ids...); err != nil {
		return fmt.Errorf("power schedule: remove jobs %s: %w", strings.Join(ids, ","), err)
	}
	return nil
}

// parseAtqJobIDs reads the job ids from `atq -q h` lines like
// "12\tSat Oct 31 23:00:00 2026 h root".
func parseAtqJobIDs(out string) []string {
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if _, err := strconv.Atoi(fields[0]); err == nil {
			ids = append(ids, fields[0])
		}
	}
	return ids
}

func queuePowerOff(ctx context.Context, rtcwake string, stop, wake time.Time) (string, error) {
	// TZ=UTC makes at read -t in UTC, independent of the host timezone.
	atTime := stop.UTC().Format("200601021504.05")
	script := fmt.Sprintf("%s -u -m off -t %d\n", rtcwake, wake.Unix())
	out, err := powerCmdRunner(ctx, powerCmdTimeout, script, []string{"TZ=UTC"}, "at", "-q", powerScheduleQueue, "-t", atTime)
	if err != nil {
		return "", fmt.Errorf("power schedule: queue power-off: %w", err)
	}
	m := atJobIDPattern.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("power schedule: unexpected at output %q", out)
	}
	return m[1], nil
}
