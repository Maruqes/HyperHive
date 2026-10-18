package celld

import (
	"context"
	"encoding/json"
	"fmt"
	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Real overlapping fleets, independent deployments/data and authoritative reconnect.
func TestOverlappingFleetsIntegration(t *testing.T) {
	binary, endpoint := os.Getenv("CELLD_TEST_BINARY"), os.Getenv("CELLD_TEST_ENDPOINT")
	if binary == "" || endpoint == "" {
		t.Skip("run scripts/test_celld_fleet.sh")
	}
	t.Setenv("CELLD_TOKIO_THREADS", "2")
	ctx := context.Background()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	a, b := fleetConfigFor("testa", 18080), fleetConfigFor("testb", 18082)
	for _, c := range []*pb.Config{a, b} {
		c.Endpoint = endpoint
		c.AccessKey = "celld-" + c.FleetId
		c.SecretKey = c.AccessKey + "-secret"
	}
	var nodes []*Registry
	for _, ip := range []string{"127.0.0.2", "127.0.0.3"} {
		r, err := NewRegistry(t.TempDir(), binary, ip)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(r.Close)
		nodes = append(nodes, r)
		if err = r.Sync([]string{"testa", "testb"}, []string{"testa", "testb"}); err != nil {
			t.Fatal(err)
		}
		for _, c := range []*pb.Config{a, b} {
			if _, err = r.Configure(ctx, c, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	ready := func(ip string, port uint32) bool {
		response, err := client.Get("http://" + net.JoinHostPort(ip, fmt.Sprint(port)) + "/.well-known/celld/health")
		if err != nil {
			return false
		}
		response.Body.Close()
		return response.StatusCode == 200
	}
	wait := func(ip string, port uint32) {
		t.Helper()
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			if ready(ip, port) {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		for _, r := range nodes {
			for _, id := range []string{"testa", "testb"} {
				data, _ := os.ReadFile(filepath.Join(r.legacy.dir, "fleets", id, "logs/celld.log"))
				t.Logf("%s/%s: %s", r.legacy.ip, id, data)
			}
		}
		t.Fatalf("%s:%d never ready", ip, port)
	}
	for _, r := range nodes {
		wait(r.legacy.ip, a.HttpPort)
		wait(r.legacy.ip, b.HttpPort)
	}
	counter := func(ip string, port uint32, fleet string, want int) {
		t.Helper()
		response, err := client.Get("http://" + net.JoinHostPort(ip, fmt.Sprint(port)) + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var result struct {
			Fleet string
			Count int
		}
		if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatalf("%s counter: %v", fleet, err)
		}
		if result.Fleet != fleet || result.Count != want {
			t.Fatalf("independent %s data: %+v, want %d", fleet, result, want)
		}
	}
	counter("127.0.0.2", a.HttpPort, "a", 1)
	counter("127.0.0.2", b.HttpPort, "b", 1)
	counter("127.0.0.3", a.HttpPort, "a", 2)
	counter("127.0.0.3", b.HttpPort, "b", 2)
	if err := nodes[0].Stop("testa", false); err != nil {
		t.Fatal(err)
	}
	if ready(nodes[0].legacy.ip, a.HttpPort) || !ready(nodes[0].legacy.ip, b.HttpPort) {
		t.Fatal("stop crossed fleet boundary")
	}
	if _, err := nodes[0].Configure(ctx, a, true); err != nil {
		t.Fatal(err)
	}
	wait(nodes[0].legacy.ip, a.HttpPort)
	counter("127.0.0.2", a.HttpPort, "a", 3)
	counter("127.0.0.2", b.HttpPort, "b", 3)
	conflict := fleetConfigFor("conflict", a.HttpPort)
	conflict.Endpoint = endpoint
	if err := nodes[0].Sync([]string{"testa", "testb", "conflict"}, []string{"testa", "testb", "conflict"}); err != nil {
		t.Fatal(err)
	}
	if _, err := nodes[0].Configure(ctx, conflict, true); err == nil {
		t.Fatal("conflicting fleet port accepted")
	}
	// Both stopped and removed fleets remain down after an agent restart.
	old := nodes[0]
	old.Close()
	resumed, err := NewRegistry(old.legacy.dir, binary, old.legacy.ip)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	nodes[0] = resumed
	if resumed.Status("testa").Running || resumed.Status("testb").Running {
		t.Fatal("children resumed from stale disk")
	}
	if err = resumed.Sync([]string{"testa", "testb"}, []string{"testb"}); err != nil {
		t.Fatal(err)
	}
	if _, err = resumed.Configure(ctx, a, true); err == nil {
		t.Fatal("stopped fleet resumed")
	}
	if _, err = resumed.Configure(ctx, b, true); err != nil {
		t.Fatal(err)
	}
	wait(resumed.legacy.ip, b.HttpPort)
	for _, r := range nodes {
		if err = r.Stop("testa", true); err != nil {
			t.Fatal(err)
		}
		if _, err = os.Stat(filepath.Join(r.legacy.dir, "fleets", "testa")); !os.IsNotExist(err) {
			t.Fatal("fleet local data not purged")
		}
	}
	counter("127.0.0.3", b.HttpPort, "b", 4)
	for _, r := range nodes {
		if err = r.Stop("testb", true); err != nil {
			t.Fatal(err)
		}
	}
}
