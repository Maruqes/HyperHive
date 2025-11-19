package smartdisk

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ForceReallocProgress represents the progress of a badblocks-based force reallocation job.
type ForceReallocProgress struct {
	Device           string `json:"device"`
	Status           string `json:"status"`            // running, completed, error, idle, cancelled
	ProgressPercent  string `json:"progress_percent"`  // e.g., "0.01%" or "100%"
	CurrentBlock     int64  `json:"current_block"`     // Current block being tested
	TotalBlocks      int64  `json:"total_blocks"`      // Total blocks (0 if unknown)
	ElapsedTime      string `json:"elapsed_time"`      // e.g., "0:15"
	ReadErrors       int64  `json:"read_errors"`       // Count of read errors
	WriteErrors      int64  `json:"write_errors"`      // Count of write errors
	CorruptionErrors int64  `json:"corruption_errors"` // Count of corruption errors
	Message          string `json:"message"`
	Error            string `json:"error,omitempty"`
}

type forceReallocState struct {
	ForceReallocProgress
	cancel context.CancelFunc
	cmd    *exec.Cmd
}

var (
	forceReallocMu       sync.Mutex
	forceReallocStateMap = map[string]*forceReallocState{}

	// Example outputs we want to handle:
	//   "0.01% done, 0:15 elapsed. (0/0/0 errors)"
	//   "37585 0.84% done, 7:31:08 elapsed. (0/0/527405 errors)"
	//   "0.03% done, 0:12 elapsed"
	//   "37585 0.84% done, 7:31:08 elapsed"
	badBlocksProgressWithErrorsRegex = regexp.MustCompile(
		`^(?:(\d+)\s+)?([\d.]+)%\s+done,?\s+([\d:]+)\s+elapsed\.?\s+\((\d+)/(\d+)/(\d+)\s+errors\)$`,
	)
	badBlocksProgressSimpleRegex = regexp.MustCompile(
		`^(?:(\d+)\s+)?([\d.]+)%\s+done,?\s+([\d:]+)\s+elapsed\.?$`,
	)

	// "Checking blocks 0 to 488386583"
	badBlocksRangeRegex = regexp.MustCompile(
		`^Checking blocks\s+(\d+)\s+to\s+(\d+)`,
	)

	// A line that is just a block number, e.g. "37584"
	badBlocksBlockOnlyRegex = regexp.MustCompile(`^\d+$`)
)

// StartForceReallocation kicks off a non-destructive badblocks pass to force sector remapping.
func StartForceReallocation(device string) (*ForceReallocProgress, error) {
	var err error
	if device, err = validateDevicePath(device); err != nil {
		return nil, err
	}

	forceReallocMu.Lock()
	if existing, ok := forceReallocStateMap[device]; ok && existing.Status == "running" {
		// Return a copy to avoid data races
		copy := existing.ForceReallocProgress
		forceReallocMu.Unlock()
		return &copy, fmt.Errorf("force reallocation already running for %s (progress: %s)", device, copy.ProgressPercent)
	}
	ctx, cancel := context.WithCancel(context.Background())

	// Use -nsv flags:
	// -n = non-destructive read-write
	// -s = show progress
	// -v = verbose
	// stdbuf to reduce buffering so we see progress updates promptly
	cmd := exec.CommandContext(ctx, "stdbuf", "-o0", "-e0", "badblocks", "-nsv", device)

	state := &forceReallocState{
		ForceReallocProgress: ForceReallocProgress{
			Device:           device,
			Status:           "running",
			ProgressPercent:  "0.00%",
			CurrentBlock:     0,
			TotalBlocks:      0,
			ElapsedTime:      "0:00",
			ReadErrors:       0,
			WriteErrors:      0,
			CorruptionErrors: 0,
			Message:          "starting badblocks (non-destructive read-write test)",
		},
		cancel: cancel,
		cmd:    cmd,
	}
	forceReallocStateMap[device] = state
	forceReallocMu.Unlock()

	// badblocks writes progress to stderr
	stderr, pipeErr := cmd.StderrPipe()
	if pipeErr != nil {
		updateForceReallocError(device, fmt.Errorf("failed to create stderr pipe for badblocks: %w", pipeErr))
		return nil, pipeErr
	}

	if err := cmd.Start(); err != nil {
		updateForceReallocError(device, fmt.Errorf("failed to start badblocks process: %w", err))
		return nil, err
	}

	go trackBadblocks(device, ctx, cmd, stderr)

	return currentForceRealloc(device), nil
}

