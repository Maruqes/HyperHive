package integration

import (
	"encoding/json"
	"reflect"
	"testing"

	"hyperhive/support/internal/domain"
)

func TestCreateLinuxMachineTemplate(t *testing.T) {
	f := setup(t)
	var created struct {
		App    domain.App `json:"app"`
		Secret string     `json:"secret"`
	}
	decode(t, f.request("POST", "/apps", `{"name":"Linux server","tab_id":"general","template_id":"linux-machine"}`, 201), &created)
	var detail domain.AppDetail
	decode(t, f.request("GET", "/apps/"+created.App.ID, "", 200), &detail)
	if detail.Layout.Revision != 0 || len(detail.Layout.Widgets) != 45 || len(detail.Streams) != 0 || detail.App.Connectivity != "unknown" {
		t.Fatal("Linux template must start with saved widgets and no telemetry", detail)
	}
	if err := f.store.AuthenticateApp(created.App.ID, created.Secret); err != nil {
		t.Fatal(err)
	}
	streams := map[string]string{}
	for i, widget := range detail.Layout.Widgets {
		streams[widget.Stream] = widget.StreamType
		for _, other := range detail.Layout.Widgets[:i] {
			if widget.X < other.X+other.W && other.X < widget.X+widget.W && widget.Y < other.Y+other.H && other.Y < widget.Y+widget.H {
				t.Fatalf("Linux widgets overlap: %s and %s", widget.ID, other.ID)
			}
		}
	}
	for name, kind := range map[string]string{
		"machine_info": "text", "cpu_usage_percent": "number", "memory_usage_percent": "number", "root_usage_percent": "number",
		"disk_read_bytes_per_second": "number", "network_receive_bytes_per_second": "number", "process_count": "number",
		"filesystems": "event", "network_interfaces": "event", "top_processes": "event", "services": "event", "hardware": "event",
		"system_logs": "log", "machine_events": "event",
	} {
		if streams[name] != kind {
			t.Fatalf("missing or incorrect Linux stream %s: %s", name, streams[name])
		}
	}
	definitions := []domain.Stream{}
	samples := []map[string]any{}
	for name, kind := range streams {
		definitions = append(definitions, domain.Stream{Name: name, Type: kind, Label: name})
		var value any = 24.5
		switch kind {
		case "text":
			value = "linux-server | Linux | x86_64"
		case "event":
			value = map[string]any{"name": "Snapshot", "fields": map[string]any{"items": []any{}}}
		case "log":
			value = map[string]any{"severity": "info", "message": "Collector connected"}
		}
		samples = append(samples, map[string]any{"stream": name, "value": value})
	}
	payload, err := json.Marshal(map[string]any{"heartbeat": true, "streams": definitions, "samples": samples})
	if err != nil {
		t.Fatal(err)
	}
	f.id, f.secret = created.App.ID, created.Secret
	f.request("POST", "/ingest", string(payload), 200)
	decode(t, f.request("GET", "/apps/"+created.App.ID, "", 200), &detail)
	if len(detail.Streams) != len(streams) || detail.App.Connectivity != "online" {
		t.Fatal("Linux collector streams were not accepted", detail)
	}
	var history domain.History
	decode(t, f.request("GET", "/apps/"+created.App.ID+"/samples?stream=cpu_usage_percent", "", 200), &history)
	if len(history.Samples) != 1 {
		t.Fatal("Linux metric was not persisted", history)
	}
}

func TestCreateAppFromSmallTemplate(t *testing.T) {
	f := setup(t)
	var created struct {
		App    domain.App `json:"app"`
		Secret string     `json:"secret"`
	}
	decode(t, f.request("POST", "/apps", `{"name":"Small","tab_id":"general","template_id":"small","tags":["test"],"heartbeat_timeout_seconds":60,"retention_days":14}`, 201), &created)
	if created.App.Timeout != 60 || created.App.Retention != 14 || len(created.App.Tags) != 1 || created.App.Connectivity != "unknown" || created.App.Health != "unknown" || len(created.App.Instances) != 0 {
		t.Fatal("incorrect template App settings or initial state", created.App)
	}
	if err := f.store.AuthenticateApp(created.App.ID, created.Secret); err != nil || created.Secret == f.secret {
		t.Fatal("template App must have independent credentials", err)
	}
	var detail domain.AppDetail
	decode(t, f.request("GET", "/apps/"+created.App.ID, "", 200), &detail)
	want, err := domain.TemplateLayout("small")
	if err != nil || len(want.Widgets) != 3 || !reflect.DeepEqual(detail.Layout, want) || len(detail.Streams) != 0 {
		t.Fatal("template layout was not saved with pending streams", detail, err)
	}
	// Pending widgets accept real telemetry through the usual sender contract.
	f.id, f.secret = created.App.ID, created.Secret
	f.request("POST", "/ingest", `{"heartbeat":true,"streams":[{"name":"cpu","type":"number","unit":"%"},{"name":"logs","type":"log"}],"samples":[{"stream":"cpu","value":24.5},{"stream":"logs","value":{"severity":"info","message":"Ready"}}]}`, 200)
	var history domain.History
	decode(t, f.request("GET", "/apps/"+created.App.ID+"/samples?stream=cpu", "", 200), &history)
	if len(history.Samples) != 1 {
		t.Fatal("template stream did not receive telemetry", history)
	}
	f.request("PUT", "/apps/"+created.App.ID+"/layout", `{"revision":0,"widgets":[]}`, 200)
	decode(t, f.request("POST", "/apps", `{"name":"Another","tab_id":"general","template_id":"small"}`, 201), &created)
	decode(t, f.request("GET", "/apps/"+created.App.ID, "", 200), &detail)
	if !reflect.DeepEqual(detail.Layout, want) {
		t.Fatal("editing a template App changed later template Apps", detail.Layout)
	}
}

func TestTemplateValidationAndAtomicCreation(t *testing.T) {
	f := setup(t)
	f.request("POST", "/apps", `{"name":"Unknown","tab_id":"general","template_id":"missing"}`, 400)
	f.request("POST", "/apps", `{"name":"Invalid tab","tab_id":"missing","template_id":"small"}`, 400)
	f.request("PUT", "/apps/"+f.id, `{"name":"Overwrite","tab_id":"general","tags":[],"heartbeat_timeout_seconds":120,"retention_days":7,"template_id":"small"}`, 400)
	if _, err := f.db.Exec(`CREATE TRIGGER fail_template BEFORE INSERT ON apps BEGIN SELECT RAISE(ABORT, 'injected creation failure'); END`); err != nil {
		t.Fatal(err)
	}
	f.request("POST", "/apps", `{"name":"Failed","tab_id":"general","template_id":"small"}`, 500)
	apps, err := f.service.Apps()
	if err != nil || len(apps) != 1 {
		t.Fatal("failed template creation left a partial App", apps, err)
	}
	var original domain.AppDetail
	decode(t, f.request("GET", "/apps/"+f.id, "", 200), &original)
	if original.App.Name != "Example" || len(original.Layout.Widgets) != 0 {
		t.Fatal("template requests modified the existing App", original)
	}
}
