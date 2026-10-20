package npmapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"512SvMan/db"
	"512SvMan/npm"
	hyperhivemcp "github.com/Maruqes/HyperHive/mcp"
	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type nginxHandlerCase struct {
	Name, Method, Path string
	Args               map[string]any
	Body               any
}

// Actual API handlers and NPM HTTP helpers; disposable NPM responses and SQLite
// descriptions. No real NPM service, proxy, certificate issuance or renewal runs.
func TestMCPNginxThroughHandlers(t *testing.T) {
	oldURL, oldDB := baseURL, db.DB
	t.Cleanup(func() { baseURL = oldURL; db.DB = oldDB })
	var err error
	db.DB, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	db.DB.SetMaxOpenConns(1)
	if err = db.CreateNPMUpstreamTables(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = npm.InitializeUpstreams(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateResourceDescriptionsTable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = db.SetResourceDescription(context.Background(), streamResourceType, 42, "stored description"); err != nil {
		t.Fatal(err)
	}
	var cases []nginxHandlerCase
	if err = json.Unmarshal([]byte(`[
  {
    "name": "list_certificates",
    "args": {},
    "method": "GET",
    "path": "/api/nginx/certificates",
    "body": null
  },
  {
    "name": "list_dns_providers",
    "args": {},
    "method": "GET",
    "path": "/api/nginx/certificates/dns-providers",
    "body": null
  },
  {
    "name": "download_certificate",
    "args": {
      "id": 42
    },
    "method": "GET",
    "path": "/api/nginx/certificates/42/download",
    "body": null
  },
  {
    "name": "renew_certificate",
    "args": {
      "id": 42
    },
    "method": "POST",
    "path": "/api/nginx/certificates/42/renew",
    "body": null
  },
  {
    "name": "list_access_lists",
    "args": {},
    "method": "GET",
    "path": "/api/nginx/access-lists",
    "body": null
  },
  {
    "name": "create_access_list",
    "args": {
      "name": "office",
      "satisfy_any": false,
      "pass_auth": false,
      "items": [
        {
          "username": "fixture-user",
          "password": "fixture-password"
        }
      ],
      "clients": [
        {
          "directive": "allow",
          "address": "10.0.0.0/8"
        },
        {
          "directive": "deny",
          "address": "all"
        }
      ]
    },
    "method": "POST",
    "path": "/api/nginx/access-lists",
    "body": {
      "name": "office",
      "satisfy_any": false,
      "pass_auth": false,
      "items": [
        {
          "username": "fixture-user",
          "password": "fixture-password"
        }
      ],
      "clients": [
        {
          "directive": "allow",
          "address": "10.0.0.0/8"
        },
        {
          "directive": "deny",
          "address": "all"
        }
      ]
    }
  },
  {
    "name": "list_streams",
    "args": {},
    "method": "GET",
    "path": "/api/nginx/streams",
    "body": null
  },
  {
    "name": "create_stream",
    "args": {
      "incoming_port": 6060,
      "forwarding_host": "upstream.test",
      "forwarding_port": 60,
      "tcp_forwarding": true,
      "udp_forwarding": false,
      "certificate_id": 0,
      "meta": {
        "dns_provider_credentials": "",
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "description": ""
    },
    "method": "POST",
    "path": "/api/nginx/streams",
    "body": {
      "incoming_port": 6060,
      "forwarding_host": "upstream.test",
      "forwarding_port": 60,
      "tcp_forwarding": true,
      "udp_forwarding": false,
      "certificate_id": 0,
      "meta": {
        "dns_provider_credentials": "",
        "letsencrypt_agree": false,
        "dns_challenge": false
      }
    }
  },
  {
    "name": "update_stream",
    "args": {
      "incoming_port": 6060,
      "forwarding_host": "upstream.test",
      "forwarding_port": 60,
      "tcp_forwarding": true,
      "udp_forwarding": false,
      "certificate_id": 0,
      "meta": {
        "dns_provider_credentials": "",
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "description": "",
      "id": 42
    },
    "method": "PUT",
    "path": "/api/nginx/streams/42",
    "body": {
      "incoming_port": 6060,
      "forwarding_host": "upstream.test",
      "forwarding_port": 60,
      "tcp_forwarding": true,
      "udp_forwarding": false,
      "certificate_id": 0,
      "meta": {
        "dns_provider_credentials": "",
        "letsencrypt_agree": false,
        "dns_challenge": false
      }
    }
  },
  {
    "name": "enable_stream",
    "args": {
      "id": 42
    },
    "method": "POST",
    "path": "/api/nginx/streams/42/enable",
    "body": null
  },
  {
    "name": "disable_stream",
    "args": {
      "id": 42
    },
    "method": "POST",
    "path": "/api/nginx/streams/42/disable",
    "body": null
  },
  {
    "name": "list_redirects",
    "args": {},
    "method": "GET",
    "path": "/api/nginx/redirection-hosts",
    "body": null
  },
  {
    "name": "create_redirect",
    "args": {
      "domain_names": [
        "redirect.test"
      ],
      "forward_scheme": "https",
      "forward_domain_name": "target.test",
      "forward_http_code": "307",
      "preserve_path": false,
      "block_exploits": false,
      "certificate_id": 0,
      "meta": {
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "advanced_config": "",
      "http2_support": false,
      "hsts_enabled": false,
      "hsts_subdomains": false,
      "ssl_forced": false
    },
    "method": "POST",
    "path": "/api/nginx/redirection-hosts",
    "body": {
      "domain_names": [
        "redirect.test"
      ],
      "forward_scheme": "https",
      "forward_domain_name": "target.test",
      "forward_http_code": "307",
      "preserve_path": false,
      "block_exploits": false,
      "certificate_id": 0,
      "meta": {
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "advanced_config": "",
      "http2_support": false,
      "hsts_enabled": false,
      "hsts_subdomains": false,
      "ssl_forced": false
    }
  },
  {
    "name": "update_redirect",
    "args": {
      "domain_names": [
        "redirect.test"
      ],
      "forward_scheme": "https",
      "forward_domain_name": "target.test",
      "forward_http_code": "307",
      "preserve_path": false,
      "block_exploits": false,
      "certificate_id": 0,
      "meta": {
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "advanced_config": "",
      "http2_support": false,
      "hsts_enabled": false,
      "hsts_subdomains": false,
      "ssl_forced": false,
      "id": 42
    },
    "method": "PUT",
    "path": "/api/nginx/redirection-hosts/42",
    "body": {
      "domain_names": [
        "redirect.test"
      ],
      "forward_scheme": "https",
      "forward_domain_name": "target.test",
      "forward_http_code": "307",
      "preserve_path": false,
      "block_exploits": false,
      "certificate_id": 0,
      "meta": {
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "advanced_config": "",
      "http2_support": false,
      "hsts_enabled": false,
      "hsts_subdomains": false,
      "ssl_forced": false
    }
  },
  {
    "name": "enable_redirect",
    "args": {
      "id": 42
    },
    "method": "POST",
    "path": "/api/nginx/redirection-hosts/42/enable",
    "body": null
  },
  {
    "name": "disable_redirect",
    "args": {
      "id": 42
    },
    "method": "POST",
    "path": "/api/nginx/redirection-hosts/42/disable",
    "body": null
  },
  {
    "name": "list_404_hosts",
    "args": {},
    "method": "GET",
    "path": "/api/nginx/dead-hosts",
    "body": null
  },
  {
    "name": "create_404_host",
    "args": {
      "domain_names": [
        "missing.test"
      ],
      "certificate_id": 0,
      "meta": {
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "advanced_config": "",
      "http2_support": false,
      "hsts_enabled": false,
      "hsts_subdomains": false,
      "ssl_forced": false
    },
    "method": "POST",
    "path": "/api/nginx/dead-hosts",
    "body": {
      "domain_names": [
        "missing.test"
      ],
      "certificate_id": 0,
      "meta": {
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "advanced_config": "",
      "http2_support": false,
      "hsts_enabled": false,
      "hsts_subdomains": false,
      "ssl_forced": false
    }
  },
  {
    "name": "update_404_host",
    "args": {
      "domain_names": [
        "missing.test"
      ],
      "certificate_id": 0,
      "meta": {
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "advanced_config": "",
      "http2_support": false,
      "hsts_enabled": false,
      "hsts_subdomains": false,
      "ssl_forced": false,
      "id": 42
    },
    "method": "PUT",
    "path": "/api/nginx/dead-hosts/42",
    "body": {
      "domain_names": [
        "missing.test"
      ],
      "certificate_id": 0,
      "meta": {
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "advanced_config": "",
      "http2_support": false,
      "hsts_enabled": false,
      "hsts_subdomains": false,
      "ssl_forced": false
    }
  },
  {
    "name": "enable_404_host",
    "args": {
      "id": 42
    },
    "method": "POST",
    "path": "/api/nginx/dead-hosts/42/enable",
    "body": null
  },
  {
    "name": "disable_404_host",
    "args": {
      "id": 42
    },
    "method": "POST",
    "path": "/api/nginx/dead-hosts/42/disable",
    "body": null
  }
]`), &cases); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var active nginxHandlerCase
	downstreamCalls := 0
	archive := []byte("PK\x00\xff\x01fixture-key")
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		downstreamCalls++
		if r.Method == "DELETE" {
			t.Error("forbidden deletion reached NPM")
			w.WriteHeader(500)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fixture-session" {
			t.Error("wrong NPM credential")
		}
		if r.Method != active.Method || r.URL.Path != active.Path {
			t.Errorf("%s downstream got %s %s, want %s %s", active.Name, r.Method, r.URL, active.Method, active.Path)
		}
		if active.Name == "list_access_lists" {
			if r.URL.Query().Get("expand") != "owner,items,clients" {
				t.Error("missing access list expansions")
			}
		} else if r.URL.RawQuery != "" {
			t.Error("unexpected query")
		}
		if active.Body != nil {
			var got any
			if json.NewDecoder(r.Body).Decode(&got) != nil || !reflect.DeepEqual(got, active.Body) {
				t.Errorf("%s downstream body got %#v want %#v", active.Name, got, active.Body)
			}
		} else {
			raw, _ := io.ReadAll(r.Body)
			if len(raw) != 0 {
				t.Errorf("unexpected downstream body: %q", raw)
			}
		}
		switch {
		case active.Name == "download_certificate":
			w.Header().Set("Content-Type", "application/zip")
			w.Write(archive)
		case active.Name == "renew_certificate":
			w.WriteHeader(202)
		case active.Name == "list_dns_providers":
			fmt.Fprint(w, `[{"id":"fixture-dns","name":"Fixture DNS","credentials":"token = <TOKEN>","future_field":true}]`)
		case strings.HasPrefix(active.Name, "list_"):
			fmt.Fprint(w, `[{"id":42,"name":"fixture","domain_names":["fixture.test"],"forward_scheme":"https","forward_domain_name":"target.test","forward_http_code":302,"incoming_port":6060,"forwarding_port":60,"forwarding_host":"upstream.test","tcp_forwarding":true,"udp_forwarding":false,"enabled":false,"certificate_id":0,"meta":{"letsencrypt_agree":false,"dns_challenge":false},"items":[],"clients":[]}]`)
		case strings.HasPrefix(active.Name, "create_"):
			fmt.Fprint(w, `{"id":42}`)
		default:
			w.WriteHeader(200)
		}
	}))
	defer downstream.Close()
	baseURL = downstream.URL
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer fixture-key" {
				w.WriteHeader(401)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), "token", "fixture-session")))
		})
	})
	SetupCertAPI(router)
	SetupAccessListAPI(router)
	SetupStreamAPI(router)
	SetupRedirectionAPI(router)
	Setup404API(router)
	api := httptest.NewServer(router)
	defer api.Close()
	endpoint := httptest.NewServer(hyperhivemcp.NewHTTPHandler(hyperhivemcp.Config{BaseURL: api.URL, MCPToken: "fixture-key"}))
	defer endpoint.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "nginx-handler-contract"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: proxyFixtureBearer{}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			mu.Lock()
			active = tc
			before := downstreamCalls
			mu.Unlock()
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.Name, Arguments: tc.Args})
			if err != nil || result.IsError {
				t.Fatalf("%s result=%#v error=%v", tc.Name, result, err)
			}
			mu.Lock()
			after := downstreamCalls
			mu.Unlock()
			if after != before+1 {
				t.Fatalf("downstream request count %d -> %d", before, after)
			}
			data, _ := json.Marshal(result.StructuredContent)
			if strings.HasPrefix(tc.Name, "list_") {
				var output struct {
					Items []map[string]any `json:"items"`
				}
				if json.Unmarshal(data, &output) != nil || len(output.Items) != 1 {
					t.Fatalf("list: %s", data)
				}
				if tc.Name == "list_streams" && output.Items[0]["description"] != "stored description" {
					t.Fatalf("description lost: %s", data)
				}
				if tc.Name == "list_dns_providers" && output.Items[0]["future_field"] != true {
					t.Fatal("provider fields lost")
				}
			} else if tc.Name == "download_certificate" {
				var output struct {
					Data        string `json:"data_base64"`
					Type        string `json:"content_type"`
					Disposition string `json:"content_disposition"`
				}
				if json.Unmarshal(data, &output) != nil {
					t.Fatal("download result")
				}
				decoded, err := base64.StdEncoding.DecodeString(output.Data)
				if err != nil || !bytes.Equal(decoded, archive) || output.Type != "application/zip" || output.Disposition != `attachment; filename="certificate-42"` {
					t.Fatal("download bytes/metadata changed")
				}
			} else if string(data) != `{"success":true}` {
				t.Fatalf("fabricated mutation result: %s", data)
			}
		})
	}
	// An NPM update succeeds before saving a stream description fails. The MCP
	// error must not imply rollback, retry the NPM mutation or reveal backend bodies.
	if _, err = db.DB.Exec("DROP TABLE resource_descriptions"); err != nil {
		t.Fatal(err)
	}
	var update nginxHandlerCase
	for _, tc := range cases {
		if tc.Name == "update_stream" {
			update = tc
		}
	}
	mu.Lock()
	active = update
	before := downstreamCalls
	mu.Unlock()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: update.Name, Arguments: update.Args})
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "HTTP 500") {
		t.Fatalf("partial failure %#v %v", result, err)
	}
	mu.Lock()
	after := downstreamCalls
	mu.Unlock()
	if after != before+1 {
		t.Fatal("partial mutation retried or not applied")
	}
	for _, forbidden := range []string{"create_certificate", "create_lets_encrypt_certificate", "update_access_list", "delete_certificate", "delete_stream", "delete_redirect", "delete_404_host", "delete_access_list", "call_api"} {
		mu.Lock()
		before := downstreamCalls
		mu.Unlock()
		result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: forbidden, Arguments: map[string]any{"id": 42}})
		if err == nil && !result.IsError {
			t.Errorf("excluded tool accepted: %s", forbidden)
		}
		mu.Lock()
		after := downstreamCalls
		mu.Unlock()
		if after != before {
			t.Error("excluded tool reached NPM")
		}
	}
}
