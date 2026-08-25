package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestGetClientIPIgnoresForwardedHeadersFromClients(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/guest_api/guest_vm", nil)
	r.RemoteAddr = "198.51.100.8:12345"
	r.Header.Set("X-Real-IP", "203.0.113.1")
	r.Header.Set("X-Forwarded-For", "203.0.113.2")

	if got := getClientIP(r); got != "198.51.100.8" {
		t.Fatalf("getClientIP() = %q, want remote address", got)
	}
}

func TestGetClientIPAcceptsProxyHeaderOnlyFromLoopback(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/guest_api/guest_vm", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("X-Real-IP", "198.51.100.8")

	if got := getClientIP(r); got != "198.51.100.8" {
		t.Fatalf("getClientIP() = %q, want proxy-provided client address", got)
	}
}

func TestGetClientIPAcceptsRemoteAddressWithoutPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/guest_api/guest_vm", nil)
	r.RemoteAddr = "127.0.0.1"
	r.Header.Set("X-Real-IP", "198.51.100.8")

	if got := getClientIP(r); got != "198.51.100.8" {
		t.Fatalf("getClientIP() = %q, want proxy-provided client address", got)
	}
}

func TestGuestPageEscapesVMName(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/guest_page/{vm_name}", serveGuestPage)

	request := httptest.NewRequest(http.MethodGet, "/guest_page/%3Cscript%3Ealert(1)%3C%2Fscript%3E", nil)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(response.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("guest page reflected an executable VM name")
	}
}

func TestSPAPageRejectsInvalidPort(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/pageallow/{port}", serveSPAPageAllow)

	request := httptest.NewRequest(http.MethodGet, "/pageallow/%3Cscript%3Ealert(1)%3C%2Fscript%3E", nil)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
