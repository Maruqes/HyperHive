package celld

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	limits "github.com/Maruqes/512SvMan/api/celldobserve"
	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	logs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	traces "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

func (m *Manager) ensureTelemetry(c *pb.Config) error {
	o := m.observations
	o.mu.Lock()
	defer o.mu.Unlock()
	o.secrets = []string{c.AccessKey, c.SecretKey}
	if o.disabled {
		return nil
	}
	if o.server != nil {
		return nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	o.token = observationID()
	path := "/fleets/" + c.FleetId
	o.endpoint = "http://" + listener.Addr().String() + path
	mux := http.NewServeMux()
	var mu sync.Mutex
	seen := map[[32]byte]bool{}
	var order [][32]byte
	slots := make(chan struct{}, 2)
	receive := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("x-hyperhive-token")), []byte(o.token)) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.Header.Get("Content-Type") != "application/x-protobuf" || r.Header.Get("Content-Encoding") != "" {
			o.dropped.Add(1)
			http.Error(w, "protobuf required", 415)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			o.dropped.Add(1)
			http.Error(w, "busy", 429)
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			o.dropped.Add(1)
			http.Error(w, "batch too large", 413)
			return
		}
		digest := sha256.Sum256(append([]byte(r.URL.Path), data...))
		mu.Lock()
		defer mu.Unlock()
		if seen[digest] {
			w.Header().Set("Content-Type", "application/x-protobuf")
			return
		}
		var records []*pb.ObservationRecord
		if strings.HasSuffix(r.URL.Path, "/logs") {
			records, err = decodeOTLPLogs(data)
		} else {
			records, err = decodeOTLPTraces(data)
		}
		if err != nil {
			o.dropped.Add(1)
			http.Error(w, "invalid telemetry", 400)
			return
		}
		for _, record := range records {
			o.add(record)
		}
		seen[digest] = true
		order = append(order, digest)
		if len(order) > 64 {
			delete(seen, order[0])
			order = order[1:]
		}
		w.Header().Set("Content-Type", "application/x-protobuf") // Empty official response serializes to zero bytes.
	}
	mux.HandleFunc(path+"/v1/logs", receive)
	mux.HandleFunc(path+"/v1/traces", receive)
	o.server = &http.Server{Handler: mux, ReadHeaderTimeout: time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8 << 10}
	go o.server.Serve(listener)
	go func() {
		<-m.life.Done()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = o.server.Shutdown(ctx)
	}()
	return nil
}

// Payload fleet/node attributes are intentionally ignored. Only these small
// scalar attributes help identify a runtime operation; request data is excluded.
func telemetryAttributes(kvs []*common.KeyValue) map[string]string {
	allowed := map[string]bool{"cell": true, "cell_id": true, "isolate": true, "script": true, "script_name": true, "request_id": true, "http.status_code": true, "http.response.status_code": true, "http.request.method": true, "http.method": true, "celld.cell": true, "celld.isolate": true, "celld.script": true, "celld.request_id": true, "error.type": true, "kind": true}
	out := map[string]string{}
	for _, kv := range kvs {
		if allowed[kv.Key] && len(out) < 16 {
			v := kv.Value.GetStringValue()
			if v == "" {
				switch kv.Value.Value.(type) {
				case *common.AnyValue_IntValue:
					v = fmt.Sprint(kv.Value.GetIntValue())
				case *common.AnyValue_BoolValue:
					v = fmt.Sprint(kv.Value.GetBoolValue())
				}
			}
			if len(v) > 256 {
				v = v[:256]
			}
			out[kv.Key] = limits.Sanitize(v)
		}
	}
	return out
}
func workloadOf(attrs map[string]string) string {
	for _, k := range []string{"celld.cell", "cell", "cell_id", "celld.script", "script", "script_name"} {
		if attrs[k] != "" {
			return attrs[k]
		}
	}
	return ""
}
func traceID(b []byte, n int) string {
	if len(b) != n {
		return ""
	}
	return hex.EncodeToString(b)
}
func decodeOTLPLogs(data []byte) ([]*pb.ObservationRecord, error) {
	var req logs.ExportLogsServiceRequest
	if err := proto.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	out := make([]*pb.ObservationRecord, 0)
	for _, resource := range req.ResourceLogs {
		for _, scope := range resource.ScopeLogs {
			for _, r := range scope.LogRecords {
				if len(out) >= limits.MaxQueueRecords {
					return nil, fmt.Errorf("too many records")
				}
				attrs := telemetryAttributes(r.Attributes)
				severity := strings.ToUpper(r.SeverityText)
				if severity == "" {
					switch {
					case r.SeverityNumber >= 17:
						severity = "ERROR"
					case r.SeverityNumber >= 13:
						severity = "WARN"
					case r.SeverityNumber >= 9:
						severity = "INFO"
					default:
						severity = "DEBUG"
					}
				}
				out = append(out, &pb.ObservationRecord{TimeMs: int64(r.TimeUnixNano / 1e6), Kind: "log", Source: "otlp", Workload: workloadOf(attrs), Severity: severity, Body: limits.Sanitize(r.Body.GetStringValue()), TraceId: traceID(r.TraceId, 16), SpanId: traceID(r.SpanId, 8), Attributes: attrs})
			}
		}
	}
	return out, nil
}
func decodeOTLPTraces(data []byte) ([]*pb.ObservationRecord, error) {
	var req traces.ExportTraceServiceRequest
	if err := proto.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	out := make([]*pb.ObservationRecord, 0)
	for _, resource := range req.ResourceSpans {
		for _, scope := range resource.ScopeSpans {
			for _, r := range scope.Spans {
				if len(out) >= limits.MaxQueueRecords {
					return nil, fmt.Errorf("too many records")
				}
				attrs := telemetryAttributes(r.Attributes)
				attrs["span_kind"] = r.Kind.String()
				failed := r.Status.GetCode() == 2
				for _, key := range []string{"http.status_code", "http.response.status_code"} {
					if len(attrs[key]) == 3 && attrs[key][0] >= '5' {
						failed = true
					}
				}
				duration := uint64(0)
				if r.EndTimeUnixNano >= r.StartTimeUnixNano {
					duration = min((r.EndTimeUnixNano-r.StartTimeUnixNano)/1000, uint64(24*time.Hour/time.Microsecond))
				}
				out = append(out, &pb.ObservationRecord{TimeMs: int64(r.StartTimeUnixNano / 1e6), Kind: "span", Source: "otlp", Workload: workloadOf(attrs), Severity: "INFO", Body: limits.Sanitize(r.Name), TraceId: traceID(r.TraceId, 16), SpanId: traceID(r.SpanId, 8), ParentSpanId: traceID(r.ParentSpanId, 8), DurationUs: duration, Failed: failed, Attributes: attrs})
			}
		}
	}
	return out, nil
}
