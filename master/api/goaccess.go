package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	goAccessTimeout          = 2 * time.Minute
	goAccessRealtimePort     = 7890
	goAccessStartupTimeout   = 30 * time.Second
	goAccessStartupPollDelay = 200 * time.Millisecond
)

var (
	goAccessProcMu  sync.Mutex
	goAccessCmd     *exec.Cmd
	goAccessArgsKey string
)

func processRunning(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}

	if state := cmd.ProcessState; state != nil && state.Exited() {
		return false
	}

	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		return false
	}

	return true
}

func ensureGoAccessProcess(logDir, outputPath string, files []string, logFormat string) error {
	key := strings.Join(files, "|")

	goAccessProcMu.Lock()
	defer goAccessProcMu.Unlock()

	if processRunning(goAccessCmd) && goAccessArgsKey == key {
		return nil
	}

	// Terminate any previous instance if still around.
	if processRunning(goAccessCmd) {
		if err := goAccessCmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to stop previous goaccess process: %w", err)
		}
	}

	args := append([]string{}, files...)
	args = append(args,
		"--no-global-config",
		"--date-format=%d/%b/%Y",
		"--time-format=%T",
		"--log-format="+logFormat,
		"--real-time-html",
		fmt.Sprintf("--port=%d", goAccessRealtimePort),
		"-o", outputPath,
	)

	cmd := exec.Command("goaccess", args...)
	cmd.Dir = logDir
	cmd.Stdout = io.Discard

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("failed to start goaccess: %s", msg)
		}
		return fmt.Errorf("failed to start goaccess: %w", err)
	}

	goAccessCmd = cmd
	goAccessArgsKey = key

	go func(cmd *exec.Cmd, buf *bytes.Buffer) {
		err := cmd.Wait()
		msg := strings.TrimSpace(buf.String())
		if err != nil && !errors.Is(err, os.ErrProcessDone) && !strings.Contains(err.Error(), "signal: killed") {
			log.Printf("goaccess exited with error: %v", err)
		}
		if msg != "" {
			log.Printf("goaccess stderr: %s", msg)
		}

		goAccessProcMu.Lock()
		if goAccessCmd == cmd {
			goAccessCmd = nil
			goAccessArgsKey = ""
		}
		goAccessProcMu.Unlock()
	}(cmd, &stderr)

	return nil
}

func waitForGoAccessReport(ctx context.Context, path string) ([]byte, error) {
	deadline := time.NewTimer(goAccessStartupTimeout)
	defer deadline.Stop()

	ticker := time.NewTicker(goAccessStartupPollDelay)
	defer ticker.Stop()

	for {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return data, nil
		}

		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("timed out waiting for goaccess to generate report")
		case <-ticker.C:
		}
	}
}

func goAccessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), goAccessTimeout)
	defer cancel()

	workDir, err := os.Getwd()
	if err != nil {
		http.Error(w, "failed to resolve working directory", http.StatusInternalServerError)
		return
	}

	logDir := filepath.Join(workDir, "npm-data", "logs")
	if stat, err := os.Stat(logDir); err != nil || !stat.IsDir() {
		http.Error(w, "logs directory not found", http.StatusNotFound)
		return
	}

	pattern := filepath.Join(logDir, "proxy-host-*_access.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		http.Error(w, "failed to enumerate log files", http.StatusInternalServerError)
		return
	}
	if len(files) == 0 {
		http.Error(w, "no proxy access logs found", http.StatusNotFound)
		return
	}

	statsDir := filepath.Join(workDir, "npm-data", "stats")
	if err := os.MkdirAll(statsDir, 0o755); err != nil {
		http.Error(w, "failed to prepare stats directory", http.StatusInternalServerError)
		return
	}

	outputPath := filepath.Join(statsDir, "goaccess.html")

	logFormat := `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	if err := ensureGoAccessProcess(logDir, outputPath, files, logFormat); err != nil {
		http.Error(w, fmt.Sprintf("failed to start goaccess: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	data, err := waitForGoAccessReport(ctx, outputPath)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			http.Error(w, "goaccess timed out while generating report", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, "failed to read generated report", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func setupGoAccessAPI(r chi.Router) {
	r.Get("/goaccess", goAccessHandler)
}
