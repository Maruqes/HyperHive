// api/goaccess.go
package api

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	goHTML        = "goaccess.html"
	goPID         = "goaccess.pid"
	wsHost        = "127.0.0.1"
	wsPort        = 7891
	htmlRefresh   = 5
	dateFmt       = "%d/%b/%Y"
	timeFmt       = "%T"
	logFmt        = `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`
)

var (
	mu            sync.Mutex
	curOrigin     string
	curWSURL      string
	wdCached      string
)

func wsAddr() string { return fmt.Sprintf("%s:%d", wsHost, wsPort) }

func wsURLAndOrigin(r *http.Request) (wsURL, origin string) {
	wss := r != nil && r.TLS != nil
	scheme := "ws"
	if wss { scheme = "wss" }

	host := r.Host
	if host == "" { host = "localhost" }

	wsURL  = scheme + "://" + host + "/goa-ws"
	if wss { origin = "https://" + host } else { origin = "http://" + host }
	return
}

func isUp() bool {
	c, err := net.DialTimeout("tcp", wsAddr(), 300*time.Millisecond)
	if err != nil { return false }
	_ = c.Close(); return true
}

func readPID(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil { return 0, err }
	p, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return p, err
}
func killPID(pid int) {
	_ = syscall.Kill(pid, syscall.SIGTERM)
	dead := time.Now().Add(2 * time.Second)
	for time.Now().Before(dead) {
		if syscall.Kill(pid, 0) != nil { return }
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

func startGoAccess(baseDir, wsURL, origin string) error {
	if _, err := exec.LookPath("goaccess"); err != nil {
		return fmt.Errorf("goaccess not found in PATH: %w", err)
	}
	logDir  := filepath.Join(baseDir, "npm-data", "logs")
	statsDir:= filepath.Join(baseDir, "npm-data", "stats")
	if err := os.MkdirAll(statsDir, 0o755); err != nil { return err }

	// só access logs
	files, err := filepath.Glob(filepath.Join(logDir, "proxy-host-*_access.log"))
	if err != nil || len(files) == 0 {
		return fmt.Errorf("no access logs in %s", logDir)
	}

	htmlOut := filepath.Join(statsDir, goHTML)
	pidFile := filepath.Join(statsDir, goPID)

	args := []string{
		"--no-global-config",
		"--date-format=" + dateFmt,
		"--time-format=" + timeFmt,
		"--log-format=" + logFmt,
		"--real-time-html",
		"--daemonize",
		"--html-refresh=" + fmt.Sprint(htmlRefresh),
		"--port=" + fmt.Sprint(wsPort),
		"--ws-url=" + wsURL,
		"--origin=" + origin,
		"--pid-file=" + pidFile,
		"-o", htmlOut,
	}
	args = append(args, files...)

	cmd := exec.Command("goaccess", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil { return err }

	curOrigin, curWSURL = origin, wsURL
	return nil
}

func stopGoAccess(baseDir string) {
	pidPath := filepath.Join(baseDir, "npm-data", "stats", goPID)
	pid, err := readPID(pidPath)
	if err == nil && pid > 0 { killPID(pid) }
	_ = os.Remove(pidPath)
}

func ensureGoAccess(r *http.Request) error {
	mu.Lock(); defer mu.Unlock()

	if wdCached == "" {
		wd, err := os.Getwd(); if err != nil { return err }
		wdCached = wd
	}
	wsu, ori := wsURLAndOrigin(r)

	if isUp() && wsu == curWSURL && ori == curOrigin { return nil }
	if isUp() && (wsu != curWSURL || ori != curOrigin) {
		stopGoAccess(wdCached)
		time.Sleep(150 * time.Millisecond)
	}
	return startGoAccess(wdCached, wsu, ori)
}

// ---------- HTTP ----------

func goaccessPage(w http.ResponseWriter, r *http.Request) {
	if err := ensureGoAccess(r); err != nil {
		http.Error(w, "goaccess start error: "+err.Error(), 500); return
	}
	// CSP que permite WebSocket + inline do GoAccess
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; connect-src 'self' ws: wss:; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filepath.Join(wdCached, "npm-data", "stats", goHTML))
}

func wsProxy(target string) http.HandlerFunc {
	u, _ := url.Parse(target) // http://127.0.0.1:7891
	rp := httputil.NewSingleHostReverseProxy(u)

	// reescreve o path para "/" — o WS do GoAccess atende na raiz
	orig := rp.Director
	rp.Director = func(req *http.Request) {
		orig(req)
		req.URL.Scheme = u.Scheme
		req.URL.Host   = u.Host
		req.Host       = u.Host
		req.URL.Path   = "/"     // <— importante
	}

	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "goaccess ws upstream unavailable", http.StatusBadGateway)
	}
	return rp.ServeHTTP
}

func setupGoAccessAPI(r chi.Router) {
	r.Get("/goaccess", goaccessPage)
	r.HandleFunc("/goa-ws", wsProxy("http://"+wsAddr()))
}
