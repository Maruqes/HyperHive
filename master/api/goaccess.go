package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"512SvMan/env512"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

const (
	goAccessTimeout       = 2 * time.Minute
	goAccessRefreshSecond = 5
	goAccessGeoIPDir      = "geoipdb"
	goAccessGeoIPMaxAge   = 7 * 24 * time.Hour
)

var (
	errGeoIPNotFound = errors.New("geoip database not found")

	// Estado global simples para coordenação de rebuilds e difusão WS.
	wsUpgrader = websocket.Upgrader{
		// Mesma origem/host: como servimos WS no mesmo domínio/porta, isto é seguro.
		CheckOrigin: func(r *http.Request) bool {
			// Se quiseres restringir mais, compara r.Header.Get("Origin") com r.Host.
			return true
		},
	}

	wsConnsMu sync.Mutex
	wsConns   = map[*websocket.Conn]struct{}{}

	lastBuildMu       sync.Mutex
	lastBuildAt       time.Time
	lastLogsModTime   time.Time
	lastGoAccessError error
)

// ------- Handlers HTTP/WS -------

func setupGoAccessAPI(r chi.Router) {
	// Página “casca” que contém o iframe e o JS do WebSocket.
	r.Get("/goaccess", goAccessPageHandler)
	// Conteúdo do relatório (HTML gerado pelo goaccess).
	r.Get("/goaccess/content", goAccessContentHandler)
	// WebSocket no MESMO host/porta – sem abrir nada novo.
	r.Get("/goaccess-ws", goAccessWSHandler)
}

// Página wrapper com iframe + WS para auto-refresh
func goAccessPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Carregamos o relatório numa iframe. Ao receber mensagens no WS, recarregamos a src (com cache-buster).
	io.WriteString(w, `<!doctype html>
<html lang="pt-PT">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>GoAccess (HyperHive)</title>
<style>
html,body{margin:0;padding:0;height:100%} #frame{border:0;width:100%;height:100vh}
</style>
</head>
<body>
<iframe id="frame" src="/goaccess/content"></iframe>
<script>
(function(){
  function openWS(){
    var proto = (location.protocol === 'https:') ? 'wss://' : 'ws://';
    var ws = new WebSocket(proto + location.host + '/goaccess-ws');
    ws.onmessage = function(){
      var f = document.getElementById('frame');
      // cache-buster para evitar qualquer caching do browser/proxy
      f.src = '/goaccess/content?ts=' + Date.now();
    };
    ws.onclose = function(){
      // tenta reconectar de forma simples
      setTimeout(openWS, 2000);
    };
  }
  openWS();
})();
</script>
</body>
</html>`)
}

// Serve o HTML gerado pelo GoAccess. Garante rebuild se necessário.
func goAccessContentHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), goAccessTimeout)
	defer cancel()

	outputPath, err := ensureGoAccessBuilt(ctx)
	if err != nil {
		http.Error(w, "falha a gerar relatório GoAccess: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		http.Error(w, "falha a ler relatório gerado", http.StatusInternalServerError)
		return
	}

	// Nota: o relatório do GoAccess já traz o seu <html> completo: servimos tal-qual.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Evita caching (para load imediato quando recarregamos a iframe).
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// WebSocket: em cada ligação, corre um ticker que verifica alterações e aciona rebuilds.
// Quando há rebuild (ou 1º load), envia uma mensagem (qualquer payload) para forçar refresh do iframe.
func goAccessWSHandler(w http.ResponseWriter, r *http.Request) {
	c, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "upgrade WebSocket falhou", http.StatusBadRequest)
		return
	}
	registerWS(c)
	defer unregisterWS(c)

	// Refresh imediato para o cliente carregar a versão atual
	_ = c.WriteMessage(websocket.TextMessage, []byte("refresh"))

	ticker := time.NewTicker(time.Duration(goAccessRefreshSecond) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(r.Context(), goAccessTimeout)
			changed, buildErr := maybeRebuildGoAccess(ctx)
			cancel()

			// Guarda último erro (podes expor em /health, se quiseres)
			lastBuildMu.Lock()
			lastGoAccessError = buildErr
			lastBuildMu.Unlock()

			if buildErr != nil {
				// Não fecha o WS; apenas regista. Cliente continua a mostrar a última versão válida.
				continue
			}
			if changed {
				// Diz ao cliente para recarregar a iframe
				if err := c.WriteMessage(websocket.TextMessage, []byte("refresh")); err != nil {
					return
				}
			}
		default:
			// Lê control frames (pings/close) sem bloquear indefinidamente.
			_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
			if _, _, err := c.ReadMessage(); err != nil {
				// Qualquer erro de leitura/timeout encerra a ligação do lado do servidor.
				return
			}
		}
	}
}

