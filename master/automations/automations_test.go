package automations

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func useWorkspace(t *testing.T) {
	t.Helper()
	t.Setenv("HH_AUTOMATIONS_DIR", t.TempDir())
}

func TestFilesStayInsideScriptDirectory(t *testing.T) {
	useWorkspace(t)
	if err := CreateStarter("auto-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFile("auto-1", "lib/util.ts", "export const x = 1;\n"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../escape.ts", "/etc/passwd", "lib/../../x", "node_modules/a.js", ".hidden", "", "a\\b"} {
		if _, err := WriteFile("auto-1", bad, "x"); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	if _, err := WriteFile("../x", "main.ts", "x"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("bad id: %v", err)
	}
	if err := os.Symlink("/etc", filepath.Join(ScriptDir("auto-1"), "etc")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile("auto-1", "etc/hostname"); err == nil {
		t.Fatal("read through a symlink escaping the script directory")
	}
	files, err := ListFiles("auto-1")
	if err != nil || len(files) != 2 || files[0].Path != "lib/util.ts" || files[1].Path != "main.ts" {
		t.Fatalf("files: %+v %v", files, err)
	}
	content, err := ReadFile("auto-1", "main.ts")
	if err != nil || !strings.Contains(content, "export default async function") {
		t.Fatalf("starter: %q %v", content, err)
	}
	if _, err := RenameFile("auto-1", "lib/util.ts", "main.ts"); !errors.Is(err, ErrFileExists) {
		t.Fatalf("rename over existing file: %v", err)
	}
	if _, err := RenameFile("auto-1", "lib/util.ts", "../escape.ts"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("rename escaping directory: %v", err)
	}
	if _, err := RenameFile("auto-1", "missing.ts", "x.ts"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename missing file: %v", err)
	}
	if info, err := RenameFile("auto-1", "lib/util.ts", "helpers/util.ts"); err != nil || info.Path != "helpers/util.ts" {
		t.Fatalf("rename: %+v %v", info, err)
	}
	if content, err := ReadFile("auto-1", "helpers/util.ts"); err != nil || !strings.Contains(content, "export const x") {
		t.Fatalf("renamed content: %q %v", content, err)
	}
	if _, err := ReadFile("auto-1", "lib/util.ts"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old path still readable: %v", err)
	}
	if err := DeleteFile("auto-1", "helpers/util.ts"); err != nil {
		t.Fatal(err)
	}
	if err := WritePackage("[]"); err == nil {
		t.Fatal("non-object package.json accepted")
	}
}

func TestGenerateSDKNames(t *testing.T) {
	declaration, runtime := GenerateSDK([]Route{
		{"GET", "/virsh/getallvms"},
		{"GET", "/automations/"},
		{"GET", "/automations/{id}"},
		{"POST", "/automations/{id}/run"},
		{"GET", "/power-schedule/"},
		{"DELETE", "/vms/{vm_name}/disks/{disk:[0-9]+}"},
		{"GET", "/support/*"},
		{"POST", "/support-internal/automations/fire"},
		{"GET", "/ws"},
	})
	for _, want := range []string{"getGetallvms<T = any>(options?", "get<T = any>(options?", "getById<T = any>(id: string | number", "postRunById", "powerSchedule: {", "deleteDisksByVmNameAndDisk", "sendCritical<T = any>(message: string"} {
		if !strings.Contains(declaration, want) {
			t.Fatalf("declaration lacks %q:\n%s", want, declaration)
		}
	}
	for _, unwanted := range []string{"support-internal", "/support/*", "/ws"} {
		if strings.Contains(declaration, unwanted) {
			t.Fatalf("declaration exposes %q", unwanted)
		}
	}
	if !strings.Contains(runtime, "`/vms/${encodeURIComponent(String(vmName))}/disks/${encodeURIComponent(String(disk))}`") {
		t.Fatalf("runtime path template:\n%s", runtime)
	}
}

func TestRunScriptSendsNotification(t *testing.T) {
	requireNode(t)
	useWorkspace(t)
	seen := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ValidRunToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) || r.Method != http.MethodPost || r.URL.Path != "/notification/send" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen <- body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err := Configure(port, []Route{{"POST", "/notification/send"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFile("auto-n", "main.ts", `
export default async function ({ hh }: HHAutomationArgs) {
  await hh.api.notification.sendCritical("disk full");
  await hh.api.notification.send({ severity: "info", message: "done", title: "Custom", event: "nightly", dedupKey: "k" });
}
`); err != nil {
		t.Fatal(err)
	}
	result := runAndWait(t, RunSpec{RunID: "run-n", AutomationID: "auto-n", AutomationName: "Disk watch", Trigger: TriggerManual, Timeout: 20 * time.Second})
	if result.Status != StatusSucceeded {
		t.Fatalf("result: %+v", result)
	}
	first, second := <-seen, <-seen
	if first["severity"] != "critical" || first["message"] != "disk full" || first["title"] != "HH Automation: Disk watch" || first["event"] != "automation.auto-n" {
		t.Fatalf("first: %v", first)
	}
	if second["severity"] != "info" || second["title"] != "Custom" || second["event"] != "nightly" || second["dedup_key"] != "k" {
		t.Fatalf("second: %v", second)
	}
}

func requireNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
}

func runAndWait(t *testing.T, spec RunSpec) Result {
	t.Helper()
	done := make(chan Result, 1)
	if err := Start(spec, nil, func(result Result) { done <- result }); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		return result
	case <-time.After(30 * time.Second):
		t.Fatal("run did not finish")
		return Result{}
	}
}

func TestRunScriptWithSDK(t *testing.T) {
	requireNode(t)
	useWorkspace(t)
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ValidRunToken(token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		seen <- r.Method + " " + r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"vm1"}]`))
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err := Configure(port, []Route{{"GET", "/virsh/getallvms"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFile("auto-2", "main.ts", `
type VM = { name: string };
export default async function ({ ctx, hh, log }: HHAutomationArgs) {
  const vms: VM[] = await hh.api.virsh.getGetallvms();
  log.info("vms", vms.map((vm) => vm.name).join(","), ctx.trigger, ctx.snapshot["db.cpu"]);
}
`); err != nil {
		t.Fatal(err)
	}
	result := runAndWait(t, RunSpec{RunID: "run-sdk", AutomationID: "auto-2", Trigger: TriggerCondition, Snapshot: map[string]any{"db.cpu": 97}, Timeout: 20 * time.Second})
	if result.Status != StatusSucceeded || result.ExitCode == nil || *result.ExitCode != 0 || !strings.Contains(result.Output, "[info] vms vm1 condition 97") {
		t.Fatalf("result: %+v", result)
	}
	if got := <-seen; got != "GET /virsh/getallvms" {
		t.Fatalf("request: %s", got)
	}
	if RunningRunID("auto-2") != "" {
		t.Fatal("run still reserved")
	}
}

func TestRunFailureTimeoutAndKill(t *testing.T) {
	requireNode(t)
	useWorkspace(t)
	if err := Configure("1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFile("fail", "main.js", "console.log('before'); process.exit(3);\n"); err != nil {
		t.Fatal(err)
	}
	result := runAndWait(t, RunSpec{RunID: "run-fail", AutomationID: "fail", Trigger: TriggerManual, Timeout: 20 * time.Second})
	if result.Status != StatusFailed || result.ExitCode == nil || *result.ExitCode != 3 || !strings.Contains(result.Output, "before") {
		t.Fatalf("failure: %+v", result)
	}

	if _, err := WriteFile("slow", "main.ts", "await new Promise((resolve) => setTimeout(resolve, 60000));\n"); err != nil {
		t.Fatal(err)
	}
	result = runAndWait(t, RunSpec{RunID: "run-timeout", AutomationID: "slow", Trigger: TriggerManual, Timeout: time.Second})
	if result.Status != StatusTimeout {
		t.Fatalf("timeout: %+v", result)
	}

	done := make(chan Result, 1)
	if err := Start(RunSpec{RunID: "run-kill", AutomationID: "slow", Trigger: TriggerManual, Timeout: time.Minute}, nil, func(result Result) { done <- result }); err != nil {
		t.Fatal(err)
	}
	if err := Start(RunSpec{RunID: "run-second", AutomationID: "slow", Trigger: TriggerManual, Timeout: time.Minute}, nil, func(Result) {}); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second concurrent run: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := Kill("run-kill"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.Status != StatusKilled {
			t.Fatalf("kill: %+v", result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("killed run did not finish")
	}
}

func TestOutputCap(t *testing.T) {
	out := newOutput("cap")
	chunk := strings.Repeat("x", 4096)
	for written := 0; written < MaxOutput+8192; written += len(chunk) {
		_, _ = out.Write([]byte(chunk))
	}
	out.close()
	text, truncated := out.text()
	if !truncated || len(text) > MaxOutput+100 {
		t.Fatalf("truncated=%v len=%d", truncated, len(text))
	}
}
