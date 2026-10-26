package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

func TestSupportServiceThroughMaster(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and starts the separate support service")
	}
	directory := t.TempDir()
	binary := filepath.Join(directory, "support")
	build := exec.Command("go", "build", "-o", binary, "./cmd/hh-support")
	build.Dir = "../../support"
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build support: %v: %s", err, output)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	secret := strings.Repeat("i", 32)
	start := func() func() {
		command := exec.Command(binary)
		command.Env = append(os.Environ(), "SUPPORT_LISTEN="+address, "SUPPORT_DB_PATH="+filepath.Join(directory, "support.db"), "SUPPORT_PROXY_SECRET="+secret)
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		stopped := false
		stop := func() {
			if !stopped {
				stopped = true
				_ = command.Process.Kill()
				_ = command.Wait()
			}
		}
		t.Cleanup(stop)
		client := &http.Client{Timeout: time.Second}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			response, err := client.Get("http://" + address + "/healthz")
			if err == nil {
				response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return stop
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("support service did not become healthy")
		return stop
	}
	stop := start()
	auth := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer hh-session" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer auth.Close()
	previousURL := baseURL
	baseURL = auth.URL
	t.Cleanup(func() { baseURL = previousURL })
	t.Setenv("SUPPORT_URL", "http://"+address)
	t.Setenv("SUPPORT_PROXY_SECRET", secret)
	router := chi.NewRouter()
	router.Use(stripAPIPrefix)
	setupSupportAPI(router)
	master := httptest.NewServer(router)
	defer master.Close()
	appID, appSecret := "", ""
	request := func(method, path string, body any, status int, target any) {
		t.Helper()
		var payload bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&payload).Encode(body); err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequest(method, master.URL+"/api/support"+path, &payload)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer hh-session")
		req.Header.Set("X-HH-Support-Key", "spoofed")
		if path == "/ingest" {
			req.Header.Set("Authorization", "Bearer "+appSecret)
			req.Header.Set("X-App-ID", appID)
		}
		response, err := master.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("%s %s: status %d, expected %d", method, path, response.StatusCode, status)
		}
		if target != nil {
			if err := json.NewDecoder(response.Body).Decode(target); err != nil {
				t.Fatal(err)
			}
		}
	}
	var tabs []struct {
		ID string `json:"id"`
	}
	request("GET", "/tabs", nil, 200, &tabs)
	if len(tabs) != 1 {
		t.Fatal("default tab missing")
	}
	var created struct {
		App struct {
			ID string `json:"id"`
		} `json:"app"`
		Secret string `json:"secret"`
	}
	request("POST", "/apps", map[string]any{"name": "Smoke", "tab_id": tabs[0].ID}, 201, &created)
	appID, appSecret = created.App.ID, created.Secret
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	request("POST", "/ws-ticket", nil, 200, &ticket)
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(master.URL, "http")+"/api/support/ws?ticket="+ticket.Ticket, nil)
	if err != nil {
		t.Fatal("support socket upgrade failed")
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	var frame struct {
		Type string `json:"type"`
	}
	if err := connection.ReadJSON(&frame); err != nil || frame.Type != "ready" {
		t.Fatal("support socket ready frame missing")
	}
	streams := []map[string]string{}
	samples := []map[string]any{}
	values := map[string]any{"number": 12.5, "text": "ok", "boolean": true, "log": map[string]string{"message": "ready", "severity": "info"}, "event": map[string]any{"name": "deploy", "fields": map[string]any{"version": 1}}}
	for kind, value := range values {
		streams = append(streams, map[string]string{"name": kind, "type": kind})
		samples = append(samples, map[string]any{"stream": kind, "value": value})
	}
	request("POST", "/ingest", map[string]any{"heartbeat": true, "streams": streams, "samples": samples}, 200, nil)
	seen := map[string]bool{}
	for !seen["changed"] || !seen["samples"] || !seen["state"] {
		if err := connection.ReadJSON(&frame); err != nil {
			t.Fatal("missing ingestion live frames")
		}
		seen[frame.Type] = true
	}
	var history struct {
		Samples []json.RawMessage `json:"samples"`
	}
	for kind := range values {
		request("GET", "/apps/"+appID+"/samples?stream="+kind, nil, 200, &history)
		if len(history.Samples) != 1 {
			t.Fatalf("missing %s history", kind)
		}
	}
	connection.Close()
	stop()
	start()
	request("GET", "/apps/"+appID+"/samples?stream=number", nil, 200, &history)
	if len(history.Samples) != 1 {
		t.Fatal("history did not survive restart")
	}
	var rotated struct {
		Secret string `json:"secret"`
	}
	request("POST", "/apps/"+appID+"/rotate-secret", nil, 200, &rotated)
	request("POST", "/ingest", map[string]any{"heartbeat": true}, 401, nil)
	appSecret = rotated.Secret
	request("POST", "/ingest", map[string]any{"heartbeat": true}, 200, nil)
	response, err := http.Get("http://" + address + "/apps")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("direct service management accepted an untrusted request")
	}
	request("DELETE", "/apps/"+appID, nil, 204, nil)
	request("GET", "/apps/"+appID+"/samples?stream=number", nil, 404, nil)
}
