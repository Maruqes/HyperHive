package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

func TestSupportProxyRoutesAndCredentials(t *testing.T) {
	secret := strings.Repeat("s", 32)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-HH-Support-Key") != secret || r.Header.Get("Cookie") != "" {
			t.Error("proxy trust or cookie boundary failed")
		}
		if r.URL.Path == "/ingest" && r.Method == http.MethodPost {
			if r.Header.Get("Authorization") != "Bearer app-secret" || r.Header.Get("X-App-ID") != "app-id" {
				t.Error("ingestion credentials were lost")
			}
		} else if r.Header.Get("Authorization") != "" || r.Header.Get("X-App-ID") != "" || r.URL.Query().Get("token") != "" {
			t.Error("HH credential leaked upstream")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"path": r.URL.Path, "query": r.URL.RawQuery})
	}))
	defer upstream.Close()
	service := newSupportProxy(upstream.URL, secret, "")
	for _, path := range []string{"/support/apps?limit=3&token=hh-token", "/support/ingest", "/support/apps/a/samples?stream=cpu&before=42"} {
		method := http.MethodGet
		if path == "/support/ingest" {
			method = http.MethodPost
		}
		request := httptest.NewRequest(method, path, nil)
		request.Header.Set("Authorization", "Bearer app-secret")
		request.Header.Set("X-App-ID", "app-id")
		request.Header.Set("X-HH-Support-Key", "spoofed")
		request.Header.Set("Cookie", "Authorization=hh-token")
		response := httptest.NewRecorder()
		service.handler.ServeHTTP(response, request)
		if response.Code != 200 || strings.Contains(response.Body.String(), "/support") {
			t.Fatalf("proxy path: %d %s", response.Code, response.Body.String())
		}
	}
	for _, address := range []string{"", "file:///tmp/service", upstream.URL + "/prefix", "http://user:pass@localhost"} {
		response := httptest.NewRecorder()
		newSupportProxy(address, secret, "").handler.ServeHTTP(response, httptest.NewRequest("GET", "/support/apps", nil))
		if response.Code != 503 {
			t.Fatalf("invalid target %q: %d", address, response.Code)
		}
	}
}

func TestSupportAPIBoundary(t *testing.T) {
	t.Setenv("SUPPORT_PROXY_SECRET", strings.Repeat("s", 32))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer upstream.Close()
	t.Setenv("SUPPORT_URL", upstream.URL)
	router := chi.NewRouter()
	router.Use(stripAPIPrefix)
	setupSupportAPI(router)
	router.Get("/existing", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"POST", "/api/support/ingest", 204},
		{"GET", "/api/support/apps", 401},
		{"GET", "/api/support/ingest", 401},
		{"POST", "/api/support/ingest/", 401},
		{"POST", "/api/support/ws-ticket", 401},
		{"GET", "/api/existing", 202},
		{"GET", "/apiary", 404},
		{"GET", "/api/supportive", 404},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.status {
			t.Fatalf("%s %s: got %d", test.method, test.path, response.Code)
		}
	}
}

func TestSupportTicketUpgradeAndReplay(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" {
			t.Error("socket path or credentials not sanitized")
		}
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		_ = connection.WriteJSON(map[string]string{"type": "ready"})
	}))
	defer upstream.Close()
	service := newSupportProxy(upstream.URL, strings.Repeat("s", 32), "https://ui.test")
	server := httptest.NewServer(http.HandlerFunc(service.websocket))
	defer server.Close()
	issue := func() string {
		response := httptest.NewRecorder()
		service.issueTicket(response, httptest.NewRequest("POST", "/support/ws-ticket", nil))
		var body struct {
			Ticket string `json:"ticket"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Ticket == "" {
			t.Fatal("missing ticket")
		}
		return body.Ticket
	}
	ticket := issue()
	address := "ws" + strings.TrimPrefix(server.URL, "http") + "/support/ws?ticket=" + ticket + "&token=do-not-forward"
	connection, response, err := websocket.DefaultDialer.Dial(address, http.Header{"Origin": []string{"https://ui.test"}})
	if err != nil {
		t.Fatalf("upgrade: %v %#v", err, response)
	}
	var frame map[string]string
	if err := connection.ReadJSON(&frame); err != nil || frame["type"] != "ready" {
		t.Fatalf("ready frame: %#v %v", frame, err)
	}
	connection.Close()
	_, response, err = websocket.DefaultDialer.Dial(address, nil)
	if err == nil || response.StatusCode != 401 {
		t.Fatal("ticket replay accepted")
	}
	response.Body.Close()
	expired := issue()
	service.tickets[expired] = time.Now().Add(-time.Second)
	for _, test := range []struct {
		ticket, origin string
		status         int
	}{
		{expired, "https://ui.test", 401},
		{issue(), "https://evil.test", 403},
	} {
		_, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/support/ws?ticket="+test.ticket, http.Header{"Origin": []string{test.origin}})
		if err == nil || response.StatusCode != test.status {
			t.Fatalf("invalid ticket/origin accepted: %v %#v", err, response)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}
}

func TestSupportTicketLogRedaction(t *testing.T) {
	handler := supportSafeRequestLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.RequestURI, "ticket") || r.URL.Query().Get("ticket") != "private" {
			t.Error("ticket must remain available only to the handler")
		}
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/support/ws?ticket=private", nil))
}
