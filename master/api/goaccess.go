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
	goAccessTimeout       = 2 * time.Minute
	goAccessRefreshSecond = 5

	// WS interno do GoAccess (o NPM faz proxy disto em /goaccess-ws)
	goAccessWSAddr   = "127.0.0.1:7891"
	goAccessWSListen = "127.0.0.1"
	goAccessWSPort   = 7891

	// Caminho público (no teu NPM) para o WS
	publicWSPath = "/goaccess-ws"
)

var (
	goaccessMu   sync.Mutex
	goaccessCmd  *exec.Cmd
	goaccessHTML string
)

// ---------- util ----------

func mainLink() string {
	base := strings.TrimSpace(env512.MAIN_LINK)
	if base == "" {
		base = "http://127.0.0.1:9595"
	}
	return strings.TrimRight(base, "/")
}

func computeOriginAndWS() (origin string, wsURL string, _ error) {
	base := mainLink()
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", fmt.Errorf("MAIN_LINK inválido: %q", base)
	}
	origin = u.Scheme + "://" + u.Host
	// Se MAIN_LINK for https, usa wss; caso contrário, ws.
	wsScheme := "ws"
	if strings.EqualFold(u.Scheme, "https") {
		wsScheme = "wss"
	}
	wsURL = fmt.Sprintf("%s://%s%s", wsScheme, u.Host, publicWSPath)
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

// ---------- GoAccess manager ----------

func ensureGoAccessRunning(ctx context.Context, logDir string, files []string, outputPath string) error {
	goaccessMu.Lock()
	defer goaccessMu.Unlock()

	origin, wsURL, err := computeOriginAndWS()
	if err != nil {
		return err
	}

	// Se já temos um processo vivo, não mexer.
	if goaccessCmd != nil && goaccessCmd.Process != nil {
		if goaccessCmd.ProcessState == nil {
			return nil
		}
	}

	// Se a porta já está ocupada por outro processo, assumimos que o WS está vivo.
	// Mesmo assim (re)geramos o HTML.
	if !portBusy(goAccessWSAddr) {
		// Arrancar o WS do GoAccess
		// NOTA: --real-time-html mantém o processo em execução a emitir updates.
		args := append([]string{}, files...)
		args = append(args,
			"--no-global-config",
			"--date-format=%d/%b/%Y",
			"--time-format=%T",
			"--real-time-html",
			fmt.Sprintf("--html-refresh=%d", goAccessRefreshSecond),
			"--addr="+goAccessWSListen,
			fmt.Sprintf("--port=%d", goAccessWSPort),
			"--ws-url="+wsURL,
			"--origin="+origin,
			// Se preferires que o HTML seja sempre escrito em ficheiro:
			"-o", outputPath,
		)

		cmd := exec.CommandContext(ctx, "goaccess", args...)
		cmd.Dir = logDir

		// stdout não interessa; guardamos stderr para debug
		var stderr bytes.Buffer
		cmd.Stdout = io.Discard
		cmd.Stderr = &stderr

		// Usamos Start() (não Run) porque isto é um daemon long-running.
		if err := cmd.Start(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("falha a iniciar goaccess: %s", msg)
		}
		goaccessCmd = cmd

		// Espera curta para o WS subir.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if portBusy(goAccessWSAddr) {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}

	// Gerar/atualizar o HTML inicial (um shot imediato).
	// Usa os MESMOS argumentos mas sem --real-time-html para não abrir outro WS.
	{
		args := append([]string{}, files...)
		args = append(args,
			"--no-global-config",
			"--date-format=%d/%b/%Y",
			"--time-format=%T",
			fmt.Sprintf("--html-refresh=%d", goAccessRefreshSecond),
			"--ws-url="+wsURL,
			"--origin="+origin,
			"-o", outputPath,
		)

		gen := exec.CommandContext(ctx, "goaccess", args...)
		gen.Dir = logDir
		var stderr bytes.Buffer
		gen.Stdout = io.Discard
		gen.Stderr = &stderr

		if err := gen.Run(); err != nil {
			// Se for timeout do contexto, devolve 504 a quem chamou.
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return context.DeadlineExceeded
			}
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			// Não matamos o WS; apenas reportamos a falha do HTML.
			return fmt.Errorf("falha a gerar HTML do goaccess: %s", msg)
		}
	}

	return nil
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
	if stat, err := os.Stat(logDir); err != nil || !stat.IsDir() {
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
	goaccessHTML = outputPath

	// Formato compatível com NPM access log (ajusta se necessário)
	logFormat := `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	// Acrescentar flags e arrancar/assegurar WS + gerar HTML
	args := append([]string{}, files...)
	args = append(args,
		"--log-format="+logFormat,
	)
	if err := ensureGoAccessRunning(ctx, logDir, args, outputPath); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "goaccess timed out while generating report", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Ler e devolver HTML atual
	data, err := os.ReadFile(outputPath)
	if err != nil {
		http.Error(w, "failed to read generated report", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// Opcional: endpoint para reiniciar o processo do GoAccess
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
