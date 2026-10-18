package celld

import (
	"fmt"
	"time"

	limits "github.com/Maruqes/512SvMan/api/celldobserve"
	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// tail reads without acknowledging: only a later durable master acknowledgement
// may remove records. Unary collection remains the compatibility/ack path.
func (o *observationBuffer) tail(after uint64) (*pb.ObservationBatch, <-chan struct{}) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.changed == nil {
		o.changed = make(chan struct{})
	}
	b := &pb.ObservationBatch{Generation: o.generation, Dropped: o.dropped.Load()}
	if o.snapshot != nil {
		b.Snapshot = proto.Clone(o.snapshot).(*pb.ObservationSnapshot)
	}
	size := proto.Size(b)
	for _, r := range o.records {
		if r.Sequence <= after {
			continue
		}
		n := proto.Size(r) + 8
		if len(b.Records) >= limits.MaxRecords || size+n > limits.MaxBatchBytes {
			break
		}
		b.Records = append(b.Records, proto.Clone(r).(*pb.ObservationRecord))
		size += n
	}
	return b, o.changed
}

func (r *Registry) TailObservations(req *pb.ObservationsRequest, stream pb.CelldService_TailObservationsServer) error {
	if !fleetIDPattern.MatchString(req.FleetId) {
		return status.Error(codes.InvalidArgument, "invalid fleet ID")
	}
	r.gate.RLock()
	r.mu.Lock()
	m := r.fleets[req.FleetId]
	allowed := r.allowed[req.FleetId]
	r.mu.Unlock()
	r.gate.RUnlock()
	if m == nil || !allowed {
		return status.Error(codes.NotFound, "fleet is not assigned")
	}
	if m.observations.disabled {
		return status.Error(codes.FailedPrecondition, "collection disabled")
	}
	// The opening cursor is a durable master acknowledgement from a previous stream.
	m.observations.batch(req)
	after := uint64(0)
	if req.Generation == m.observations.generation {
		after = req.Acknowledge
	}
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	first := true
	for {
		r.gate.RLock()
		r.mu.Lock()
		valid := r.fleets[req.FleetId] == m && r.allowed[req.FleetId]
		r.mu.Unlock()
		r.gate.RUnlock()
		if !valid {
			return status.Error(codes.NotFound, "fleet assignment changed")
		}
		b, changed := m.observations.tail(after)
		if b.Snapshot == nil {
			b.Snapshot = &pb.ObservationSnapshot{FleetId: req.FleetId, NodeId: m.ip, State: "pending", Capabilities: baseCapabilities()}
		}
		if first || len(b.Records) > 0 {
			if err := stream.Send(b); err != nil {
				return err
			}
			for _, record := range b.Records {
				if record.Sequence > after {
					after = record.Sequence
				}
			}
			first = false
			if len(b.Records) > 0 {
				continue
			}
		}
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-m.life.Done():
			return fmt.Errorf("fleet manager closed")
		case <-changed:
			// Coalesce bursts without tying delivery to the metrics interval.
			timer := time.NewTimer(20 * time.Millisecond)
			select {
			case <-stream.Context().Done():
				timer.Stop()
				return stream.Context().Err()
			case <-timer.C:
			}
		case <-tick.C:
			first = true
		}
	}
}

func (*Service) TailObservations(req *pb.ObservationsRequest, stream pb.CelldService_TailObservationsServer) error {
	if Local == nil {
		return status.Error(codes.Unavailable, "celld manager is not ready")
	}
	return Local.TailObservations(req, stream)
}
