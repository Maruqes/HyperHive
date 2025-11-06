package api

import (
	"512SvMan/env512"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	goAccessTimeout      = 2 * time.Minute
	goAccessWSAddr       = "127.0.0.1:7891" // porta interna do WS do GoAccess
	goAccessPublicWSPath = "/goaccess/ws"   // caminho público do WS
)

var debugGoAccess = os.Getenv("DEBUG_GOACCESS") == "1"

var goAccessAllPanels = []string{
	"VISITORS", "REQUESTS", "REQUESTS_STATIC", "NOT_FOUND", "HOSTS", "OS",
	"BROWSERS", "VISIT_TIMES", "VIRTUAL_HOSTS", "REFERRERS", "REFERRING_SITES",
	"KEYPHRASES", "STATUS_CODES", "REMOTE_USER", "CACHE_STATUS", "GEO_LOCATION",
	"MIME_TYPE", "TLS_TYPE",
}

// ---------------- utils ----------------

func isPortListening(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func fileContains(p, needle string) bool {
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), needle)
}

func requestPublicBaseURL(r *http.Request) string {
	if r == nil {
		return ""
	}

	var proto string
	var host string

	if fwd := r.Header.Get("Forwarded"); fwd != "" {
		first := strings.Split(fwd, ",")[0]
		for _, token := range strings.Split(first, ";") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			lower := strings.ToLower(token)
			switch {
			case strings.HasPrefix(lower, "proto=") && proto == "":
				proto = strings.Trim(strings.TrimPrefix(token, "proto="), `"`)
			case strings.HasPrefix(lower, "host=") && host == "":
				host = strings.Trim(strings.TrimPrefix(token, "host="), `"`)
			}
		}
	}

	if host == "" {
		if h := r.Header.Get("X-Forwarded-Host"); h != "" {
			host = strings.TrimSpace(strings.Split(h, ",")[0])
		}
	}
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}

	if proto == "" {
		proto = strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	}
	if proto == "" {
		proto = strings.TrimSpace(r.Header.Get("X-Forwarded-Protocol"))
	}
	if proto == "" {
		proto = strings.TrimSpace(r.Header.Get("X-Forwarded-Scheme"))
	}
	if proto == "" && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Ssl")), "on") {
		proto = "https"
	}
	if proto == "" && strings.EqualFold(strings.TrimSpace(r.Header.Get("Front-End-Https")), "on") {
		proto = "https"
	}
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	proto = strings.ToLower(proto)
	if proto != "https" {
		proto = "http"
	}

	if host == "" {
		return ""
	}

	if !strings.Contains(host, ":") {
		if port := strings.TrimSpace(r.Header.Get("X-Forwarded-Port")); port != "" {
			if !(proto == "http" && port == "80") && !(proto == "https" && port == "443") {
				host = net.JoinHostPort(host, port)
			}
		}
	}

	basePath := ""
	if mainLink := strings.TrimSpace(env512.MAIN_LINK); mainLink != "" {
		if u, err := url.Parse(mainLink); err == nil && u.Path != "" && u.Path != "/" {
			basePath = strings.TrimRight(u.Path, "/")
		}
	}

	return fmt.Sprintf("%s://%s%s", proto, host, basePath)
}

// ws:// ou wss:// a partir do MAIN_LINK
func buildPublicWSURL(base, wsPath string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", errors.New("MAIN_LINK is empty")
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}

	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid MAIN_LINK: %w", err)
	}
	if u.Host == "" && u.Path != "" {
		reparse := "http://" + u.Path
		u, err = url.Parse(reparse)
		if err != nil {
			return "", fmt.Errorf("invalid MAIN_LINK (host): %w", err)
		}
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	basePath := strings.TrimRight(u.Path, "/")
	if !strings.HasPrefix(wsPath, "/") {
		wsPath = "/" + wsPath
	}
	u.Path, u.RawQuery, u.Fragment = basePath+wsPath, "", ""
	return u.String(), nil
}

