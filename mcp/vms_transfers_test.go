package hyperhivemcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestVMStreamingTransfers(t *testing.T) {
	payload := bytes.Repeat([]byte{0, 0xff, 1, 2, 3}, (17<<20)/5+1)
	hash := sha256.Sum256(payload)
	for _, transport := range []string{"http", "stdio", "legacy-http", "legacy-stdio"} {
		for _, tool := range []string{"export_vm", "download_vm_backup"} {
			t.Run(transport+"/"+tool, func(t *testing.T) {
				tc := vmPinningCase()
				var transfers atomic.Int32
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer vm-fixture" {
						t.Error("missing auth")
					}
					path := strings.TrimPrefix(r.URL.Path, "/prefix/api")
					if path == "/virsh/backups" {
						fmt.Fprint(w, `[{"db_res":{"Id":7,"Name":"guest","Path":"/fixture/backup.qcow2","NfsId":2,"CreatedAt":"","Automatic":false},"live":true,"size":1}]`)
						return
					}
					if strings.HasPrefix(path, "/virsh/export/") || strings.HasPrefix(path, "/virsh/downloadbackup/") {
						transfers.Add(1)
						w.Header().Set("Content-Type", "application/octet-stream")
						w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
						w.Write(payload)
						return
					}
					raw, _ := vmSnapshotFixture(path, tc)
					fmt.Fprint(w, raw)
				}))
				defer backend.Close()
				private := filepath.Join(t.TempDir(), "private")
				session := vmConnect(t, transport, backend.URL, private, &mcp.ClientOptions{ElicitationHandler: vmApprove})
				args := map[string]any{"vm_name": "guest"}
				if tool == "download_vm_backup" {
					args = map[string]any{"id": 7}
				}
				out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
				if err != nil || out.IsError {
					t.Fatalf("stream failed %v %#v", err, out)
				}
				data, _ := json.Marshal(out.StructuredContent)
				var result struct {
					Result struct {
						Path  string `json:"artifact_path"`
						Bytes int64  `json:"bytes"`
						Hash  string `json:"sha256"`
					} `json:"result"`
					Backup string `json:"backup_path"`
				}
				if json.Unmarshal(data, &result) != nil {
					t.Fatal("invalid transfer output")
				}
				if result.Result.Bytes != int64(len(payload)) || result.Result.Hash != hex.EncodeToString(hash[:]) || filepath.Dir(result.Result.Path) != filepath.Dir(result.Backup) {
					t.Fatalf("wrong full transfer metadata %s", data)
				}
				artifact, err := os.ReadFile(result.Result.Path)
				if err != nil || !bytes.Equal(artifact, payload) {
					t.Fatal("artifact is incomplete")
				}
				for _, path := range []string{result.Result.Path, result.Backup, filepath.Dir(result.Backup)} {
					info, err := os.Stat(path)
					if err != nil || info.Mode().Perm()&0077 != 0 {
						t.Error("artifact or backup is not private")
					}
				}
				if transfers.Load() != 1 {
					t.Error("transfer retried")
				}
			})
		}
	}
}

func TestVMTransferFailuresPreserveRecord(t *testing.T) {
	for _, transport := range []string{"http", "stdio"} {
		for _, failure := range []string{"http-error", "wrong-type", "truncated"} {
			t.Run(transport+"/"+failure, func(t *testing.T) {
				tc := vmPinningCase()
				var transfers atomic.Int32
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					path := strings.TrimPrefix(r.URL.Path, "/prefix/api")
					if path != "/virsh/export/guest" {
						raw, _ := vmSnapshotFixture(path, tc)
						fmt.Fprint(w, raw)
						return
					}
					transfers.Add(1)
					switch failure {
					case "http-error":
						http.Error(w, "PRIVATE sensitive backend", 500)
					case "wrong-type":
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, `{"password":"PRIVATE"}`)
					case "truncated":
						w.Header().Set("Content-Type", "application/octet-stream")
						w.Header().Set("Content-Length", "1024")
						w.Write([]byte("partial"))
					}
				}))
				defer backend.Close()
				private := filepath.Join(t.TempDir(), "private")
				session := vmConnect(t, transport, backend.URL, private, &mcp.ClientOptions{ElicitationHandler: vmApprove})
				out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "export_vm", Arguments: map[string]any{"vm_name": "guest"}})
				if err != nil {
					t.Fatal(err)
				}
				data, _ := json.Marshal(out)
				if !out.IsError || !strings.Contains(string(data), "Retained prior-state record") || strings.Contains(string(data), "PRIVATE") {
					t.Fatalf("unsafe transfer failure %s", data)
				}
				records, _ := filepath.Glob(filepath.Join(private, "change-*", "before.json"))
				artifacts, _ := filepath.Glob(filepath.Join(private, "change-*", "disk-*"))
				if len(records) != 1 || len(artifacts) != 0 || transfers.Load() != 1 {
					t.Fatalf("record/artifact/retry failure records=%d artifacts=%d calls=%d", len(records), len(artifacts), transfers.Load())
				}
			})
		}
	}
}

func TestVMTransferCancellation(t *testing.T) {
	for _, transport := range []string{"http", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			tc := vmPinningCase()
			started := make(chan struct{})
			stopped := make(chan struct{})
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path := strings.TrimPrefix(r.URL.Path, "/prefix/api")
				if path != "/virsh/export/guest" {
					raw, _ := vmSnapshotFixture(path, tc)
					fmt.Fprint(w, raw)
					return
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Length", "1024")
				w.Write([]byte("partial"))
				w.(http.Flusher).Flush()
				close(started)
				select {
				case <-r.Context().Done():
				case <-time.After(3 * time.Second):
					t.Error("transfer did not honor cancellation")
				}
				close(stopped)
			}))
			defer backend.Close()
			private := filepath.Join(t.TempDir(), "private")
			session := vmConnect(t, transport, backend.URL, private, &mcp.ClientOptions{ElicitationHandler: vmApprove})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				defer close(done)
				session.CallTool(ctx, &mcp.CallToolParams{Name: "export_vm", Arguments: map[string]any{"vm_name": "guest"}})
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("transfer did not start")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("canceled call hung")
			}
			select {
			case <-stopped:
			case <-time.After(3 * time.Second):
				t.Fatal("backend read not canceled")
			}
			deadline := time.Now().Add(time.Second)
			for {
				artifacts, _ := filepath.Glob(filepath.Join(private, "change-*", "disk-*"))
				if len(artifacts) == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("canceled transfer left a partial artifact")
				}
				time.Sleep(time.Millisecond)
			}
			records, _ := filepath.Glob(filepath.Join(private, "change-*", "before.json"))
			if len(records) != 1 {
				t.Fatal("canceled transfer lost pre-change record")
			}
		})
	}
}
