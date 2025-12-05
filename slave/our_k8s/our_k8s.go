package ourk8s

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slave/env512"
	"strings"
)

const (
	k3sBinaryPath     = "/usr/local/bin/k3s"
	k3sKubeconfigPath = "/etc/rancher/k3s/k3s.yaml"
	k3sTokenPath      = "/var/lib/rancher/k3s/server/node-token"
)

var (
	ErrNodeAlreadyInCluster = errors.New("node already connected to a k3s cluster")
)

type ServerInstallOptions struct {
	NodeIP         string
	TLSSANs        []string
	DisableTraefik bool
	Version        string
	ExtraArgs      []string
}

type JoinClusterOptions struct {
	ServerURL string
	Token     string
	NodeIP    string
	Labels    map[string]string
	Taints    []string
	Version   string
	ExtraArgs []string
}

var TOKEN string

// this functions tells us if we are the "slave" running on master server
// remember that master server needs also to run slave client for this to work properly
func AreWeMasterSlave() bool {
	return env512.SlaveIP == env512.MasterIP
}

func InstallK3sServer(ctx context.Context, opts ServerInstallOptions) (string, error) {
	if opts.NodeIP == "" {
		return "", fmt.Errorf("install k3s server: node IP is required")
	}

	if installed, err := isK3sInstalled(); err != nil {
		return "", fmt.Errorf("install k3s server: check k3s binary: %w", err)
	} else if installed {
		ready, err := isClusterReady(ctx)
		if err != nil {
			return "", fmt.Errorf("install k3s server: check cluster readiness: %w", err)
		}
		if ready {
			token, err := readK3sToken()
			if err != nil {
				return "", fmt.Errorf("install k3s server: read existing token: %w", err)
			}
			return token, nil
		}
	}

	if err := ensureFirewallPorts(ctx, []portSpec{
		{Port: "6443", Protocol: "tcp"},
		{Port: "8472", Protocol: "udp"},
	}); err != nil {
		return "", fmt.Errorf("install k3s server: configure firewall: %w", err)
	}

	version := opts.Version
	if version == "" {
		version = "stable"
	}

	serverArgs := []string{
		"server",
		"--node-ip", opts.NodeIP,
		"--advertise-address", opts.NodeIP,
		"--bind-address", "0.0.0.0",
		"--write-kubeconfig-mode", "644",
	}
	if opts.DisableTraefik {
		serverArgs = append(serverArgs, "--disable", "traefik")
	}
	for _, san := range opts.TLSSANs {
		if strings.TrimSpace(san) == "" {
			continue
		}
		serverArgs = append(serverArgs, "--tls-san", san)
	}
	serverArgs = append(serverArgs, opts.ExtraArgs...)

	cmd := exec.CommandContext(ctx, "bash", "-c", "curl -sfL https://get.k3s.io | sh -")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("INSTALL_K3S_EXEC=%s", strings.Join(serverArgs, " ")),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("install k3s server: run installer script: %w", err)
	}

	token, err := readK3sToken()
	if err != nil {
		return "", fmt.Errorf("install k3s server: read token after install: %w", err)
	}
	return token, nil
}

func JoinExistingCluster(ctx context.Context, opts JoinClusterOptions) error {
	if opts.ServerURL == "" {
		return errors.New("server URL is required")
	}
	if opts.Token == "" {
		return errors.New("cluster token is required")
	}

	if installed, err := isK3sInstalled(); err != nil {
		return err
	} else if installed {
		ready, err := isClusterReady(ctx)
		if err != nil {
			return err
		}
		if ready {
			return ErrNodeAlreadyInCluster
		}
	}

	if err := ensureFirewallPorts(ctx, []portSpec{
		{Port: "8472", Protocol: "udp"}, // flannel/vxlan
	}); err != nil {
		return fmt.Errorf("configure firewall: %w", err)
	}

	version := opts.Version
	if version == "" {
		version = "stable"
	}

	agentArgs := []string{"agent"}
	if opts.NodeIP != "" {
		agentArgs = append(agentArgs, "--node-ip", opts.NodeIP)
	}
	for key, value := range opts.Labels {
		if key == "" {
			continue
		}
		agentArgs = append(agentArgs, "--node-label", fmt.Sprintf("%s=%s", key, value))
	}
	for _, taint := range opts.Taints {
		if taint == "" {
			continue
		}
		agentArgs = append(agentArgs, "--node-taint", taint)
	}
	agentArgs = append(agentArgs, opts.ExtraArgs...)

	cmd := exec.CommandContext(ctx, "bash", "-c", "curl -sfL https://get.k3s.io | sh -")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("INSTALL_K3S_VERSION=%s", version),
		fmt.Sprintf("INSTALL_K3S_EXEC=%s", strings.Join(agentArgs, " ")),
		"K3S_URL="+opts.ServerURL,
		"K3S_TOKEN="+opts.Token,
	)

	return cmd.Run()
}

func PrepareLocalKubeconfig(serverEndpoint, destination string) error {
	if serverEndpoint == "" {
		return errors.New("server endpoint is required to prepare kubeconfig")
	}
	if destination == "" {
		return errors.New("destination path is required to prepare kubeconfig")
	}

	data, err := os.ReadFile(k3sKubeconfigPath)
	if err != nil {
		return fmt.Errorf("read k3s kubeconfig: %w", err)
	}

	adjusted := strings.Replace(string(data), "https://127.0.0.1:6443", serverEndpoint, 1)
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create kubeconfig directory: %w", err)
	}
	return os.WriteFile(destination, []byte(adjusted), 0o600)
}

func isK3sInstalled() (bool, error) {
	if _, err := os.Stat(k3sBinaryPath); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, fmt.Errorf("stat %s: %w", k3sBinaryPath, err)
	}
}

func isClusterReady(ctx context.Context) (bool, error) {
	if _, err := os.Stat(k3sBinaryPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", k3sBinaryPath, err)
	}

	cmd := exec.CommandContext(ctx, k3sBinaryPath, "kubectl", "cluster-info")
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, fmt.Errorf("check cluster: %w", err)
	}
	return true, nil
}

func readK3sToken() (string, error) {
	data, err := os.ReadFile(k3sTokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("k3s token not found (is the server running?): %w", err)
		}
		return "", fmt.Errorf("read k3s token: %w", err)
	}

	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("k3s token file is empty")
	}
	return token, nil
}

type portSpec struct {
	Port     string
	Protocol string
}

func ensureFirewallPorts(ctx context.Context, ports []portSpec) error {
	if len(ports) == 0 {
		return nil
	}

	if _, err := exec.LookPath("firewall-cmd"); err != nil {
		// firewalld not installed, nothing to do.
		return nil
	}

	for _, p := range ports {
		if p.Port == "" || p.Protocol == "" {
			continue
		}
		portArg := fmt.Sprintf("%s/%s", p.Port, strings.ToLower(p.Protocol))
		if err := exec.CommandContext(ctx, "firewall-cmd", "--permanent", "--add-port", portArg).Run(); err != nil {
			return fmt.Errorf("add permanent port %s: %w", portArg, err)
		}
		if err := exec.CommandContext(ctx, "firewall-cmd", "--add-port", portArg).Run(); err != nil {
			return fmt.Errorf("add runtime port %s: %w", portArg, err)
		}
	}

	return exec.CommandContext(ctx, "firewall-cmd", "--reload").Run()
}
