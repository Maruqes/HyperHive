package hyperhivemcp

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
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
	"golang.org/x/crypto/ssh"
)

func vmPublicKey(t *testing.T) string {
	t.Helper()
	key, err := ssh.NewPublicKey(ed25519.NewKeyFromSeed(make([]byte, 32)).Public())
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " fixture-comment"
}
func TestVMSSHInjectionRecoveryBothTransports(t *testing.T) {
	for _, transport := range []string{"http", "stdio", "legacy-http", "legacy-stdio"} {
		for _, mode := range []string{"success", "decline", "cancel", "missing-picker", "save-fail", "running", "wrong-disk", "read-fail", "export-error", "truncated", "manifest-fail", "changed", "backend-error"} {
			t.Run(transport+"/"+mode, func(t *testing.T) {
				private := filepath.Join(t.TempDir(), "private")
				tc := vmPinningCase()
				// Isolate shared maps from other cases and include the export disk identity.
				tc.Snapshots = map[string]string{}
				tc.Snapshots["vm"] = `{"name":"guest","machineName":"host","state":5,"diskSizeGB":10,"diskPath":"/fixture/boot.qcow2"}`
				tc.Snapshots["domain_xml"] = `{"vm_xml":"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>","machine_name":"host"}`
				payload := bytes.Repeat([]byte("guest disk bytes including original permissions"), 128)
				if mode == "success" {
					payload = bytes.Repeat([]byte{0, 255, 1, 2}, (17<<20)/4+1)
				}
				var reads, exports, writes atomic.Int32
				key := vmPublicKey(t)
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer vm-fixture" {
						t.Error("wrong auth")
					}
					path := strings.TrimPrefix(r.URL.Path, "/prefix/api")
					if path == "/virsh/add_ssh_key/guest" {
						writes.Add(1)
						if r.Method != "POST" {
							t.Error("wrong injection method")
						}
						var body map[string]string
						if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 || body["ssh_key"] != key {
							t.Error("wrong injection body")
						}
						manifests, _ := filepath.Glob(filepath.Join(private, "change-*", "ssh-disk.json"))
						if len(manifests) != 1 {
							t.Error("injection before recovery manifest")
							http.Error(w, "fixture", 500)
							return
						}
						data, err := os.ReadFile(manifests[0])
						if err != nil {
							t.Error(err)
						}
						var manifest struct {
							Path  string `json:"artifact_path"`
							Bytes int64  `json:"bytes"`
							Hash  string `json:"sha256"`
						}
						if json.Unmarshal(data, &manifest) != nil {
							t.Error("invalid manifest")
						}
						disk, err := os.ReadFile(manifest.Path)
						hash := sha256.Sum256(payload)
						if err != nil || !bytes.Equal(disk, payload) || manifest.Bytes != int64(len(payload)) || manifest.Hash != hex.EncodeToString(hash[:]) {
							t.Error("injection before complete verified disk copy")
						}
						for _, p := range []string{manifest.Path, manifests[0], filepath.Join(filepath.Dir(manifests[0]), "before.json"), filepath.Dir(manifests[0])} {
							info, e := os.Stat(p)
							if e != nil || info.Mode().Perm()&0077 != 0 {
								t.Error("insecure recovery data")
							}
						}
						if mode == "backend-error" {
							http.Error(w, "PRIVATE secret backend failure", 500)
						}
						return
					}
					if r.Method != "GET" {
						t.Error("unexpected write")
						http.Error(w, "fixture", 500)
						return
					}
					reads.Add(1)
					if mode == "read-fail" {
						http.Error(w, "PRIVATE read failure", 500)
						return
					}
					if path == "/virsh/export/guest" {
						exports.Add(1)
						if mode == "export-error" {
							http.Error(w, "PRIVATE export failure", 500)
							return
						}
						w.Header().Set("Content-Type", "application/octet-stream")
						if mode == "truncated" {
							w.Header().Set("Content-Length", fmt.Sprint(len(payload)+10))
						}
						if mode == "manifest-fail" {
							dirs, _ := filepath.Glob(filepath.Join(private, "change-*"))
							if len(dirs) == 1 {
								if err := os.Mkdir(filepath.Join(dirs[0], "ssh-disk.json"), 0700); err != nil {
									t.Error(err)
								}
							}
						}
						w.Write(payload)
						return
					}
					raw, _ := vmSnapshotFixture(path, tc)
					if path == "/virsh/getvmbyname/guest" {
						if mode == "running" {
							raw = strings.Replace(raw, `"state":5`, `"state":1`, 1)
						}
						if mode == "wrong-disk" {
							raw = strings.Replace(raw, "boot.qcow2", "different.qcow2", 1)
						}
						if mode == "changed" && exports.Load() > 0 {
							raw = strings.Replace(raw, `"diskSizeGB":10`, `"diskSizeGB":11`, 1)
						}
					}
					fmt.Fprint(w, raw)
				}))
				defer backend.Close()
				opts := &mcp.ClientOptions{ElicitationHandler: func(ctx context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
					if strings.Contains(req.Params.Message, key) {
						t.Error("picker exposed key instead of fingerprint")
					}
					if mode == "decline" || mode == "cancel" {
						return &mcp.ElicitResult{Action: mode}, nil
					}
					return vmApprove(ctx, req)
				}}
				if mode == "missing-picker" {
					opts = nil
				}
				if mode == "save-fail" {
					if err := os.WriteFile(private, []byte("occupied"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				session := vmConnect(t, transport, backend.URL, private, opts)
				out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "add_vm_ssh_key", Arguments: map[string]any{"vm_name": "guest", "ssh_key": key}})
				success := err == nil && out != nil && !out.IsError && !out.NeedsInput()
				if success != (mode == "success") {
					data, _ := json.Marshal(out)
					t.Fatalf("unexpected result %s: %v %s", mode, err, data)
				}
				if mode == "success" || mode == "backend-error" {
					if writes.Load() != 1 || exports.Load() != 1 {
						t.Fatal("skipped or retried injection/export")
					}
				} else if writes.Load() != 0 {
					t.Fatal("unsafe injection")
				}
				if mode == "decline" || mode == "cancel" || mode == "missing-picker" {
					if reads.Load() != 0 {
						t.Fatal("backend accessed without approval")
					}
				}
				if exports.Load() > 0 {
					data, _ := json.Marshal(out)
					if strings.Contains(string(data), "PRIVATE") {
						t.Error("backend body leaked")
					}
					if mode != "success" && !strings.Contains(string(data), "Retained prior-state record") {
						t.Error("failure lost recovery location")
					}
				}
			})
		}
	}
}
