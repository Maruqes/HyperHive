package npmapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyCustomConfigurationEdit(t *testing.T) {
	const snippet = "# café\nproxy_set_header X-Example \"$host\";\n"
	for _, tc := range []struct {
		name, body                   string
		managed, failRead, failWrite bool
		status                       int
		want                         string
	}{
		{name: "preserve omitted", body: `{"id":42,"domain_names":["renamed.test"]}`, status: 200, want: snippet},
		{name: "replace", body: `{"id":42,"domain_names":["renamed.test"],"advanced_config":"proxy_buffering off;\n"}`, status: 200, want: "proxy_buffering off;\n"},
		{name: "explicit empty", body: `{"id":42,"domain_names":["renamed.test"],"advanced_config":""}`, status: 200},
		{name: "null", body: `{"id":42,"domain_names":["renamed.test"],"advanced_config":null}`, status: 400},
		{name: "wrong type", body: `{"id":42,"domain_names":["renamed.test"],"advanced_config":12}`, status: 400},
		{name: "no domains", body: `{"id":42}`, status: 400},
		{name: "empty domain", body: `{"id":42,"domain_names":[" "]}`, status: 400},
		{name: "no id", body: `{"domain_names":["renamed.test"]}`, status: 400},
		{name: "unknown id", body: `{"id":99,"domain_names":["renamed.test"]}`, status: 404},
		{name: "managed renamed target", body: `{"id":42,"domain_names":["renamed.test"]}`, managed: true, status: 403},
		{name: "read failure", body: `{"id":42,"domain_names":["renamed.test"]}`, failRead: true, status: 500},
		{name: "write failure", body: `{"id":42,"domain_names":["renamed.test"]}`, failWrite: true, status: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var writes int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/nginx/proxy-hosts" && r.URL.Path != "/api/nginx/proxy-hosts/42" {
					t.Errorf("path: %s", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer fixture-session" {
					t.Error("missing downstream auth")
				}
				if r.Method == "GET" {
					if tc.failRead {
						w.WriteHeader(502)
						return
					}
					meta := map[string]any{}
					if tc.managed {
						meta[managedByKey] = managedByValue
					}
					json.NewEncoder(w).Encode([]map[string]any{{"id": 42, "domain_names": []string{"old.test"}, "advanced_config": snippet, "meta": meta}})
					return
				}
				writes++
				if r.Method != "PUT" {
					t.Errorf("method: %s", r.Method)
				}
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				if tc.status == 200 && got["advanced_config"] != tc.want {
					t.Errorf("snippet: %#v, want %q", got["advanced_config"], tc.want)
				}
				if tc.failWrite {
					w.WriteHeader(400)
					return
				}
				w.Write([]byte(`{"id":42}`))
			}))
			defer server.Close()
			oldURL := baseURL
			baseURL = server.URL
			defer func() { baseURL = oldURL }()
			r := httptest.NewRequest("PUT", "/proxy/edit", strings.NewReader(tc.body))
			r = r.WithContext(context.WithValue(r.Context(), "token", "fixture-session"))
			w := httptest.NewRecorder()
			editProxy(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			wantWrites := 0
			if tc.status == 200 || tc.failWrite {
				wantWrites = 1
			}
			if writes != wantWrites {
				t.Fatalf("writes %d, want %d", writes, wantWrites)
			}
		})
	}
}

func TestProxyCustomConfigurationList(t *testing.T) {
	const payload = `[{"id":42,"advanced_config":"proxy_set_header X-Host $host;\n","locations":[{"path":"/app","advanced_config":"proxy_buffering off;\n"},{"path":"/empty","advanced_config":""},{"path":"/legacy"}]}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(payload)) }))
	defer server.Close()
	oldURL := baseURL
	baseURL = server.URL
	defer func() { baseURL = oldURL }()
	w := httptest.NewRecorder()
	listProxies(w, httptest.NewRequest("GET", "/proxy/list", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var got []struct {
		AdvancedConfig string           `json:"advanced_config"`
		Locations      []map[string]any `json:"locations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].AdvancedConfig != "proxy_set_header X-Host $host;\n" || got[0].Locations[0]["advanced_config"] != "proxy_buffering off;\n" || got[0].Locations[1]["advanced_config"] != "" {
		t.Fatalf("lost snippets: %s", w.Body.String())
	}
	if _, ok := got[0].Locations[2]["advanced_config"]; ok {
		t.Fatal("omitted location config was invented")
	}
}
