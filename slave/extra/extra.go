package extra

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
)

type SoftwareUpdate struct {
	Name    string
	Version string
}

func CheckForUpdates() ([]SoftwareUpdate, error) {
	if _, err := exec.LookPath("dnf"); err != nil {
		return nil, fmt.Errorf("dnf not found in PATH: %w", err)
	}

	cmd := exec.Command("dnf", "--assumeyes", "--color=never", "--quiet", "check-update")
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 100:
		// Exit status 100 indicates updates are available. Treat as success.
	default:
		logger.Error("dnf check-update failed", "error", err, "output", string(output))
		return nil, fmt.Errorf("dnf check-update: %w", err)
	}

	updates := parseCheckUpdateOutput(output)
	logger.Info("dnf check-update completed", "updateCount", len(updates))
	return updates, nil
}

func PerformUpdate(name string, restartAfter bool) error {
	if _, err := exec.LookPath("dnf"); err != nil {
		return fmt.Errorf("dnf not found in PATH: %w", err)
	}

	args := []string{"dnf", "-y", "upgrade"}
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		args = append(args, trimmed)
	}

	logger.Info("starting system update", "name", name)
	updateCmd := exec.Command(args[0], args[1:]...)
	updateCmd.Stdin = os.Stdin
	updateCmd.Stdout = os.Stdout
	updateCmd.Stderr = os.Stderr

	if err := updateCmd.Run(); err != nil {
		logger.Error("dnf upgrade failed", "error", err)
		return fmt.Errorf("dnf upgrade: %w", err)
	}
	logger.Info("system update completed", "name", name)

	if restartAfter {
		logger.Info("requesting forced system reboot after update")
		rebootCmd := exec.Command("systemctl", "reboot", "-i", "--force")
		rebootCmd.Stdin = os.Stdin
		rebootCmd.Stdout = os.Stdout
		rebootCmd.Stderr = os.Stderr
		if err := rebootCmd.Start(); err != nil {
			logger.Error("failed to trigger forced reboot", "error", err)
			return fmt.Errorf("system reboot: %w", err)
		}
	}
	return nil
}

func parseCheckUpdateOutput(b []byte) []SoftwareUpdate {
	scanner := bufio.NewScanner(bytes.NewReader(b))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var updates []SoftwareUpdate
	seen := make(map[string]struct{})

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" ||
			strings.HasPrefix(line, "Last metadata expiration check") ||
			strings.HasPrefix(line, "Security") ||
			strings.HasPrefix(line, "Install") ||
			strings.HasPrefix(line, "Upgrade") ||
			strings.HasSuffix(line, "Packages") ||
			strings.HasPrefix(line, "Available") ||
			strings.HasPrefix(line, "Obsoleting") ||
			strings.HasPrefix(line, "Module") ||
			strings.HasPrefix(line, "Package") ||
			strings.HasPrefix(line, "=") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		name, version := extractNameAndVersion(fields)
		if name == "" || version == "" || !containsDigit(version) {
			continue
		}

		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}

		updates = append(updates, SoftwareUpdate{Name: name, Version: version})
	}

	if err := scanner.Err(); err != nil {
		logger.Warn("scanner error while parsing dnf output", "error", err)
	}
	return updates
}

func extractNameAndVersion(fields []string) (string, string) {
	if len(fields) < 2 {
		return "", ""
	}

	name := fields[0]

	if len(fields) == 2 {
		return name, fields[1]
	}

	arch := fields[1]
	if isArchField(arch) && len(fields) >= 3 {
		if len(fields) == 3 {
			// Version without repository column.
			return name, fields[2]
		}
		return name, strings.Join(fields[2:len(fields)-1], " ")
	}

	// Fallback for modular output without architecture column.
	if len(fields) == 3 {
		return name, fields[1]
	}
	return name, strings.Join(fields[1:len(fields)-1], " ")
}

func isArchField(value string) bool {
	switch value {
	case "noarch", "src", "x86_64", "aarch64", "i686", "ppc64le", "s390x", "armhfp":
		return true
	default:
		return false
	}
}

func containsDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}

// shutdownPc shuts down the PC. If now is true, it shuts down immediately.
func shutdownPc(now bool) error {
	args := []string{"shutdown"}
	if now {
		args = append(args, "now")
	}
	cmd := exec.Command(args[0], args[1:]...)
	_, err := cmd.Output()
	if err != nil {
		logger.Error(err.Error())
		return err
	}
	return nil
}

// restartPc restarts the PC. If now is true, it restarts immediately.
func restartPc(now bool) error {
	args := []string{"reboot"}
	if now {
		args = append(args, "now")
	}
	cmd := exec.Command(args[0], args[1:]...)
	_, err := cmd.Output()
	if err != nil {
		logger.Error(err.Error())
		return err
	}
	return nil
}
