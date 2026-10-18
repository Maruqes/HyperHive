package npmapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"512SvMan/npm"
)

func TestFrontendProxyTargetsConfiguredAPIPort(t *testing.T) {
	oldURL, oldPort := baseURL, apiPort
	t.Cleanup(func() { baseURL, apiPort = oldURL, oldPort })
	SetAPIPort(10595)
	for _, tc := range []struct {
		name, body  string
		handler     http.HandlerFunc
		wantCreates int32
	}{
		{"standard", `{"domain":"frontend.test"}`, setupFrontEnd, 1},
		{"secure", `{"publicDomain":"public.test","privateDomain":"private.test"}`, setupFrontEndSecure, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var creates atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					fmt.Fprint(w, "[]")
					return
				}
				var proxy npm.Proxy
				if err := json.NewDecoder(r.Body).Decode(&proxy); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if len(proxy.Locations) == 0 {
					t.Error("missing frontend routes")
				}
				for _, loc := range proxy.Locations {
					if loc.ForwardPort != 10595 {
						t.Errorf("%s still targets port %d", loc.Path, loc.ForwardPort)
					}
				}
				creates.Add(1)
				fmt.Fprint(w, `{"id":1}`)
			}))
			defer backend.Close()
			SetBaseURL(backend.URL)
			w := httptest.NewRecorder()
			tc.handler(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body)))
			if w.Code != http.StatusOK || creates.Load() != tc.wantCreates {
				t.Fatalf("frontend setup failed: %d %s, creates=%d", w.Code, w.Body, creates.Load())
			}
		})
	}
}
