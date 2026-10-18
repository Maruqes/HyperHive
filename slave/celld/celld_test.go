package celld

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/celld"
)

func testConfig() *pb.Config {
	return &pb.Config{Version: "v0.6.2", InternalPort: 18079, Bucket: "s3://hyperhive-celld", Endpoint: "http://127.0.0.1:9000", Region: "us-east-1", AccessKey: "fleet-key", SecretKey: "fleet-secret"}
}

func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func TestOwnedProcessLifecycle(t *testing.T) {
	dir := t.TempDir()
	starts := filepath.Join(dir, "starts")
	binary := filepath.Join(dir, "celld")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'celld 0.6.2'; exit; fi\necho $$ >> %s\necho child-stdout\necho child-stderr >&2\ntrap 'exit 0' TERM\nwhile :; do sleep 0.05; done\n", strconv.Quote(starts))
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	m, err := New(filepath.Join(dir, "state"), binary, "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	m.restartDelay, m.stopGrace = 20*time.Millisecond, time.Second
	if _, err = New(m.dir, binary, m.ip); err == nil {
		t.Fatal("second manager acquired the same process lock")
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err = m.Configure(ctx, testConfig(), true); err != nil {
		t.Fatal(err)
	}
	cancel()
	pids := func() []string { data, _ := os.ReadFile(starts); return strings.Fields(string(data)) }
	eventually(t, func() bool { return m.Status().Running && len(pids()) == 1 })
	eventually(t, func() bool {
		data, _ := os.ReadFile(filepath.Join(m.dir, "logs", "celld.log"))
		return strings.Contains(string(data), "child-stdout") && strings.Contains(string(data), "child-stderr")
	})
	for i := 0; i < 3; i++ {
		if err = m.Configure(context.Background(), testConfig(), true); err != nil {
			t.Fatal(err)
		}
	}
	if len(pids()) != 1 {
		t.Fatal("repeated Configure started duplicate children")
	}
	info, err := os.Stat(filepath.Join(m.dir, "config.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("credentials permissions: %v, %v", info, err)
	}
	pid, _ := strconv.Atoi(pids()[0])
	if err = syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return len(pids()) == 2 && m.Status().Running })
	m.Close()
	if m.Status().Running {
		t.Fatal("child survived intentional shutdown")
	}
	if err = m.Configure(context.Background(), testConfig(), true); err == nil {
		t.Fatal("closed manager accepted setup")
	}
	resumed, err := New(m.dir, binary, m.ip)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	resumed.restartDelay = 20 * time.Millisecond
	if err = resumed.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return resumed.Status().Running && len(pids()) == 3 })
}

func TestRejectUnsafeConfigAndUpgrade(t *testing.T) {
	for _, change := range []func(*pb.Config){
		func(c *pb.Config) { c.Version = "latest" },
		func(c *pb.Config) { c.Endpoint = "http://8.8.8.8:9000" },
		func(c *pb.Config) { c.Endpoint = "http://127.0.0.1:9000/path" },
		func(c *pb.Config) { c.SecretKey = "" },
	} {
		c := testConfig()
		change(c)
		if validate(c) == nil {
			t.Fatalf("accepted unsafe configuration %q", c.Version)
		}
	}
	dir := t.TempDir()
	m, err := New(dir, "/does/not/exist", "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	old := testConfig()
	old.Version = "v0.5.1"
	data, _ := json.Marshal(old)
	if err = os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = m.Configure(context.Background(), testConfig(), true); err == nil || !strings.Contains(err.Error(), "stopped-fleet") {
		t.Fatalf("unsafe upgrade was not blocked: %v", err)
	}
	old.Version = "v0.6.2"
	old.SecretKey = "different"
	data, _ = json.Marshal(old)
	if err = os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = m.Configure(context.Background(), testConfig(), true); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("storage replacement was not blocked: %v", err)
	}
}

func TestPrepareDoesNotResumeAndShutdownIsBounded(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "celld")
	started := filepath.Join(dir, "started")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'celld 0.6.2'; exit; fi\ntrap '' TERM\necho started > %s\nwhile :; do sleep 0.05; done\n", strconv.Quote(started))
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	m, err := New(state, binary, "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Configure(context.Background(), testConfig(), false); err != nil {
		t.Fatal(err)
	}
	m.Close()
	m, err = New(state, binary, "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.Status().Running {
		t.Fatal("prepare enabled a child before storage validation")
	}
	m.stopGrace = 30 * time.Millisecond
	if err = m.Configure(context.Background(), testConfig(), true); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { _, err := os.Stat(started); return err == nil })
	before := time.Now()
	m.Close()
	if time.Since(before) > time.Second || m.Status().Running {
		t.Fatal("SIGTERM-resistant child exceeded the shutdown bound")
	}
}
