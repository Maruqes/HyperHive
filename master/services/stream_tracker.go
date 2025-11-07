package services

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"512SvMan/npm"
	"github.com/Maruqes/512SvMan/logger"
)

type streamConnectionTracker struct {
	rawPath    string
	outputPath string
	mu         sync.Mutex
	open       map[string]string
}

var (
	streamTrackerOnce sync.Once
	streamTrackerStop context.CancelFunc
)

// StartStreamOpenTracker begins tailing the raw stream event log and keeps
// stream-proxy-open.logs limited to active connections only.
func StartStreamOpenTracker() {
	streamTrackerOnce.Do(func() {
		workdir, err := os.Getwd()
		if err != nil {
			logger.Error("stream tracker: getwd: %v", err)
			return
		}

		tracker := newStreamConnectionTracker(workdir)
		ctx, cancel := context.WithCancel(context.Background())
		streamTrackerStop = cancel
		go tracker.run(ctx)
	})
}

// StopStreamOpenTracker stops the background tracker if it was started.
func StopStreamOpenTracker() {
	if streamTrackerStop != nil {
		streamTrackerStop()
	}
}

func newStreamConnectionTracker(baseDir string) *streamConnectionTracker {
	logsDir := filepath.Join(baseDir, "npm-data", "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		logger.Error("stream tracker: mkdir %s: %v", logsDir, err)
	}
	tracker := &streamConnectionTracker{
		rawPath:    filepath.Join(logsDir, npm.StreamOpenEventsFile),
		outputPath: filepath.Join(logsDir, npm.StreamOpenLiveFile),
		open:       make(map[string]string),
	}
	tracker.writeSnapshot()
	return tracker
}

func (t *streamConnectionTracker) run(ctx context.Context) {
	logger.Info("stream tracker: watching %s", t.rawPath)
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		newOffset, err := t.consume(offset)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				offset = 0
				t.resetOpen()
			} else {
				logger.Warn("stream tracker: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		offset = newOffset
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (t *streamConnectionTracker) consume(offset int64) (int64, error) {
	file, err := os.Open(t.rawPath)
	if err != nil {
		return offset, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return offset, err
	}
	if info.Size() < offset {
		offset = 0
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return offset, err
		}
	} else {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return offset, err
		}
	}

	reader := bufio.NewReader(file)
	for {
		raw, err := reader.ReadString('\n')
		if len(raw) > 0 {
			offset += int64(len(raw))
			line := strings.TrimSpace(raw)
			if line != "" {
				t.handleLine(line)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return offset, err
		}
	}
	return offset, nil
}

func (t *streamConnectionTracker) handleLine(line string) {
	eventType := classifyStreamEvent(line)
	if eventType == streamEventUnknown {
		return
	}
	id, ok := extractConnectionID(line)
	if !ok {
		return
	}

	t.mu.Lock()
	changed := false
	switch eventType {
	case streamEventConnected:
		if existing, exists := t.open[id]; !exists || existing != line {
			t.open[id] = line
			changed = true
		}
	case streamEventDisconnected:
		if _, exists := t.open[id]; exists {
			delete(t.open, id)
			changed = true
		}
	}
	t.mu.Unlock()

	if changed {
		t.writeSnapshot()
	}
}

type streamEvent int

const (
	streamEventUnknown streamEvent = iota
	streamEventConnected
	streamEventDisconnected
)

func classifyStreamEvent(line string) streamEvent {
	if strings.Contains(line, " disconnected") {
		return streamEventDisconnected
	}
	if strings.Contains(line, " connected to ") {
		return streamEventConnected
	}
	return streamEventUnknown
}

func extractConnectionID(line string) (string, bool) {
	idx := strings.Index(line, "*")
	if idx == -1 || idx+1 >= len(line) {
		return "", false
	}
	idx++
	end := idx
	for end < len(line) {
		ch := line[end]
		if ch < '0' || ch > '9' {
			break
		}
		end++
	}
	if end == idx {
		return "", false
	}
	return line[idx:end], true
}

func (t *streamConnectionTracker) resetOpen() {
	t.mu.Lock()
	if len(t.open) == 0 {
		t.mu.Unlock()
		return
	}
	t.open = make(map[string]string)
	t.mu.Unlock()
	t.writeSnapshot()
}

func (t *streamConnectionTracker) writeSnapshot() {
	lines := t.snapshotLines()
	tmp := t.outputPath + ".tmp"
	var builder strings.Builder
	for i, line := range lines {
		if i > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(line)
	}
	if err := os.WriteFile(tmp, []byte(builder.String()), 0o644); err != nil {
		logger.Warn("stream tracker: write snapshot: %v", err)
		return
	}
	if err := os.Rename(tmp, t.outputPath); err != nil {
		logger.Warn("stream tracker: rename snapshot: %v", err)
	}
}

func (t *streamConnectionTracker) snapshotLines() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := make([]string, 0, len(t.open))
	for _, line := range t.open {
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return lines
}
