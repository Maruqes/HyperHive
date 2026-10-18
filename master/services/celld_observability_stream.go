package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type observationTail struct {
	conn            *grpc.ClientConn
	cancel          context.CancelFunc
	fleet, node, ip string
	retryAt         time.Time
}

func (s *CelldService) ensureObservationTail(ctx context.Context, f *celldRecord, n CelldMachine, password string, conn *grpc.ClientConn) {
	o := s.observations
	key := f.ID + "/" + n.Name
	o.mu.Lock()
	if old := o.tails[key]; old != nil {
		if old.conn == conn && old.ip == n.IP && (old.retryAt.IsZero() || time.Now().Before(old.retryAt)) {
			o.mu.Unlock()
			return
		}
		old.cancel()
	}
	if o.tails[key] == nil && len(o.tails) >= 1024 {
		o.mu.Unlock()
		return
	}
	child, cancel := context.WithCancel(ctx)
	tail := &observationTail{conn: conn, cancel: cancel, fleet: f.ID, node: n.Name, ip: n.IP}
	o.tails[key] = tail
	o.mu.Unlock()
	go func() {
		defer cancel()
		delay := time.Second
		for child.Err() == nil {
			o.mu.Lock()
			ack := o.acks[key]
			o.mu.Unlock()
			call, stop := context.WithCancel(child)
			stream, err := pb.NewCelldServiceClient(conn).TailObservations(call, &pb.ObservationsRequest{FleetId: f.ID, Generation: ack.generation, Acknowledge: ack.sequence})
			if err == nil {
				for {
					batch, e := stream.Recv()
					err = e
					if err != nil {
						break
					}
					if !s.acceptObservation(f, n, password, batch, nil) {
						err = fmt.Errorf("live ingestion unavailable")
						break
					}
					// Free durable records promptly so a healthy stream does not
					// fill the bounded agent queue between metrics polls. Any
					// records returned by this ack stay unacknowledged and are
					// independently delivered by the tail.
					o.mu.Lock()
					durable := o.acks[key]
					o.mu.Unlock()
					ackCall, doneAck := context.WithTimeout(child, 3*time.Second)
					_, _ = pb.NewCelldServiceClient(conn).Observations(ackCall, &pb.ObservationsRequest{FleetId: f.ID, Generation: durable.generation, Acknowledge: durable.sequence})
					doneAck()
					delay = time.Second
				}
			}
			stop()
			// Older agents retain unary collection; retry capability after a backoff.
			if status.Code(err) == codes.Unimplemented || status.Code(err) == codes.FailedPrecondition {
				o.mu.Lock()
				if o.tails[key] == tail {
					tail.retryAt = time.Now().Add(time.Minute)
				}
				o.mu.Unlock()
				return
			}
			timer := time.NewTimer(delay)
			select {
			case <-child.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			delay = min(delay*2, 30*time.Second)
		}
	}()
}

type observationSubscriber struct {
	fleet string
	query ObservationQuery
	pages chan []StoredObservation
	done  chan struct{}
}

type ObservationSubscription struct {
	Records <-chan []StoredObservation
	Done    <-chan struct{}
	Cancel  func()
}

func observationMatches(row StoredObservation, q ObservationQuery) bool {
	r := row.Record
	if r == nil || r.TimeMs < q.From || q.Kind != "" && r.Kind != q.Kind || q.Node != "" && row.Node != q.Node || q.Workload != "" && r.Workload != q.Workload || q.Severity != "" && r.Severity != q.Severity || q.TraceID != "" && r.TraceId != q.TraceID || q.Text != "" && !strings.Contains(r.Body, q.Text) {
		return false
	}
	if q.Source == "process" {
		return r.Source == "stdout" || r.Source == "stderr"
	}
	return q.Source == "" || q.Source == r.Source
}

func (o *celldObservability) publish(fleet string, rows []StoredObservation) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for sub := range o.subscribers {
		if sub.fleet != fleet {
			continue
		}
		selected := make([]StoredObservation, 0, len(rows))
		for _, row := range rows {
			if observationMatches(row, sub.query) {
				selected = append(selected, row)
			}
		}
		if len(selected) == 0 {
			continue
		}
		select {
		case sub.pages <- selected:
		default:
			// Never hold up ingestion for a browser. Reconnect replays durable rows.
			delete(o.subscribers, sub)
			close(sub.done)
		}
	}
}

func (s *CelldService) SubscribeObservations(id string, q ObservationQuery) (*ObservationSubscription, error) {
	o, store, _, err := s.observationAccess(id)
	if err != nil {
		return nil, err
	}
	if o == nil || store == nil {
		return nil, fmt.Errorf("observability streaming unavailable")
	}
	q, err = normalizeObservationQuery(q, time.Now())
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	count := 0
	for sub := range o.subscribers {
		if sub.fleet == id {
			count++
		}
	}
	if len(o.subscribers) >= 128 || count >= 16 {
		return nil, fmt.Errorf("too many live log streams")
	}
	sub := &observationSubscriber{fleet: id, query: q, pages: make(chan []StoredObservation, 8), done: make(chan struct{})}
	o.subscribers[sub] = true
	return &ObservationSubscription{Records: sub.pages, Done: sub.done, Cancel: func() {
		o.mu.Lock()
		defer o.mu.Unlock()
		if o.subscribers[sub] {
			delete(o.subscribers, sub)
			close(sub.done)
		}
	}}, nil
}
