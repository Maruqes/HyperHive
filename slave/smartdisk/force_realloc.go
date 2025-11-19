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
	// badblocks output formats:
	// "0.01% done, 0:15 elapsed. (0/0/0 errors)"
	// "0.01% done, 0:15 elapsed (0/0/0 errors)" (no period)
	badBlocksProgressRegex = regexp.MustCompile(`([\d.]+)%\s+done,?\s+([\d:]+)\s+elapsed\.?\s+\((\d+)/(\d+)/(\d+)\s+errors\)`)
	// Also capture just the block numbers if present
	// Format: "Reading and comparing: 1234567"
	badBlocksBlockRegex = regexp.MustCompile(`(?:Reading and comparing|Checking|Testing with pattern):\s*(\d+)`)
)

// StartForceReallocation kicks off a non-destructive badblocks pass to force sector remapping.
func StartForceReallocation(device string) (*ForceReallocProgress, error) {
	var err error
	if device, err = validateDevicePath(device); err != nil {
		return nil, err
	}

	forceReallocMu.Lock()
	if existing, ok := forceReallocStateMap[device]; ok && existing.Status == "running" {
		defer forceReallocMu.Unlock()
		return &existing.ForceReallocProgress, fmt.Errorf("force reallocation already running for %s (progress: %s)", device, existing.ProgressPercent)
	}
	ctx, cancel := context.WithCancel(context.Background())

	// Use -nsvf flags:
	// -n = non-destructive read-write
	// -s = show progress
	// -v = verbose
	// Use stdbuf to disable buffering so we get realtime progress
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
	device, _ = validateDevicePath(device) // best effort; invalid device will just return nil
	return currentForceRealloc(device)
}

// GetAllForceReallocationProgress returns the current state for all devices.
func GetAllForceReallocationProgress() []*ForceReallocProgress {
	forceReallocMu.Lock()
	defer forceReallocMu.Unlock()

	var results []*ForceReallocProgress
	for _, state := range forceReallocStateMap {
		// Return a copy to avoid data races
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
		forceReallocMu.Unlock()
		return fmt.Errorf("force reallocation job for %s is not running (current status: %s)", device, state.Status)
	}

	// Call the cancel function to stop the badblocks process
	if state.cancel != nil {
		state.cancel()
	}

	// Kill the process directly to ensure it stops
	if state.cmd != nil && state.cmd.Process != nil {
		state.cmd.Process.Kill()
	}

	// Immediately update the status to prevent race conditions
	state.Status = "cancelled"
	state.Message = "force reallocation cancelled by user"
	state.Error = ""
	forceReallocMu.Unlock()

	return nil
}

func trackBadblocks(device string, ctx context.Context, cmd *exec.Cmd, r io.ReadCloser) {
	defer func() {
		r.Close()
		forceReallocMu.Lock()
		if state, ok := forceReallocStateMap[device]; ok && state.cancel != nil {
			state.cancel()
		}
		forceReallocMu.Unlock()
	}()

	// Create a scanner that splits on both \n and \r
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

	for scanner.Scan() {
		line := scanner.Text()

		// Check if context is cancelled during scanning
		select {
		case <-ctx.Done():
			// Context cancelled, kill the process
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			forceReallocMu.Lock()
			if state, ok := forceReallocStateMap[device]; ok {
				state.Status = "cancelled"
				state.Message = "force reallocation cancelled by user"
				state.Error = ""
			}
			forceReallocMu.Unlock()
			return
		default:
			updateForceReallocProgress(device, line)
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		// Don't report error if context was cancelled
		if !errors.Is(ctx.Err(), context.Canceled) {
			updateForceReallocError(device, fmt.Errorf("badblocks read: %w", scanErr))
		}
		return
	}

	waitErr := cmd.Wait()

	if waitErr != nil {
		// If the context was cancelled (user request), keep/correct the state
		// as "cancelled" instead of overwriting with an error.
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

	// Update last activity to show we're receiving output
	forceReallocMu.Lock()
	if state, ok := forceReallocStateMap[device]; ok && state.Status == "running" {
		// Update message to show we're getting output (even if we can't parse it yet)
		if state.Message == "starting badblocks (non-destructive read-write test)" {
			state.Message = "badblocks running, receiving output..."
		}
	}
	forceReallocMu.Unlock()

	// Try to match the progress line format: "0.01% done, 0:15 elapsed. (0/0/0 errors)"
	matches := badBlocksProgressRegex.FindStringSubmatch(line)
	if len(matches) == 6 {
		progressStr := matches[1] + "%"
		elapsed := matches[2]
		readErrs, _ := strconv.ParseInt(matches[3], 10, 64)
		writeErrs, _ := strconv.ParseInt(matches[4], 10, 64)
		corruptErrs, _ := strconv.ParseInt(matches[5], 10, 64)

		forceReallocMu.Lock()
		if state, ok := forceReallocStateMap[device]; ok && state.Status == "running" {
			state.ProgressPercent = progressStr
			state.ElapsedTime = elapsed
			state.ReadErrors = readErrs
			state.WriteErrors = writeErrs
			state.CorruptionErrors = corruptErrs

			totalErrs := readErrs + writeErrs + corruptErrs
			if totalErrs > 0 {
				state.Message = fmt.Sprintf("badblocks scanning: %s complete, %s elapsed (%d/%d/%d errors)",
					progressStr, elapsed, readErrs, writeErrs, corruptErrs)
			} else {
				state.Message = fmt.Sprintf("badblocks scanning: %s complete, %s elapsed",
					progressStr, elapsed)
			}
		}
		forceReallocMu.Unlock()
		return
	}

	// Try to extract block numbers if present
	blockMatches := badBlocksBlockRegex.FindStringSubmatch(line)
	if len(blockMatches) == 2 {
		currentBlock, err := strconv.ParseInt(blockMatches[1], 10, 64)
		if err == nil {
			forceReallocMu.Lock()
			if state, ok := forceReallocStateMap[device]; ok && state.Status == "running" {
				state.CurrentBlock = currentBlock
			}
			forceReallocMu.Unlock()
		}
	}
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
		return &ForceReallocProgress{
			Device:          device,
			Status:          "idle",
			ProgressPercent: "0.00%",
			ElapsedTime:     "0:00",
			Message:         "no force reallocation job",
		}
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
