package ourk8s

import (
	"bytes"
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

	var stderrBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, "bash", "-c", "curl -sfL https://get.k3s.io | sh -")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("INSTALL_K3S_EXEC=%s", strings.Join(serverArgs, " ")),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		stderrOutput := stderrBuf.String()
		return "", fmt.Errorf("install k3s server: run installer script failed: %w\nServer args: %v\nStderr: %s", err, serverArgs, stderrOutput)
	}

	token, err := readK3sToken()
	if err != nil {
		return "", fmt.Errorf("install k3s server: read token after install: %w", err)
	}

	return token, nil
}

func JoinExistingCluster(ctx context.Context, opts JoinClusterOptions) error {
	if opts.ServerURL == "" {
		return fmt.Errorf("join k3s cluster: server URL is required")
	}
	if opts.Token == "" {
		return fmt.Errorf("join k3s cluster: cluster token is required")
	}

	if installed, err := isK3sInstalled(); err != nil {
		return fmt.Errorf("join k3s cluster: check k3s installation: %w", err)
	} else if installed {
		ready, err := isClusterReady(ctx)
		if err != nil {
			return fmt.Errorf("join k3s cluster: check cluster readiness: %w", err)
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

	var stderrBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, "bash", "-c", "curl -sfL https://get.k3s.io | sh -")

	envVars := []string{
		fmt.Sprintf("INSTALL_K3S_EXEC=%s", strings.Join(agentArgs, " ")),
		"K3S_URL=" + opts.ServerURL,
		"K3S_TOKEN=" + opts.Token,
	}
	if opts.Version != "" {
		envVars = append(envVars, fmt.Sprintf("INSTALL_K3S_VERSION=%s", opts.Version))
	}

	cmd.Env = append(os.Environ(), envVars...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		stderrOutput := stderrBuf.String()
		return fmt.Errorf("join k3s cluster: installer script failed: %w\nK3S_VERSION: %s\nK3S_URL: %s\nAgent args: %v\nStderr: %s", err, opts.Version, opts.ServerURL, agentArgs, stderrOutput)
	}

	return nil
}

func PrepareLocalKubeconfig(serverEndpoint, destination string) error {
	if serverEndpoint == "" {
		return fmt.Errorf("prepare kubeconfig: server endpoint is required")
	}
	if destination == "" {
		return fmt.Errorf("prepare kubeconfig: destination path is required")
	}

	data, err := os.ReadFile(k3sKubeconfigPath)
	if err != nil {
		return fmt.Errorf("prepare kubeconfig: read k3s kubeconfig at %s: %w", k3sKubeconfigPath, err)
	}

	adjusted := strings.Replace(string(data), "https://127.0.0.1:6443", serverEndpoint, 1)
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("prepare kubeconfig: create directory %s: %w", filepath.Dir(destination), err)
	}
	if err := os.WriteFile(destination, []byte(adjusted), 0o600); err != nil {
		return fmt.Errorf("prepare kubeconfig: write kubeconfig to %s: %w", destination, err)
	}
	return nil
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

	var stderrBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, k3sBinaryPath, "kubectl", "cluster-info")
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderrOutput := stderrBuf.String()
			if stderrOutput != "" {
				return false, fmt.Errorf("cluster not ready: kubectl error: %s", stderrOutput)
			}
			return false, nil
		}
		return false, fmt.Errorf("check cluster: run kubectl cluster-info: %w", err)
	}
	return true, nil
}

