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

type celldTestTransport func(*http.Request) (*http.Response, error)

func (f celldTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFleetHealthRetainsMissingMachines(t *testing.T) {
	previous := celldHTTP
	t.Cleanup(func() { celldHTTP = previous })
	celldHTTP = &http.Client{Transport: celldTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Path != "/.well-known/celld/health" {
			t.Errorf("health performed an unexpected request: %s %s", r.Method, r.URL)
		}
		code := 200
		if r.URL.Hostname() == "192.168.76.2" {
			code = 503
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("true")), Header: make(http.Header)}, nil
	})}
	peers := map[string]celldDiagnostic{
		"192.168.76.1:8081": {Verdict: "ok"},
		"192.168.76.2:8081": {Verdict: "ok"},
	}
	nodes := []CelldMachine{{Name: "master", IP: "192.168.76.1"}, {Name: "unready", IP: "192.168.76.2"}, {Name: "missing", IP: "192.168.76.3", Connected: true, Healthy: true}}
	health := finishCelldHealth(context.Background(), CelldHealth{Storage: CelldCheck{Healthy: true}, Machines: nodes}, peers, nil)
	if health.Healthy || len(health.Machines) != 3 {
		t.Fatalf("missing node was dropped: %+v", health)
	}
	for _, n := range health.Machines {
		switch n.Name {
		case "master":
			if !n.Connected || !n.Healthy {
				t.Errorf("master is not healthy: %+v", n)
			}
		case "unready":
			if !n.Connected || n.Healthy || n.Error == "" {
				t.Errorf("unready peer: %+v", n)
			}
		case "missing":
			if n.Connected || n.Healthy || n.Error == "" {
				t.Errorf("missing peer: %+v", n)
			}
		}
	}
	health = finishCelldHealth(context.Background(), CelldHealth{Storage: CelldCheck{Healthy: true}, Machines: []CelldMachine{nodes[0]}}, peers, nil)
	if !health.Healthy {
		t.Fatalf("healthy fleet failed: %+v", health)
	}
	health.Storage.Healthy = false
	if finishCelldHealth(context.Background(), health, peers, nil).Healthy {
		t.Fatal("storage outage did not fail fleet health")
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
			cfg := &pb.Config{Endpoint: server.URL, Region: "us-east-1", AccessKey: "test", SecretKey: "secret"}
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
	cfg := &pb.Config{Endpoint: server.URL, Region: "us-east-1", AccessKey: "test", SecretKey: "secret"}
	if err := bootstrapCelld(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestSiloCompatibilityIntegration(t *testing.T) {
	endpoint := os.Getenv("CELLD_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set CELLD_TEST_ENDPOINT for a disposable SILO bucket")
	}
	cfg := &pb.Config{Endpoint: endpoint, Region: "us-east-1", AccessKey: "celld-test", SecretKey: "celld-test-secret"}
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
