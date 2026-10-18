package services

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type liveObservationAgent struct {
	pb.UnimplementedCelldServiceServer
	batches    chan *pb.ObservationBatch
	started    chan *pb.ObservationsRequest
	disconnect chan struct{}
}

func (a *liveObservationAgent) TailObservations(req *pb.ObservationsRequest, stream pb.CelldService_TailObservationsServer) error {
	a.started <- req
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-a.disconnect:
			return context.Canceled
		case b := <-a.batches:
			if err := stream.Send(b); err != nil {
				return err
			}
		}
	}
}
func TestObservationLiveTailPersistsPublishesAndResumes(t *testing.T) {
	s := newTestFleets(t)
	info, err := s.Create(fleetInput("a", "one"))
	if err != nil {
		t.Fatal(err)
	}
	store := testObservationStore(t)
	s.observations.store = store
	store.onAppend = s.observations.publish
	s.mu.RLock()
	f := s.state.Fleets[info.ID]
	n := f.Servers[0]
	s.mu.RUnlock()
	sub, err := s.SubscribeObservations(info.ID, ObservationQuery{Kind: "log", Source: "otlp"})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Cancel()
	agent := &liveObservationAgent{batches: make(chan *pb.ObservationBatch, 2), started: make(chan *pb.ObservationsRequest, 2), disconnect: make(chan struct{}, 1)}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterCelldServiceServer(server, agent)
	go server.Serve(listener)
	defer server.Stop()
	conn, err := grpc.NewClient("passthrough:///observations", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ensureObservationTail(ctx, f, n, "", conn)
	select {
	case <-agent.started:
	case <-time.After(time.Second):
		t.Fatal("tail not opened")
	}
	b := observationBatch(time.Now(), 1, "pushed console")
	b.Snapshot.FleetId = info.ID
	b.Snapshot.NodeId = n.IP
	agent.batches <- b
	select {
	case rows := <-sub.Records:
		if len(rows) != 1 || rows[0].Record.Body != "pushed console" {
			t.Fatalf("unexpected live rows: %+v", rows)
		}
		paths, _ := filepath.Glob(filepath.Join(store.root, info.ID, "*.sqlite"))
		if len(paths) != 1 {
			t.Fatal("published without a history segment")
		}
		db, e := openObservationDB(paths[0], store.segmentBytes, true)
		if e != nil {
			t.Fatal(e)
		}
		var count int
		e = db.QueryRow("SELECT count(*) FROM observations WHERE id=?", rows[0].ID).Scan(&count)
		db.Close()
		if e != nil || count != 1 {
			t.Fatal("published before durable persistence or ID mismatch")
		}
	case <-time.After(time.Second):
		t.Fatal("live delivery waited for unary polling")
	}
	agent.disconnect <- struct{}{}
	select {
	case req := <-agent.started:
		if req.Generation != b.Generation || req.Acknowledge != 1 {
			t.Fatalf("resume lost durable ack: %+v", req)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("tail did not reconnect")
	}
	// An old connection cannot ingest after its node is removed/replaced.
	s.mu.Lock()
	s.state.Fleets[info.ID].Servers[0].IP = "127.0.0.99"
	s.mu.Unlock()
	b.Records[0].Sequence = 2
	if s.acceptObservation(f, n, "", b, nil) {
		t.Fatal("removed node accepted streamed observations")
	}
}

func TestObservationLiveFanoutFiltersBoundsAndReplayReceiptTime(t *testing.T) {
	s := newTestFleets(t)
	f, err := s.Create(fleetInput("a", "one"))
	if err != nil {
		t.Fatal(err)
	}
	store := testObservationStore(t)
	s.observations.store = store
	store.onAppend = s.observations.publish
	app, err := s.SubscribeObservations(f.ID, ObservationQuery{Kind: "log", Source: "otlp"})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cancel()
	process, err := s.SubscribeObservations(f.ID, ObservationQuery{Kind: "log", Source: "process"})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Cancel()
	for i := 0; i < 9; i++ {
		s.observations.publish(f.ID, []StoredObservation{{Record: &pb.ObservationRecord{Kind: "log", Source: "stderr", TimeMs: time.Now().UnixMilli()}}})
	}
	select {
	case <-process.Done:
	default:
		t.Fatal("slow reader was not disconnected")
	}
	select {
	case <-app.Done:
		t.Fatal("unrelated process noise overflowed application stream")
	default:
	}
	now := time.Now()
	store.now = func() time.Time { return now }
	b := observationBatch(now.Add(-10*time.Minute), 1, "late application log")
	b.Snapshot.FleetId = f.ID
	if err = store.append(f.ID, "node-a", b); err != nil {
		t.Fatal(err)
	}
	select {
	case rows := <-app.Records:
		if len(rows) != 1 || rows[0].ReceivedMS != now.UnixMilli() {
			t.Fatal("receipt timestamp missing")
		}
	case <-time.After(time.Second):
		t.Fatal("committed log not published")
	}
	if err = store.append(f.ID, "node-a", b); err != nil {
		t.Fatal(err)
	}
	select {
	case <-app.Records:
		t.Fatal("retry published duplicate")
	default:
	}
	page, err := s.Observations(context.Background(), f.ID, ObservationQuery{Kind: "log", ReceivedAfter: now.UnixMilli(), From: now.Add(-time.Hour).UnixMilli(), To: now.UnixMilli()})
	if err != nil || len(page.Records) != 1 {
		t.Fatal("late record not replayed by receipt time")
	}
}
