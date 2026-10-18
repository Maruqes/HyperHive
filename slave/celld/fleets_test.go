package celld

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/celld"
)

func fleetConfigFor(id string, port uint32) *pb.Config {
	c := testConfig()
	c.FleetId = id
	c.Bucket = "s3://hyperhive-celld-" + id
	c.HttpPort = port
	c.InternalPort = port + 1
	return c
}

func TestFleetRegistryIsolationAndAuthoritativeRestart(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "celld")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'celld 0.6.2'; exit; fi\ntrap 'exit 0' TERM\nwhile :; do sleep 0.02; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	r, err := NewRegistry(state, binary, "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.startAfter = time.Time{}
	a, b := fleetConfigFor("a", 28080), fleetConfigFor("b", 28082)
	if _, err = r.Configure(context.Background(), a, true); err == nil {
		t.Fatal("started without authoritative membership")
	}
	if err = r.Sync([]string{"a", "b"}, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*pb.Config{a, b} {
		if _, err = r.Configure(context.Background(), c, true); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, func() bool { return r.Status("a").Running && r.Status("b").Running })
	if err = r.Stop("a", false); err != nil {
		t.Fatal(err)
	}
	if r.Status("a").Running || !r.Status("b").Running {
		t.Fatal("stop affected the wrong fleet")
	}
	if _, err = r.Configure(context.Background(), a, true); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return r.Status("a").Running })
	if err = r.Sync([]string{"b"}, nil); err != nil {
		t.Fatal(err)
	}
	if r.Status("a").Running || r.Status("b").Running {
		t.Fatal("removed/stopped membership kept running")
	}
	if _, err = r.Configure(context.Background(), a, true); err == nil {
		t.Fatal("removed fleet was reenabled")
	}
	if _, err = r.Configure(context.Background(), b, true); err == nil {
		t.Fatal("stopped fleet was reenabled")
	}
	r.Close()
	r, err = NewRegistry(state, binary, "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.startAfter = time.Time{}
	if r.Status("a").Running || r.Status("b").Running {
		t.Fatal("stale local state resumed before synchronization")
	}
	if err = r.Sync([]string{"b"}, []string{"b"}); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Configure(context.Background(), b, true); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return r.Status("b").Running })
	if err = r.Stop("a", true); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(state, "fleets", "a")); !os.IsNotExist(err) {
		t.Fatal("removed fleet data remains")
	}
	if !r.Status("b").Running {
		t.Fatal("remove stopped another fleet")
	}
}

func TestFleetPortsAndLegacyPurge(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "celld")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho 'celld 0.6.2'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(filepath.Join(dir, "state"), binary, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.startAfter = time.Time{}
	if err = r.Sync([]string{"a", "b"}, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	a, b := fleetConfigFor("a", 28080), fleetConfigFor("b", 28081)
	if _, err = r.Configure(context.Background(), a, false); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Configure(context.Background(), b, false); err == nil {
		t.Fatal("overlapping internal/HTTP reservations accepted")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := uint32(listener.Addr().(*net.TCPAddr).Port)
	b.HttpPort = port
	b.InternalPort = port - 1
	if _, err = r.Configure(context.Background(), b, true); err == nil {
		t.Fatal("external bind conflict accepted")
	}
	for _, name := range []string{"config.json", "enabled", "data/object"} {
		path := filepath.Join(r.legacy.dir, name)
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, []byte("legacy"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = r.Stop("", true); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(r.legacy.dir, "data")); !os.IsNotExist(err) {
		t.Fatal("legacy data remains")
	}
	if _, err = os.Stat(filepath.Join(r.legacy.dir, "fleets", "a", "config.json")); err != nil {
		t.Fatal(fmt.Errorf("legacy purge touched fleet: %w", err))
	}
}

func TestRegistryShutdownDrainsChildrenTogether(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "celld")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'celld 0.6.2'; exit; fi\ntrap '' TERM\necho ready > \"${CELLD_WATCH}-ready\"\nwhile :; do sleep 0.02; done\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(filepath.Join(dir, "state"), binary, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.startAfter = time.Time{}
	ids := []string{"a", "b", "c", "d", "e", "f"}
	if err = r.Sync(ids, ids); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		if _, err = r.Configure(context.Background(), fleetConfigFor(id, uint32(29000+i*2)), true); err != nil {
			t.Fatal(err)
		}
		r.fleets[id].stopGrace = 100 * time.Millisecond
	}
	eventually(t, func() bool {
		for _, id := range ids {
			if _, err := os.Stat(filepath.Join(r.fleets[id].dir, "data-ready")); err != nil {
				return false
			}
		}
		return true
	})
	before := time.Now()
	r.Close()
	if elapsed := time.Since(before); elapsed > 450*time.Millisecond {
		t.Fatalf("children drained serially: %s", elapsed)
	}
}
