package integration

import (
	"reflect"
	"testing"

	"hyperhive/support/internal/domain"
	"hyperhive/support/internal/httpapi"
	"hyperhive/support/internal/realtime"
	appservice "hyperhive/support/internal/service"
	"hyperhive/support/internal/storage/sqlite"
)

func TestLayoutAcrossClientsAndDatabaseRestart(t *testing.T) {
	first := setup(t)
	path := "/apps/" + first.id + "/layout"
	widgets := []domain.Widget{{ID: "cpu", Stream: "number_cpu", StreamType: "number", Kind: "area", X: 1, Y: 2, W: 6, H: 3}}
	var saved domain.Layout
	decode(t, first.request("PUT", path, domain.EncodeJSON(domain.Layout{Widgets: widgets}), 200), &saved)
	if saved.Revision != 1 || !reflect.DeepEqual(saved.Widgets, widgets) {
		t.Fatal("saved layout differs", saved)
	}

	second := *first
	var err error
	second.handler, err = httpapi.New(first.service, first.hub, testProxySecret)
	if err != nil {
		t.Fatal(err)
	}
	var loaded domain.Layout
	decode(t, second.request("GET", path, "", 200), &loaded)
	if !reflect.DeepEqual(loaded, saved) {
		t.Fatal("another client did not receive saved layout", loaded)
	}
	second.request("PUT", path, domain.EncodeJSON(domain.Layout{Widgets: widgets}), 409)

	if err := first.service.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(first.path)
	if err != nil {
		t.Fatal(err)
	}
	hub := realtime.NewHub()
	restarted, err := appservice.New(store, hub, nil)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	defer restarted.Close()
	second.handler, err = httpapi.New(restarted, hub, testProxySecret)
	if err != nil {
		t.Fatal(err)
	}
	decode(t, second.request("GET", path, "", 200), &loaded)
	if !reflect.DeepEqual(loaded, saved) {
		t.Fatal("database restart lost layout", loaded)
	}
	var detail domain.AppDetail
	decode(t, second.request("GET", "/apps/"+first.id, "", 200), &detail)
	if !reflect.DeepEqual(detail.Layout, saved) || len(detail.Streams) != 0 {
		t.Fatal("App detail lost pending widgets or declared a stream implicitly", detail)
	}
}

func TestPendingWidgetStreamTypeIsAtomic(t *testing.T) {
	fixture := setup(t)
	path := "/apps/" + fixture.id + "/layout"
	fixture.request("PUT", path, `{"revision":0,"widgets":[{"id":"cpu","stream":"number_cpu","stream_type":"number","kind":"progress","x":0,"y":0,"w":6,"h":2}]}`, 200)
	fixture.request("POST", "/ingest", `{"instance_id":"rejected","heartbeat":true,"health":{"status":"unhealthy"},"streams":[{"name":"other","type":"number"},{"name":"number_cpu","type":"text"}],"samples":[{"stream":"other","value":42}]}`, 409)
	if app := fixture.app(); len(app.Instances) != 0 || app.Health != "unknown" || app.LastHeartbeat != nil {
		t.Fatal("conflicting stream changed participant state", app)
	}
	for _, table := range []string{"streams", "samples"} {
		var count int
		if err := fixture.db.QueryRow("SELECT count(*) FROM "+table+" WHERE app_id=?", fixture.id).Scan(&count); err != nil || count != 0 {
			t.Fatalf("conflicting stream persisted %s: count=%d, error=%v", table, count, err)
		}
	}
	fixture.request("POST", "/ingest", `{"heartbeat":true,"streams":[{"name":"number_cpu","type":"number"}],"samples":[{"stream":"number_cpu","value":42}]}`, 200)
	samples, _ := fixture.history("stream=number_cpu")
	if len(samples) != 1 || string(samples[0].Value) != "42" {
		t.Fatal("matching declaration failed", samples)
	}
	fixture.request("PUT", path, `{"revision":1,"widgets":[{"id":"cpu","stream":"number_cpu","stream_type":"text","kind":"text","x":0,"y":0,"w":6,"h":2}]}`, 400)
	fixture.request("PUT", path, `{"revision":1,"widgets":[{"id":"cpu","stream":"number_cpu","kind":"line","x":0,"y":0,"w":6,"h":2}]}`, 200)
}
