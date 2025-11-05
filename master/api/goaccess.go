// Package api - GoAccess real-time dashboard (Chi) com WS e gestão de Origin
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
	goAccessHTMLName     = "goaccess.html"
	goAccessPIDName      = "goaccess.pid"
	goAccessWSListenHost = "127.0.0.1"
	goAccessWSListenPort = 7891
	goAccessHTMLRefresh  = 5
	goAccessDateFormat   = "%d/%b/%Y"
	goAccessTimeFormat   = "%T"
	// Formato para os teus logs NPM (HTTP):
	// [05/Nov/2025:21:27:53 +0000] - 200 200 - DELETE https host "/path" [Client ip] [Length 0] ...
	goAccessLogFormat = `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`
)

var (
	goaMu           sync.Mutex
	currentOrigin   string
	currentWSURL    string
	workedDirectory string
)

// --- utils ---

func wsAddr() string { return fmt.Sprintf("%s:%d", goAccessWSListenHost, goAccessWSListenPort) }

func isGoAccessUp() bool {
	c, err := net.DialTimeout("tcp", wsAddr(), 350*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func wsURLAndOriginFromRequest(r *http.Request) (wsURL, origin string) {
	// ws-url que o HTML vai usar
	scheme := "ws"
	if r != nil && r.TLS != nil {
		scheme = "wss"
	}
	host := "localhost"
	if r != nil && r.Host != "" {
		host = r.Host
	}
	wsURL = scheme + "://" + host + "/goa-ws"

	// origin aceites pelo servidor WS
	if r != nil && r.TLS != nil {
		origin = "https://" + host
	} else {
		origin = "http://" + host
	}
	return wsURL, origin
}

func readPID(pidFile string) (int, error) {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(b))
	p, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return p, nil
}

func killPID(pid int) error {
	// SIGTERM e aguardar
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	// esperar (máx 2s)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			// já morreu
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	// força SIGKILL
	_ = syscall.Kill(pid, syscall.SIGKILL)
	return nil
}

// --- core ---

func startGoAccessRealtime(workDir, wsURL, origin string) error {
	if _, err := exec.LookPath("goaccess"); err != nil {
		return fmt.Errorf("goaccess not found in PATH: %w", err)
	}

	logDir := filepath.Join(workDir, "npm-data", "logs")
	statsDir := filepath.Join(workDir, "npm-data", "stats")
	if err := os.MkdirAll(statsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir stats: %w", err)
	}

	// só access logs dos hosts
	files, err := filepath.Glob(filepath.Join(logDir, "proxy-host-*_access.log"))
	if err != nil || len(files) == 0 {
		return fmt.Errorf("no access logs found in %s", logDir)
	}

	htmlOut := filepath.Join(statsDir, goAccessHTMLName)
	pidFile := filepath.Join(statsDir, goAccessPIDName)

	args := []string{
		"--no-global-config",
		"--date-format=" + goAccessDateFormat,
		"--time-format=" + goAccessTimeFormat,
		"--log-format=" + goAccessLogFormat,
		"--real-time-html",
		"--daemonize",
		"--html-refresh=" + fmt.Sprint(goAccessHTMLRefresh),
		"--port=" + fmt.Sprint(goAccessWSListenPort), // WS listen interno
		"--ws-url=" + wsURL,                          // URL público para o HTML
		"--origin=" + origin,                         // validação do Origin
		"--pid-file=" + pidFile,                      // para podermos reiniciar
		"-o", htmlOut,
	}
	args = append(args, files...)

	cmd := exec.Command("goaccess", args...)
	// Opcional: herdar stdout/stderr para ver logs de arranque do goaccess
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}

	currentOrigin = origin
	currentWSURL = wsURL
	return nil
}

func stopGoAccess(workDir string) error {
	statsDir := filepath.Join(workDir, "npm-data", "stats")
	pidFile := filepath.Join(statsDir, goAccessPIDName)
	pid, err := readPID(pidFile)
	if err != nil {
		// se não existir, tenta matar por porta
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if pid > 0 {
		_ = killPID(pid)
	}
	_ = os.Remove(pidFile)
	return nil
}

// se o Origin/WS URL solicitado mudou, reinicia o goaccess com as novas flags
func ensureGoAccess(r *http.Request) error {
	goaMu.Lock()
	defer goaMu.Unlock()

	// resolve base dir uma única vez
	if workedDirectory == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		workedDirectory = wd
	}

	wsURL, origin := wsURLAndOriginFromRequest(r)

	// Se já está a ouvir e o origin/URL coincidem, não faz nada
	if isGoAccessUp() && origin == currentOrigin && wsURL == currentWSURL {
		return nil
	}

	// Se está a ouvir mas o origin mudou, reinicia
	if isGoAccessUp() && (origin != currentOrigin || wsURL != currentWSURL) {
		_ = stopGoAccess(workedDirectory)
		// pequena pausa para libertar a porta
		time.Sleep(200 * time.Millisecond)
	}

	return startGoAccessRealtime(workedDirectory, wsURL, origin)
}

// --- HTTP handlers ---

func goAccessHandler(w http.ResponseWriter, r *http.Request) {
	if err := ensureGoAccess(r); err != nil {
		http.Error(w, "failed to start goaccess: "+err.Error(), http.StatusInternalServerError)
		return
	}
	html := filepath.Join(workedDirectory, "npm-data", "stats", goAccessHTMLName)
	http.ServeFile(w, r, html)
}

func wsProxyHandler(target string) http.HandlerFunc {
	u, _ := url.Parse(target) // http://127.0.0.1:7891
	rp := httputil.NewSingleHostReverseProxy(u)
	// Não vamos reescrever o path — preserva tokens/query do WS
	// Apenas garantimos Host correta para upstream
	orig := rp.Director
	rp.Director = func(req *http.Request) {
		orig(req)
		req.Host = u.Host
	}
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "goaccess websocket upstream unavailable", http.StatusBadGateway)
	}
	return rp.ServeHTTP
}

func SetupGoAccessAPI(r chi.Router) {
	r.Get("/goaccess", goAccessHandler)
	r.HandleFunc("/goa-ws", wsProxyHandler("http://"+wsAddr()))
}
