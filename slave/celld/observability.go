package celld

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	limits "github.com/Maruqes/512SvMan/api/celldobserve"
	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	"google.golang.org/protobuf/proto"
)

type observationBuffer struct {
	disabled        bool
	mu              sync.Mutex
	generation      string
	next            uint64
	records         []*pb.ObservationRecord
	bytes           int
	dropped         atomic.Uint64
	snapshot        *pb.ObservationSnapshot
	server          *http.Server
	endpoint, token string
	session         string
	secrets         []string
	containerSeen   map[string]map[string]bool
	containerSince  map[string]string
}

func observationID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func newObservations() *observationBuffer {
	return &observationBuffer{disabled: os.Getenv("CELLD_OBSERVABILITY") == "0", generation: observationID(), containerSeen: map[string]map[string]bool{}, containerSince: map[string]string{}}
}
func (o *observationBuffer) add(r *pb.ObservationRecord) {
	if o.disabled {
		return
	}
	// This path is called by process output. A busy collector never holds up output.
	if !o.mu.TryLock() {
		o.dropped.Add(1)
		return
	}
	defer o.mu.Unlock()
	if len(o.records) >= limits.MaxQueueRecords || o.bytes > limits.MaxQueueBytes-limits.MaxRecordBytes-1024 {
		o.dropped.Add(1)
		return
	}
	r.Body = limits.Sanitize(r.Body, o.secrets...)
	if r.TimeMs == 0 {
		r.TimeMs = time.Now().UnixMilli()
	}
	n := proto.Size(r)
	if n > limits.MaxRecordBytes+1024 || len(o.records) >= limits.MaxQueueRecords || o.bytes+n > limits.MaxQueueBytes {
		o.dropped.Add(1)
		return
	}
	o.next++
	r.Sequence = o.next
	o.records = append(o.records, r)
	o.bytes += proto.Size(r)
}
func (o *observationBuffer) batch(req *pb.ObservationsRequest) *pb.ObservationBatch {
	o.mu.Lock()
	defer o.mu.Unlock()
	if req.Generation == o.generation && req.Acknowledge <= o.next {
		cut := 0
		for cut < len(o.records) && o.records[cut].Sequence <= req.Acknowledge {
			o.bytes -= proto.Size(o.records[cut])
			cut++
		}
		copy(o.records, o.records[cut:])
		o.records = o.records[:len(o.records)-cut]
	}
	b := &pb.ObservationBatch{Generation: o.generation, Dropped: o.dropped.Load()}
	if o.snapshot != nil {
		b.Snapshot = proto.Clone(o.snapshot).(*pb.ObservationSnapshot)
	}
	size := proto.Size(b)
	for _, r := range o.records {
		n := proto.Size(r) + 8
		if len(b.Records) >= limits.MaxRecords || size+n > limits.MaxBatchBytes {
			break
		}
		b.Records = append(b.Records, proto.Clone(r).(*pb.ObservationRecord))
		size += n
	}
	return b
}

var observationHTTP = &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

type statePool struct{ Live, Retiring, HeapBytes, Cells, Requests uint64 }

