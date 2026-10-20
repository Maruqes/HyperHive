package hyperhivemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRemoveVMISOsDiscoveryAndGuards(t *testing.T) {
	var tc vmWriteCase
	for _, candidate := range vmWriteCases {
		if candidate.Name == "remove_vm_isos" {
			tc = candidate
		}
	}
	for _, transport := range []string{"http", "stdio"} {
		for _, failure := range []string{"missing-picker", "save-fail", "secure-xml-missing"} {
			t.Run(transport+"/"+failure, func(t *testing.T) {
				var writes atomic.Int32
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" {
						writes.Add(1)
						return
					}
					path := strings.TrimPrefix(r.URL.Path, "/prefix/api")
					raw, _ := vmSnapshotFixture(path, tc)
					if failure == "secure-xml-missing" && path == "/virsh/snapshot/guest" {
						raw = `{}`
					}
					fmt.Fprint(w, raw)
				}))
				defer backend.Close()
				backup := filepath.Join(t.TempDir(), "private")
				if failure == "save-fail" {
					if err := os.WriteFile(backup, []byte("occupied"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				opts := &mcp.ClientOptions{ElicitationHandler: vmApprove}
				if failure == "missing-picker" {
					opts = nil
				}
				session := vmConnect(t, transport, backend.URL, backup, opts)
				listed, err := session.ListTools(context.Background(), nil)
				if err != nil {
					t.Fatal(err)
				}
				var tool *mcp.Tool
				for _, candidate := range listed.Tools {
					if candidate.Name == tc.Name {
						tool = candidate
					}
				}
				if tool == nil || tool.Meta["hyperhive/risk"] != "high" || tool.Annotations == nil || tool.Annotations.ReadOnlyHint || tool.OutputSchema == nil {
					t.Fatalf("missing or unsafe discovery: %#v", tool)
				}
				for _, args := range []string{`{"vm_name":"guest","iso_id":1}`, `{"vm_name":null}`, `{"vm_name":""}`, `{"vm_name":1}`, tc.Args} {
					out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.Name, Arguments: json.RawMessage(args)})
					if err == nil && out != nil && !out.IsError && !out.NeedsInput() {
						t.Fatalf("unsafe call accepted: %s", args)
					}
				}
				if writes.Load() != 0 {
					t.Fatal("unsafe mutation occurred")
				}
			})
		}
	}
}
