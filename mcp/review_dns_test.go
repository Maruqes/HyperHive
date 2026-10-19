package hyperhivemcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReviewDNSWritesRejectUnsafeConfigValues(t *testing.T) {
	for _, transport := range []string{"http", "stdio", "direct-client"} {
		t.Run(transport, func(t *testing.T) {
			var writes atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/prefix/api/dnsmasq/alias/add" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				writes.Add(1)
			}))
			defer backend.Close()
			var add func(client.DNSAliasWriteInput) bool
			if transport == "direct-client" {
				c, err := client.New(client.Config{BaseURL: backend.URL + "/prefix/api", Token: "vm-fixture"})
				if err != nil {
					t.Fatal(err)
				}
				add = func(in client.DNSAliasWriteInput) bool { return c.AddDNSAlias(context.Background(), in) == nil }
			} else {
				session := vmConnect(t, transport, backend.URL, t.TempDir(), nil)
				add = func(in client.DNSAliasWriteInput) bool {
					out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "dns_alias_add", Arguments: in})
					if err != nil {
						t.Fatal(err)
					}
					return !out.IsError
				}
			}
			for _, value := range []string{" fixture.test", "fixture.test ", "\nfixture.test", "fixture.test\n", "fixture\x00.test", "fixture\x01.test", "fixture\u00a0.test", "fixture\v.test"} {
				if add(client.DNSAliasWriteInput{Alias: value, IP: "192.0.2.1"}) {
					t.Errorf("unsafe alias accepted %q", value)
				}
			}
			for _, value := range []string{" 192.0.2.1", "192.0.2.1 ", "\n192.0.2.1", "192.0.2.1\n", "192.0.2.1\t", "192.0.2.1\x00"} {
				if add(client.DNSAliasWriteInput{Alias: "fixture.test", IP: value}) {
					t.Errorf("unsafe IP accepted %q", value)
				}
			}
			if writes.Load() != 0 {
				t.Fatalf("unsafe values reached backend %d times", writes.Load())
			}
			if !add(client.DNSAliasWriteInput{Alias: "one.test/two.test", IP: "2001:db8::1"}) {
				t.Fatal("existing multidomain alias contract rejected")
			}
			if writes.Load() != 1 {
				t.Fatal("valid mutation missing or retried")
			}
		})
	}
}