func (p *statePool) UnmarshalJSON(data []byte) error {
	var v struct {
		Live     uint64 `json:"live"`
		Retiring uint64 `json:"retiring"`
		Heap     uint64 `json:"heap_bytes"`
		Cells    uint64 `json:"cells"`
		Requests uint64 `json:"requests"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	p.Live, p.Retiring, p.HeapBytes, p.Cells, p.Requests = v.Live, v.Retiring, v.Heap, v.Cells, v.Requests
	return nil
}

type isolatePools struct {
	Stateless statePool            `json:"stateless"`
	Services  map[string]statePool `json:"services"`
	Cells     map[string]statePool `json:"cells"`
}
type runtimeState struct {
	Residents []string `json:"residents"`
	Error     string   `json:"error"`
	Load      *struct {
		SampledMS  int64             `json:"sampled_ms"`
		Owned      *uint64           `json:"owned_cells"`
		Resident   *uint64           `json:"resident_cells"`
		RSS        *uint64           `json:"rss_bytes"`
		InUse      *uint64           `json:"in_use_bytes"`
		CPU        *uint64           `json:"cpu_percent_x100"`
		FDs        *uint64           `json:"open_fds"`
		FDLimit    *uint64           `json:"fd_limit"`
		Sockets    *uint64           `json:"host_websockets"`
		Pressure   *bool             `json:"pressured"`
		Headroom   *bool             `json:"memory_headroom"`
		Shed       *uint64           `json:"shed_cells"`
		Restoring  *uint64           `json:"restoring"`
		Containers map[string]uint64 `json:"container_instances"`
	} `json:"node_load"`
	Capacity      *uint64           `json:"capacity_waiting"`
	Activation    *uint64           `json:"activation_waiting"`
	HandoffFailed *uint64           `json:"handoff_failed"`
	Phases        map[string]uint64 `json:"phases"`
	Deployment    struct {
		Version  string            `json:"version"`
		Prefix   string            `json:"prefix"`
		Cells    map[string]uint64 `json:"cells"`
		Isolates isolatePools      `json:"isolates"`
		Draining []struct {
			Version  string       `json:"version"`
			Isolates isolatePools `json:"isolates"`
		} `json:"draining"`
	} `json:"deployment"`
}

func parseRuntimeState(data []byte, snapshot *pb.ObservationSnapshot) error {
	var st runtimeState
	if err := json.Unmarshal(data, &st); err != nil {
		return fmt.Errorf("invalid runtime state")
	}
	if st.Error != "" || st.Load == nil || st.Load.SampledMS == 0 || st.Load.RSS == nil {
		return fmt.Errorf("runtime load unavailable")
	}
	l := st.Load
	snapshot.Resources = &pb.ResourceSample{SampledMs: l.SampledMS, OwnedCells: l.Owned, ResidentCells: l.Resident, RssBytes: l.RSS, InUseBytes: l.InUse, CpuPercentX100: l.CPU, OpenFds: l.FDs, FdLimit: l.FDLimit, HostWebsockets: l.Sockets, Pressured: l.Pressure, MemoryHeadroom: l.Headroom, ShedCells: l.Shed, Restoring: l.Restoring, CapacityWaiting: st.Capacity, ActivationWaiting: st.Activation, HandoffFailed: st.HandoffFailed, Phases: st.Phases, ContainerInstances: l.Containers}
	if len(l.Containers) > limits.MaxWorkloads || len(st.Phases) > 64 {
		return fmt.Errorf("runtime cardinality exceeds limit")
	}
	pools := func(p isolatePools, version, state string) {
		// services contains the primary script too; do not add stateless again.
		for kind, group := range map[string]map[string]statePool{"worker": p.Services, "cell_pool": p.Cells} {
			for name, v := range group {
				if len(snapshot.Workloads) >= limits.MaxWorkloads {
					snapshot.State = "partial"
					continue
				}
				snapshot.Workloads = append(snapshot.Workloads, &pb.WorkloadSample{Kind: kind, Id: boundedObservationName(name), Version: boundedObservationName(version), State: state, Live: &v.Live, Retiring: &v.Retiring, HeapBytes: &v.HeapBytes, Cells: &v.Cells, Requests: &v.Requests})
			}
		}
	}
	pools(st.Deployment.Isolates, st.Deployment.Version, "current")
	for _, g := range st.Deployment.Draining {
		pools(g.Isolates, g.Version, "draining")
	}
	for _, cell := range st.Residents {
		if len(snapshot.Workloads) >= limits.MaxWorkloads {
			snapshot.State = "partial"
			break
		}
		class, _, _ := strings.Cut(cell, ":")
		kind := "cell"
		switch {
		case class == "__Queue":
			kind = "queue_cell"
		case strings.HasPrefix(class, "__Workflow"):
			kind = "workflow_cell"
		case class == "__D1Database":
			kind = "d1_cell"
		case class == "__KvNamespace":
			kind = "kv_cell"
		case class == ".cron":
			kind = "cron_cell"
		}
		version := ""
		if generation, ok := st.Deployment.Cells[cell]; ok {
			version = "generation:" + strconv.FormatUint(generation, 10)
		}
		snapshot.Workloads = append(snapshot.Workloads, &pb.WorkloadSample{Kind: kind, Id: boundedObservationName(cell), Parent: boundedObservationName(class), Version: version, State: "resident"})
	}
	return nil
}
func baseCapabilities() []*pb.Capability {
	return []*pb.Capability{
		{Name: "runtime", State: "supported", Detail: "v0.6.2 /state; 5s collection; per-node process CPU and memory"},
		{Name: "logs_traces", State: "partial", Detail: "OTLP observed records; some thrown Worker failures omit a matching span; upstream loss is not an exact exported counter"},
		{Name: "cell_activity", State: "partial", Detail: "Resident identities/generations and node phase totals; per-cell active/idle/alarm state is unavailable"},
		{Name: "queue_counters", State: "unsupported", Detail: "No operator backlog/oldest/in-flight/retry/dead-letter API"},
		{Name: "workflow_census", State: "unsupported", Detail: "No operator instance/step enumeration; waiting is not unhealthy"},
		{Name: "cron_history", State: "unsupported", Detail: "Manifest expressions only; app scheduled logs are observed executions"},
		{Name: "dynamic_workers", State: "partial", Detail: "Loader metadata and request logs; no loaded-runtime census"},
		{Name: "facets", State: "partial", Detail: "Parent context only; never independently counted as cells"},
		{Name: "binding_operations", State: "partial", Detail: "Deployment identity and request context; no exact D1/KV/R2/assets operation counters"},
	}
}
func (m *Manager) observeLoop() {
	if m.observations.disabled {
		return
	}
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-m.life.Done():
			return
		case <-tick.C:
			m.collectObservations(m.life)
		}
	}
}

var observationCollectionSlots = make(chan struct{}, 4)

func (m *Manager) collectObservations(ctx context.Context) {
	select {
	case observationCollectionSlots <- struct{}{}:
		defer func() { <-observationCollectionSlots }()
	case <-ctx.Done():
		return
	default:
		return
	}
	m.mu.Lock()
	c := m.config
	running := m.running
	if c != nil {
		c = proto.Clone(c).(*pb.Config)
	}
	m.mu.Unlock()
	if c == nil {
		return
	}
	o := m.observations
	o.mu.Lock()
	session := o.session
	o.mu.Unlock()
	sn := &pb.ObservationSnapshot{FleetId: c.FleetId, NodeId: m.ip, Version: c.Version, ProcessGeneration: session, AttemptedMs: time.Now().UnixMilli(), State: "offline", Capabilities: baseCapabilities(), Dropped: o.dropped.Load()}
	var err error
	if c.Version != "v0.6.2" {
		sn.State = "unsupported"
		sn.Capabilities[0].State = "unsupported"
		sn.Capabilities[0].Detail = "Only v0.6.2 runtime schema is supported"
		err = fmt.Errorf("runtime schema supported only for v0.6.2")
	} else if !running {
		err = fmt.Errorf("celld process is stopped")
	} else {
		call, cancel := context.WithTimeout(ctx, 2*time.Second)
		req, _ := http.NewRequestWithContext(call, "GET", "http://"+net.JoinHostPort(m.ip, strconv.Itoa(int(internalPort(c))))+"/state", nil)
		resp, e := observationHTTP.Do(req)
		if e == nil {
			data, readErr := io.ReadAll(io.LimitReader(resp.Body, limits.MaxStateBytes+1))
			resp.Body.Close()
			if readErr != nil || len(data) > limits.MaxStateBytes || resp.StatusCode != 200 {
				e = fmt.Errorf("runtime state unavailable or exceeds limit")
			} else {
				sn.State = "fresh"
				e = parseRuntimeState(data, sn)
			}
		}
		cancel()
		err = e
		if err == nil {
			sn.CollectedMs = time.Now().UnixMilli()
			m.collectContainers(ctx, sn, session)
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if err != nil {
		sn.Error = limits.Sanitize(err.Error(), c.AccessKey, c.SecretKey)
		if !running || c.Version != "v0.6.2" {
			sn.Error = err.Error()
		}
		if o.snapshot != nil && o.snapshot.CollectedMs > 0 {
			prior := proto.Clone(o.snapshot).(*pb.ObservationSnapshot)
			prior.State = "stale"
			if sn.State == "unsupported" {
				prior.State = sn.State
			}
			prior.AttemptedMs = sn.AttemptedMs
			prior.Error = sn.Error
			prior.Dropped = sn.Dropped
			sn = prior
		}
	}
	o.snapshot = sn
}
func (r *Registry) Observations(ctx context.Context, req *pb.ObservationsRequest) (*pb.ObservationBatch, error) {
	if !fleetIDPattern.MatchString(req.FleetId) {
		return nil, fmt.Errorf("invalid fleet ID")
	}
	r.gate.RLock()
	defer r.gate.RUnlock()
	r.mu.Lock()
	m := r.fleets[req.FleetId]
	r.mu.Unlock()
	if m == nil || !r.allowed[req.FleetId] {
		return nil, fmt.Errorf("fleet is not assigned")
	}
	b := m.observations.batch(req)
	if b.Snapshot == nil {
		b.Snapshot = &pb.ObservationSnapshot{FleetId: req.FleetId, NodeId: m.ip, State: "pending", Capabilities: baseCapabilities()}
	}
	if m.observations.disabled {
		b.Snapshot.State = "unsupported"
		b.Snapshot.Error = "collection disabled by CELLD_OBSERVABILITY=0"
	}
	return b, nil
}

// processTap frames bounded chunks without retaining an unbounded partial line.
type processTap struct {
	o      *observationBuffer
	stream string
}

func (w processTap) Write(data []byte) (int, error) {
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			w.o.add(&pb.ObservationRecord{Kind: "log", Source: w.stream, Severity: "INFO", Body: line})
		}
	}
	return len(data), nil
}

func boundedObservationName(s string) string {
	if len(s) > 256 {
		s = s[:256]
	}
	return limits.Sanitize(s)
}
