package services

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// GenerateGoAccessReport runs goaccess against the NPM access logs and returns the HTML report.
func GenerateGoAccessReport() ([]byte, error) {
	goAccessPath, err := exec.LookPath("goaccess")
	if err != nil {
		return nil, fmt.Errorf("goaccess binary not found in PATH")
	}

	workDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}

	npmDataDir := filepath.Join(workDir, "npm-data")
	confPath := filepath.Join(npmDataDir, "goaccess.conf")
	logDir := filepath.Join(npmDataDir, "logs")

	entries, err := os.ReadDir(logDir)
	if err != nil {
		return nil, fmt.Errorf("read log dir: %w", err)
	}

	var logFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, "_access.log") || name == "fallback_access.log" {
			logFiles = append(logFiles, filepath.Join(logDir, name))
		}
	}

	if len(logFiles) == 0 {
		return nil, fmt.Errorf("no access log files found in %s", logDir)
	}

	sort.Strings(logFiles)

	args := []string{"--config-file=" + confPath, "--output=-"}
	for _, file := range logFiles {
		args = append(args, "--log-file="+file)
	}

	cmd := exec.Command(goAccessPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return nil, fmt.Errorf("goaccess: %s", errMsg)
		}
		return nil, fmt.Errorf("goaccess: %w", err)
	}

	return stdout.Bytes(), nil
}
