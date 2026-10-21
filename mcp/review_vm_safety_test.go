package hyperhivemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReviewVMApprovalPreservesExactIntegerTarget(t *testing.T) {
	for _, transport := range []string{"http", "stdio"} {
		for _, exact := range []string{"9007199254740993", "9223372036854775807"} {
			t.Run(transport+"/"+exact, func(t *testing.T) {
				var changed atomic.Value
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					path := strings.TrimPrefix(r.URL.Path, "/prefix/api")
					if r.Method == http.MethodGet && path == "/virsh/autobak" {
						fmt.Fprint(w, `[{"Id":9007199254740992,"VmName":"other","FrequencyDays":1,"MinTime":"01:00","MaxTime":"02:00","NfsMountId":1,"MaxBackupsRetain":1,"Enabled":true},{"Id":`+exact+`,"VmName":"requested","FrequencyDays":1,"MinTime":"01:00","MaxTime":"02:00","NfsMountId":1,"MaxBackupsRetain":1,"Enabled":true}]`)
						return
					}
					changed.Store(path)
					fmt.Fprint(w, `{"message":"disabled"}`)
				}))
				defer backend.Close()
				session := vmConnect(t, transport, backend.URL, filepath.Join(t.TempDir(), "private"), &mcp.ClientOptions{ElicitationHandler: vmApprove})
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "disable_vm_automatic_backup", Arguments: json.RawMessage(`{"id":` + exact + `}`)})
				if err != nil || result.IsError {
					t.Fatalf("exact target invocation failed: %v %#v", err, result)
				}
				if changed.Load() != "/virsh/autopak/disable/"+exact {
					t.Fatalf("approval executed different target: requested %s, wrote %s", exact, changed.Load())
				}
			})
		}
	}
}

func TestReviewExactIntegerAndInvalidArguments(t *testing.T) {
	for _, transport := range []string{"http", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			var calls atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch r.URL.Path {
				case "/prefix/api/notes/9007199254740993", "/prefix/api/notes/9223372036854775807":
					fmt.Fprint(w, `{"id":9007199254740993,"titulo":"title","nota":"body"}`)
				case "/prefix/api/isos/download":
					body, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(body), `"nfs_share_id":9007199254740993`) {
						t.Errorf("ISO target rounded: %s", body)
					}
					w.WriteHeader(http.StatusAccepted)
					fmt.Fprint(w, `{"status":"started"}`)
				default:
					t.Errorf("unexpected target %s", r.URL.Path)
					fmt.Fprint(w, `{}`)
				}
			}))
			defer backend.Close()
			session := vmConnect(t, transport, backend.URL, filepath.Join(t.TempDir(), "private"), &mcp.ClientOptions{ElicitationHandler: vmApprove})
			for _, tc := range []struct{ name, args string }{
				{"get_note", `{"id":9007199254740993}`},
				{"get_note", `{"id":9223372036854775807}`},
				{"start_iso_download", `{"url":"https://fixture.test/image.iso","iso_name":"image.iso","nfs_share_id":9007199254740993}`},
			} {
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.name, Arguments: json.RawMessage(tc.args)})
				if err != nil || result.IsError {
					t.Fatalf("exact %s invocation failed: %v %#v", tc.name, err, result)
				}
			}
			before := calls.Load()
			for _, tc := range []struct{ name, args string }{
				{"list_spa_ports", `null`},
				{"list_spa_ports", `[]`},
				{"get_note", `{"id":1.0000000000000001}`},
				{"get_note", `{"id":9223372036854775808}`},
				{"disable_vm_automatic_backup", `{"id":1.0000000000000001}`},
			} {
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.name, Arguments: json.RawMessage(tc.args)})
				if err == nil && result != nil && !result.IsError {
					t.Fatalf("invalid %s input accepted: %s", tc.name, tc.args)
				}
			}
			if calls.Load() != before {
				t.Fatal("invalid arguments reached backend")
			}
		})
	}
}

func TestReviewProxyNumericAccessListAndMetadata(t *testing.T) {
	for _, transport := range []string{"http", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			var writes atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writes.Add(1)
				body, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.URL.Path != "/prefix/api/proxy/create" || !strings.Contains(string(body), `"access_list_id":42`) || !strings.Contains(string(body), `"counter":9007199254740993`) {
					t.Errorf("unexpected proxy mutation: %s %s %s", r.Method, r.URL.Path, body)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer backend.Close()
			session := vmConnect(t, transport, backend.URL, filepath.Join(t.TempDir(), "private"), &mcp.ClientOptions{ElicitationHandler: vmApprove})
			args := changed(configuration(), "access_list_id", json.Number("42"))
			args["meta"] = map[string]any{"counter": json.Number("9007199254740993")}
			data, _ := json.Marshal(args)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_proxy", Arguments: json.RawMessage(data)})
			if err != nil || result.IsError {
				t.Fatalf("numeric access list failed: %v %#v", err, result)
			}
			for _, invalid := range []string{"1.00000000000000001", "9007199254740992", "-1"} {
				args["access_list_id"] = json.Number(invalid)
				data, _ = json.Marshal(args)
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_proxy", Arguments: json.RawMessage(data)})
				if err == nil && result != nil && !result.IsError {
					t.Fatalf("invalid access list accepted: %s", invalid)
				}
			}
			if writes.Load() != 1 {
				t.Fatal("invalid access list reached backend")
			}
		})
	}
}
