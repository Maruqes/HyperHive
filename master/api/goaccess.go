package api

import (
	"512SvMan/env512"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
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
	goAccessWSAddr        = "127.0.0.1:7891" // onde o goaccess vai escutar WS localmente
)

// ---- utils ----

func parsePanels(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	sep := func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '|' }
	raw := strings.FieldsFunc(s, sep)

	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key := strings.ToUpper(p)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func isTCPListening(addr string) bool {
	d := net.Dialer{Timeout: 300 * time.Millisecond}
	c, err := d.Dial("tcp", addr)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func mainOriginAndWS(mainLink string) (origin string, wsURL string, err error) {
	u, err := url.Parse(strings.TrimSpace(mainLink))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", fmt.Errorf("MAIN_LINK inválido: %q", mainLink)
	}
	host := u.Host
	wsScheme := "ws"

	// Para HTTPS, força host sem porto (TLS no reverse proxy) e wss://
	if u.Scheme == "https" {
		host = u.Hostname() // remove :porto
		wsScheme = "wss"
	}

	origin = u.Scheme + "://" + host // sem :porto em https
	wsURL = fmt.Sprintf("%s://%s/goaccess", wsScheme, host)
	return origin, wsURL, nil
}

func findGeoDB(workDir string) (string, bool) {
	paths := []string{
		filepath.Join(workDir, "npm-data", "geo", "GeoLite2-City.mmdb"),
		filepath.Join(workDir, "npm-data", "geo", "GeoLite2-Country.mmdb"),
		"/usr/share/GeoIP/GeoLite2-City.mmdb",
		"/usr/share/GeoIP/GeoLite2-Country.mmdb",
	}
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// ---- handler ----

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

	// recolha de logs do NPM
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

	// formato compatível com Nginx Proxy Manager (access.log padrão do NPM)
	logFormat := `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	// origem e ws-url baseados em MAIN_LINK
	origin, wsURL, err := mainOriginAndWS(env512.MAIN_LINK)
	if err != nil {
		http.Error(w, "MAIN_LINK inválido/configura MAIN_LINK (ex.: https://hyperhive.maruqes.com)", http.StatusInternalServerError)
		return
	}

	// montar argumentos do goaccess
	args := append([]string{}, files...)
	args = append(args,
		"--no-global-config",
		"--date-format=%d/%b/%Y",
		"--time-format=%T",
		fmt.Sprintf("--html-refresh=%d", goAccessRefreshSecond),
		"--log-format="+logFormat,
		"-o", outputPath,

		// realtime + servidor WS embutido
		"--real-time-html",
		"--origin="+origin,
		"--ws-url="+wsURL,
		"--addr=127.0.0.1",
		"--port=7891",
	)

	// GeoIP se existir DB
	if geoDB, ok := findGeoDB(workDir); ok {
		args = append(args, "--geoip-database="+geoDB)
	}

	// painéis (se ambos vazios -> não passar flags -> todos os painéis por defeito)
	enable := env512.GoAccessEnablePanels
	disable := env512.GoAccessDisablePanels
	for _, p := range enable {
		args = append(args, "--enable-panel="+p)
	}
	for _, p := range disable {
		args = append(args, "--ignore-panel="+p)
	}

	// garantir que o servidor WS do goaccess está a correr; se não, arrancar em background
	startNew := !isTCPListening(goAccessWSAddr)
	if startNew {
		cmd := exec.CommandContext(ctx, "goaccess", args...)
		cmd.Dir = logDir
		// redirecionar stdout/stderr para ficheiros para debugging
		stdoutFile := filepath.Join(statsDir, "goaccess.stdout.log")
		stderrFile := filepath.Join(statsDir, "goaccess.stderr.log")
		stdout, _ := os.OpenFile(stdoutFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		stderr, _ := os.OpenFile(stderrFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		cmd.Stdout = stdout
		cmd.Stderr = stderr

		// iniciar sem bloquear o handler
		if err := cmd.Start(); err != nil {
			http.Error(w, "failed to start goaccess realtime server: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// dar um pequeno tempo para o servidor abrir o porto
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if isTCPListening(goAccessWSAddr) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// garantir que o HTML existe (a 1ª geração pode demorar um instante)
	waitHTML := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(outputPath); err == nil {
			break
		}
		if time.Now().After(waitHTML) {
			// se ainda não existe, forçar uma geração "rápida" numa execução síncrona (sem realtime) só para entregar algo
			genArgs := append([]string{}, files...)
			genArgs = append(genArgs,
				"--no-global-config",
				"--date-format=%d/%b/%Y",
				"--time-format=%T",
				fmt.Sprintf("--html-refresh=%d", goAccessRefreshSecond),
				"--log-format="+logFormat,
				"-o", outputPath,
			)
			// aplicar geoip/painéis também nesta geração
			if geoDB, ok := findGeoDB(workDir); ok {
				genArgs = append(genArgs, "--geoip-database="+geoDB)
			}
			for _, p := range enable {
				genArgs = append(genArgs, "--enable-panel="+p)
			}
			for _, p := range disable {
				genArgs = append(genArgs, "--ignore-panel="+p)
			}

			var stderr bytes.Buffer
			cmd := exec.CommandContext(ctx, "goaccess", genArgs...)
			cmd.Dir = logDir
			cmd.Stdout = io.Discard
			cmd.Stderr = &stderr
			_ = cmd.Run() // melhor esforço
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// servir o HTML
	data, err := os.ReadFile(outputPath)
	if err != nil {
		http.Error(w, "failed to read generated report", http.StatusInternalServerError)
		return
	}
	// Injeta uma pequena nota (não obrigatória) com a origem e ws detectados (útil para debug visual)
	// Nota: se não quiseres nada disto, remove o bloco abaixo.
	dataStr := string(data)
	if !strings.Contains(dataStr, "data-origin-note") {
		inject := fmt.Sprintf(`<!-- data-origin-note --><div style="position:fixed;right:8px;bottom:8px;opacity:.35;font:12px/1.2 monospace;padding:4px 6px;background:#000;color:#fff;border-radius:6px;z-index:2147483647">origin=%s<br>ws=%s</div>`, origin, wsURL)
		// tentar inserir antes do fechamento do body
		dataStr = strings.Replace(dataStr, "</body>", inject+"</body>", 1)
		data = []byte(dataStr)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func setupGoAccessAPI(r chi.Router) {
	r.Get("/goaccess", goAccessHandler)
}
