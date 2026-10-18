package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	limits "github.com/Maruqes/512SvMan/api/celldobserve"
	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type observationAck struct {
	generation string
	sequence   uint64
}
type celldObservability struct {
	streaming   bool
	tails       map[string]*observationTail
	subscribers map[*observationSubscriber]bool
	mu          sync.Mutex
	ingest      sync.RWMutex
	store       *observationStore
	cache       map[string]map[string]*pb.ObservationSnapshot
	acks        map[string]observationAck
	deployments map[string]ObservationDeployment
	error       string
}
type ObservationDeployment struct {
	CollectedMS int64                `json:"collected_ms"`
	State       string               `json:"state"`
	Error       string               `json:"error,omitempty"`
	Workloads   []*pb.WorkloadSample `json:"workloads"`
}
type FleetObservability struct {
	FleetID    string                    `json:"fleet_id"`
	Historical bool                      `json:"historical"`
	Nodes      []*pb.ObservationSnapshot `json:"nodes"`
	Deployment ObservationDeployment     `json:"deployment"`
	Storage    ObservationStorage        `json:"storage"`
	Error      string                    `json:"error,omitempty"`
	CPU        string                    `json:"cpu_aggregation"`
	Incomplete bool                      `json:"incomplete"`
}

func newCelldObservability() *celldObservability {
	return &celldObservability{tails: map[string]*observationTail{}, subscribers: map[*observationSubscriber]bool{}, cache: map[string]map[string]*pb.ObservationSnapshot{}, acks: map[string]observationAck{}, deployments: map[string]ObservationDeployment{}}
}
func (s *CelldService) observationLoop(ctx context.Context) {
	o := s.observations
	if o == nil || os.Getenv("CELLD_OBSERVABILITY") == "0" {
		return
	}
	root := filepath.Join(filepath.Dir(s.path), "celld-observability")
	store, err := newObservationStore(root)
	o.mu.Lock()
	o.store = store
	o.streaming = true
	if store != nil {
		store.onAppend = o.publish
	}
	if err != nil {
		o.error = "observability storage unavailable"
	}
	o.mu.Unlock()
	go s.deploymentObservationLoop(ctx)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if store == nil {
			store, err = newObservationStore(root)
			o.mu.Lock()
			o.store = store
			if store != nil {
				store.onAppend = o.publish
			}
			if err != nil {
				o.error = "observability storage unavailable"
			}
			o.mu.Unlock()
		}
		s.pollObservations(ctx)
		if store != nil {
			store.mu.Lock()
			err = store.cleanupLocked("", 0)
			if err != nil {
				store.lastError = "retention cleanup failed"
			}
			store.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (s *CelldService) pollObservations(ctx context.Context) {
	if err := s.load(); err != nil {
		return
	}
	s.mu.RLock()
	st := s.copyLocked()
	s.mu.RUnlock()
	o := s.observations
	if o == nil {
		return
	}
	connections := s.snapshotConnections()
	type job struct {
		f *celldRecord
		n CelldMachine
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range jobs {
				key := task.f.ID + "/" + task.n.Name
				o.mu.Lock()
				ack := o.acks[key]
				streaming := o.streaming
				o.mu.Unlock()
				var batch *pb.ObservationBatch
				var err error
				found := false
				for _, conn := range connections {
					if conn.MachineName == task.n.Name && conn.Addr == task.n.IP && conn.Connection != nil {
						found = true
						if streaming {
							s.ensureObservationTail(ctx, task.f, task.n, st.RootPassword, conn.Connection)
						}
						call, cancel := context.WithTimeout(ctx, 3*time.Second)
						batch, err = pb.NewCelldServiceClient(conn.Connection).Observations(call, &pb.ObservationsRequest{FleetId: task.f.ID, Generation: ack.generation, Acknowledge: ack.sequence})
						cancel()
						break
					}
				}
				if !found {
					err = fmt.Errorf("node is offline")
				}
				s.acceptObservation(task.f, task.n, st.RootPassword, batch, err)
			}
		}()
	}
	count := 0
outer:
	for _, f := range st.Fleets {
		if f.DesiredState == "deleting" {
			continue
		}
		for _, n := range f.Servers {
			if count >= 1024 {
				break outer
			}
			select {
			case jobs <- job{f, n}:
				count++
			case <-ctx.Done():
				break outer
			}
		}
	}
	close(jobs)
	wg.Wait()

	o.mu.Lock()
	for key, tail := range o.tails {
		f := st.Fleets[tail.fleet]
		valid := false
		if f != nil && f.DesiredState != "deleting" {
			for _, n := range f.Servers {
				if n.Name == tail.node && n.IP == tail.ip {
					valid = true
					break
				}
			}
		}
		if !valid {
			tail.cancel()
			delete(o.tails, key)
		}
	}
	for id := range o.cache {
		if st.Fleets[id] == nil {
			delete(o.cache, id)
			delete(o.deployments, id)
			for key := range o.acks {
				if strings.HasPrefix(key, id+"/") {
					delete(o.acks, key)
				}
			}
		}
	}
	o.mu.Unlock()
}
func validateObservationBatch(b *pb.ObservationBatch, fleet, node string, secrets ...string) error {
	if b == nil || b.Snapshot == nil || b.Snapshot.FleetId != fleet || b.Snapshot.NodeId != node || len(b.Generation) != 32 || len(b.Records) > limits.MaxRecords || proto.Size(b) > limits.MaxBatchBytes || len(b.Snapshot.Workloads) > limits.MaxWorkloads || len(b.Snapshot.Capabilities) > 32 {
		return fmt.Errorf("invalid observation envelope")
	}
	// The trusted connection supplies identity; reject rather than use a payload's
	// claimed identity. Never accept arbitrary attributes across the trust boundary.
	b.Snapshot.Error = limits.Sanitize(b.Snapshot.Error, secrets...)
	last := uint64(0)
	for _, r := range b.Records {
		if r == nil || r.Sequence <= last || proto.Size(r) > limits.MaxRecordBytes+2048 || len(r.Attributes) > 20 || len(r.Source) > 32 || len(r.Workload) > 256 || len(r.Severity) > 16 || r.TraceId != "" && !observationTraceID.MatchString(r.TraceId) {
			return fmt.Errorf("invalid observation record")
		}
		last = r.Sequence
		r.Body = limits.Sanitize(r.Body, secrets...)
		r.Workload = limits.Sanitize(r.Workload, secrets...)
		for k, v := range r.Attributes {
			switch k {
			case "cell", "cell_id", "isolate", "script", "script_name", "request_id", "http.status_code", "http.response.status_code", "http.request.method", "http.method", "celld.cell", "celld.isolate", "celld.script", "celld.request_id", "error.type", "kind", "span_kind", "process_generation", "container_id", "class", "stream":
				r.Attributes[k] = limits.Sanitize(v, secrets...)
			default:
				delete(r.Attributes, k)
			}
		}
	}
	for _, w := range b.Snapshot.Workloads {
		if w == nil {
			return fmt.Errorf("invalid workload")
		}
		w.Id = limits.Sanitize(w.Id, secrets...)
		w.Parent = limits.Sanitize(w.Parent, secrets...)
		w.Version = limits.Sanitize(w.Version, secrets...)
	}
	return nil
}

var observationPrefix = regexp.MustCompile(`^deploy/[a-zA-Z0-9_-]{1,128}/[a-f0-9]{1,128}$`)

func readObservationDeployment(ctx context.Context, cfg *pb.Config) ObservationDeployment {
	d := ObservationDeployment{State: "unavailable", Workloads: []*pb.WorkloadSample{}}
	fail := func() ObservationDeployment { d.Error = "bounded deployment metadata unavailable"; return d }
	current, err := celldS3(ctx, cfg, "GET", "deploy/current.json", nil, "")
	if err != nil || current.code != 200 || len(current.body) > 16<<10 {
		return fail()
	}
	var pointer struct {
		Prefix string `json:"prefix"`
	}
	if json.Unmarshal([]byte(current.body), &pointer) != nil || !observationPrefix.MatchString(pointer.Prefix) {
		return fail()
	}
	response, err := celldS3(ctx, cfg, "GET", pointer.Prefix+"/manifest.json", nil, "")
	if err != nil || response.code != 200 || len(response.body) > 256<<10 {
		return fail()
	}
	workloads, err := parseObservationManifest([]byte(response.body))
	if err != nil {
		return fail()
	}
	d.Workloads = workloads
	d.State = "fresh"
	d.CollectedMS = time.Now().UnixMilli()
	return d
}
func parseObservationManifest(data []byte) ([]*pb.WorkloadSample, error) {
	var m struct {
		Schema     int             `json:"schema_version"`
		Script     string          `json:"script_name"`
		Version    string          `json:"version"`
		Classes    []string        `json:"do_classes"`
		Features   []string        `json:"required_features"`
		Crons      []string        `json:"crons"`
		Assets     json.RawMessage `json:"assets"`
		Containers []struct {
			Class string  `json:"class_name"`
			Max   *uint64 `json:"max_instances"`
		} `json:"containers"`
		Consumers []struct {
			Queue string `json:"queue"`
		} `json:"queue_consumers"`
		Metadata struct {
			Bindings []struct {
				Type    string `json:"type"`
				Name    string `json:"name"`
				Class   string `json:"class_name"`
				Service string `json:"service"`
			} `json:"bindings"`
		} `json:"raw_metadata"`
	}
	if len(data) > 256<<10 || json.Unmarshal(data, &m) != nil || m.Script == "" || m.Schema > 2 {
		return nil, fmt.Errorf("unsupported deployment manifest")
	}
	rows := []*pb.WorkloadSample{}
	add := func(kind, id, parent, state string, limit *uint64) {
		if len(rows) <= limits.MaxWorkloads {
			rows = append(rows, &pb.WorkloadSample{Kind: kind, Id: limits.Sanitize(id), Parent: limits.Sanitize(parent), Version: limits.Sanitize(m.Version), State: state, Limit: limit})
		}
	}
	kind := "worker"
	for _, f := range m.Features {
		if f == "python-workers-v1" {
			kind = "python"
		}
	}
	add(kind, m.Script, "", "configured", nil)
	for _, class := range m.Classes {
		if strings.HasPrefix(class, "__") {
			continue
		}
		add("cell_class", class, m.Script, "configured", nil)
	}
	for _, b := range m.Metadata.Bindings {
		switch b.Type {
		case "service", "workflow", "queue", "d1", "kv_namespace", "r2_bucket", "assets", "worker_loader", "durable_object_namespace":
			add(b.Type, b.Name, m.Script, "configured", nil)
		}
	}
	for _, q := range m.Consumers {
		add("queue_consumer", q.Queue, m.Script, "configured", nil)
	}
	for _, cron := range m.Crons {
		add("cron", cron, m.Script, "configured", nil)
	}
	for _, c := range m.Containers {
		add("container_class", c.Class, m.Script, "configured", c.Max)
	}
	if len(m.Assets) > 0 && string(m.Assets) != "null" {
		add("assets", m.Script+"/assets", m.Script, "configured", nil)
	}
	if len(rows) > limits.MaxWorkloads {
		return nil, fmt.Errorf("deployment cardinality exceeds limit")
	}
	return rows, nil
}
func (s *CelldService) observationAccess(id string) (*celldObservability, *observationStore, bool, error) {
	if !observationFleetID.MatchString(id) {
		return nil, nil, false, ErrCelldNotFound
	}
	if err := s.load(); err != nil {
		return nil, nil, false, err
	}
	s.mu.RLock()
	exists := s.state.Fleets[id] != nil
	s.mu.RUnlock()
	o := s.observations
	if o == nil {
		if !exists {
			return nil, nil, false, ErrCelldNotFound
		}
		return nil, nil, false, nil
	}
	o.mu.Lock()
	store := o.store
	o.mu.Unlock()
	if !exists && (store == nil || !store.hasFleet(id)) {
		return nil, nil, false, ErrCelldNotFound
	}
	return o, store, !exists, nil
}
func (s *CelldService) Observability(ctx context.Context, id string) (FleetObservability, error) {
	result := FleetObservability{FleetID: id, Nodes: []*pb.ObservationSnapshot{}, CPU: "per-node process percentages; not averaged; 100% equals one CPU core", Incomplete: true, Deployment: ObservationDeployment{State: "pending", Workloads: []*pb.WorkloadSample{}}}
	o, store, historical, err := s.observationAccess(id)
	if err != nil {
		return result, err
	}
	result.Historical = historical
	if o != nil {
		o.mu.Lock()
		for _, sn := range o.cache[id] {
			result.Nodes = append(result.Nodes, proto.Clone(sn).(*pb.ObservationSnapshot))
		}
		if d, ok := o.deployments[id]; ok {
			result.Deployment = d
		}
		result.Error = o.error
		o.mu.Unlock()
	}
	if store != nil {
		result.Storage = store.storage(id)
		if len(result.Nodes) == 0 {
			result.Nodes, _ = store.latest(ctx, id)
		}
	} else {
		result.Error = "observability collector has not initialized storage"
	}
	if !historical {
		s.mu.RLock()
		fleet := s.state.Fleets[id]
		if fleet != nil {
			for _, n := range fleet.Servers {
				found := false
				for _, sn := range result.Nodes {
					if sn.NodeId == n.IP {
						found = true
						break
					}
				}
				if !found {
					result.Nodes = append(result.Nodes, &pb.ObservationSnapshot{FleetId: id, NodeId: n.IP, State: "pending", Error: "no successful collection"})
				}
			}
		}
		s.mu.RUnlock()
	}
	now := time.Now().UnixMilli()
	for _, sn := range result.Nodes {
		if sn.CollectedMs > 0 && now-sn.CollectedMs > 15000 {
			sn.State = "stale"
		}
	}
	sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].NodeId < result.Nodes[j].NodeId })
	return result, nil
}
func (s *CelldService) Observations(ctx context.Context, id string, q ObservationQuery) (ObservationPage, error) {
	_, store, _, err := s.observationAccess(id)
	if err != nil {
		return ObservationPage{}, err
	}
	q, err = normalizeObservationQuery(q, time.Now())
	if err != nil {
		return ObservationPage{}, err
	}
	if store == nil {
		return ObservationPage{Records: []StoredObservation{}, From: q.From, To: q.To, Incomplete: true, Error: "observability storage unavailable"}, nil
	}
	return store.query(ctx, id, q)
}

