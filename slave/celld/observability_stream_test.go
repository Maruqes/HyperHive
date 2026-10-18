package celld

import (
	"context"
	"testing"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	"google.golang.org/grpc"
)

type testTailStream struct {
	grpc.ServerStream
	ctx     context.Context
	batches chan *pb.ObservationBatch
}

func (s *testTailStream) Context() context.Context { return s.ctx }
func (s *testTailStream) Send(b *pb.ObservationBatch) error {
	select {
	case s.batches <- b:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}
func TestObservationTailNotifiesWithoutDestructiveAcknowledgement(t *testing.T) {
	m, err := New(t.TempDir(), "/unused", "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	r := &Registry{fleets: map[string]*Manager{"a": m}, allowed: map[string]bool{"a": true}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &testTailStream{ctx: ctx, batches: make(chan *pb.ObservationBatch, 4)}
	done := make(chan error, 1)
	go func() { done <- r.TailObservations(&pb.ObservationsRequest{FleetId: "a"}, stream) }()
	select {
	case <-stream.batches:
	case <-time.After(time.Second):
		t.Fatal("tail did not open")
	}
	for i := 0; i < 200; i++ {
		m.observations.add(&pb.ObservationRecord{Kind: "log", Source: "stdout", Body: "live"})
	}
	received := 0
	for received < 200 {
		select {
		case b := <-stream.batches:
			received += len(b.Records)
			if len(b.Records) > 128 || b.Snapshot.FleetId != "a" {
				t.Fatal("invalid streamed batch")
			}
		case <-time.After(time.Second):
			t.Fatalf("notification delivery stalled at %d records", received)
		}
	}
	m.observations.mu.Lock()
	retained := len(m.observations.records)
	m.observations.mu.Unlock()
	if retained != 200 {
		t.Fatal("tail discarded records before durable master acknowledgement")
	}
	// Reconnect skips already persisted records but keeps the agent queue intact.
	b, _ := m.observations.tail(199)
	if len(b.Records) != 1 || b.Records[0].Sequence != 200 {
		t.Fatal("resume sequence did not advance")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("tail leaked on cancellation")
	}
}

func TestObservationTailRejectsUnassignedFleet(t *testing.T) {
	r := &Registry{fleets: map[string]*Manager{}, allowed: map[string]bool{}}
	if err := r.TailObservations(&pb.ObservationsRequest{FleetId: "other"}, &testTailStream{ctx: context.Background()}); err == nil {
		t.Fatal("unassigned stream accepted")
	}
}
