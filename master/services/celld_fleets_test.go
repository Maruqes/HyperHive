package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"512SvMan/protocol"
	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func newTestFleets(t *testing.T) *CelldService {
	t.Helper()
	s := NewCelldService(filepath.Join(t.TempDir(), "fleets.json"))
	if err := s.load(); err != nil {
		t.Fatal(err)
	}
	s.state.Machines = []CelldMachine{{Name: "one", IP: "127.0.0.2"}, {Name: "two", IP: "127.0.0.3"}, {Name: "three", IP: "127.0.0.4"}}
	return s
}
func fleetInput(name string, servers ...string) CelldFleetInput {
	return CelldFleetInput{Name: &name, Servers: &servers}
}
func settled(s *CelldService, id string) {
	s.state.Fleets[id].Operation = ""
	s.state.Fleets[id].Provisioned = true
}

func TestFleetValidationReservationsAndDurability(t *testing.T) {
	s := newTestFleets(t)
	for _, in := range []CelldFleetInput{fleetInput("", "one"), fleetInput("x"), fleetInput("x", "unknown"), fleetInput("x", "one", "one")} {
		if _, err := s.Create(in); !errors.Is(err, ErrCelldInvalid) {
			t.Fatalf("invalid input: %v", err)
		}
	}
	a, err := s.Create(fleetInput("a", "one", "two"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(fleetInput("b", "two", "three"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Ports == b.Ports || a.Storage.Bucket == b.Storage.Bucket {
		t.Fatal("overlapping fleets share storage/ports")
	}
	if _, err = s.Create(fleetInput("a", "three")); !errors.Is(err, ErrCelldConflict) {
		t.Fatal(err)
	}
	in := fleetInput("c", "one")
	in.Ports = &a.Ports
	if _, err = s.Create(in); !errors.Is(err, ErrCelldConflict) {
		t.Fatal("explicit reservation collision accepted")
	}
	if _, err = s.Update(a.ID, fleetInput("renamed", "one")); !errors.Is(err, ErrCelldConflict) {
		t.Fatal("in-flight edit accepted")
	}
	settled(s, a.ID)
	renamed := "renamed"
	after, err := s.Update(a.ID, CelldFleetInput{Name: &renamed})
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != a.ID || after.Storage.Bucket != a.Storage.Bucket {
		t.Fatal("rename moved bucket")
	}
	reloaded := NewCelldService(s.path)
	if err = reloaded.load(); err != nil {
		t.Fatal(err)
	}
	if reloaded.state.Fleets[a.ID].Operation != "edit" {
		t.Fatal("operation was not durable")
	}
	data, _ := json.Marshal(after)
	creds, _ := s.Credentials(a.ID)
	if strings.Contains(string(data), creds.SecretKey) || strings.Contains(string(data), creds.AccessKey) {
		t.Fatal("ordinary response contains credentials")
	}
	info, err := os.Stat(s.path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("state is not private: %v", err)
	}
	// A failed publication must not leave an accepted in-memory mutation.
	settled(s, a.ID)
	original := s.path
	s.path = filepath.Join(s.path, "invalid")
	if _, err = s.Action(a.ID, "stop"); err == nil {
		t.Fatal("unsaved stop accepted")
	}
	s.path = original
	if s.state.Fleets[a.ID].DesiredState != "running" {
		t.Fatal("failed save changed desired state")
	}
}

type fleetEvents struct {
	mu   sync.Mutex
	rows []string
}

func (e *fleetEvents) add(row string) { e.mu.Lock(); defer e.mu.Unlock(); e.rows = append(e.rows, row) }

type fleetAgent struct {
	pb.UnimplementedCelldServiceServer
	name   string
	events *fleetEvents
}

func (a *fleetAgent) Sync(context.Context, *pb.SyncRequest) (*pb.Empty, error) {
	return &pb.Empty{}, nil
}
func (a *fleetAgent) Configure(_ context.Context, r *pb.ConfigureRequest) (*pb.Status, error) {
	a.events.add(fmt.Sprintf("%s configure %s %v", a.name, r.Config.FleetId, r.Start))
	return &pb.Status{Running: r.Start}, nil
}
func (a *fleetAgent) Stop(_ context.Context, r *pb.Empty) (*pb.Status, error) {
	a.events.add(a.name + " stop " + r.FleetId)
	return &pb.Status{}, nil
}
func (a *fleetAgent) Remove(_ context.Context, r *pb.Empty) (*pb.Empty, error) {
	a.events.add(a.name + " remove " + r.FleetId)
	return &pb.Empty{}, nil
}
func testFleetConnection(t *testing.T, name string, events *fleetEvents) protocol.ConnectionsStruct {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	pb.RegisterCelldServiceServer(server, &fleetAgent{name: name, events: events})
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return protocol.ConnectionsStruct{MachineName: name, Addr: "127.0.0.2", Connection: conn}
}

func TestFleetMembershipStopAndInterruptedRestart(t *testing.T) {
	s := newTestFleets(t)
	events := &fleetEvents{}
	conns := []protocol.ConnectionsStruct{testFleetConnection(t, "one", events), testFleetConnection(t, "two", events)}
	s.connections = func() []protocol.ConnectionsStruct { return conns }
	s.ready = func(_ context.Context, cfg *pb.Config, ip string) error {
		events.add("ready " + cfg.FleetId)
		return nil
	}
	a, err := s.Create(fleetInput("a", "one"))
	if err != nil {
		t.Fatal(err)
	}
	settled(s, a.ID)
	b, err := s.Create(fleetInput("b", "one"))
	if err != nil {
		t.Fatal(err)
	}
	settled(s, b.ID)
	if _, err = s.Update(a.ID, fleetInput("a", "two")); err != nil {
		t.Fatal(err)
	}
	if err = s.reconcileFleet(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	events.mu.Lock()
	rows := strings.Join(events.rows, "\n")
	events.rows = nil
	events.mu.Unlock()
	if strings.Index(rows, "ready "+a.ID) > strings.Index(rows, "one remove "+a.ID) || !strings.Contains(rows, "one remove "+a.ID) {
		t.Fatalf("old node drained before new readiness: %s", rows)
	}
	if strings.Contains(rows, b.ID) {
		t.Fatal("membership edit affected other fleet")
	}
	if _, err = s.Action(a.ID, "stop"); err != nil {
		t.Fatal(err)
	}
	if err = s.reconcileFleet(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	name := "stopped-rename"
	ports := CelldPorts{19000, 19001}
	if _, err = s.Update(a.ID, CelldFleetInput{Name: &name, Ports: &ports}); err != nil {
		t.Fatal(err)
	}
	if err = s.reconcileFleet(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	info, _ := s.Get(a.ID)
	if info.Status != "stopped" || info.DesiredState != "stopped" {
		t.Fatalf("edit resumed stopped fleet: %+v", info)
	}
	if _, err = s.Action(a.ID, "restart"); err != nil {
		t.Fatal(err)
	}
	s.ready = func(context.Context, *pb.Config, string) error { return errors.New("not ready yet") }
	if err = s.reconcileFleet(context.Background(), a.ID); err == nil {
		t.Fatal("failed readiness completed restart")
	}
	// Reload midway after stop: retry must start this node without stopping it twice.
	resumed := NewCelldService(s.path)
	if err = resumed.load(); err != nil {
		t.Fatal(err)
	}
	resumed.connections = s.connections
	resumed.ready = func(context.Context, *pb.Config, string) error { return nil }
	events.mu.Lock()
	events.rows = nil
	events.mu.Unlock()
	if err = resumed.reconcileFleet(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	events.mu.Lock()
	defer events.mu.Unlock()
	if strings.Contains(strings.Join(events.rows, "\n"), " stop ") {
		t.Fatal("resumed restart repeated completed drain")
	}
}

func TestFleetDeletionWaitsForOfflineNodesAndRetriesCleanup(t *testing.T) {
	s := newTestFleets(t)
	events := &fleetEvents{}
	one := testFleetConnection(t, "one", events)
	two := testFleetConnection(t, "two", events)
	conns := []protocol.ConnectionsStruct{one}
	s.connections = func() []protocol.ConnectionsStruct { return conns }
	a, err := s.Create(fleetInput("a", "one", "two"))
	if err != nil {
		t.Fatal(err)
	}
	settled(s, a.ID)
	if _, err = s.Action(a.ID, "delete"); err != nil {
		t.Fatal(err)
	}
	purges := 0
	s.removeStorage = func(context.Context, celldFleetState, string, string) error {
		purges++
		return errors.New("policy cleanup unavailable")
	}
	if err = s.reconcileFleet(context.Background(), a.ID); err == nil || purges != 0 {
		t.Fatalf("offline delete purged storage: %v", err)
	}
	s.fleetError(a.ID, errors.New("offline"))
	resumed := NewCelldService(s.path)
	if err = resumed.load(); err != nil {
		t.Fatal(err)
	}
	conns = append(conns, two)
	resumed.connections = s.connections
	resumed.removeStorage = s.removeStorage
	if err = resumed.reconcileFleet(context.Background(), a.ID); err == nil || purges != 1 {
		t.Fatalf("cleanup failure not retained: %v", err)
	}
	if _, err = resumed.Get(a.ID); err != nil {
		t.Fatal("partial delete was reported complete")
	}
	events.mu.Lock()
	events.rows = nil
	events.mu.Unlock()
	resumed.removeStorage = func(context.Context, celldFleetState, string, string) error { return nil }
	if err = resumed.reconcileFleet(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = resumed.Get(a.ID); !errors.Is(err, ErrCelldNotFound) {
		t.Fatal("finished delete still exists")
	}
	events.mu.Lock()
	defer events.mu.Unlock()
	if len(events.rows) != 0 {
		t.Fatal("acknowledged agent cleanup repeated")
	}
}

func TestLegacyMigrationDoesNotEnrollFleets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy := `{"root_user":"admin","root_password":"private","access_key":"old-key","secret_key":"old-secret","machines":[{"name":"offline"}]}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewCelldService(path)
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Fleets) != 0 || !list.ResetPending || list.Storage.BucketCount != nil {
		t.Fatalf("invalid migrated state: %+v", list)
	}
	if _, err = s.Create(fleetInput("a", "offline")); !errors.Is(err, ErrCelldConflict) {
		t.Fatal("created fleet before legacy cleanup")
	}
	if err = s.resetLegacy(context.Background()); err == nil {
		t.Fatal("offline legacy node was ignored")
	}
	reloaded := NewCelldService(path)
	if err = reloaded.load(); err != nil {
		t.Fatal(err)
	}
	if !reloaded.state.ResetPending || reloaded.state.AccessKey != "old-key" {
		t.Fatal("reset lost cleanup state")
	}
}

func TestBucketListingNeverReportsIncompleteCounts(t *testing.T) {
	for _, body := range []string{
		`<ListAllMyBucketsResult><Buckets><Bucket><Name>a</Name></Bucket></Buckets></ListAllMyBucketsResult>`,
		`<ListAllMyBucketsResult><Buckets/><IsTruncated>true</IsTruncated></ListAllMyBucketsResult>`,
		`<ListAllMyBucketsResult><Buckets/><NextContinuationToken>more</NextContinuationToken></ListAllMyBucketsResult>`,
		`<ListAllMyBucketsResult>`, strings.Repeat("x", (2<<20)+1),
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				t.Error("count request scanned objects")
			}
			fmt.Fprint(w, body)
		}))
		result := observeCelldStorage(context.Background(), &pb.Config{Endpoint: server.URL, Region: "us-east-1", AccessKey: "test", SecretKey: "private"})
		server.Close()
		if strings.Contains(body, "<Name>a</Name>") {
			if !result.Healthy || result.BucketCount == nil || *result.BucketCount != 1 {
				t.Fatal(result)
			}
		} else {
			if result.Healthy || result.BucketCount != nil || result.Error == "" {
				t.Fatalf("incomplete count presented as valid: %+v", result)
			}
		}
	}
}

func TestFleetWorkDoesNotBlockOtherFleetsOrReads(t *testing.T) {
	s := newTestFleets(t)
	events := &fleetEvents{}
	conns := []protocol.ConnectionsStruct{testFleetConnection(t, "one", events), testFleetConnection(t, "two", events)}
	s.connections = func() []protocol.ConnectionsStruct { return conns }
	a, err := s.Create(fleetInput("a", "one"))
	if err != nil {
		t.Fatal(err)
	}
	settled(s, a.ID)
	b, err := s.Create(fleetInput("b", "two"))
	if err != nil {
		t.Fatal(err)
	}
	settled(s, b.ID)
	release := make(chan struct{})
	defer close(release)
	s.ready = func(ctx context.Context, cfg *pb.Config, _ string) error {
		if cfg.FleetId == a.ID {
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.scheduleFleets(ctx, make(chan struct{}, 4), true)
	deadline := time.Now().Add(3 * time.Second)
	for {
		info, err := s.Get(b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if info.Status == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("one waiting fleet blocked another")
		}
		time.Sleep(time.Millisecond)
	}
	s.mu.RLock()
	busy := s.busy[a.ID]
	s.mu.RUnlock()
	if !busy {
		t.Fatal("blocking readiness was not exercised")
	}
	if _, err := s.Update(a.ID, fleetInput("changed", "one")); !errors.Is(err, ErrCelldConflict) {
		t.Fatal("accepted an edit during running work")
	}
	// Join the workers before the test's gRPC servers and files disappear.
	cancel()
	for {
		s.mu.RLock()
		active := len(s.busy)
		s.mu.RUnlock()
		if active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("workers ignored cancellation")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMembershipRemovalDrainsOnlineNodesWhileOfflineRemovalWaits(t *testing.T) {
	s := newTestFleets(t)
	events := &fleetEvents{}
	conns := []protocol.ConnectionsStruct{testFleetConnection(t, "two", events), testFleetConnection(t, "three", events)}
	s.connections = func() []protocol.ConnectionsStruct { return conns }
	s.ready = func(context.Context, *pb.Config, string) error { return nil }
	a, err := s.Create(fleetInput("a", "one", "two", "three"))
	if err != nil {
		t.Fatal(err)
	}
	settled(s, a.ID)
	if _, err = s.Update(a.ID, fleetInput("a", "three")); err != nil {
		t.Fatal(err)
	}
	if err = s.reconcileFleet(context.Background(), a.ID); err == nil {
		t.Fatal("offline removal was reported complete")
	}
	s.mu.RLock()
	removed := s.state.Fleets[a.ID].Removed
	s.mu.RUnlock()
	if len(removed) != 1 || removed[0].Name != "one" {
		t.Fatalf("online removal blocked behind offline node: %+v", removed)
	}
	events.mu.Lock()
	defer events.mu.Unlock()
	if !strings.Contains(strings.Join(events.rows, "\n"), "two remove "+a.ID) {
		t.Fatal("online retiring node was not drained")
	}
}