// GetForceReallocationProgress returns the current state for a device.
func GetForceReallocationProgress(device string) *ForceReallocProgress {
	device, _ = validateDevicePath(device) // best effort
	return currentForceRealloc(device)
}

// GetAllForceReallocationProgress returns the current state for all devices.
func GetAllForceReallocationProgress() []*ForceReallocProgress {
	forceReallocMu.Lock()
	defer forceReallocMu.Unlock()

	var results []*ForceReallocProgress
	for _, state := range forceReallocStateMap {
		copy := state.ForceReallocProgress
		results = append(results, &copy)
	}
	return results
}

// CancelForceReallocation cancels a running force reallocation job for a device.
func CancelForceReallocation(device string) error {
	var err error
	if device, err = validateDevicePath(device); err != nil {
		return err
	}

	forceReallocMu.Lock()
	state, ok := forceReallocStateMap[device]
	if !ok {
		forceReallocMu.Unlock()
		return fmt.Errorf("no force reallocation job found for %s", device)
	}

	if state.Status != "running" {
		currentStatus := state.Status
		forceReallocMu.Unlock()
		return fmt.Errorf("force reallocation job for %s is not running (current status: %s)", device, currentStatus)
	}

	cancel := state.cancel
	cmd := state.cmd
	forceReallocMu.Unlock()

	// Stop the context/process without holding the global mutex
	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}

	// Mark as cancelled (if still present and not already in another terminal state)
	forceReallocMu.Lock()
	if state, ok := forceReallocStateMap[device]; ok {
		if state.Status == "running" {
			state.Status = "cancelled"
			state.Message = "force reallocation cancelled by user"
			state.Error = ""
		}
	}
	forceReallocMu.Unlock()

	return nil
}

func trackBadblocks(device string, ctx context.Context, cmd *exec.Cmd, r io.ReadCloser) {
	defer r.Close()

	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 1024)
	scanner.Buffer(buf, 1024*1024)

	// Custom split function to handle both \n and \r
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}

		// Look for \r or \n
		if i := strings.IndexAny(string(data), "\r\n"); i >= 0 {
			// Found a delimiter
			if data[i] == '\r' {
				// Check if it's \r\n
				if i+1 < len(data) && data[i+1] == '\n' {
					return i + 2, data[0:i], nil
				}
				return i + 1, data[0:i], nil
			}
			// It's \n
			return i + 1, data[0:i], nil
		}

		// If we're at EOF, return what we have
		if atEOF {
			return len(data), data, nil
		}

		// Request more data
		return 0, nil, nil
	})

scanLoop:
	for scanner.Scan() {
		line := scanner.Text()

		select {
		case <-ctx.Done():
			// Context cancelled, break out and let cmd.Wait() handle cleanup
			break scanLoop
		default:
			updateForceReallocProgress(device, line)
		}
	}

	if scanErr := scanner.Err(); scanErr != nil && !errors.Is(ctx.Err(), context.Canceled) {
		updateForceReallocError(device, fmt.Errorf("badblocks read: %w", scanErr))
	}

	// Always reap the process
	waitErr := cmd.Wait()

	if waitErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			forceReallocMu.Lock()
			if state, ok := forceReallocStateMap[device]; ok {
				state.Status = "cancelled"
				if state.Message == "" {
					state.Message = "force reallocation cancelled by user"
				}
				state.Error = ""
			}
			forceReallocMu.Unlock()
			return
		}

		updateForceReallocError(device, fmt.Errorf("badblocks exited with error: %w", waitErr))
		return
	}

	forceReallocMu.Lock()
	if state, ok := forceReallocStateMap[device]; ok {
		state.Status = "completed"
		state.ProgressPercent = "100.00%"
		state.Message = "badblocks completed successfully"
		state.Error = ""
	}
	forceReallocMu.Unlock()
}

