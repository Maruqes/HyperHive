// Package api - GoAccess real-time via Chi (com WS fix)
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
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	goAccessHTMLName     = "goaccess.html"
	goAccessWSListenHost = "127.0.0.1"
	goAccessWSListenPort = 7891
	goAccessHTMLRefresh  = 5
	goAccessDateFormat   = "%d/%b/%Y"
	goAccessTimeFormat   = "%T"
	// Formato que bate com os teus logs NPM:
	goAccessLogFormat = `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`
)

var goaMu sync.Mutex

func wsAddr() string { return fmt.Sprintf("%s:%d", goAccessWSListenHost, goAccessWSListenPort) }

func isGoAccessUp() bool {
	c, err := net.DialTimeout("tcp", wsAddr(), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func wsURLFromRequest(r *http.Request) (wsURL, origin string) {
	scheme := "ws"
	if reqScheme := originalScheme(r); reqScheme == "https" {
		scheme = "wss"
	}
	host := "localhost"
	if r != nil && r.Host != "" {
		host = r.Host
	}
	wsURL = scheme + "://" + host + "/goa-ws"
	origin = originalScheme(r) + "://" + host
	return wsURL, origin
}

func originalScheme(r *http.Request) string {
	if r == nil {
		return "http"
	}

	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		if idx := strings.Index(forwarded, ","); idx != -1 {
			forwarded = forwarded[:idx]
		}
		if proto := strings.TrimSpace(forwarded); proto != "" {
			return proto
		}
	}

	if forwarded := r.Header.Get("X-Forwarded-Scheme"); forwarded != "" {
		if idx := strings.Index(forwarded, ","); idx != -1 {
			forwarded = forwarded[:idx]
		}
		if proto := strings.TrimSpace(forwarded); proto != "" {
			return proto
		}
	}

	if forwarded := r.Header.Get("Forwarded"); forwarded != "" {
		for _, entry := range strings.Split(forwarded, ",") {
			entry = strings.TrimSpace(entry)
			for _, part := range strings.Split(entry, ";") {
				part = strings.TrimSpace(part)
				if idx := strings.Index(part, "proto="); idx != -1 {
					proto := strings.TrimSpace(part[idx+6:])
					proto = strings.Trim(proto, `"`)
					if proto != "" {
						return proto
					}
				}
			}
		}
	}

	if r.TLS != nil {
		return "https"
	}

	if r.URL != nil && r.URL.Scheme != "" {
		return r.URL.Scheme
	}

	return "http"
}

func startGoAccessRealtime(workDir, wsURL, origin string) error {
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

	files, err := filepath.Glob(filepath.Join(logDir, "proxy-host-*_access.log"))
	if err != nil || len(files) == 0 {
		return fmt.Errorf("no access logs found in %s", logDir)
	}

	htmlOut := filepath.Join(statsDir, goAccessHTMLName)
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
		"--origin=" + origin, // <— importante quando há proxy/HTTPS
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
	if isGoAccessUp() {
		return nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	wsURL, origin := wsURLFromRequest(r)
	return startGoAccessRealtime(wd, wsURL, origin)
}

func goAccessHandler(w http.ResponseWriter, r *http.Request) {
	if err := ensureGoAccess(r); err != nil {
		http.Error(w, "failed to start goaccess: "+err.Error(), http.StatusInternalServerError)
		return
	}
	wd, _ := os.Getwd()
	http.ServeFile(w, r, filepath.Join(wd, "npm-data", "stats", goAccessHTMLName))
}

func wsProxyHandler(target string) http.HandlerFunc {
	u, _ := url.Parse(target)
	rp := httputil.NewSingleHostReverseProxy(u)
	origDirector := rp.Director
	rp.Director = func(req *http.Request) {
		origDirector(req)
		req.URL.Path = "/"
		req.Host = u.Host
	}
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "goaccess websocket upstream unavailable", http.StatusBadGateway)
	}
	return rp.ServeHTTP
}

func setupGoAccessAPI(r chi.Router) {
	r.Get("/goaccess", goAccessHandler)
	r.HandleFunc("/goa-ws", wsProxyHandler("http://"+wsAddr()))
}
