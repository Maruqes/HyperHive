package services

import (
	"512SvMan/protocol"
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	"google.golang.org/protobuf/proto"
)

func testObservationStore(t *testing.T) *observationStore {
	t.Helper()
	s, err := newObservationStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.reserve = 0
	s.space = func(string) (uint64, uint64, error) { return 1 << 40, 1 << 40, nil }
	return s
}
func observationBatch(now time.Time, sequence uint64, body string) *pb.ObservationBatch {
	return &pb.ObservationBatch{Generation: strings.Repeat("a", 32), Snapshot: &pb.ObservationSnapshot{FleetId: "a", NodeId: "node-a", CollectedMs: now.UnixMilli(), State: "fresh", Resources: &pb.ResourceSample{SampledMs: now.UnixMilli()}}, Records: []*pb.ObservationRecord{{Sequence: sequence, TimeMs: now.UnixMilli(), Kind: "log", Source: "otlp", Body: body, TraceId: strings.Repeat("b", 32)}}}
}
func TestObservationPersistenceDedupCursorIsolation(t *testing.T) {
	s := testObservationStore(t)
	now := time.Now().Add(-time.Minute)
	s.now = func() time.Time { return now }
	a := observationBatch(now, 1, "first")
	if err := s.append("a", "node-a", a); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	if err := s.append("a", "node-a", a); err != nil {
		t.Fatal(err)
	} // Retry after rotation.
	b := observationBatch(now, 2, "second")
	if err := s.append("a", "node-a", b); err != nil {
		t.Fatal(err)
	}
	if err := s.append("b", "node-a", b); err != nil {
		t.Fatal(err)
	}
	reopened, err := newObservationStore(s.root)
	if err != nil {
		t.Fatal(err)
	}
	reopened.now = s.now
	q := ObservationQuery{Kind: "log", From: now.Add(-3 * time.Hour).UnixMilli(), To: now.UnixMilli(), Limit: 1}
	page, err := reopened.query(context.Background(), "a", q)
	if err != nil || len(page.Records) != 1 || page.Records[0].Record.Body != "first" || page.NextCursor == "" {
		t.Fatalf("first page %+v %v", page, err)
	}
	q.Cursor = page.NextCursor
	q.To = 0
	page, err = reopened.query(context.Background(), "a", q)
	if err != nil || len(page.Records) != 1 || page.Records[0].Record.Body != "second" || page.NextCursor != "" {
		t.Fatalf("retry/cursor failed %+v %v", page, err)
	}
	if _, err = reopened.query(context.Background(), "b", q); err == nil {
		t.Fatal("cross fleet cursor accepted")
	}
	sn, err := reopened.latest(context.Background(), "a")
	if err != nil || len(sn) != 1 || sn[0].CollectedMs != now.UnixMilli() {
		t.Fatalf("latest snapshot recovery %v %v", sn, err)
	}
}
func TestObservationPhysicalRetentionQuotasAndReserve(t *testing.T) {
	s := testObservationStore(t)
	s.fleetBudget = 1 << 20
	s.globalBudget = 2 << 20
	s.segmentBytes = 64 << 10
	now := time.Now()
	s.now = func() time.Time { return now }
	if err := s.append("quiet", "node-a", observationBatch(now, 1, "quiet")); err != nil {
		t.Fatal(err)
	}
	for i := uint64(1); i < 24; i++ {
		now = now.Add(time.Hour)
		if err := s.append("noisy", "node-a", observationBatch(now, i, strings.Repeat("n", 7000))); err != nil {
			t.Fatal(err)
		}
	}
	quiet, err := s.query(context.Background(), "quiet", ObservationQuery{Kind: "log", From: now.Add(-2 * 24 * time.Hour).UnixMilli(), To: now.UnixMilli()})
	if err != nil || len(quiet.Records) != 1 {
		t.Fatal("noisy fleet pruned quiet fleet")
	}
	usage := s.storage("noisy")
	if usage.Bytes > s.fleetBudget || usage.GlobalBytes > s.globalBudget || usage.Pruned == 0 {
		t.Fatalf("physical quota failed %+v", usage)
	}
	files, _ := filepath.Glob(filepath.Join(s.root, "*", "*"))
	for _, file := range files {
		if strings.HasSuffix(file, "-wal") || strings.HasSuffix(file, "-journal") {
			t.Fatalf("unbounded journal retained: %s", file)
		}
	}
	s.space = func(string) (uint64, uint64, error) { return 100, 1000, nil }
	s.reserve = 200
	if err = s.append("a", "node-a", observationBatch(now, 99, "out of space")); err == nil {
		t.Fatal("free-space reserve ignored")
	}
	now = now.Add(8 * 24 * time.Hour)
	s.space = func(string) (uint64, uint64, error) { return 1 << 40, 1 << 40, nil }
	s.mu.Lock()
	err = s.cleanupLocked("", 0)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if usage = s.storage("noisy"); usage.GlobalBytes != 0 {
		t.Fatalf("age expiry left files %+v", usage)
	}
}
func TestObservationLateTimesCorruptionAndFilters(t *testing.T) {
	s := testObservationStore(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	b := observationBatch(now, 1, "first")
	if err := s.append("a", "node-a", b); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	b = observationBatch(now.Add(-3*time.Hour), 2, "late")
	if err := s.append("a", "node-a", b); err != nil {
		t.Fatal(err)
	}
	q := ObservationQuery{Kind: "log", From: now.Add(-6 * time.Hour).UnixMilli(), To: now.UnixMilli(), Limit: 1}
	page, err := s.query(context.Background(), "a", q)
	if err != nil || len(page.Records) != 1 || page.Records[0].Record.Body != "late" {
		t.Fatalf("late timestamp order %+v %v", page, err)
	}
	q.Text = "first"
	q.Limit = 200
	page, err = s.query(context.Background(), "a", q)
	if err != nil || len(page.Records) != 1 {
		t.Fatal("text filter failed")
	}
	corrupt := filepath.Join(s.root, "a", "1.sqlite")
	if err = os.WriteFile(corrupt, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	page, err = s.query(context.Background(), "a", q)
	if err != nil || page.Error == "" || len(page.Records) != 1 {
		t.Fatal("corrupt segment must expose a gap and retain readable results")
	}
}
func TestObservationEnvelopeSecretsAndMetadata(t *testing.T) {
	b := observationBatch(time.Now(), 1, "password=private key-secret")
	b.Records[0].Workload = "key-secret"
	b.Records[0].Attributes = map[string]string{"db.statement": "SELECT secret_value", "Authorization": "Bearer private"}
	if err := validateObservationBatch(b, "other", "node-a"); err == nil {
		t.Fatal("forged fleet accepted")
	}
	if err := validateObservationBatch(b, "a", "node-a", "key-secret"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.Records[0].Body, "private") || strings.Contains(b.Records[0].Body, "key-secret") || strings.Contains(b.Records[0].Workload, "key-secret") || len(b.Records[0].Attributes) != 0 {
		t.Fatal("secret leaked")
	}
	rows, err := parseObservationManifest([]byte(`{"schema_version":1,"script_name":"app","version":"v1","do_classes":["Counter","__Queue"],"required_features":["python-workers-v1"],"crons":["* * * * *"],"containers":[{"class_name":"Box","max_instances":3}],"raw_metadata":{"bindings":[{"type":"plain_text","name":"PASSWORD","text":"private"},{"type":"workflow","name":"FLOW","class_name":"Job"},{"type":"queue","name":"QUEUE"}]}}`))
	if err != nil || len(rows) != 6 || rows[0].Kind != "python" {
		t.Fatalf("metadata %+v %v", rows, err)
	}
	data, _ := json.Marshal(rows)
	if strings.Contains(string(data), "private") || strings.Contains(string(data), "__Queue") {
		t.Fatal("raw secret or internal cell counted separately")
	}
}
func TestObservationHistoryAfterFleetRemoval(t *testing.T) {
	service := newTestFleets(t)
	service.observations = newCelldObservability()
	store := testObservationStore(t)
	service.observations.store = store
	f, err := service.Create(fleetInput("a", "one"))
	if err != nil {
		t.Fatal(err)
	}
	b := observationBatch(time.Now(), 1, "retained")
	if err = store.append(f.ID, "node-a", b); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	delete(service.state.Fleets, f.ID)
	service.mu.Unlock()
	page, err := service.Observations(context.Background(), f.ID, ObservationQuery{Kind: "log"})
	if err != nil || len(page.Records) != 1 {
		t.Fatal("deleted fleet lost central history")
	}
	if _, err = service.Observations(context.Background(), "unknown", ObservationQuery{}); err != ErrCelldNotFound {
		t.Fatal("unknown historical ID resolved")
	}
}
func TestObservedRequestSummary(t *testing.T) {
	s := testObservationStore(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	b := observationBatch(now, 1, "request")
	b.Records[0].Kind = "span"
	b.Records[0].Failed = true
	b.Records[0].DurationUs = 500
	b.Records[0].Attributes = map[string]string{"span_kind": "SPAN_KIND_SERVER"}
	if err := s.append("a", "node-a", b); err != nil {
		t.Fatal(err)
	}
	retry := proto.Clone(b).(*pb.ObservationBatch)
	if err := s.append("a", "node-a", retry); err != nil {
		t.Fatal(err)
	}
	summary := s.requestSummary(context.Background(), "a", ObservationQuery{From: now.Add(-time.Minute).UnixMilli(), To: now.UnixMilli()})
	if summary.Count != 1 || summary.Errors != 1 || summary.MeanDurationUS != 500 || !summary.Incomplete || summary.Error != "" {
		t.Fatalf("observed summary %+v", summary)
	}
}

func TestObservationSlowReaderHasBoundedJournalAndFailure(t *testing.T) {
	s := testObservationStore(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	if err := s.append("a", "node-a", observationBatch(now, 1, "first")); err != nil {
		t.Fatal(err)
	}
	segs, _, _, err := s.segmentsLocked()
	if err != nil || len(segs) != 1 {
		t.Fatal("missing test segment")
	}
	db, err := openObservationDB(segs[0].path, s.segmentBytes, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRow("SELECT count(*) FROM observations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = s.append("a", "node-a", observationBatch(now.Add(time.Second), 2, "second"))
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("slow reader write did not fail within deadline: %v", err)
	}
	if info, e := os.Stat(segs[0].path + "-journal"); e == nil && info.Size() > s.segmentBytes {
		t.Fatal("rollback journal exceeded reserved segment bytes")
	}
	tx.Rollback()
	if err = s.append("a", "node-a", observationBatch(now.Add(time.Second), 2, "second")); err != nil {
		t.Fatalf("failed store did not recover: %v", err)
	}
}

type observationTestAgent struct {
	pb.UnimplementedCelldServiceServer
	mu      sync.Mutex
	batch   *pb.ObservationBatch
	ack     *pb.ObservationsRequest
	offline bool
}

func (a *observationTestAgent) Observations(_ context.Context, req *pb.ObservationsRequest) (*pb.ObservationBatch, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ack = proto.Clone(req).(*pb.ObservationsRequest)
	if a.offline {
		return nil, fmt.Errorf("offline")
	}
	return proto.Clone(a.batch).(*pb.ObservationBatch), nil
}
func TestObservationPollingAcknowledgementAndNodeLoss(t *testing.T) {
	s := newTestFleets(t)
	f, err := s.Create(fleetInput("a", "one"))
	if err != nil {
		t.Fatal(err)
	}
	store := testObservationStore(t)
	s.observations.store = store
	agent := &observationTestAgent{batch: observationBatch(time.Now(), 1, "retained")}
	agent.batch.Snapshot.FleetId = f.ID
	agent.batch.Snapshot.NodeId = "127.0.0.2"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	pb.RegisterCelldServiceServer(server, agent)
	go server.Serve(listener)
	defer server.Stop()
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s.connections = func() []protocol.ConnectionsStruct {
		return []protocol.ConnectionsStruct{{MachineName: "one", Addr: "127.0.0.2", Connection: conn}}
	}
	s.pollObservations(context.Background())
	s.pollObservations(context.Background())
	agent.mu.Lock()
	ack := agent.ack
	agent.offline = true
	agent.mu.Unlock()
	if ack.Generation != agent.batch.Generation || ack.Acknowledge != 1 {
		t.Fatalf("commit did not advance agent ack: %+v", ack)
	}
	s.pollObservations(context.Background())
	overview, err := s.Observability(context.Background(), f.ID)
	if err != nil || len(overview.Nodes) != 1 || overview.Nodes[0].State != "stale" || overview.Nodes[0].CollectedMs == 0 {
		t.Fatal("node loss erased last snapshot")
	}
	page, err := s.Observations(context.Background(), f.ID, ObservationQuery{Kind: "log"})
	if err != nil || len(page.Records) != 1 {
		t.Fatal("history lost after node loss or retry duplicated")
	}
	// Delete intent rejects further ingestion while retaining readable history.
	s.mu.Lock()
	s.state.Fleets[f.ID].DesiredState = "deleting"
	s.mu.Unlock()
	agent.mu.Lock()
	agent.offline = false
	agent.batch.Records[0].Sequence = 2
	agent.mu.Unlock()
	s.pollObservations(context.Background())
	page, err = s.Observations(context.Background(), f.ID, ObservationQuery{Kind: "log"})
	if err != nil || len(page.Records) != 1 {
		t.Fatal("deleting fleet accepted ingestion")
	}
}

func TestObservationLateSampleExpiresByEventTime(t *testing.T) {
	s := testObservationStore(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	b := observationBatch(now.Add(-6*24*time.Hour), 1, "late event")
	if err := s.append("a", "node-a", b); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * 24 * time.Hour)
	s.mu.Lock()
	err := s.cleanupLocked("", 0)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if s.storage("a").GlobalBytes != 0 {
		t.Fatal("late event retained past seven days because it arrived recently")
	}
}