type ObservationMetrics struct {
	Samples  ObservationPage  `json:"samples"`
	Requests ObservedRequests `json:"requests"`
}

func (s *CelldService) ObservationMetrics(ctx context.Context, id string, q ObservationQuery) (ObservationMetrics, error) {
	result := ObservationMetrics{Requests: ObservedRequests{Observed: true, Incomplete: true}}
	_, store, _, err := s.observationAccess(id)
	if err != nil {
		return result, err
	}
	q, err = normalizeObservationQuery(q, time.Now())
	if err != nil {
		return result, err
	}
	q.Kind = "metric"
	result.Samples, err = s.Observations(ctx, id, q)
	if err != nil {
		return result, err
	}
	if store != nil {
		result.Requests = store.requestSummary(ctx, id, q)
	} else {
		result.Requests.Error = "summary unavailable"
	}
	return result, nil
}

func (s *CelldService) deploymentObservationLoop(ctx context.Context) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.mu.RLock()
		st := s.copyLocked()
		s.mu.RUnlock()
		o := s.observations
		// One bounded deployment read per fleet, separate from HTTP readers.
		count := 0
		for _, f := range st.Fleets {
			if count >= 64 {
				break
			}
			if f.DesiredState == "deleting" || !f.Provisioned {
				continue
			}
			count++
			o.mu.Lock()
			prior := o.deployments[f.ID]
			o.mu.Unlock()
			if time.Now().UnixMilli()-prior.CollectedMS < 30000 {
				continue
			}
			call, cancel := context.WithTimeout(ctx, 2*time.Second)
			deployment := readObservationDeployment(call, recordConfig(st, f))
			cancel()
			o.mu.Lock()
			if deployment.State != "fresh" && prior.CollectedMS > 0 {
				prior.State = "stale"
				prior.Error = deployment.Error
				deployment = prior
			}
			o.deployments[f.ID] = deployment
			o.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Both live delivery and unary recovery use the same validation and durable ack.
func (s *CelldService) acceptObservation(f *celldRecord, n CelldMachine, password string, batch *pb.ObservationBatch, err error) bool {
	o := s.observations
	key := f.ID + "/" + n.Name
	o.mu.Lock()
	store := o.store
	o.mu.Unlock()
	if err == nil {
		err = validateObservationBatch(batch, f.ID, n.IP, f.AccessKey, f.SecretKey, password)
	}
	if err != nil {
		o.mu.Lock()
		nodes := o.cache[f.ID]
		if nodes == nil {
			nodes = map[string]*pb.ObservationSnapshot{}
			o.cache[f.ID] = nodes
		}
		sn := nodes[n.Name]
		if sn == nil {
			sn = &pb.ObservationSnapshot{FleetId: f.ID, NodeId: n.IP, State: "offline"}
		} else {
			sn = proto.Clone(sn).(*pb.ObservationSnapshot)
			sn.State = "stale"
		}
		sn.AttemptedMs = time.Now().UnixMilli()
		sn.Error = "node observations unavailable"
		if status.Code(err) == codes.Unimplemented {
			sn.State = "unsupported"
			sn.Error = "agent does not support observations; update the agent"
			sn.Capabilities = []*pb.Capability{{Name: "agent_observability", State: "unsupported", Detail: sn.Error}}
		}
		nodes[n.Name] = sn
		o.mu.Unlock()
		return false
	}
	o.ingest.RLock()
	s.mu.RLock()
	current := s.state.Fleets[f.ID]
	allowed := current != nil && current.DesiredState != "deleting"
	if allowed {
		allowed = false
		for _, member := range current.Servers {
			if member.Name == n.Name && member.IP == n.IP {
				allowed = true
				break
			}
		}
	}
	s.mu.RUnlock()
	if !allowed {
		o.ingest.RUnlock()
		return false
	}
	if store != nil {
		err = store.append(f.ID, n.IP, batch)
	} else {
		err = fmt.Errorf("observability storage unavailable")
	}
	o.mu.Lock()
	if o.cache[f.ID] == nil {
		o.cache[f.ID] = map[string]*pb.ObservationSnapshot{}
	}
	sn := batch.Snapshot
	if sn.CollectedMs == 0 {
		if previous := o.cache[f.ID][n.Name]; previous != nil && previous.CollectedMs > 0 {
			sn = proto.Clone(previous).(*pb.ObservationSnapshot)
			sn.State = "stale"
			sn.Error = batch.Snapshot.Error
		}
	}
	o.cache[f.ID][n.Name] = sn
	if err == nil {
		o.error = ""
		next := observationAck{generation: batch.Generation}
		for _, r := range batch.Records {
			next.sequence = observationMax(next.sequence, r.Sequence)
		}
		if currentAck := o.acks[key]; next.generation == currentAck.generation {
			next.sequence = observationMax(next.sequence, currentAck.sequence)
		}
		o.acks[key] = next
	} else {
		sn.Error = "central history ingestion unavailable; bounded agent queue retains pending records"
		o.error = sn.Error
	}
	o.mu.Unlock()
	o.ingest.RUnlock()
	return err == nil
}
