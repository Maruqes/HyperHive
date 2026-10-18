package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/celld"
)

func TestParseCelldLivePeers(t *testing.T) {
	rows := `{"check":"bucket s3://hyperhive-celld","verdict":"ok"}
{"check":"peer dead","verdict":"skip","detail":"lease is expired"}
{"check":"peer a at 192.168.76.1:8081","verdict":"ok","detail":"restoring=0"}
{"check":"peer b at 192.168.76.2:8081","verdict":"fail","detail":"connection refused"}
{"check":"peer old-session at 192.168.76.1:8081","verdict":"fail","detail":"HTTP 409 Conflict"}`
	peers, err := parseCelldPeers(strings.NewReader(rows))
	if err != nil || len(peers) != 2 || peers["192.168.76.1:8081"].Verdict != "ok" || peers["192.168.76.2:8081"].Verdict != "fail" {
		t.Fatalf("wrong peers: %v, %v", peers, err)
	}
	if _, err = parseCelldPeers(strings.NewReader(rows + "\n{")); err == nil {
		t.Fatal("truncated diagnostics were accepted")
	}
}

func TestFleetHealthRetainsMissingMachines(t *testing.T) {
	s := newTestFleets(t)
	a, err := s.Create(fleetInput("a", "one", "two"))
	if err != nil {
		t.Fatal(err)
	}
	settled(s, a.ID)
	f := s.state.Fleets[a.ID]
	f.Status = "running"
	f.Servers[0].Running = true
	f.Servers[0].Healthy = true
	s.storage = CelldStorage{CelldCheck: CelldCheck{Healthy: true}, CheckedAt: time.Now().UTC()}
	health := s.Health(context.Background())
	if health.Healthy || len(health.Machines) != 2 || len(health.Fleets) != 1 || health.Fleets[0].Healthy {
		t.Fatalf("missing node was dropped from cached health: %+v", health)
	}
	f.Servers[1].Running = true
	f.Servers[1].Healthy = true
	if !s.Health(context.Background()).Healthy {
		t.Fatal("healthy running fleet failed")
	}
	f.DesiredState = "stopped"
	f.Servers[1].Healthy = false
	if !s.Health(context.Background()).Healthy {
		t.Fatal("stopped fleet affected health")
	}
	s.storage.Healthy = false
	if s.Health(context.Background()).Healthy {
		t.Fatal("storage outage did not fail health")
	}
}

func TestStorageCompatibility(t *testing.T) {
	for _, broken := range []string{"", "conditional-create", "conditional-update", "range", "consistency"} {
		t.Run(broken, func(t *testing.T) {
			var object string
			var revision int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case "PUT":
					if r.Header.Get("If-None-Match") == "*" && object != "" && broken != "conditional-create" {
						w.WriteHeader(412)
						return
					}
					if match := r.Header.Get("If-Match"); match != "" && match != fmt.Sprintf(`"%d"`, revision) && broken != "conditional-update" {
						w.WriteHeader(412)
						return
					}
					data, _ := io.ReadAll(r.Body)
					object = string(data)
					revision++
					w.Header().Set("ETag", fmt.Sprintf(`"%d"`, revision))
				case "GET":
					if r.Header.Get("Range") != "" && broken != "range" {
						w.Header().Set("Content-Range", "bytes 2-5/10")
						w.WriteHeader(206)
						fmt.Fprint(w, object[2:6])
						return
					}
					if broken == "consistency" {
						fmt.Fprint(w, "stale")
						return
					}
					fmt.Fprint(w, object)
				case "DELETE":
					object = ""
				default:
					t.Errorf("unexpected storage request: %s", r.Method)
				}
			}))
			defer server.Close()
			cfg := &pb.Config{Bucket: "s3://hyperhive-celld", Endpoint: server.URL, Region: "us-east-1", AccessKey: "test", SecretKey: "secret"}
			err := validateCelldStorage(context.Background(), cfg)
			if (err != nil) != (broken != "") {
				t.Fatalf("compatibility check for %q: %v", broken, err)
			}
			if object != "" {
				t.Fatal("probe object was not cleaned up")
			}
		})
	}
}