// clusterReadyWithKubeconfig checks readiness using available k3s kubeconfigs without
// changing the legacy isClusterReady behavior. It tries known kubeconfig locations
// with sudo to handle root-only permissions.
func clusterReadyWithKubeconfig(ctx context.Context) (bool, error) {
	if _, err := os.Stat(k3sBinaryPath); err != nil {
		if os.IsNotExist(err) {
			return false, fmt.Errorf("k3s binary not found")
		}
		return false, fmt.Errorf("stat %s: %w", k3sBinaryPath, err)
	}

	kubeconfigs := []string{k3sKubeconfigPath, "/etc/rancher/k3s/kubelet.conf"}
	var lastErr error

	runCheck := func(cfg string) (bool, error) {
		var stderrBuf bytes.Buffer
		cmd := exec.CommandContext(ctx, "sudo", k3sBinaryPath, "kubectl", "cluster-info", "--kubeconfig", cfg)
		cmd.Env = append(os.Environ(), "KUBECONFIG="+cfg)
		cmd.Stderr = &stderrBuf
		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				stderrOutput := stderrBuf.String()
				if stderrOutput != "" {
					return false, fmt.Errorf("cluster not ready: kubectl error: %s", stderrOutput)
				}
				return false, nil
			}
			return false, fmt.Errorf("check cluster: run kubectl cluster-info: %w", err)
		}
		return true, nil
	}

	for _, cfg := range kubeconfigs {
		if _, err := os.Stat(cfg); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			lastErr = fmt.Errorf("stat %s: %w", cfg, err)
			continue
		}

		ok, err := runCheck(cfg)
		if err != nil {
			lastErr = err
			continue
		}
		return ok, nil
	}

	if lastErr != nil {
		return false, lastErr
	}
	return false, fmt.Errorf("cluster not ready: no kubeconfig found on node")
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

func runFirewallCmd(ctx context.Context, args ...string) (string, error) {
	var stderrBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, "firewall-cmd", args...)
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(stderrBuf.String()), err
	}
	return strings.TrimSpace(stderrBuf.String()), nil
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

		// Add permanent rule
		var stderrBuf bytes.Buffer
		cmd := exec.CommandContext(ctx, "firewall-cmd", "--permanent", "--add-port", portArg)
		cmd.Stderr = &stderrBuf
		_ = cmd.Run() // Ignore errors, may already exist

		// Add runtime rule
		stderrBuf.Reset()
		cmd = exec.CommandContext(ctx, "firewall-cmd", "--add-port", portArg)
		cmd.Stderr = &stderrBuf
		_ = cmd.Run() // Ignore errors, may already exist
	}

	if stderrOutput, err := runFirewallCmd(ctx, "--reload"); err != nil {
		return fmt.Errorf("ensure firewall ports: reload firewall: %w (stderr: %s)", err, stderrOutput)
	}
	return nil
}

func AllowFirewalldAcceptAll(ctx context.Context) error {
	if _, err := exec.LookPath("firewall-cmd"); err != nil {
		// firewalld not installed
		return nil
	}

	run := func(args ...string) error {
		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, "firewall-cmd", args...)
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("firewall-cmd %v: %w (stderr: %s)", args, err, strings.TrimSpace(stderr.String()))
		}
		return nil
	}

	output := func(args ...string) (string, error) {
		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, "firewall-cmd", args...)
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("firewall-cmd %v: %w (stderr: %s)", args, err, strings.TrimSpace(stderr.String()))
		}
		return strings.TrimSpace(string(out)), nil
	}

	// 1) Descobrir zonas existentes
	zonesOut, err := output("--get-zones")
	zones := []string{"public", "dmz", "external", "internal", "work", "home", "trusted"}
	if err == nil && zonesOut != "" {
		zones = strings.Fields(zonesOut)
	}

	// 2) Tornar TUDO ACCEPT (permanente)
	//    Isto é o coração do "aceito tudoooo".
	for _, zone := range zones {
		_ = run("--permanent", "--zone", zone, "--set-target=ACCEPT")
	}

	// 3) Meter a default zone em trusted (permanente)
	_ = run("--permanent", "--set-default-zone=trusted")

	// 4) Aplicar config permanente
	if err := run("--reload"); err != nil {
		return err
	}

	// 5) Garantir também em runtime (por segurança)
	//    Após reload normalmente já fica certo, mas isto evita casos estranhos.
	_ = run("--set-default-zone=trusted")
	for _, zone := range zones {
		_ = run("--zone", zone, "--set-target=ACCEPT")
	}

	return nil
}
