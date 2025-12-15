package ourk8s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slave/env512"
	"strings"
	"time"
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

func clusterReadyWithKubeconfig(ctx context.Context) (bool, string, error) {
	kubeconfig, err := findKubeconfigPath()
	if err != nil {
		return false, "", err
	}

	serverURL, err := kubeconfigServerURL(ctx, kubeconfig)
	if err != nil {
		return false, "", err
	}
	serverURL, err = normalizeServerURL(serverURL)
	if err != nil {
		return false, "", err
	}

	// Se o kubeconfig apontar para localhost (típico no master), troca para IP real do master
	serverURL = fixLocalhostServerURL(serverURL)

	// Teste rápido de conectividade ao API server via kubeconfig
	testCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	stdout, stderr, err := runKubectl(testCtx,
		"--kubeconfig", kubeconfig,
		"--request-timeout=5s",
		"get", "--raw=/readyz",
	)

	if err != nil {
		// Se deu erro e há stderr útil, devolve erro (vai aparecer no status.Error)
		if strings.TrimSpace(stderr) != "" {
			return false, serverURL, fmt.Errorf("cluster not ready: %s", strings.TrimSpace(stderr))
		}
		// Caso “falhou mas sem info”, trata como não ligado
		return false, serverURL, nil
	}

	// Normalmente devolve "ok"
	if strings.Contains(strings.ToLower(stdout), "ok") || strings.TrimSpace(stdout) != "" {
		return true, serverURL, nil
	}
	// Se saiu 0 mas veio vazio, assume ligado na mesma
	return true, serverURL, nil
}

func findKubeconfigPath() (string, error) {
	// 1) KUBECONFIG pode ter vários separados por ':'
	if kc := strings.TrimSpace(os.Getenv("KUBECONFIG")); kc != "" {
		parts := strings.Split(kc, ":")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}

	// 2) kubeconfig do k3s (server)
	if _, err := os.Stat(k3sKubeconfigPath); err == nil {
		return k3sKubeconfigPath, nil
	}

	// 3) ~/.kube/config
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		p := filepath.Join(home, ".kube", "config")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("kubeconfig not found (KUBECONFIG, %s, or ~/.kube/config)", k3sKubeconfigPath)
}

func kubeconfigServerURL(ctx context.Context, kubeconfigPath string) (string, error) {
	// Usar o próprio kubectl para interpretar o kubeconfig e respeitar current-context.
	// Isto evita parse YAML em Go.
	out, stderr, err := runKubectl(ctx,
		"config", "view",
		"--raw",
		"--kubeconfig", kubeconfigPath,
		"-o", "json",
	)
	if err != nil {
		if strings.TrimSpace(stderr) != "" {
			return "", fmt.Errorf("kubectl config view failed: %s", strings.TrimSpace(stderr))
		}
		return "", fmt.Errorf("kubectl config view failed: %w", err)
	}

	type kc struct {
		CurrentContext string `json:"current-context"`
		Contexts       []struct {
			Name    string `json:"name"`
			Context struct {
				Cluster string `json:"cluster"`
			} `json:"context"`
		} `json:"contexts"`
		Clusters []struct {
			Name    string `json:"name"`
			Cluster struct {
				Server string `json:"server"`
			} `json:"cluster"`
		} `json:"clusters"`
	}

	var cfg kc
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		return "", fmt.Errorf("parse kubeconfig json: %w", err)
	}

	clusterName := ""
	if cfg.CurrentContext != "" {
		for _, c := range cfg.Contexts {
			if c.Name == cfg.CurrentContext {
				clusterName = c.Context.Cluster
				break
			}
		}
	}

	// fallback: se não houver current-context, tenta primeiro cluster
	if clusterName == "" && len(cfg.Clusters) > 0 {
		if s := strings.TrimSpace(cfg.Clusters[0].Cluster.Server); s != "" {
			return s, nil
		}
	}

	for _, cl := range cfg.Clusters {
		if cl.Name == clusterName {
			s := strings.TrimSpace(cl.Cluster.Server)
			if s == "" {
				return "", fmt.Errorf("kubeconfig cluster server is empty for cluster %q", clusterName)
			}
			return s, nil
		}
	}

	return "", fmt.Errorf("could not resolve cluster server from kubeconfig (current-context=%q cluster=%q)", cfg.CurrentContext, clusterName)
}

func normalizeServerURL(server string) (string, error) {
	server = strings.TrimSpace(server)
	if server == "" {
		return "", fmt.Errorf("empty server url")
	}

	u, err := url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("invalid server url %q: %w", server, err)
	}

	// Se vier sem scheme, assume https
	if u.Scheme == "" {
		u.Scheme = "https"
	}

	host := u.Host
	// Às vezes pode vir em u.Path (se faltava scheme)
	if host == "" && u.Path != "" && strings.Contains(u.Path, ":") {
		host = u.Path
		u.Path = ""
	}

	if host == "" {
		return "", fmt.Errorf("server url missing host: %q", server)
	}

	// Garantir port 6443 se faltar
	if !strings.Contains(host, ":") {
		host = host + ":6443"
	}
	u.Host = host

	return u.String(), nil
}

func fixLocalhostServerURL(serverURL string) string {
	u, err := url.Parse(serverURL)
	if err != nil {
		return serverURL
	}
	host := u.Hostname()
	if host == "127.0.0.1" || host == "localhost" {
		m := strings.TrimSpace(env512.MasterIP)
		if m != "" {
			// mantém a porta original
			port := u.Port()
			if port == "" {
				port = "6443"
			}
			u.Host = fmt.Sprintf("%s:%s", m, port)
			return u.String()
		}
	}
	return serverURL
}

func runKubectl(ctx context.Context, args ...string) (stdout string, stderr string, err error) {
	var cmd *exec.Cmd

	// Preferir k3s kubectl se existir (é o caso típico nos teus nós)
	if _, statErr := os.Stat(k3sBinaryPath); statErr == nil {
		all := append([]string{"kubectl"}, args...)
		cmd = exec.CommandContext(ctx, k3sBinaryPath, all...)
	} else {
		kubectlPath, lookErr := exec.LookPath("kubectl")
		if lookErr != nil {
			return "", "", fmt.Errorf("kubectl not found and %s missing", k3sBinaryPath)
		}
		cmd = exec.CommandContext(ctx, kubectlPath, args...)
	}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	return outBuf.String(), errBuf.String(), runErr
}
