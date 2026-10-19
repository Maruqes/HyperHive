package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListProxiesContract(t *testing.T) {
	for _, suffix := range []string{"", "/", "/api", "/api/", "/hyperhive/api"} {
		t.Run(suffix, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wantPath := strings.TrimRight(suffix, "/") + "/proxy/list"
				if r.Method != "GET" || r.URL.Path != wantPath || r.URL.RawQuery != "" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Accept") != "application/json" {
					t.Error("missing authentication or Accept header")
				}
				if r.ContentLength != 0 {
					t.Error("list must not send a body")
				}
				fmt.Fprint(w, `[{"id":7,"domain_names":["example.test"],"enabled":false,"forward_port":443,"access_list_id":"0","locations":[{"path":"/app"}],"meta":{"managed_by":"setup_frontend"},"future_field":true}]`)
			}))
			defer server.Close()
			api, err := New(Config{BaseURL: server.URL + suffix, Token: "Bearer test-token"})
			if err != nil {
				t.Fatal(err)
			}
			proxies, err := api.ListProxies(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(proxies) != 1 || proxies[0]["id"] != json.Number("7") || proxies[0]["enabled"] != false || proxies[0]["future_field"] != true || proxies[0]["access_list_id"] != "0" {
				t.Fatalf("proxy fields not preserved: %#v", proxies)
			}
			if proxies[0]["meta"].(map[string]any)["managed_by"] != "setup_frontend" || len(proxies[0]["locations"].([]any)) != 1 {
				t.Fatal("nested fields not preserved")
			}
		})
	}
}

func TestListProxiesResponses(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantError string
	}{
		{"empty list", 200, "[]", ""},
		{"nil list", 200, "null", ""},
		{"empty body", 200, "", "empty response"},
		{"no content", 204, "", "empty response"},
		{"invalid JSON", 200, "not JSON", "invalid JSON"},
		{"wrong shape", 200, `{ "proxies": [] }`, "unexpected response shape"},
		{"invalid element", 200, `[null]`, "invalid proxy object"},
		{"trailing JSON", 200, `[] {}`, "invalid JSON"},
		{"plain auth error", 401, "secret-token unauthorized", "HTTP 401"},
		{"JSON backend error", 500, `{"error":"secret-token"}`, "HTTP 500"},
		{"forbidden", 403, "secret-token", "HTTP 403"},
		{"too large", 200, strings.Repeat(" ", maxResponseBytes+1), "16 MiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			api, err := New(Config{BaseURL: server.URL, Token: "secret-token"})
			if err != nil {
				t.Fatal(err)
			}
			got, err := api.ListProxies(context.Background())
			if tc.wantError == "" {
				if err != nil || got == nil || len(got) != 0 {
					t.Fatalf("want non-nil empty list, got %#v, %v", got, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) || strings.Contains(err.Error(), "secret-token") {
				t.Fatalf("want safe error containing %q, got %v", tc.wantError, err)
			}
		})
	}
}

func TestRedirectDoesNotForwardToken(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer server.Close()
	api, _ := New(Config{BaseURL: server.URL, Token: "test-token"})
	_, err := api.ListProxies(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 302") || called {
		t.Fatalf("redirect followed or wrong error: %v, target called %v", err, called)
	}
}

func TestCancellationAndTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	api, _ := New(Config{BaseURL: server.URL, Token: "test-token"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := api.ListProxies(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation, got %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := api.ListProxies(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline, got %v", err)
	}
	api.http.Timeout = 30 * time.Millisecond
	if _, err := api.ListProxies(context.Background()); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want HTTP client timeout, got %v", err)
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, cfg := range []Config{
		{Token: "token"}, {BaseURL: "http://localhost"},
		{BaseURL: "/relative", Token: "token"},
		{BaseURL: "ftp://localhost", Token: "token"},
		{BaseURL: "http://user:secret@localhost", Token: "token"},
		{BaseURL: "http://localhost?token=secret", Token: "token"},
		{BaseURL: "http://localhost#fragment", Token: "token"},
		{BaseURL: "http://localhost", Token: "Bearer "},
		{BaseURL: "http://localhost", Token: "token\r\nInjected: yes"},
	} {
		if _, err := New(cfg); err == nil {
			t.Error("invalid configuration accepted")
		}
	}
}
