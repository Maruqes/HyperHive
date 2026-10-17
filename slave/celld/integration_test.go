package celld

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Run through scripts/test_celld_fleet.sh; all node data and storage are disposable.
func TestFleetIntegration(t *testing.T) {
	binary := os.Getenv("CELLD_TEST_BINARY")
	endpoint := os.Getenv("CELLD_TEST_ENDPOINT")
	if binary == "" || endpoint == "" {
		t.Skip("set CELLD_TEST_BINARY and CELLD_TEST_ENDPOINT for the real fleet test")
	}
	t.Setenv("CELLD_TOKIO_THREADS", "2")
	cfg := testConfig()
	cfg.Endpoint = endpoint
	cfg.AccessKey = "celld-test"
	cfg.SecretKey = "celld-test-secret"
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	ready := func(ip string) bool {
		resp, err := client.Get("http://" + net.JoinHostPort(ip, "8080") + "/.well-known/celld/health")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == 200
	}
	waitReady := func(ip string) {
		t.Helper()
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			if ready(ip) {
				return
			}
			time.Sleep(300 * time.Millisecond)
		}
		t.Fatalf("node %s never became ready", ip)
	}
	var nodes []*Manager
	start := func(ip string) *Manager {
		t.Helper()
		m, err := New(t.TempDir(), binary, ip)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			m.Close()
			if t.Failed() {
				data, _ := os.ReadFile(filepath.Join(m.dir, "logs", "celld.log"))
				if len(data) > 4096 {
					data = data[len(data)-4096:]
				}
				t.Logf("celld %s log tail:\n%s", ip, data)
			}
		})
		if err = m.Configure(context.Background(), cfg, true); err != nil {
			t.Fatal(err)
		}
		waitReady(ip)
		nodes = append(nodes, m)
		return m
	}
	start("127.0.0.2")
	start("127.0.0.3")
	start("127.0.0.4")
	probe := func(expected int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, "diagnose", "--read-only", "--json", "--bucket", cfg.Bucket, "--endpoint", endpoint, "--region", cfg.Region,
			"--listen", "127.0.0.1:0", "--internal-listen", "127.0.0.1:0")
		cmd.Env = append(os.Environ(), "AWS_ACCESS_KEY_ID="+cfg.AccessKey, "AWS_SECRET_ACCESS_KEY="+cfg.SecretKey)
		out, err := cmd.Output()
		if err != nil && len(out) == 0 {
			t.Fatalf("diagnose failed: %v\n%s", err, out)
		}
		d := json.NewDecoder(strings.NewReader(string(out)))
		count := 0
		for {
			var row struct{ Check, Verdict string }
			if err := d.Decode(&row); err == io.EOF {
				break
			} else if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(row.Check, "peer ") && row.Verdict == "ok" {
				count++
			}
		}
		if count != expected {
			t.Fatalf("expected %d live peers, got %d: %s", expected, count, out)
		}
	}
	probe(3)
	start("127.0.0.5")
	probe(4)
	for _, m := range nodes {
		if err := m.Configure(context.Background(), cfg, true); err != nil {
			t.Fatal(err)
		}
	}
	probe(4)
	// Kill only a direct child of this test, identified by its dedicated listener.
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	killed := false
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		status, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "status"))
		if !strings.Contains(string(status), fmt.Sprintf("PPid:\t%d\n", os.Getpid())) {
			continue
		}
		cmdline, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if !strings.Contains(string(cmdline), "127.0.0.3:8080") {
			continue
		}
		if err = syscall.Kill(pid, syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		killed = true
		break
	}
	if !killed {
		t.Fatal("did not find the supervised celld child")
	}
	deadline := time.Now().Add(5 * time.Second)
	for ready("127.0.0.3") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if ready("127.0.0.3") {
		t.Fatal("killed node still answers readiness")
	}
	waitReady("127.0.0.3")
	probe(4)
	old := nodes[1]
	old.Close()
	resumed, err := New(old.dir, binary, old.ip)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if err = resumed.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitReady(old.ip)
	probe(4)
}