func updateForceReallocProgress(device, line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	// Basic "we're alive" message tweak
	forceReallocMu.Lock()
	if state, ok := forceReallocStateMap[device]; ok && state.Status == "running" {
		if state.Message == "starting badblocks (non-destructive read-write test)" {
			state.Message = "badblocks running, receiving output..."
		}
	}
	forceReallocMu.Unlock()

	// 1) Range line: "Checking blocks 0 to 488386583"
	if m := badBlocksRangeRegex.FindStringSubmatch(line); m != nil {
		first, _ := strconv.ParseInt(m[1], 10, 64)
		last, _ := strconv.ParseInt(m[2], 10, 64)
		total := last - first + 1
		if total < 0 {
			total = 0
		}

		forceReallocMu.Lock()
		if state, ok := forceReallocStateMap[device]; ok && state.Status == "running" {
			state.TotalBlocks = total
			state.CurrentBlock = first
		}
		forceReallocMu.Unlock()
		return
	}

	// 2) Full progress line with (r/w/c errors)
	if m := badBlocksProgressWithErrorsRegex.FindStringSubmatch(line); m != nil {
		var currentBlock int64
		if m[1] != "" {
			currentBlock, _ = strconv.ParseInt(m[1], 10, 64)
		}
		pctStr := m[2] + "%"
		elapsed := m[3]
		readErrs, _ := strconv.ParseInt(m[4], 10, 64)
		writeErrs, _ := strconv.ParseInt(m[5], 10, 64)
		corruptErrs, _ := strconv.ParseInt(m[6], 10, 64)

		forceReallocMu.Lock()
		if state, ok := forceReallocStateMap[device]; ok && state.Status == "running" {
			if currentBlock > 0 {
				state.CurrentBlock = currentBlock
			}
			state.ProgressPercent = pctStr
			state.ElapsedTime = elapsed
			state.ReadErrors = readErrs
			state.WriteErrors = writeErrs
			state.CorruptionErrors = corruptErrs

			totalErrs := readErrs + writeErrs + corruptErrs
			if totalErrs > 0 {
				state.Message = fmt.Sprintf("badblocks scanning: %s complete, %s elapsed (%d/%d/%d errors)",
					pctStr, elapsed, readErrs, writeErrs, corruptErrs)
			} else {
				state.Message = fmt.Sprintf("badblocks scanning: %s complete, %s elapsed",
					pctStr, elapsed)
			}
		}
		forceReallocMu.Unlock()
		return
	}

	// 3) Simpler progress line without error tuple
	if m := badBlocksProgressSimpleRegex.FindStringSubmatch(line); m != nil {
		var currentBlock int64
		if m[1] != "" {
			currentBlock, _ = strconv.ParseInt(m[1], 10, 64)
		}
		pctStr := m[2] + "%"
		elapsed := m[3]

		forceReallocMu.Lock()
		if state, ok := forceReallocStateMap[device]; ok && state.Status == "running" {
			if currentBlock > 0 {
				state.CurrentBlock = currentBlock
			}
			state.ProgressPercent = pctStr
			state.ElapsedTime = elapsed
			state.Message = fmt.Sprintf("badblocks scanning: %s complete, %s elapsed", pctStr, elapsed)
		}
		forceReallocMu.Unlock()
		return
	}

	// 4) A line that is just a block number (common in -w/-n modes)
	if badBlocksBlockOnlyRegex.MatchString(line) {
		currentBlock, err := strconv.ParseInt(line, 10, 64)
		if err == nil {
			forceReallocMu.Lock()
			if state, ok := forceReallocStateMap[device]; ok && state.Status == "running" {
				state.CurrentBlock = currentBlock
			}
			forceReallocMu.Unlock()
		}
		return
	}

	// Any other line is ignored for progress purposes
}

func updateForceReallocError(device string, err error) {
	forceReallocMu.Lock()
	defer forceReallocMu.Unlock()

	if state, ok := forceReallocStateMap[device]; ok {
		// Don't overwrite cancelled status with error
		if state.Status != "cancelled" {
			state.Status = "error"
			state.Error = err.Error()
			state.Message = "badblocks failed"
		}
	}
}

func currentForceRealloc(device string) *ForceReallocProgress {
	forceReallocMu.Lock()
	defer forceReallocMu.Unlock()

	state, ok := forceReallocStateMap[device]
	if !ok {
		return &ForceReallocProgress(
			Device:          device,
			Status:          "idle",
			ProgressPercent: "0.00%",
			ElapsedTime:     "0:00",
			Message:         "no force reallocation job",
		)
	}

	// Return a copy to avoid data races.
	copy := state.ForceReallocProgress
	return &copy
}

// ClearCompletedForceReallocation removes a completed/error/cancelled job from the state map
func ClearCompletedForceReallocation(device string) error {
	var err error
	if device, err = validateDevicePath(device); err != nil {
		return err
	}

	forceReallocMu.Lock()
	defer forceReallocMu.Unlock()

	state, ok := forceReallocStateMap[device]
	if !ok {
		return fmt.Errorf("no force reallocation job found for %s", device)
	}

	if state.Status == "running" {
		return fmt.Errorf("cannot clear running job for %s", device)
	}

	delete(forceReallocStateMap, device)
	return nil
}

// formatElapsedTime converts seconds to "h:mm:ss" or "m:ss" format
func formatElapsedTime(seconds int64) string {
	duration := time.Duration(seconds) * time.Second
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	secs := int(duration.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
}
