package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSupportTicketChecksFrontendOrigin(test *testing.T) {
	for _, testcase := range []struct {
		name, allowed, origin string
		status                int
	}{
		{"default accepts development frontend", "", "http://localhost:8081", http.StatusOK},
		{"wildcard accepts any origin", "*", "https://other.test", http.StatusOK},
		{"wildcard accepts opaque origin", "*", "null", http.StatusOK},
		{"explicit allowlist rejects other frontend", "https://ui.test", "http://localhost:8081", http.StatusForbidden},
		{"allowed development frontend", "http://localhost:8081", "http://localhost:8081", http.StatusOK},
		{"same host", "", "http://master.test:9595", http.StatusOK},
		{"non-browser client", "", "", http.StatusOK},
	} {
		test.Run(testcase.name, func(test *testing.T) {
			service := newSupportProxy("http://127.0.0.1:8091", strings.Repeat("s", 32), testcase.allowed)
			request := httptest.NewRequest(http.MethodPost, "http://master.test:9595/support/ws-ticket", nil)
			request.Header.Set("Origin", testcase.origin)
			response := httptest.NewRecorder()
			service.issueTicket(response, request)
			if response.Code != testcase.status {
				test.Fatalf("unexpected ticket response: %d", response.Code)
			}
			if testcase.status == http.StatusForbidden && (len(service.tickets) != 0 || !strings.Contains(response.Body.String(), "SUPPORT_ALLOWED_ORIGINS")) {
				test.Fatal("blocked frontend should get setup instructions without a ticket")
			}
		})
	}
}

func TestSupportTicketConcurrentConsumption(t *testing.T) {
	var forwarded atomic.Int32
	service := &supportProxy{
		tickets: map[string]time.Time{"single-use": time.Now().Add(time.Minute)},
		handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			forwarded.Add(1)
			writer.WriteHeader(http.StatusNoContent)
		}),
	}
	var workers sync.WaitGroup
	for index := 0; index < 20; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			request := httptest.NewRequest(http.MethodGet, "/support/ws?ticket=single-use", nil)
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			response := httptest.NewRecorder()
			service.websocket(response, request)
			if response.Code != http.StatusUnauthorized && response.Code != http.StatusNoContent {
				t.Errorf("unexpected concurrent consumption status: %d", response.Code)
			}
		}()
	}
	workers.Wait()
	if forwarded.Load() != 1 {
		t.Fatalf("single-use ticket forwarded %d requests", forwarded.Load())
	}
}

func TestSupportTicketCapacityAndExpiredCleanup(t *testing.T) {
	service := newSupportProxy("http://127.0.0.1:8091", strings.Repeat("s", 32), "")
	for index := 0; index < 1024; index++ {
		service.tickets[fmt.Sprint(index)] = time.Now().Add(time.Minute)
	}
	response := httptest.NewRecorder()
	service.issueTicket(response, httptest.NewRequest(http.MethodPost, "/support/ws-ticket", nil))
	if response.Code != http.StatusServiceUnavailable || len(service.tickets) != 1024 {
		t.Fatal("pending ticket capacity was not enforced")
	}
	service.tickets["0"] = time.Now().Add(-time.Second)
	response = httptest.NewRecorder()
	service.issueTicket(response, httptest.NewRequest(http.MethodPost, "/support/ws-ticket", nil))
	if response.Code != http.StatusOK || len(service.tickets) != 1024 {
		t.Fatal("expired ticket did not free capacity")
	}
	if _, exists := service.tickets["0"]; exists {
		t.Fatal("expired ticket was retained")
	}
}

func TestSupportRejectsMalformedOriginsBeforeConsumingTicket(t *testing.T) {
	service := newSupportProxy("http://127.0.0.1:8091", strings.Repeat("s", 32), "https://ui.test")
	for _, origin := range []string{"null", "https://example.com/path", "https://user@example.com", "https://example.com?query=yes", "https://example.com#fragment", "ftp://example.com"} {
		service.tickets["valid"] = time.Now().Add(time.Minute)
		request := httptest.NewRequest(http.MethodGet, "http://example.com/support/ws?ticket=valid", nil)
		request.Header.Set("Origin", origin)
		request.Header.Set("Connection", "Upgrade")
		request.Header.Set("Upgrade", "websocket")
		response := httptest.NewRecorder()
		service.websocket(response, request)
		if response.Code != http.StatusForbidden {
			t.Errorf("origin %q returned %d", origin, response.Code)
		}
		if _, exists := service.tickets["valid"]; !exists {
			t.Errorf("origin %q consumed valid ticket", origin)
		}
	}
}
