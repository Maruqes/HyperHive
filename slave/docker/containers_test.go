package docker

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
)

const engineFixture = `[
 {"Names":["/web"],"Image":"nginx:1","State":"running","Command":"cmd-secret","Labels":{"token":"label-secret"},
  "Ports":[{"IP":"0.0.0.0","PrivatePort":80,"PublicPort":8080,"Type":"tcp"},{"PrivatePort":443,"Type":"tcp"}],
  "NetworkSettings":{"Networks":{"z":{"IPAddress":"172.18.0.2"},"a":{"IPAddress":"172.17.0.2"}}}},
 {"Names":["/cell"],"Image":"cell","State":"running","Labels":{"celld.node":"n"}}
]`

func TestParseContainers(t *testing.T) {
	got, err := parseContainers([]byte(engineFixture))
	if err != nil {
		t.Fatal(err)
	}
	// Exact equality also proves labels and the command were not retained.
	want := []Container{{
		Name: "web", Image: "nginx:1", State: "running",
		Networks: []ContainerNetwork{{"a", "172.17.0.2"}, {"z", "172.18.0.2"}},
		Ports:    []ContainerPort{{"0.0.0.0", 80, 8080, "tcp"}, {"", 443, 0, "tcp"}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestListContainersReadsSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" || r.URL.Query().Get("all") != "true" {
			t.Errorf("unexpected request %s", r.URL)
		}
		io.WriteString(w, engineFixture)
	})}
	go server.Serve(listener)
	defer server.Close()
	t.Setenv("DOCKER_HOST", "unix://"+socket)
	containers, available := ListContainers(context.Background())
	if !available || len(containers) != 1 {
		t.Fatalf("available=%v containers=%+v", available, containers)
	}
}

func TestListContainersMissingSocket(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix://"+filepath.Join(t.TempDir(), "missing.sock"))
	res, err := (&Service{}).ListContainers(context.Background(), nil)
	if err != nil || res.Available || len(res.Containers) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
