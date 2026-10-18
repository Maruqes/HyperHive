package celld

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	limits "github.com/Maruqes/512SvMan/api/celldobserve"
	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	logs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

func TestObservationQueueAcknowledgementAndLimits(t *testing.T) {
	o := newObservations()
	for i := 0; i < limits.MaxQueueRecords+10; i++ {
		o.add(&pb.ObservationRecord{Kind: "log", Body: strings.Repeat("x", limits.MaxRecordBytes)})
	}
	b := o.batch(&pb.ObservationsRequest{})
	if len(b.Records) == 0 || proto.Size(b) > limits.MaxBatchBytes || o.bytes > limits.MaxQueueBytes || o.dropped.Load() == 0 {
		t.Fatal("queue or batch limit failed")
	}
	first := b.Records[0].Sequence
	retry := o.batch(&pb.ObservationsRequest{Generation: "wrong", Acknowledge: b.Records[len(b.Records)-1].Sequence})
	if retry.Records[0].Sequence != first {
		t.Fatal("foreign generation ack discarded data")
	}
	next := o.batch(&pb.ObservationsRequest{Generation: o.generation, Acknowledge: b.Records[len(b.Records)-1].Sequence})
	if len(next.Records) > 0 && next.Records[0].Sequence <= first {
		t.Fatal("ack did not advance")
	}
	for len(next.Records) > 0 {
		next = o.batch(&pb.ObservationsRequest{Generation: o.generation, Acknowledge: next.Records[len(next.Records)-1].Sequence})
	}
	if o.bytes != 0 {
		t.Fatalf("queue accounting drift: %d", o.bytes)
	}
}
func TestOTLPIsolationSanitizationAndBounds(t *testing.T) {
	m, err := New(t.TempDir(), "/unused", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	cfg := fleetConfigFor("fleet-a", 29100)
	if err = m.ensureTelemetry(cfg); err != nil {
		t.Fatal(err)
	}
	sn := &pb.ObservationSnapshot{FleetId: cfg.FleetId, NodeId: m.ip, State: "pending"}
	m.observations.snapshot = sn
	record := &logsv1.LogRecord{TimeUnixNano: uint64(time.Now().UnixNano()), SeverityNumber: 17, Body: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "password=private AWS_SECRET_ACCESS_KEY=" + cfg.SecretKey}}, TraceId: bytes.Repeat([]byte{1}, 16), SpanId: bytes.Repeat([]byte{2}, 8)}
	req := &logs.ExportLogsServiceRequest{ResourceLogs: []*logsv1.ResourceLogs{{Resource: &resource.Resource{Attributes: []*common.KeyValue{{Key: "fleet_id", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "forged"}}}}}, ScopeLogs: []*logsv1.ScopeLogs{{LogRecords: []*logsv1.LogRecord{record}}}}}}
	data, _ := proto.Marshal(req)
	post := func(token string, data []byte) int {
		r, _ := http.NewRequest("POST", m.observations.endpoint+"/v1/logs", bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/x-protobuf")
		r.Header.Set("x-hyperhive-token", token)
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if post("wrong", data) != 401 || post(m.observations.token, bytes.Repeat([]byte{1}, (1<<20)+1)) != 413 {
		t.Fatal("receiver auth/body cap")
	}
	if post(m.observations.token, data) != 200 || post(m.observations.token, data) != 200 {
		t.Fatal("OTLP failed")
	}
	b := m.observations.batch(&pb.ObservationsRequest{})
	if len(b.Records) != 1 || b.Snapshot.FleetId != "fleet-a" || b.Records[0].TraceId != strings.Repeat("01", 16) {
		t.Fatalf("identity/dedup/trace failed: %+v", b)
	}
	if strings.Contains(b.Records[0].Body, "private") || strings.Contains(b.Records[0].Body, cfg.SecretKey) {
		t.Fatal("secret leaked")
	}
}
func TestRuntimeCollectorPreservesLastSuccess(t *testing.T) {
	data := []byte(`{"node_load":{"sampled_ms":1,"rss_bytes":123,"owned_cells":null,"resident_cells":0,"cpu_percent_x100":0,"container_instances":{}},"deployment":{"version":"abc","isolates":{"stateless":{"live":2},"services":{"app":{"live":2,"heap_bytes":456}},"cells":{"app":{"live":1,"cells":3}}}},"phases":{"resident":3}}`)
	sn := &pb.ObservationSnapshot{}
	if err := parseRuntimeState(data, sn); err != nil {
		t.Fatal(err)
	}
	if sn.Resources.OwnedCells != nil || sn.Resources.ResidentCells == nil || *sn.Resources.ResidentCells != 0 || len(sn.Workloads) != 2 {
		t.Fatal("unknown/zero or duplicate pools")
	}
	up := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up {
			w.WriteHeader(503)
			return
		}
		w.Write(data)
	}))
	defer server.Close()
	m, err := New(t.TempDir(), "/unused", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	cfg := testConfig()
	parts := strings.Split(server.URL, ":")
	var port uint32
	fmtSscanPort(parts[len(parts)-1], &port)
	cfg.InternalPort = port
	m.config = cfg
	m.running = true
	m.collectObservations(context.Background())
	first := m.observations.batch(&pb.ObservationsRequest{}).Snapshot
	up = false
	m.collectObservations(context.Background())
	last := m.observations.batch(&pb.ObservationsRequest{}).Snapshot
	if first.CollectedMs == 0 || last.State != "stale" || last.CollectedMs != first.CollectedMs || last.Resources.RssBytes == nil || *last.Resources.RssBytes != 123 {
		t.Fatal("last success was lost")
	}
}
func fmtSscanPort(s string, p *uint32) {
	var n uint64
	for _, c := range s {
		n = n*10 + uint64(c-'0')
	}
	*p = uint32(n)
}
func TestDecodeRealCapabilityProbe(t *testing.T) {
	dir := os.Getenv("CELLD_OBSERVABILITY_PROBE")
	if dir == "" {
		t.Skip("run scripts/probe_celld_observability.py and set CELLD_OBSERVABILITY_PROBE")
	}
	data, err := os.ReadFile(filepath.Join(dir, "snapshots.json"))
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]json.RawMessage{}
	if err = json.Unmarshal(data, &states); err != nil {
		t.Fatal(err)
	}
	for name, data := range states {
		if err = parseRuntimeState(data, &pb.ObservationSnapshot{}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.pb"))
	logs, spans := 0, 0
	traces := map[string]bool{}
	var ids []string
	for _, file := range files {
		data, _ := os.ReadFile(file)
		var records []*pb.ObservationRecord
		if strings.HasPrefix(filepath.Base(file), "logs") {
			records, err = decodeOTLPLogs(data)
			logs += len(records)
			for _, r := range records {
				if strings.Contains(r.Body, "probe console error") {
					ids = append(ids, r.TraceId)
				}
			}
		} else {
			records, err = decodeOTLPTraces(data)
			spans += len(records)
			for _, r := range records {
				traces[r.TraceId] = true
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if logs == 0 || spans == 0 || len(ids) == 0 {
		t.Fatalf("missing real signals logs=%d spans=%d console_errors=%d", logs, spans, len(ids))
	}
	for _, id := range ids {
		if id == "" || !traces[id] {
			t.Fatal("console error did not join request trace")
		}
	}
	t.Logf("verified %d runtime states, %d app logs, %d spans, %d correlated console errors", len(states), logs, spans, len(ids))
}

func TestContainerCollectorScopesIdentityStatsAndLogCursor(t *testing.T) {
	session := strings.Repeat("c", 32)
	owned := strings.Repeat("a", 64)
	foreign := strings.Repeat("b", 64)
	socket := filepath.Join(t.TempDir(), "engine.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("collector mutated engine")
		}
		if strings.Contains(r.URL.Path, foreign) {
			t.Error("collector read another fleet")
		}
		switch {
		case r.URL.Path == "/containers/json":
			if !strings.Contains(r.URL.Query().Get("filters"), session) {
				t.Error("missing trusted session filter")
			}
			json.NewEncoder(w).Encode([]any{map[string]any{"Id": owned, "State": "running", "Labels": map[string]string{"celld.node": session, "celld.cell": "Counter:one", "celld.class": "Box"}}, map[string]any{"Id": foreign, "State": "running", "Labels": map[string]string{"celld.node": "other-fleet"}}})
		case strings.HasSuffix(r.URL.Path, "/stats"):
			io.WriteString(w, `{"memory_stats":{"usage":123},"cpu_stats":{"cpu_usage":{"total_usage":300},"system_cpu_usage":1000,"online_cpus":2},"precpu_stats":{"cpu_usage":{"total_usage":100},"system_cpu_usage":500}}`)
		case strings.HasSuffix(r.URL.Path, "/logs"):
			line := "2026-10-18T12:00:00.123456789Z owned container line\n"
			w.Write([]byte{1, 0, 0, 0, 0, 0, 0, byte(len(line))})
			io.WriteString(w, line)
		default:
			t.Error("unexpected engine path")
			w.WriteHeader(404)
		}
	})}
	go server.Serve(listener)
	defer server.Close()
	t.Setenv("DOCKER_HOST", "unix://"+socket)
	m, err := New(t.TempDir(), "/unused", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	sn := &pb.ObservationSnapshot{}
	m.collectContainers(context.Background(), sn, session)
	m.collectContainers(context.Background(), &pb.ObservationSnapshot{}, session)
	if len(sn.Workloads) != 1 || sn.Workloads[0].Id != owned || sn.Workloads[0].MemoryBytes == nil || *sn.Workloads[0].MemoryBytes != 123 || sn.Workloads[0].CpuPercent == nil || *sn.Workloads[0].CpuPercent != 80 {
		t.Fatalf("incorrect container stats %+v", sn)
	}
	b := m.observations.batch(&pb.ObservationsRequest{})
	if len(b.Records) != 1 || b.Records[0].Workload != "Counter:one" || b.Records[0].Body != "owned container line" {
		t.Fatalf("container logs duplicated or crossed fleet: %+v", b)
	}
}