func TestBootstrapPreservesDeployment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/hyperhive-celld/deploy/current.json" {
			t.Errorf("bootstrap touched an existing deployment: %s %s", r.Method, r.URL)
		}
		fmt.Fprint(w, `{"version":"existing"}`)
	}))
	defer server.Close()
	cfg := &pb.Config{Bucket: "s3://hyperhive-celld", Endpoint: server.URL, Region: "us-east-1", AccessKey: "test", SecretKey: "secret"}
	if err := bootstrapCelld(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestSiloCompatibilityIntegration(t *testing.T) {
	endpoint := os.Getenv("CELLD_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set CELLD_TEST_ENDPOINT for a disposable SILO bucket")
	}
	cfg := &pb.Config{Bucket: "s3://hyperhive-celld", Endpoint: endpoint, Region: "us-east-1", AccessKey: "celld-test", SecretKey: "celld-test-secret"}
	if err := validateCelldStorage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	listing, err := celldS3(context.Background(), cfg, "GET", "?list-type=2&max-keys=0", nil, "")
	if err != nil || listing.code != 200 {
		t.Fatalf("health storage query: %v, %v", listing, err)
	}
	pointer, err := celldS3(context.Background(), cfg, "GET", "deploy/current.json", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if pointer.code == 200 {
		if err := bootstrapCelld(context.Background(), cfg); err != nil {
			t.Fatalf("existing deployment was not preserved: %v", err)
		}
	} else if pointer.code != 404 {
		t.Fatalf("read deployment pointer: HTTP %d", pointer.code)
	}
}

func TestFleetStorageIntegration(t *testing.T) {
	endpoint, container := os.Getenv("CELLD_TEST_ENDPOINT"), os.Getenv("CELLD_TEST_CONTAINER")
	if endpoint == "" || container == "" {
		t.Skip("run scripts/test_celld_fleet.sh")
	}
	previous := siloName
	siloName = container
	t.Cleanup(func() { siloName = previous })
	st := celldFleetState{RootUser: "hyperhive-test-admin", RootPassword: "hyperhive-test-admin-password"}
	ctx := context.Background()
	admin := &pb.Config{Endpoint: endpoint, Region: "us-east-1", AccessKey: st.RootUser, SecretKey: st.RootPassword}
	for _, id := range []string{"testa", "testb"} {
		f := &celldRecord{CelldFleetInfo: CelldFleetInfo{ID: id, Storage: CelldLocation{Bucket: "hyperhive-celld-" + id}}, AccessKey: "celld-" + id, SecretKey: "celld-" + id + "-secret"}
		if os.Getenv("CELLD_TEST_CLEANUP") == "1" {
			for i := 0; i < 2; i++ {
				if err := removeCelldStorage(ctx, st, f.Storage.Bucket, f.AccessKey); err != nil {
					t.Fatal(err)
				}
			}
			continue
		}
		for i := 0; i < 2; i++ {
			if err := ensureCelldStorage(ctx, st, f); err != nil {
				t.Fatal(err)
			}
		}
		cfg := &pb.Config{Bucket: "s3://" + f.Storage.Bucket, Endpoint: endpoint, Region: "us-east-1", AccessKey: f.AccessKey, SecretKey: f.SecretKey}
		if err := validateCelldStorage(ctx, cfg); err != nil {
			t.Fatal(err)
		}
		cfg.Bucket = "s3://hyperhive-celld"
		response, err := celldS3(ctx, cfg, "GET", "?list-type=2&max-keys=0", nil, "")
		if err != nil || response.code != 403 {
			t.Fatalf("fleet credentials escaped bucket scope: %+v %v", response, err)
		}
	}
	storage := observeCelldStorage(ctx, admin)
	expected := 3
	if os.Getenv("CELLD_TEST_CLEANUP") == "1" {
		expected = 1
	}
	if !storage.Healthy || storage.BucketCount == nil || *storage.BucketCount != expected {
		t.Fatalf("incorrect actual bucket counts: %+v", storage)
	}
}
