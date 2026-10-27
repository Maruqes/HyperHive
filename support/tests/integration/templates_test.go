package integration

import (
	"reflect"
	"testing"

	"hyperhive/support/internal/domain"
)

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
