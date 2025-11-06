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
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	goAccessTimeout       = 2 * time.Minute
	goAccessRefreshSecond = 5
	goAccessWSAddr        = "127.0.0.1:7891" // WS local do GoAccess
	goAccessPublicWSPath  = "/goaccess/ws"   // caminho público do WS
)

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

// buildPublicWSURL constrói o ws/wss final a partir do MAIN_LINK e do path do WS.
// - Se MAIN_LINK for https → usa wss
// - Se MAIN_LINK for http → usa ws
// - Remove/evita barras duplicadas
func buildPublicWSURL(base, wsPath string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", errors.New("MAIN_LINK vazio")
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base // default se faltou o esquema
	}

	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("MAIN_LINK inválido: %w", err)
	}
	if u.Host == "" && u.Path != "" {
		// Caso tipo "hyperhive.maruqes.com" tenha caído em Path
		reparse := "http://" + u.Path
		u, err = url.Parse(reparse)
		if err != nil {
			return "", fmt.Errorf("MAIN_LINK inválido (host): %w", err)
		}
	}

	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	default:
		// qualquer outro (inclui http, vazio, etc.) cai para ws
		u.Scheme = "ws"
	}

	// Normaliza path: preserva eventual prefixo e anexa /goaccess/ws
	basePath := strings.TrimRight(u.Path, "/")
	if !strings.HasPrefix(wsPath, "/") {
		wsPath = "/" + wsPath
	}
	u.Path = basePath + wsPath
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// --- GoAccess realtime ---

func ensureGoAccessDaemon() error {
	// Verifica/instala goaccess se necessário (Fedora/RHEL via dnf)
	if _, err := exec.LookPath("goaccess"); err != nil {
		if _, derr := exec.LookPath("dnf"); derr != nil {
			return fmt.Errorf("goaccess não encontrado e dnf também não: %w", err)
		}
		if err := exec.Command("dnf", "-y", "install", "goaccess").Run(); err != nil {
			return fmt.Errorf("a instalar goaccess: %w", err)
		}
	}

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

	// Ajusta o padrão aos teus logs
	pattern := filepath.Join(logDir, "proxy-host-*_access.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("listar logs: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("sem logs em %s", pattern)
	}

	// Formato do Nginx Proxy Manager
	logFormat := `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	// Constrói a URL pública do WebSocket a partir do MAIN_LINK
	publicWSURL, err := buildPublicWSURL(env512.MAIN_LINK, goAccessPublicWSPath)
	if err != nil {
		return err
	}

	// Se já está a correr e HTML existe, não arrancar outro
	if isPortListening(goAccessWSAddr) && fileExists(outputPath) {
		return nil
	}

	args := []string{}
	args = append(args, files...)
	args = append(args,
		"--no-global-config",
		"--date-format=%d/%b/%Y",
		"--time-format=%T",
		"--log-format="+logFormat,

		"--real-time-html",
		"--daemonize",
		"--persist",
		"--restore",

		"-o", outputPath,

		"--addr=127.0.0.1",
		"--port=7891",

		"--ws-url="+publicWSURL,
	)

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
		return fmt.Errorf("arrancar goaccess realtime: %s", msg)
	}

	// Espera curto pelo WS/HTML
	for i := 0; i < 20; i++ {
		if isPortListening(goAccessWSAddr) && fileExists(outputPath) {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("goaccess arrancou mas não vejo o WS/HTML prontos")
}

// --- Proxy WS e página ---

func newGoAccessWSProxy() http.Handler {
	u := &url.URL{Scheme: "http", Host: goAccessWSAddr}
	p := httputil.NewSingleHostReverseProxy(u)

	origDirector := p.Director
	p.Director = func(r *http.Request) {
		origDirector(r)
		r.URL.Scheme = "http"
		r.URL.Host = goAccessWSAddr
		r.URL.Path = "/" // GoAccess espera raiz
		r.Host = goAccessWSAddr
		// headers de upgrade mantidos pelo ReverseProxy
	}

	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "websocket proxy error: "+err.Error(), http.StatusBadGateway)
	}
	return p
}

func goAccessPageHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), goAccessTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- ensureGoAccessDaemon() }()

	select {
	case <-ctx.Done():
		http.Error(w, "timeout a preparar GoAccess", http.StatusGatewayTimeout)
		return
	case err := <-done:
		if err != nil {
			http.Error(w, "GoAccess não disponível: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	workDir, _ := os.Getwd()
	html := filepath.Join(workDir, "npm-data", "stats", "goaccess.html")
	http.ServeFile(w, r, html)
}

func setupGoAccessAPI(r chi.Router) {
	// Página HTML
	r.Get("/goaccess", goAccessPageHandler)
	// WebSocket público → proxy para 127.0.0.1:7891
	r.Handle(goAccessPublicWSPath, newGoAccessWSProxy())
}