func registerWS(c *websocket.Conn) {
	wsConnsMu.Lock()
	wsConns[c] = struct{}{}
	wsConnsMu.Unlock()
}

func unregisterWS(c *websocket.Conn) {
	wsConnsMu.Lock()
	delete(wsConns, c)
	wsConnsMu.Unlock()
	_ = c.Close()
}

// ------- Build / Rebuild do relatório -------

func ensureGoAccessBuilt(ctx context.Context) (string, error) {
	changed, err := maybeRebuildGoAccess(ctx)
	if err != nil {
		return "", err
	}
	_ = changed
	workDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return filepath.Join(workDir, "npm-data", "stats", "goaccess.html"), nil
}

func maybeRebuildGoAccess(ctx context.Context) (bool, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return false, fmt.Errorf("resolve working directory: %w", err)
	}

	logDir := filepath.Join(workDir, "npm-data", "logs")
	if stat, err := os.Stat(logDir); err != nil || !stat.IsDir() {
		return false, fmt.Errorf("logs directory not found")
	}
	pattern := filepath.Join(logDir, "proxy-host-*_access.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return false, fmt.Errorf("enumerate log files: %w", err)
	}
	if len(files) == 0 {
		return false, fmt.Errorf("no proxy access logs found")
	}

	// MTime mais recente dos logs
	currentMTime := time.Time{}
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			if st.ModTime().After(currentMTime) {
				currentMTime = st.ModTime()
			}
		}
	}

	// Debounce: só reconstruímos se os logs mudaram desde a última build,
	// ou se já passou o intervalo de refresh.
	lastBuildMu.Lock()
	shouldBuild := currentMTime.After(lastLogsModTime) || time.Since(lastBuildAt) >= time.Duration(goAccessRefreshSecond)*time.Second
	lastBuildMu.Unlock()

	if !shouldBuild {
		return false, nil
	}

	// Lock para que apenas um build ocorra de cada vez.
	lastBuildMu.Lock()
	defer lastBuildMu.Unlock()

	// Reavalia dentro do lock para evitar rebuild duplicado.
	if currentMTime.After(lastLogsModTime) || time.Since(lastBuildAt) >= time.Duration(goAccessRefreshSecond)*time.Second {
		if err := runGoAccessOnce(ctx, workDir, files); err != nil {
			return false, err
		}
		lastLogsModTime = currentMTime
		lastBuildAt = time.Now()
		return true, nil
	}

	return false, nil
}

