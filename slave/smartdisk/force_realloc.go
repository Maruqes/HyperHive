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

	// Covers the progress output with optional leading block number and optional "(x/x/x errors)" suffix.
	// Examples:
	//   "0.01% done, 0:15 elapsed. (0/0/0 errors)"
	//   "37585 0.84% done, 7:31:08 elapsed. (0/0/527405 errors)"
	//   "0.03% done, 0:12 elapsed"
	badBlocksProgressRegex = regexp.MustCompile(
		`^(?:(\d+)\s+)?([\d.]+)%\s+done,?\s+([\d:]+)\s+elapsed\.?(?:\s+\((\d+)/(\d+)/(\d+)\s+errors\))?$`,
	)
	// "Checking blocks 0 to 488386583"
	badBlocksRangeRegex = regexp.MustCompile(`^Checking blocks\s+(\d+)\s+to\s+(\d+)`)
	// Format: "Reading and comparing: 1234567" / "Testing with pattern: 1234567" / "Checking: 1234567"
	badBlocksActivityRegex = regexp.MustCompile(`(?:Reading and comparing|Testing with pattern|Checking|Writing and reading):\s*(\d+)`)
	// A bare integer line, e.g. "37584" when -s is printing carriage-return updates.
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

	// badblocks writes progress updates to stdout (some builds use stderr), so read both streams
	stdout, pipeErr := cmd.StdoutPipe()
	if pipeErr != nil {
		updateForceReallocError(device, fmt.Errorf("failed to create stdout pipe for badblocks: %w", pipeErr))
		return nil, pipeErr
	}
	// Mirror stderr into the same pipe so we don't miss progress/errors regardless of the stream used.
	if cmd.Stdout != nil {
		cmd.Stderr = cmd.Stdout
	}

	if err := cmd.Start(); err != nil {
		updateForceReallocError(device, fmt.Errorf("failed to start badblocks process: %w", err))
		return nil, err
	}

	go trackBadblocks(device, ctx, cmd, stdout)

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

	// Try to match the common progress line formats
	matches := badBlocksProgressRegex.FindStringSubmatch(line)
	if len(matches) == 7 {
		blockStr := matches[1]
		progressValue := matches[2]
		progressStr := progressValue + "%"
		elapsed := matches[3]
		readStr, writeStr, corruptStr := matches[4], matches[5], matches[6]

		blockVal, blockSet := parseOptionalInt(blockStr)
		readErrs, readSet := parseOptionalInt(readStr)
		writeErrs, writeSet := parseOptionalInt(writeStr)
		corruptErrs, corruptSet := parseOptionalInt(corruptStr)

		forceReallocMu.Lock()
		if state, ok := forceReallocStateMap[device]; ok && state.Status == "running" {
			state.ProgressPercent = progressStr
			state.ElapsedTime = elapsed
			if blockSet {
				state.CurrentBlock = blockVal
			}
			if readSet {
				state.ReadErrors = readErrs
			}
			if writeSet {
				state.WriteErrors = writeErrs
			}
			if corruptSet {
				state.CorruptionErrors = corruptErrs
			}

			if readSet && writeSet && corruptSet {
				state.Message = fmt.Sprintf("badblocks scanning: %s complete, %s elapsed (%d/%d/%d errors)",
					progressStr, elapsed, state.ReadErrors, state.WriteErrors, state.CorruptionErrors)
			} else {
				state.Message = fmt.Sprintf("badblocks scanning: %s complete, %s elapsed", progressStr, elapsed)
			}
		}
		forceReallocMu.Unlock()
		return
	}

	// Capture reported total block ranges to estimate total work
	rangeMatches := badBlocksRangeRegex.FindStringSubmatch(line)
	if len(rangeMatches) == 3 {
		start, startErr := strconv.ParseInt(rangeMatches[1], 10, 64)
		end, endErr := strconv.ParseInt(rangeMatches[2], 10, 64)
		if startErr == nil && endErr == nil && end >= start {
			total := (end - start) + 1
			if total < 0 {
				total = 0
			}
			forceReallocMu.Lock()
			if state, ok := forceReallocStateMap[device]; ok && state.Status == "running" {
				state.TotalBlocks = total
			}
			forceReallocMu.Unlock()
		}
		return
	}

	// Try to extract block numbers from explicit activity lines
	blockMatches := badBlocksActivityRegex.FindStringSubmatch(line)
	if len(blockMatches) == 2 {
		if currentBlock, err := strconv.ParseInt(blockMatches[1], 10, 64); err == nil {
			updateCurrentBlock(device, currentBlock)
		}
		return
	}

	// Fall back to treating bare integers as the current block counter.
	if badBlocksBlockOnlyRegex.MatchString(line) {
		if currentBlock, err := strconv.ParseInt(line, 10, 64); err == nil {
			updateCurrentBlock(device, currentBlock)
		}
	}
}

func updateCurrentBlock(device string, block int64) {
	forceReallocMu.Lock()
	if state, ok := forceReallocStateMap[device]; ok && state.Status == "running" {
		state.CurrentBlock = block
	}
	forceReallocMu.Unlock()
}

func parseOptionalInt(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
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
