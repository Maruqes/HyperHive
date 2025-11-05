// Package api - GoAccess real-time dashboard behind Chi
package api

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	goAccessHTMLName     = "goaccess.html"
	goAccessWSListenHost = "127.0.0.1"
	goAccessWSListenPort = 7891 // porta interna do WS do GoAccess
	goAccessHTMLRefresh  = 5    // fallback sem WS
	goAccessDateFormat   = "%d/%b/%Y"
	goAccessTimeFormat   = "%T"
	// Formato que corresponde às tuas linhas do NPM:
	// [05/Nov/2025:21:27:53 +0000] - 200 200 - DELETE https host "/path" [Client ip] [Length 0] ...
	goAccessLogFormat = `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`
)

var (
	goaMu sync.Mutex // garante arranque único / restart seguro
)

// --- util ---

func wsAddr() string { return fmt.Sprintf("%s:%d", goAccessWSListenHost, goAccessWSListenPort) }

func isGoAccessUp() bool {
	c, err := net.DialTimeout("tcp", wsAddr(), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func wsURLFromRequest(r *http.Request) string {
	scheme := "ws"
	if r != nil && r.TLS != nil {
		scheme = "wss"
	}
	host := "localhost"
	if r != nil && r.Host != "" {
		host = r.Host
	}
	return scheme + "://" + host + "/goa-ws"
}

func startGoAccessRealtime(workDir string, wsURL string) error {
	if _, err := exec.LookPath("goaccess"); err != nil {
		return fmt.Errorf("goaccess not found in PATH: %w", err)
	}
	if isGoAccessUp() {
		return nil
	}

	logDir := filepath.Join(workDir, "npm-data", "logs")
	statsDir := filepath.Join(workDir, "npm-data", "stats")
	if err := os.MkdirAll(statsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir stats: %w", err)
	}

	// escolhe só access logs
	files, err := filepath.Glob(filepath.Join(logDir, "proxy-host-*_access.log"))
	if err != nil {
		return fmt.Errorf("glob logs: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no access logs found in %s", logDir)
	}

	htmlOut := filepath.Join(statsDir, goAccessHTMLName)

	// Flags do GoAccess
	args := []string{
		"--no-global-config",
		"--date-format=" + goAccessDateFormat,
		"--time-format=" + goAccessTimeFormat,
		"--log-format=" + goAccessLogFormat,
		"--real-time-html",
		"--daemonize",
		"--html-refresh=" + fmt.Sprint(goAccessHTMLRefresh),
		"--port=" + fmt.Sprint(goAccessWSListenPort),
		"--ws-url=" + wsURL,
		"-o", htmlOut,
	}
	args = append(args, files...)

	cmd := exec.Command("goaccess", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func ensureGoAccess(r *http.Request) error {
	goaMu.Lock()
	defer goaMu.Unlock()

	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	if isGoAccessUp() {
		return nil
	}
	return startGoAccessRealtime(wd, wsURLFromRequest(r))
}

func goAccessHandler(w http.ResponseWriter, r *http.Request) {
	if err := ensureGoAccess(r); err != nil {
		http.Error(w, "failed to start goaccess: "+err.Error(), http.StatusInternalServerError)
		return
	}
	wd, _ := os.Getwd()
	html := filepath.Join(wd, "npm-data", "stats", goAccessHTMLName)
	http.ServeFile(w, r, html)
}

func wsProxyHandler(target string) http.HandlerFunc {
	u, _ := url.Parse(target) // ex.: "http://127.0.0.1:7891"
	rp := httputil.NewSingleHostReverseProxy(u)

	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "goaccess websocket upstream unavailable", http.StatusBadGateway)
	}
	return rp.ServeHTTP
}

// SetupGoAccessAPI regista as rotas do dashboard e do WS proxy.
func setupGoAccessAPI(r chi.Router) {
	r.Get("/goaccess", goAccessHandler)
	r.HandleFunc("/goa-ws", wsProxyHandler("http://"+wsAddr()))
}
