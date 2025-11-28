package docker

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/docker/docker/client"
)

func runCommand(desc string, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s: no command provided", desc)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	err := cmd.Run()

	cmdStr := strings.Join(cmd.Args, " ")
	stdoutStr := strings.TrimSpace(stdoutBuf.String())
	stderrStr := strings.TrimSpace(stderrBuf.String())

	if err != nil {
		exitInfo := err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitInfo = fmt.Sprintf("exit code %d", exitErr.ExitCode())
		}

		logger.Error(fmt.Sprintf("%s failed (cmd=%s)", desc, cmdStr))
		if exitInfo != "" {
			logger.Error(desc + " exit info: " + exitInfo)
		}
		if stderrStr != "" {
			logger.Error(desc + " stderr: " + stderrStr)
		}
		if stdoutStr != "" {
			logger.Error(desc + " stdout: " + stdoutStr)
		}

		details := []string{fmt.Sprintf("cmd=%s", cmdStr)}
		if exitInfo != "" {
			details = append(details, "exit="+exitInfo)
		}
		if stderrStr != "" {
			details = append(details, "stderr="+stderrStr)
		}
		if stdoutStr != "" {
			details = append(details, "stdout="+stdoutStr)
		}

		return fmt.Errorf("%s failed (%s): %w", desc, strings.Join(details, "; "), err)
	}

	logger.Info(fmt.Sprintf("%s succeeded (cmd=%s)", desc, cmdStr))
	return nil
}

// InstallLatestDocker installs Docker using DNF only. It will attempt to run
// commands as the current user; if not running as root it will prefix
// commands with `sudo`.
func InstallLatestDocker() error {
	installCmd := "curl -fsSL https://get.docker.com | sh"
	if err := runCommand("install docker", "sh", "-c", installCmd); err != nil {
		return fmt.Errorf("failed to install docker via get.docker.com: %w", err)
	}

	// Enable + start Docker daemon
	if err := runCommand("start docker", "systemctl", "enable", "--now", "docker"); err != nil {
		return fmt.Errorf("failed to start docker: %w", err)
	}
	return nil
}

var cli *client.Client

func NewDockerService() error {
	cliii, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return err
	}
	cli = cliii
	return nil
}
