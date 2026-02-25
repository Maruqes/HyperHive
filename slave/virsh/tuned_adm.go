package virsh

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
)

const tunedAdmBinary = "tuned-adm"

type TunedAdmProfile struct {
	Name        string
	Description string
	Active      bool
}

type TunedAdmProfiles struct {
	Profiles             []TunedAdmProfile
	CurrentActiveProfile string
}

func EnsureTunedAdmInstalled() error {
	if _, err := exec.LookPath(tunedAdmBinary); err == nil {
		return nil
	}

	logger.Info("tuned-adm not found, attempting installation")
	if err := installTunedAdm(); err != nil {
		return fmt.Errorf("install tuned/tuned-adm: %w", err)
	}
	if _, err := exec.LookPath(tunedAdmBinary); err != nil {
		return fmt.Errorf("tuned-adm binary still missing after install: %w", err)
	}

	// Best effort: in host environments with systemd, keep tuned service running.
	if err := ensureTunedServiceRunning(); err != nil {
		logger.Warnf("failed to enable/start tuned service (non-fatal): %v", err)
	}

	return nil
}

func GetTunedAdmProfiles() (*TunedAdmProfiles, error) {
	if err := EnsureTunedAdmInstalled(); err != nil {
		return nil, err
	}

	out, err := runCmdOutput(tunedAdmBinary, "list")
	if err != nil {
		return nil, err
	}

	parsed, err := parseTunedAdmListOutput(out)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

func SetTunedAdmProfile(profile string) (*TunedAdmProfiles, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return nil, fmt.Errorf("profile is required")
	}

	if err := EnsureTunedAdmInstalled(); err != nil {
		return nil, err
	}

	if _, err := runCmdOutputMaybeSudo(tunedAdmBinary, "profile", profile); err != nil {
		return nil, err
	}

	return GetTunedAdmProfiles()
}

func installTunedAdm() error {
	if !hasBinary("dnf") {
		return fmt.Errorf("dnf is required to install tuned")
	}
	return runCmdDiscardOutputMaybeSudo("dnf", "-y", "install", "tuned")
}

func ensureTunedServiceRunning() error {
	if !hasBinary("systemctl") {
		return nil
	}
	return runCmdDiscardOutputMaybeSudo("systemctl", "enable", "--now", "tuned")
}

func parseTunedAdmListOutput(out string) (*TunedAdmProfiles, error) {
	result := &TunedAdmProfiles{
		Profiles: make([]TunedAdmProfile, 0, 16),
	}

	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "Current active profile:") {
			result.CurrentActiveProfile = strings.TrimSpace(strings.TrimPrefix(line, "Current active profile:"))
			continue
		}
		if strings.EqualFold(line, "No current active profile.") {
			result.CurrentActiveProfile = ""
			continue
		}
		if !strings.HasPrefix(line, "- ") {
			continue
		}

		rest := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if rest == "" {
			continue
		}

		name := rest
		description := ""
		if parts := strings.SplitN(rest, " - ", 2); len(parts) == 2 {
			name = strings.TrimSpace(parts[0])
			description = strings.TrimSpace(parts[1])
		}
		if name == "" {
			continue
		}

		result.Profiles = append(result.Profiles, TunedAdmProfile{
			Name:        name,
			Description: description,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse tuned-adm list output: %w", err)
	}

	for i := range result.Profiles {
		if result.Profiles[i].Name == result.CurrentActiveProfile {
			result.Profiles[i].Active = true
		}
	}

	if len(result.Profiles) == 0 && strings.TrimSpace(out) == "" {
		return nil, fmt.Errorf("tuned-adm list returned empty output")
	}

	return result, nil
}

func runCmdOutput(name string, args ...string) (string, error) {
	return runCmdOutputWithPrefix(nil, name, args...)
}

func runCmdOutputMaybeSudo(name string, args ...string) (string, error) {
	if os.Geteuid() == 0 {
		return runCmdOutput(name, args...)
	}
	if !hasBinary("sudo") {
		return "", fmt.Errorf("sudo is required to run %s as non-root", name)
	}

	sudoArgs := append([]string{"-n", name}, args...)
	return runCmdOutputWithPrefix(nil, "sudo", sudoArgs...)
}

func runCmdDiscardOutputMaybeSudo(name string, args ...string) error {
	if os.Geteuid() == 0 {
		return runCmdDiscardOutput(name, args...)
	}
	if !hasBinary("sudo") {
		return fmt.Errorf("sudo is required to run %s as non-root", name)
	}

	sudoArgs := append([]string{"-n", name}, args...)
	return runCmdDiscardOutput("sudo", sudoArgs...)
}

func runCmdOutputWithPrefix(stdin io.Reader, name string, args ...string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.Command(name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			return "", fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return "", fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}

	return stdout.String(), nil
}

func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
