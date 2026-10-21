package vmsafety

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func approvalSession(t *testing.T, handler func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error), manual bool, calls *atomic.Int32, options ...*mcp.ClientSessionOptions) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "approval-test"}, nil)
	gate := &Gate{}
	mcp.AddTool(server, &mcp.Tool{Name: "vm_change"}, func(ctx context.Context, req *mcp.CallToolRequest, input struct {
		Target string `json:"target"`
	}) (*mcp.CallToolResult, any, error) {
		pending, err := gate.Require(ctx, req, High, "Change VM "+input.Target)
		if err != nil || pending != nil {
			return pending, nil, err
		}
		calls.Add(1)
		return nil, map[string]any{"changed": true}, nil
	})
	httpOptions := &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}
	if len(options) > 0 && options[0] != nil && options[0].ProtocolVersion < "2026-07-28" {
		httpOptions = &mcp.StreamableHTTPOptions{SessionTimeout: time.Minute}
	}
	endpoint := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, httpOptions))
	t.Cleanup(endpoint.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "approval-client"}, &mcp.ClientOptions{ElicitationHandler: handler, MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: manual}})
	var connectOptions *mcp.ClientSessionOptions
	if len(options) > 0 {
		connectOptions = options[0]
	}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL}, connectOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestUnsupportedPickerCannotMutate(t *testing.T) {
	for _, version := range []string{"2025-11-25", "2026-07-28"} {
		t.Run(version, func(t *testing.T) {
			var calls atomic.Int32
			session := approvalSession(t, nil, false, &calls, &mcp.ClientSessionOptions{ProtocolVersion: version})
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "vm_change", Arguments: map[string]any{"target": "fixture"}})
			if calls.Load() != 0 {
				t.Fatal("unsupported picker executed a write")
			}
			if err == nil && result != nil && !result.IsError && !result.NeedsInput() {
				t.Fatal("unsupported picker reported a complete success")
			}
		})
	}
}

func TestMutationLockCancellation(t *testing.T) {
	g := &Gate{}
	release, err := g.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Lock(ctx); err == nil {
		t.Fatal("canceled caller acquired mutation lock")
	}
	release()
	release()
	if _, err := g.Lock(ctx); err == nil {
		t.Fatal("canceled caller acquired free mutation lock")
	}
	second, err := g.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second()
}

func TestApprovalPicker(t *testing.T) {
	for _, decision := range []string{"approve", "cancel", "decline", "dismiss", "missing"} {
		t.Run(decision, func(t *testing.T) {
			var calls, prompts atomic.Int32
			session := approvalSession(t, func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
				prompts.Add(1)
				if !strings.Contains(req.Params.Message, "Risk: high") || !strings.Contains(req.Params.Message, "fixture") {
					t.Fatal("missing risk/target in picker")
				}
				res := &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": decision}}
				switch decision {
				case "decline":
					res.Action = "decline"
				case "dismiss":
					res.Action = "cancel"
				case "missing":
					res.Content = nil
				}
				return res, nil
			}, false, &calls)
			res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "vm_change", Arguments: map[string]any{"target": "fixture"}})
			if decision == "approve" {
				if err != nil || res.IsError || calls.Load() != 1 {
					t.Fatalf("approved invocation failed: %v %#v calls=%d", err, res, calls.Load())
				}
			} else if calls.Load() != 0 {
				t.Fatal("unapproved mutation executed")
			}
			if prompts.Load() != 1 {
				t.Fatal("expected exactly one picker")
			}
		})
	}
}

func TestApprovalBindingAndSingleUse(t *testing.T) {
	var calls atomic.Int32
	session := approvalSession(t, nil, true, &calls)
	args := map[string]any{"target": "fixture"}
	start := func() *mcp.CallToolResult {
		t.Helper()
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "vm_change", Arguments: args})
		if err != nil || !res.NeedsInput() || calls.Load() != 0 {
			t.Fatalf("initial approval request: %v %#v", err, res)
		}
		return res
	}
	approve := func(token string, arguments map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "vm_change", Arguments: arguments, RequestState: token, InputResponses: mcp.InputResponseMap{"tool_approval": &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}}}})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	first := start()
	if !approve(first.RequestState, map[string]any{"target": "other"}).IsError || calls.Load() != 0 {
		t.Fatal("changed arguments bypassed approval")
	}
	second := start()
	if approve(second.RequestState, args).IsError || calls.Load() != 1 {
		t.Fatal("valid approval failed")
	}
	if !approve(second.RequestState, args).IsError || calls.Load() != 1 {
		t.Fatal("approval replay executed mutation")
	}
	if !approve("invented", args).IsError || calls.Load() != 1 {
		t.Fatal("forged approval executed mutation")
	}
}

func TestBackupPersistenceAndPrivacy(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	store := Store{Dir: dir}
	record := Record{Tool: "update_vm_xml", Target: "../../fixture", Risk: High, Snapshots: map[string]json.RawMessage{"domain": json.RawMessage(`{"vm_xml":"<domain><name>fixture</name></domain>"}`), "cpupinning": json.RawMessage(`{"rangeStart":0,"hyperThreading":false}`)}}
	first, err := store.Save(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Save(context.Background(), record)
	if err != nil || first == second {
		t.Fatal("backups overwrite")
	}
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	var got Record
	if json.Unmarshal(data, &got) != nil || got.Target != record.Target || string(got.Snapshots["cpupinning"]) == "" || got.CreatedAt.IsZero() {
		t.Fatal("incomplete backup")
	}
	for _, path := range []string{dir, filepath.Dir(first), first} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatal("backup is not private")
		}
	}
	if !strings.HasPrefix(first, dir+string(os.PathSeparator)) {
		t.Fatal("target escaped backup directory")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Save(ctx, record); err == nil {
		t.Fatal("canceled backup accepted")
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(context.Background(), record); err == nil {
		t.Fatal("public backup directory accepted")
	}
	if _, err := (Store{Dir: "relative"}).Save(context.Background(), record); err == nil {
		t.Fatal("relative directory accepted")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Dir(first), link); err != nil {
		t.Fatal(err)
	}
	if _, err := (Store{Dir: link}).Save(context.Background(), record); err == nil {
		t.Fatal("symlink directory accepted")
	}
}

// A host may advertise a handler yet cancel without ever rendering a picker.
// The server must report the client response, not assert that a human declined.
func TestApprovalClientResponseDiagnostics(t *testing.T) {
	for _, version := range []string{"2025-11-25", "2026-07-28"} {
		for _, tc := range []struct {
			name     string
			response *mcp.ElicitResult
			want     string
		}{
			{"cancel-without-ui", &mcp.ElicitResult{Action: "cancel"}, "client canceled"},
			{"decline-without-ui", &mcp.ElicitResult{Action: "decline"}, "client declined"},
			{"cancel-choice", &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "cancel"}}, "cancel was selected"},
			{"missing-choice", &mcp.ElicitResult{Action: "accept"}, "no explicit approve choice"},
		} {
			t.Run(version+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32
				session := approvalSession(t, func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) { return tc.response, nil }, false, &calls, &mcp.ClientSessionOptions{ProtocolVersion: version})
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "vm_change", Arguments: map[string]any{"target": "fixture"}})
				if err != nil {
					t.Fatal(err)
				}
				data, _ := json.Marshal(result)
				if !result.IsError || !strings.Contains(string(data), tc.want) || !strings.Contains(string(data), "form elicitation") || !strings.Contains(string(data), "no change made") {
					t.Fatalf("missing diagnostic: %s", data)
				}
				if calls.Load() != 0 {
					t.Fatal("unapproved change")
				}
			})
		}
	}
}
