package api

import (
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
	goAccessTimeout       = 2 * time.Minute
	goAccessRefreshSecond = 5
)

var goaOnce sync.Once

func startGoAccessRealtime(workDir string) error {
	logDir := filepath.Join(workDir, "npm-data", "logs")
	statsDir := filepath.Join(workDir, "npm-data", "stats")
	_ = os.MkdirAll(statsDir, 0o755)

	htmlOut := filepath.Join(statsDir, "goaccess.html")

	logFormat := `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	// ⚠️ WS local a 7891; o HTML vai apontar para wss://<teu-dominio>/goa-ws
	args := []string{
		filepath.Join(logDir, "proxy-host-*_access.log"),
		"--no-global-config",
		"--date-format=%d/%b/%Y",
		"--time-format=%T",
		"--log-format=" + logFormat,
		"--real-time-html",
		"--daemonize",
		"--port=7891", // porta WS interna
		"--ws-url=wss://hyperhive.maruqes.com/goa-ws",
		"-o", htmlOut,
	}

	// usa /bin/sh para expandir o wildcard *.log
	cmd := exec.Command("/bin/sh", "-lc", "goaccess "+strings.Join(args, " "))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func EnsureGoAccess() error {
	var err error
	goaOnce.Do(func() {
		wd, e := os.Getwd()
		if e != nil {
			err = e
			return
		}
		err = startGoAccessRealtime(wd)
	})
	return err
}

func goAccessHandler(w http.ResponseWriter, r *http.Request) {
	if err := EnsureGoAccess(); err != nil {
		http.Error(w, "failed to start goaccess: "+err.Error(), 500)
		return
	}
	wd, _ := os.Getwd()
	html := filepath.Join(wd, "npm-data", "stats", "goaccess.html")
	http.ServeFile(w, r, html)
}

func wsProxyHandler(target string) http.HandlerFunc {
	u, _ := url.Parse(target) // "http://127.0.0.1:7891"
	rp := httputil.NewSingleHostReverseProxy(u)
	// garantir cabeçalhos de upgrade
	rp.Director = func(req *http.Request) {
		req.URL.Scheme = u.Scheme
		req.URL.Host = u.Host
		req.Host = u.Host
		if strings.Contains(strings.ToLower(req.Header.Get("Connection")), "upgrade") {
			req.Header.Set("Connection", "upgrade")
			req.Header.Set("Upgrade", "websocket")
		}
	}
	return rp.ServeHTTP
}

func setupGoAccessAPI(r chi.Router) {
	r.Get("/goaccess", goAccessHandler)
	r.HandleFunc("/goa-ws", wsProxyHandler("http://127.0.0.1:7891"))
}
