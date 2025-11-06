package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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

	// GoAccess WS interno (o NPM faz proxy em /goaccess-ws).
	goAccessWSListen = "127.0.0.1"
	goAccessWSPort   = 7891
)

var (
	goaccessMu  sync.Mutex
	goaccessCmd *exec.Cmd
)

// --- util ---

func portBusy(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}

// Extrai scheme/host públicos do pedido (SEM porta) e constrói origin/ws-url.
func publicOriginAndWS(r *http.Request) (origin, wsURL string) {
	// Host público (sem porta)
	host := r.Host
	if i := strings.IndexByte(host, ':'); i > 0 {
		host = host[:i]
	}

	// Proto a partir de X-Forwarded-Proto ou TLS
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}

	origin = proto + "://" + host
	wss := "ws"
	if proto == "https" {
		wss = "wss"
	}
	wsURL = wss + "://" + host + "/goaccess-ws" // SEM porta pública
	return
}

// --- GoAccess RT manager ---

func ensureGoAccessRunning(ctx context.Context, logDir string, logFiles []string, outputPath string, logFormat string, origin, wsURL string) error {
	goaccessMu.Lock()
	defer goaccessMu.Unlock()

	addr := fmt.Sprintf("%s:%d", goAccessWSListen, goAccessWSPort)

	// Arranca processo RT se não estiver vivo/porta não ocupada
	if goaccessCmd == nil || goaccessCmd.Process == nil || goaccessCmd.ProcessState != nil {
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
				"--origin="+origin, // ex.: https://hyperhive.maruqes.com
				"--ws-url="+wsURL,  // ex.: wss://hyperhive.maruqes.com/goaccess-ws (SEM porta)
				"-o", outputPath,   // o próprio processo RT escreve o HTML
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

			// Espera a porta abrir
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

	// Espera o HTML ficar disponível (é o RT que o escreve)
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

// --- HTTP handlers ---

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

	// Logs do NPM
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

	// Formato (ajusta se o teu NPM divergir)
	logFormat := `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	origin, wsURL := publicOriginAndWS(r)

	if err := ensureGoAccessRunning(ctx, logDir, files, outputPath, logFormat, origin, wsURL); err != nil {
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

	// Sem cache para não ficar com HTML antigo que aponta para :7891
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
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
