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
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	goAccessTimeout = 2 * time.Minute

	// WS interno do GoAccess (proxy pelo NPM no /goaccess-ws)
	goAccessWSListen = "127.0.0.1"
	goAccessWSPort   = 7891
)

var (
	goaccessMu  sync.Mutex
	goaccessCmd *exec.Cmd
)

// ---------- util ----------

func mainLink() string {
	base := strings.TrimSpace(env512.MAIN_LINK)
	if base == "" {
		base = "http://127.0.0.1:9595"
	}
	return strings.TrimRight(base, "/")
}

// Origin/WS públicos SEM porta (vais usar só 443/80 no NPM)
func computeOriginAndWS() (origin string, wsURL string, _ error) {
	u, err := url.Parse(mainLink())
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", fmt.Errorf("MAIN_LINK inválido")
	}
	host := u.Hostname() // sem :port
	origin = u.Scheme + "://" + host
	wsScheme := "ws"
	if strings.EqualFold(u.Scheme, "https") {
		wsScheme = "wss"
	}
	wsURL = fmt.Sprintf("%s://%s/goaccess-ws", wsScheme, host)
	return origin, wsURL, nil
}

func portBusy(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}

// ---------- GoAccess RT ----------

func ensureGoAccessRunning(ctx context.Context, logDir string, logFiles []string, outputPath string, logFormat string) error {
	goaccessMu.Lock()
	defer goaccessMu.Unlock()

	origin, wsURL, err := computeOriginAndWS()
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("%s:%d", goAccessWSListen, goAccessWSPort)

	// Se já está a correr, não arranca outro
	if goaccessCmd == nil || goaccessCmd.Process == nil || goaccessCmd.ProcessState != nil {
		// Se a porta não está ocupada, arrancar o WS
		if !portBusy(addr) {
			args := append([]string{}, logFiles...)
			args = append(args,
				"--no-global-config",
				"--date-format=%d/%b/%Y",
				"--time-format=%T",
				"--real-time-html",
				"--log-format="+logFormat,
				"--addr="+goAccessWSListen,
				fmt.Sprintf("--port=%d", goAccessWSPort),
				"--ws-url="+wsURL,  // público, sem :port
				"--origin="+origin, // público, sem :port
				"-o", outputPath,   // HTML inicial escrito por este processo
			)

			cmd := exec.CommandContext(ctx, "goaccess", args...)
			cmd.Dir = logDir

			var stderr bytes.Buffer
			cmd.Stdout = io.Discard
			cmd.Stderr = &stderr

			if err := cmd.Start(); err != nil {
				msg := strings.TrimSpace(stderr.String())
				if msg == "" {
					msg = err.Error()
				}
				return fmt.Errorf("falha ao iniciar goaccess: %s", msg)
			}
			goaccessCmd = cmd

			// Espera breve para socket abrir
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) && !portBusy(addr) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(120 * time.Millisecond):
				}
			}
		}
	}

	// Espera o HTML aparecer (o processo RT escreve-o)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(outputPath); err == nil && st.Size() > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(120 * time.Millisecond):
		}
	}

	return errors.New("goaccess em execução mas o HTML ainda não foi gerado")
}

// ---------- HTTP handlers ----------

func goAccessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), goAccessTimeout)
	defer cancel()

	workDir, err := os.Getwd()
	if err != nil {
		http.Error(w, "failed to resolve working directory", http.StatusInternalServerError)
		return
	}

	logDir := filepath.Join(workDir, "npm-data", "logs")
	if st, err := os.Stat(logDir); err != nil || !st.IsDir() {
		http.Error(w, "logs directory not found", http.StatusNotFound)
		return
	}

	// Descobrir logs do NPM
	pattern := filepath.Join(logDir, "proxy-host-*_access.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		http.Error(w, "failed to enumerate log files", http.StatusInternalServerError)
		return
	}
	if len(files) == 0 {
		http.Error(w, "no proxy access logs found", http.StatusNotFound)
		return
	}

	statsDir := filepath.Join(workDir, "npm-data", "stats")
	if err := os.MkdirAll(statsDir, 0o755); err != nil {
		http.Error(w, "failed to prepare stats directory", http.StatusInternalServerError)
		return
	}

	outputPath := filepath.Join(statsDir, "goaccess.html")

	// Formato típico dos access logs do NPM (ajusta se necessário)
	logFormat := `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	if err := ensureGoAccessRunning(ctx, logDir, files, outputPath, logFormat); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "goaccess timeout", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		http.Error(w, "failed to read generated report", http.StatusInternalServerError)
		return
	}

	// Evitar cache do HTML
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func goAccessRestartHandler(w http.ResponseWriter, r *http.Request) {
	goaccessMu.Lock()
	defer goaccessMu.Unlock()
	if goaccessCmd != nil && goaccessCmd.Process != nil {
		_ = goaccessCmd.Process.Kill()
		goaccessCmd = nil
	}
	w.WriteHeader(http.StatusNoContent)
}

func setupGoAccessAPI(r chi.Router) {
	r.Get("/goaccess", goAccessHandler)
	r.Post("/goaccess/restart", goAccessRestartHandler)
}