func runGoAccessOnce(ctx context.Context, workDir string, files []string) error {
	statsDir := filepath.Join(workDir, "npm-data", "stats")
	if err := os.MkdirAll(statsDir, 0o755); err != nil {
		return fmt.Errorf("prepare stats directory: %w", err)
	}
	outputPath := filepath.Join(statsDir, "goaccess.html")

	// Formato compatível com NPM proxy-host-*_access.log
	logFormat := `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	args := append([]string{}, files...)
	args = append(args,
		"--no-global-config",
		"--date-format=%d/%b/%Y",
		"--time-format=%T",
		// Não usamos o servidor WS do GoAccess: geramos ficheiro estático e
		// fazemos auto-refresh via o nosso WS.
		"--log-format="+logFormat,
		"-o", outputPath,
	)

	// painéis enable/disable vindos do env512
	addPanelArgs := func(flag string, values []string) {
		for _, p := range values {
			if s := strings.TrimSpace(p); s != "" {
				args = append(args, fmt.Sprintf("--%s=%s", flag, s))
			}
		}
	}
	addPanelArgs("enable-panel", env512.GoAccessEnablePanels)
	addPanelArgs("disable-panel", env512.GoAccessDisablePanels)

	// GeoIP
	geoIPDBPath, err := ensureGeoIPDatabase(ctx, workDir)
	if err == nil && strings.TrimSpace(geoIPDBPath) != "" {
		args = append(args, "--geoip-database="+geoIPDBPath)
	}

	cmd := exec.CommandContext(ctx, "goaccess", args...)
	cmd.Dir = filepath.Join(workDir, "npm-data", "logs")

	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("goaccess: %s", msg)
	}
	return nil
}

// ------- GeoIP helpers (iguais à tua versão, com pequenos ajustes) -------

func ensureGeoIPDatabase(ctx context.Context, workDir string) (string, error) {
	cached, err := findCachedGeoIP(workDir)
	if errors.Is(err, errGeoIPNotFound) {
		return downloadGeoIPDatabase(ctx, workDir)
	}
	if err != nil {
		return "", err
	}

	info, err := os.Stat(cached)
	if err != nil {
		if os.IsNotExist(err) {
			return downloadGeoIPDatabase(ctx, workDir)
		}
		return "", fmt.Errorf("inspect cached GeoIP database: %w", err)
	}

	if time.Since(info.ModTime()) > goAccessGeoIPMaxAge {
		if strings.TrimSpace(env512.GoAccessGeoIPLicenseKey) == "" {
			return cached, nil
		}
		freshPath, err := downloadGeoIPDatabase(ctx, workDir)
		if err != nil {
			return "", fmt.Errorf("refresh GeoIP database: %w", err)
		}
		return freshPath, nil
	}

	return cached, nil
}

func findCachedGeoIP(workDir string) (string, error) {
	dir := filepath.Join(workDir, goAccessGeoIPDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("unable to prepare GeoIP directory %q: %w", dir, err)
	}

	preferred := []string{
		"GeoLite2-City.mmdb",
		"GeoLite2-Country.mmdb",
		"GeoIP2-City.mmdb",
		"GeoIP2-Country.mmdb",
	}
	for _, name := range preferred {
		candidate := filepath.Join(dir, name)
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return candidate, nil
		}
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.mmdb"))
	if err != nil {
		return "", fmt.Errorf("scan GeoIP directory %q: %w", dir, err)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("%w: %s", errGeoIPNotFound, dir)
	}
	return files[0], nil
}

func downloadGeoIPDatabase(ctx context.Context, workDir string) (string, error) {
	key := strings.TrimSpace(env512.GoAccessGeoIPLicenseKey)
	if key == "" {
		return "", fmt.Errorf("GOACCESS_GEOIP_LICENSE_KEY must be set to download GeoIP databases automatically")
	}

	edition := env512.GoAccessGeoIPEdition
	if edition == "" {
		edition = "GeoLite2-City"
	}

	dir := filepath.Join(workDir, goAccessGeoIPDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("unable to prepare GeoIP directory %q: %w", dir, err)
	}

	downloadURL := fmt.Sprintf(
		"https://download.maxmind.com/app/geoip_download?edition_id=%s&license_key=%s&suffix=tar.gz",
		url.QueryEscape(edition),
		url.QueryEscape(key),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("construct GeoIP download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download GeoIP database: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("download GeoIP database: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("decompress GeoIP archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var extracted string

	for {
		header, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", fmt.Errorf("read GeoIP archive: %w", err)
		}
		if header.FileInfo().IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(header.Name), ".mmdb") {
			continue
		}

		base := filepath.Base(header.Name)
		tmp, err := os.CreateTemp(dir, "geoip-*.mmdb")
		if err != nil {
			return "", fmt.Errorf("create temp GeoIP file: %w", err)
		}
		if _, err := io.Copy(tmp, tr); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return "", fmt.Errorf("write GeoIP database: %w", err)
		}
		tmp.Close()

		dest := filepath.Join(dir, base)
		_ = os.Remove(dest)
		if err := os.Rename(tmp.Name(), dest); err != nil {
			os.Remove(tmp.Name())
			return "", fmt.Errorf("place GeoIP database: %w", err)
		}

		extracted = dest
		break
	}

	if extracted == "" {
		return "", fmt.Errorf("downloaded GeoIP archive but no .mmdb file was found")
	}
	return extracted, nil
}
