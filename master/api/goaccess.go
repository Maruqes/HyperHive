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
)

const (
	goAccessTimeout       = 2 * time.Minute
	goAccessRefreshSecond = 5

	// Servidor WS interno do GoAccess (atrás do NPM)
	goAccessWSAddr = "127.0.0.1:7891"

	// Diretório/ficheiro
	goAccessGeoIPDir    = "geoipdb"
	goAccessGeoIPMaxAge = 7 * 24 * time.Hour
)

var (
	errGeoIPNotFound = errors.New("geoip database not found")

	// Guarda o processo do GoAccess em modo realtime
	goRTMu       sync.Mutex
	goRTCmd      *exec.Cmd
	goRTReport   string
	goRTLogDir   string
	goRTArgsHash string
)

// ---------- HTTP / Router ----------

func setupGoAccessAPI(r chi.Router) {
	r.Get("/goaccess", goAccessRealtimeHandler)
}

// Serve o HTML em realtime e garante que o daemon do GoAccess está a correr
func goAccessRealtimeHandler(w http.ResponseWriter, r *http.Request) {
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

	// Colhemos os logs do NPM (proxy-host-*_access.log)
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

	// Diretório dos relatórios
	statsDir := filepath.Join(workDir, "npm-data", "stats")
	if err := os.MkdirAll(statsDir, 0o755); err != nil {
		http.Error(w, "failed to prepare stats directory", http.StatusInternalServerError)
		return
	}
	outputPath := filepath.Join(statsDir, "goaccess.html")

	// Deriva base e ws-url públicos
	baseURL, wsURL := derivePublicURLs(r)

	// Prepara GeoIP
	geoIPDBPath, err := ensureGeoIPDatabase(ctx, workDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Formato do NPM (custom). Se preferires COMBINED, troca por "--log-format=COMBINED"
	logFormat := `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	// Args do GoAccess realtime
	args := []string{
		"--no-global-config",
		"--real-time-html",
		"--restore",
		"--persist",
		"--addr=127.0.0.1",
		"--port=7891",
		"--origin=" + baseURL, // segurança de origem
		"--ws-url=" + wsURL,   // URL público do WS (sem porta)
		"--date-format=%d/%b/%Y",
		"--time-format=%T",
		"--log-format=" + logFormat,
		"-o", outputPath,
		"--geoip-database=" + geoIPDBPath,
	}

	// painéis (enable/disable)
	addPanelArgs := func(flag string, values []string) {
		for _, p := range values {
			p = strings.TrimSpace(p)
			if p != "" {
				args = append(args, fmt.Sprintf("--%s=%s", flag, p))
			}
		}
	}
	addPanelArgs("enable-panel", env512.GoAccessEnablePanels)
	addPanelArgs("disable-panel", env512.GoAccessDisablePanels)

	// logs (-f por cada ficheiro)
	for _, f := range files {
		args = append(args, "-f", f)
	}

	// Arranca (ou mantém) o daemon do GoAccess realtime
	if err := ensureGoAccessDaemon(logDir, outputPath, args); err != nil {
		http.Error(w, "failed to start goaccess realtime: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Pequena espera inicial para o HTML aparecer na 1ª execução
	deadline := time.Now().Add(3 * time.Second)
	for {
		if st, err := os.Stat(outputPath); err == nil && st.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(120 * time.Millisecond)
	}

	// Serve o HTML (o JS lá dentro liga ao WS público)
	data, err := os.ReadFile(outputPath)
	if err != nil {
		http.Error(w, "failed to read generated report", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// ---------- GoAccess realtime daemon management ----------

func ensureGoAccessDaemon(logDir, outputPath string, args []string) error {
	goRTMu.Lock()
	defer goRTMu.Unlock()

	hash := strings.Join(args, "\x00") + "|dir=" + logDir
	if goRTCmd != nil && goRTArgsHash == hash && goRTReport == outputPath && goRTLogDir == logDir {
		// Já está a correr com os mesmos args
		return nil
	}

	// Se existir um processo antigo, tenta terminar
	if goRTCmd != nil && goRTCmd.Process != nil {
		_ = goRTCmd.Process.Kill()
		_, _ = goRTCmd.Process.Wait()
	}
	goRTCmd = nil

	cmd := exec.Command("goaccess", args...)
	cmd.Dir = logDir
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("start goaccess: %s", msg)
	}

	// Guarda estado
	goRTCmd = cmd
	goRTReport = outputPath
	goRTLogDir = logDir
	goRTArgsHash = hash

	// Observa término em background
	go func(c *exec.Cmd) {
		_ = c.Wait()
	}(cmd)

	return nil
}

// ---------- Helpers: URLs públicos ----------

func derivePublicURLs(r *http.Request) (origin string, wsURL string) {
	// 1) Se MAIN_LINK estiver definido, usa-o
	base := strings.TrimSpace(env512.MAIN_LINK)

	var baseU *url.URL
	if base != "" {
		if u, err := url.Parse(base); err == nil && u.Scheme != "" && u.Host != "" {
			u.Path = "" // base pura
			baseU = u
		}
	}
	// 2) Senão, deriva do pedido/NPM
	if baseU == nil {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if xf := r.Header.Get("X-Forwarded-Proto"); xf != "" {
			// típico em NPM
			scheme = strings.ToLower(strings.TrimSpace(strings.Split(xf, ",")[0]))
		}
		baseU = &url.URL{Scheme: scheme, Host: r.Host}
	}

	// Path público do WS
	publicWSPath := "/ws-goaccess"

	// origin = https://dominio
	origin = (&url.URL{Scheme: baseU.Scheme, Host: baseU.Host}).String()

	// wsURL = wss://dominio/ws-goaccess  (ou ws:// em http)
	wsScheme := "ws"
	if baseU.Scheme == "https" || baseU.Scheme == "wss" {
		wsScheme = "wss"
	}
	wsURL = (&url.URL{Scheme: wsScheme, Host: baseU.Host, Path: publicWSPath}).String()

	return origin, wsURL
}

// ---------- GeoIP (igual ao teu, com pequenos ajustes) ----------

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
