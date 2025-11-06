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
	goAccessWSAddr       = "127.0.0.1:7891" // WS interno do GoAccess
	goAccessPublicWSPath = "/goaccess/ws"   // WS público (browser liga aqui)
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

// mata o daemon antigo via pid-file
func stopGoAccessIfRunning(pidFile string) {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	for i := 0; i < 20; i++ {
		if !isPortListening(goAccessWSAddr) {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// ---------------- deteção de formato ----------------

type fmtCand struct {
	Name      string
	DateFmt   string
	TimeFmt   string
	LogFormat string
}

// formatos comuns de NPM/NGINX
var candidates = []fmtCand{
	{
		Name:    "npm-verbose",
		DateFmt: "%d/%b/%Y", TimeFmt: "%T",
		// alguns templates do NPM com [Client ...] etc.
		LogFormat: `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] %^ "%u" "%R"`,
	},
	{
		Name:    "nginx-combined",
		DateFmt: "%d/%b/%Y", TimeFmt: "%T",
		// 127.0.0.1 - - [06/Nov/2025:19:27:39 +0000] "GET /path HTTP/1.1" 200 123 "-" "UA"
		LogFormat: `%h %^[%d:%t %^] "%r" %s %b "%R" "%u"`,
	},
	{
		Name:    "nginx-vhost",
		DateFmt: "%d/%b/%Y", TimeFmt: "%T",
		// vhost na frente
		LogFormat: `%v %h %^[%d:%t %^] "%r" %s %b "%R" "%u"`,
	},
}

// tenta verificar um formato com --verify-format
func verifyFormat(sampleFile, dateFmt, timeFmt, logFmt string) error {
	args := []string{
		"--no-global-config",
		"--date-format=" + dateFmt,
		"--time-format=" + timeFmt,
		"--log-format=" + logFmt,
		"--verify-format",
		"-f", sampleFile,
	}
	cmd := exec.Command("goaccess", args...)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(strings.TrimSpace(stderr.String()))
	}
	return nil
}

func detectLogFormat(files []string) (dateFmt, timeFmt, logFmt string, chosen string, err error) {
	// escolhe um ficheiro com linhas
	var sample string
	for _, f := range files {
		if fi, e := os.Stat(f); e == nil && fi.Size() > 0 {
			sample = f
			break
		}
	}
	if sample == "" {
		return "", "", "", "", errors.New("no non-empty log file to verify format")
	}
	for _, c := range candidates {
		if e := verifyFormat(sample, c.DateFmt, c.TimeFmt, c.LogFormat); e == nil {
			return c.DateFmt, c.TimeFmt, c.LogFormat, c.Name, nil
		}
	}
	return "", "", "", "", fmt.Errorf("none of the candidate log formats matched %s", sample)
}

// ---------------- GoAccess realtime ----------------

func ensureGoAccessDaemon() error {
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

	// 3) logs NPM: proxy-host + default-host
	var files []string
	globs := []string{
		filepath.Join(logDir, "proxy-host-*_access.log"),
		filepath.Join(logDir, "default-host*_access.log"),
	}
	seen := map[string]struct{}{}
	for _, g := range globs {
		m, _ := filepath.Glob(g)
		for _, f := range m {
			if _, ok := seen[f]; !ok {
				seen[f] = struct{}{}
				files = append(files, f)
			}
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("no logs found in %s", logDir)
	}

	// 4) detectar formato
	dateFmt, timeFmt, logFmt, chosen, derr := detectLogFormat(files)
	if derr != nil {
		// usa fallback combinado, mas loga o erro
		fmt.Println("[GoAccess] WARN format detect failed:", derr.Error())
		dateFmt = "%d/%b/%Y"
		timeFmt = "%T"
		logFmt = `%h %^[%d:%t %^] "%r" %s %b "%R" "%u"`
		chosen = "fallback-combined"
	}
	fmt.Printf("[GoAccess] using format: %s\n", chosen)

	// 5) base + ws/origin
	base := strings.TrimSpace(env512.MAIN_LINK)
	if base == "" {
		base = "http://localhost"
	}

	publicWSURL, err := buildPublicWSURL(base, goAccessPublicWSPath)
	if err != nil {
		return err
	}
	origin, err := originFromBase(base)
	if err != nil {
		return err
	}

	fmt.Println("[GoAccess] ws-url =", publicWSURL)
	fmt.Println("[GoAccess] origin =", origin)

	// 6) reiniciar se ws-url mudou no HTML
	if isPortListening(goAccessWSAddr) && fileExists(outputPath) && !fileContains(outputPath, publicWSURL) {
		stopGoAccessIfRunning(pidFile)
	}
	// 7) se já está ok, sai
	if isPortListening(goAccessWSAddr) && fileExists(outputPath) {
		return nil
	}

	// 8) args
	args := append([]string{}, files...)
	args = append(args,
		"--no-global-config",
		"--date-format="+dateFmt,
		"--time-format="+timeFmt,
		"--log-format="+logFmt,
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

	// painéis
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

	// debug / daemonize
	if debugGoAccess {
		args = append(args, "--debug-file=/tmp/goaccess-debug.log")
		fmt.Println("[GoAccess] args:", strings.Join(args, " "))
	} else {
		args = append(args, "--daemonize")
	}

	// 9) run
	cmd := exec.Command("goaccess", args...)
	// directory onde estão os logs (para paths relativos do goaccess, se algum)
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

	// 10) espera
	for i := 0; i < 20; i++ {
		if isPortListening(goAccessWSAddr) && fileExists(outputPath) {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("GoAccess started but WS/HTML are not ready")
}

// ---------------- WS túnel e página ----------------

// Túnel WS manual (hijack).
func wsTunnelToGoAccess(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") ||
		!strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "upgrade required", http.StatusBadRequest)
		return
	}

	back, err := net.DialTimeout("tcp", goAccessWSAddr, 5*time.Second)
	if err != nil {
		http.Error(w, "backend ws unavailable", http.StatusBadGateway)
		return
	}

	req := r.Clone(context.Background())
	req.URL = &url.URL{Scheme: "http", Host: goAccessWSAddr, Path: "/", RawQuery: r.URL.RawQuery}
	req.Host = goAccessWSAddr
	req.RequestURI = ""
	req.Header = r.Header.Clone()
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")

	if err := req.Write(back); err != nil {
		_ = back.Close()
		http.Error(w, "failed to write backend handshake", http.StatusBadGateway)
		return
	}

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

	errCh := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(back, clientConn); _ = back.Close(); errCh <- struct{}{} }()
	go func() { _, _ = io.Copy(clientConn, back); _ = clientConn.Close(); errCh <- struct{}{} }()
	<-errCh
}

func goAccessPageHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), goAccessTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- ensureGoAccessDaemon() }()

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
	r.HandleFunc(goAccessPublicWSPath, wsTunnelToGoAccess)
}