// origem p/ --origin (ex.: https://hyperhive.maruqes.com)
func originFromBase(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", errors.New("MAIN_LINK is empty")
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid MAIN_LINK for origin")
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	if u.Scheme != "http" && u.Scheme != "https" {
		u.Scheme = "http"
	}
	return u.Scheme + "://" + u.Host, nil
}

// mata o daemon antigo via pid-file ou comando
func stopGoAccessIfRunning(pidFile, logDir string) {
	killed := false

	if b, err := os.ReadFile(pidFile); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
			if err := syscall.Kill(pid, syscall.SIGTERM); err == nil {
				killed = true
			}
		}
	}

	if !killed && logDir != "" {
		if terminateGoAccessByLogDir(logDir) {
			killed = true
		}
	}

	if killed {
		for i := 0; i < 20; i++ {
			if !isPortListening(goAccessWSAddr) {
				break
			}
			time.Sleep(150 * time.Millisecond)
		}
		_ = os.Remove(pidFile)
	}
}

func terminateGoAccessByLogDir(logDir string) bool {
	dir := filepath.Clean(logDir)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	terminated := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		cmdlinePath := filepath.Join("/proc", entry.Name(), "cmdline")
		data, err := os.ReadFile(cmdlinePath)
		if err != nil || len(data) == 0 {
			continue
		}
		parts := strings.Split(string(data), "\x00")
		if len(parts) == 0 || parts[0] != "goaccess" {
			continue
		}
		found := false
		for _, part := range parts[1:] {
			if part == "" {
				continue
			}
			if strings.HasPrefix(part, dir) {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err == nil {
			terminated = true
		}
	}
	return terminated
}

// ---------------- GoAccess realtime ----------------

func ensureGoAccessDaemon(requestBase string) error {
	// 1) binário
	if _, err := exec.LookPath("goaccess"); err != nil {
		if _, derr := exec.LookPath("dnf"); derr != nil {
			return fmt.Errorf("goaccess not found and dnf not found either: %w", err)
		}
		if err := exec.Command("dnf", "-y", "install", "goaccess").Run(); err != nil {
			return fmt.Errorf("installing goaccess: %w", err)
		}
	}

	// 2) paths
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cwd: %w", err)
	}

	logDir := filepath.Join(workDir, "npm-data", "logs")
	statsDir := filepath.Join(workDir, "npm-data", "stats")
	if err := os.MkdirAll(statsDir, 0o755); err != nil {
		return fmt.Errorf("stats dir: %w", err)
	}
	outputPath := filepath.Join(statsDir, "goaccess.html")
	pidFile := filepath.Join(statsDir, "goaccess.pid")

	// 3) logs NPM
	pattern := filepath.Join(logDir, "proxy-host-*_access.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("listing logs: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no logs found in %s", pattern)
	}

	// 4) flags
	logFormat := `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	base := strings.TrimSpace(requestBase)
	if base == "" {
		base = strings.TrimSpace(env512.MAIN_LINK)
	}
	if base == "" {
		base = "http://localhost"
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}

	publicWSURL, err := buildPublicWSURL(base, goAccessPublicWSPath)
	if err != nil {
		return err
	}
	origin, err := originFromBase(base)
	if err != nil {
		return err
	}

	if debugGoAccess {
		fmt.Println("[GoAccess] ws-url =", publicWSURL)
		fmt.Println("[GoAccess] origin =", origin)
	}

	// 5) se já está a correr mas o HTML não tem o ws-url atual → reinicia
	if isPortListening(goAccessWSAddr) && fileExists(outputPath) && !fileContains(outputPath, publicWSURL) {
		stopGoAccessIfRunning(pidFile, logDir)
	}

	// 6) se continua a correr e HTML existe → ok
	if isPortListening(goAccessWSAddr) && fileExists(outputPath) {
		return nil
	}

	args := append([]string{}, files...)
	args = append(args,
		"--no-global-config",
		"--date-format=%d/%b/%Y",
		"--time-format=%T",
		"--log-format="+logFormat,
		"--real-time-html",
		"--persist",
		"--restore",
		"--pid-file="+pidFile,
		"-o", outputPath,
		"--addr=127.0.0.1",
		"--port="+strings.Split(goAccessWSAddr, ":")[1],
		"--ws-url="+publicWSURL,
		"--origin="+origin,
	)

	enablePanels := env512.GoAccessEnablePanels
	disablePanels := env512.GoAccessDisablePanels
	if len(enablePanels) == 0 && len(disablePanels) == 0 {
		enablePanels = goAccessAllPanels
	}
	for _, p := range enablePanels {
		args = append(args, "--enable-panel="+p)
	}
	for _, p := range disablePanels {
		args = append(args, "--ignore-panel="+p)
	}

	if debugGoAccess {
		args = append(args, "--debug-file=/tmp/goaccess-debug.log")
		fmt.Println("[GoAccess] args:", strings.Join(args, " "))
	} else {
		args = append(args, "--daemonize")
	}

	cmd := exec.Command("goaccess", args...)
	cmd.Dir = logDir
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if strings.Contains(msg, "Unknown option") && strings.Contains(msg, "ws-url") {
			return fmt.Errorf("your goaccess doesn't support --ws-url (upgrade to >=1.7). Error: %s", msg)
		}
		return fmt.Errorf("starting GoAccess realtime: %s", msg)
	}

	// 7) espera pelo WS/HTML
	for i := 0; i < 20; i++ {
		if isPortListening(goAccessWSAddr) && fileExists(outputPath) {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("GoAccess started but WS/HTML are not ready")
}

// ---------------- WS túnel e página ----------------

// Túnel WS manual: evita problemas do ReverseProxy com Upgrade.
func wsTunnelToGoAccess(w http.ResponseWriter, r *http.Request) {
	// só aceitamos Upgrade: websocket
	if !strings.EqualFold(r.Header.Get("Connection"), "Upgrade") ||
		!strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "upgrade required", http.StatusBadRequest)
		return
	}

	// liga ao backend WS do GoAccess
	back, err := net.DialTimeout("tcp", goAccessWSAddr, 5*time.Second)
	if err != nil {
		http.Error(w, "backend ws unavailable", http.StatusBadGateway)
		return
	}

	// cria um pedido novo para o backend, preserva a query (?p=...)
	req := r.Clone(context.Background())
	req.URL = &url.URL{
		Scheme:   "http",
		Host:     goAccessWSAddr,
		Path:     "/",
		RawQuery: r.URL.RawQuery,
	}
	req.Host = goAccessWSAddr
	req.RequestURI = "" // obrigatório para clientes http
	// reforça cabeçalhos de upgrade
	req.Header = r.Header.Clone()
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")

	// envia o handshake para o backend
	if err := req.Write(back); err != nil {
		_ = back.Close()
		http.Error(w, "failed to write backend handshake", http.StatusBadGateway)
		return
	}

	// hijack do cliente para tunnel raw
	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = back.Close()
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hj.Hijack()
	if err != nil {
		_ = back.Close()
		http.Error(w, "hijack failed", http.StatusInternalServerError)
		return
	}

	// pump nos dois sentidos
	errCh := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(back, clientConn)
		_ = back.Close()
		errCh <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(clientConn, back)
		_ = clientConn.Close()
		errCh <- struct{}{}
	}()
	<-errCh // fecha quando uma das direções termina
}

func goAccessPageHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), goAccessTimeout)
	defer cancel()

	publicBase := requestPublicBaseURL(r)

	done := make(chan error, 1)
	go func() { done <- ensureGoAccessDaemon(publicBase) }()

	select {
	case <-ctx.Done():
		http.Error(w, "timeout preparing GoAccess", http.StatusGatewayTimeout)
		return
	case err := <-done:
		if err != nil {
			http.Error(w, "GoAccess unavailable: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// desativar cache do HTML no cliente e proxies
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	workDir, _ := os.Getwd()
	html := filepath.Join(workDir, "npm-data", "stats", "goaccess.html")
	http.ServeFile(w, r, html)
}

func setupGoAccessAPI(r chi.Router) {
	r.Get("/goaccess", goAccessPageHandler)
	// WebSocket público → túnel raw para 127.0.0.1:7891/
	r.HandleFunc(goAccessPublicWSPath, wsTunnelToGoAccess)
}
