package celld

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	limits "github.com/Maruqes/512SvMan/api/celldobserve"
	pb "github.com/Maruqes/512SvMan/api/proto/celld"
)

var containerIDPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func containerHTTP() *http.Client {
	socket := "/var/run/docker.sock"
	if host := os.Getenv("DOCKER_HOST"); strings.HasPrefix(host, "unix://") {
		socket = strings.TrimPrefix(host, "unix://")
	}
	return &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, DisableKeepAlives: true}}
}
func readContainer(ctx context.Context, client *http.Client, path string, maxBytes int) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://docker"+path, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil || len(b) > maxBytes || resp.StatusCode != 200 {
		return nil, fmt.Errorf("engine response unavailable or exceeds limit")
	}
	return b, nil
}
func (m *Manager) collectContainers(parent context.Context, sn *pb.ObservationSnapshot, session string) {
	capability := &pb.Capability{Name: "containers", State: "unavailable", Detail: "Local Docker Unix socket unavailable"}
	sn.Capabilities = append(sn.Capabilities, capability)
	if session == "" {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	client := containerHTTP()
	defer client.CloseIdleConnections()
	filters, _ := json.Marshal(map[string][]string{"label": {"celld.node=" + session}})
	data, err := readContainer(ctx, client, "/containers/json?all=true&filters="+url.QueryEscape(string(filters)), 256<<10)
	if err != nil {
		return
	}
	var containers []struct {
		ID     string            `json:"Id"`
		Labels map[string]string `json:"Labels"`
		State  string            `json:"State"`
	}
	if json.Unmarshal(data, &containers) != nil || len(containers) > 64 {
		capability.Detail = "Container inventory exceeds limit"
		return
	}
	capability.State = "supported"
	capability.Detail = "celld.node session labels; bounded local engine stats and timestamped logs"
	seenIDs := map[string]bool{}
	for i, c := range containers {
		if c.Labels["celld.node"] != session || !containerIDPattern.MatchString(c.ID) {
			continue
		}
		seenIDs[c.ID] = true
		if i >= 16 {
			capability.State = "partial"
			capability.Detail = "Only first 16 containers collected per sample"
			break
		}
		w := &pb.WorkloadSample{Kind: "container", Id: c.ID, Parent: limits.Sanitize(c.Labels["celld.cell"]), Version: limits.Sanitize(c.Labels["celld.class"]), State: c.State}
		if len(sn.Workloads) < limits.MaxWorkloads {
			sn.Workloads = append(sn.Workloads, w)
		}
		if c.State == "running" {
			raw, e := readContainer(ctx, client, "/containers/"+c.ID+"/stats?stream=false&one-shot=true", 128<<10)
			var stats struct {
				Memory struct {
					Usage uint64 `json:"usage"`
				} `json:"memory_stats"`
				CPU struct {
					Usage struct {
						Total uint64 `json:"total_usage"`
					} `json:"cpu_usage"`
					System uint64 `json:"system_cpu_usage"`
					Online uint64 `json:"online_cpus"`
				} `json:"cpu_stats"`
				Previous struct {
					Usage struct {
						Total uint64 `json:"total_usage"`
					} `json:"cpu_usage"`
					System uint64 `json:"system_cpu_usage"`
				} `json:"precpu_stats"`
			}
			if e == nil && json.Unmarshal(raw, &stats) == nil {
				w.MemoryBytes = &stats.Memory.Usage
				if stats.CPU.System > stats.Previous.System && stats.CPU.Usage.Total >= stats.Previous.Usage.Total && stats.CPU.Online > 0 {
					v := float64(stats.CPU.Usage.Total-stats.Previous.Usage.Total) / float64(stats.CPU.System-stats.Previous.System) * float64(stats.CPU.Online) * 100
					w.CpuPercent = &v
				}
			} else {
				capability.State = "partial"
			}
		}
		o := m.observations
		since := o.containerSince[c.ID]
		if since == "" {
			since = time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339Nano)
		}
		raw, e := readContainer(ctx, client, "/containers/"+c.ID+"/logs?stdout=true&stderr=true&timestamps=true&tail=128&since="+url.QueryEscape(since), 64<<10)
		if e != nil {
			capability.State = "partial"
			continue
		}

		prior := o.containerSeen[c.ID]
		next := map[string]bool{}
		latest := since
		latestTime, _ := time.Parse(time.RFC3339Nano, since)
		lineCount := 0
		for _, frame := range containerLogFrames(raw) {
			scanner := bufio.NewScanner(strings.NewReader(string(frame.data)))
			scanner.Buffer(make([]byte, 4096), 64<<10)
			for scanner.Scan() {
				line := scanner.Text()
				stamp, body, ok := strings.Cut(line, " ")
				if !ok {
					continue
				}
				ts, e := time.Parse(time.RFC3339Nano, stamp)
				if e != nil {
					continue
				}
				key := frame.stream + line
				if !prior[key] {
					o.add(&pb.ObservationRecord{TimeMs: ts.UnixMilli(), Kind: "log", Source: "container", Workload: w.Parent, Severity: "INFO", Body: body, Attributes: map[string]string{"container_id": c.ID, "class": w.Version, "stream": frame.stream}})
				}
				next[key] = true
				lineCount++
				if ts.After(latestTime) {
					latest = stamp
					latestTime = ts
				}
			}
			if scanner.Err() != nil {
				capability.State = "partial"
				o.dropped.Add(1)
			}
		}
		if lineCount >= 128 {
			capability.State = "partial"
			capability.Detail = "Container log tail reached 128-line cap; older lines may be missing"
		}

		o.containerSeen[c.ID] = next
		o.containerSince[c.ID] = latest
	}
	for id := range m.observations.containerSince {
		if !seenIDs[id] {
			delete(m.observations.containerSince, id)
			delete(m.observations.containerSeen, id)
		}
	}
}

type containerLogFrame struct {
	stream string
	data   []byte
}

func containerLogFrames(raw []byte) []containerLogFrame {
	if len(raw) < 8 || raw[0] > 2 || raw[1] != 0 || raw[2] != 0 || raw[3] != 0 {
		return []containerLogFrame{{"tty", raw}}
	}
	// An engine frame may split a line: join each stream before scanning it.
	streams := map[string][]byte{"stdout": {}, "stderr": {}}
	for len(raw) >= 8 {
		stream := "stdout"
		if raw[0] == 2 {
			stream = "stderr"
		}
		n := int(raw[4])<<24 | int(raw[5])<<16 | int(raw[6])<<8 | int(raw[7])
		raw = raw[8:]
		if n < 0 || n > len(raw) {
			break
		}
		streams[stream] = append(streams[stream], raw[:n]...)
		raw = raw[n:]
	}
	return []containerLogFrame{{"stdout", streams["stdout"]}, {"stderr", streams["stderr"]}}
}
