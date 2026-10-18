package hyperhivemcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPTokenBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, token, header, query, cookie string
		duplicate                          bool
		want                               int
	}{
		{name: "valid", token: "fixed-key", header: "Bearer fixed-key", want: 204},
		{name: "case insensitive scheme", token: "fixed-key", header: "bearer fixed-key", want: 204},
		{name: "missing header", token: "fixed-key", want: 401},
		{name: "wrong token", token: "fixed-key", header: "Bearer wrong-key", want: 401},
		{name: "raw key", token: "fixed-key", header: "fixed-key", want: 401},
		{name: "query rejected", token: "fixed-key", query: "?token=fixed-key", want: 401},
		{name: "cookie rejected", token: "fixed-key", cookie: "fixed-key", want: 401},
		{name: "duplicate rejected", token: "fixed-key", header: "Bearer fixed-key", duplicate: true, want: 401},
		{name: "missing configuration", want: 503},
		{name: "invalid configuration", token: "key\nvalue", want: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp"+tc.query, nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			if tc.duplicate {
				req.Header.Add("Authorization", tc.header)
			}
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "Authorization", Value: tc.cookie})
			}
			w := httptest.NewRecorder()
			requireMCPToken(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }), tc.token).ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("want %d, got %d", tc.want, w.Code)
			}
			if w.Code == 401 && w.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("missing Bearer challenge")
			}
		})
	}
}
