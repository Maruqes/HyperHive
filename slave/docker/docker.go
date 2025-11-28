package docker

import (
	"fmt"
	"os"
	"os/exec"
)

// InstallLatestDocker installs Docker using DNF only. It will attempt to run
// commands as the current user; if not running as root it will prefix
// commands with `sudo`.
func InstallLatestDocker() error {
	// Ensure dnf exists
	if _, err := exec.LookPath("dnf"); err != nil {
		return fmt.Errorf("dnf not found: %w", err)
	}

	run := func(cmd string, args ...string) error {
		var c *exec.Cmd
		if os.Geteuid() != 0 {
			// Prepend sudo
			c = exec.Command("sudo", append([]string{cmd}, args...)...)
		} else {
			c = exec.Command(cmd, args...)
		}
		out, err := c.CombinedOutput()
		if err != nil {
			return fmt.Errorf("command %s %v failed: %v: %s", cmd, args, err, string(out))
		}
		return nil
	}

	// Install dnf-plugins-core (provides config-manager)
	if err := run("dnf", "-y", "install", "dnf-plugins-core"); err != nil {
		return err
	}

	// Add Docker CE repo (Fedora/CentOS style from Docker official)
	if err := run("dnf", "config-manager", "--add-repo", "https://download.docker.com/linux/fedora/docker-ce.repo"); err != nil {
		return err
	}

	// Install Docker packages
	if err := run("dnf", "-y", "install", "docker-ce", "docker-ce-cli", "containerd.io", "docker-compose-plugin"); err != nil {
		return err
	}

	// Enable and start Docker
	if err := run("systemctl", "enable", "--now", "docker"); err != nil {
		return err
	}

	return nil
}
