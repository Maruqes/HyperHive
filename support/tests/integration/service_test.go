package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"hyperhive/support/internal/domain"
	"hyperhive/support/internal/httpapi"
	"hyperhive/support/internal/realtime"
	appservice "hyperhive/support/internal/service"
	"hyperhive/support/internal/storage/sqlite"
)

type fixture struct {
	testing          *testing.T
	service          *appservice.Service
	handler          http.Handler
	store            *sqlite.Store
	db               *sql.DB
	hub              *realtime.Hub
	id, secret, path string
	now              time.Time
}

const testProxySecret = "proxy-test-secret-at-least-32-bytes"

func setup(testing *testing.T) *fixture {
	testing.Helper()
	path := filepath.Join(testing.TempDir(), "support.db")
	fixture := &fixture{testing: testing, path: path, now: time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)}
	store, err := sqlite.Open(path)
	if err != nil {
		testing.Fatal(err)
	}
	hub := realtime.NewHub()
	application, err := appservice.New(store, hub, func() time.Time { return fixture.now })
	if err != nil {
		store.Close()
		testing.Fatal(err)
	}
	handler, err := httpapi.New(application, hub, testProxySecret)
	if err != nil {
		application.Close()
		testing.Fatal(err)
	}
	database, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		application.Close()
		testing.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	fixture.service = application
	fixture.handler = handler
	fixture.store = store
	fixture.db = database
	fixture.hub = hub
	testing.Cleanup(func() { application.Close(); database.Close() })
	response := fixture.request("POST", "/apps", `{"name":"Example","tab_id":"general","tags":[" Go ","Go"],"heartbeat_timeout_seconds":10}`, 201)
	var created struct {
		App    domain.App `json:"app"`
		Secret string     `json:"secret"`
	}
	decode(testing, response, &created)
	fixture.id = created.App.ID
	fixture.secret = created.Secret
	if len(fixture.secret) < 32 || len(created.App.Tags) != 1 {
		testing.Fatal("creation defaults or credential missing")
	}
	return fixture
}
func decode(testing *testing.T, body string, value any) {
	testing.Helper()
	if err := json.Unmarshal([]byte(body), value); err != nil {
		testing.Fatal(err, body)
	}
}
func (fixture *fixture) request(method, path, body string, status int) string {
	fixture.testing.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("X-HH-Support-Key", testProxySecret)
	if path == "/ingest" {
		request.Header.Set("Authorization", "Bearer "+fixture.secret)
		request.Header.Set("X-App-ID", fixture.id)
	}
	writer := httptest.NewRecorder()
	fixture.handler.ServeHTTP(writer, request)
	if writer.Code != status {
		fixture.testing.Fatalf("%s %s: got %d want %d: %s", method, path, writer.Code, status, writer.Body.String())
	}
	return writer.Body.String()
}
func (fixture *fixture) app() domain.App {
	fixture.testing.Helper()
	var detail struct {
		App domain.App `json:"app"`
	}
	decode(fixture.testing, fixture.request("GET", "/apps/"+fixture.id, "", 200), &detail)
	return detail.App
}
func (fixture *fixture) history(query string) ([]domain.Sample, *int64) {
	var page struct {
		Samples    []domain.Sample `json:"samples"`
		NextBefore *int64          `json:"next_before"`
	}
	decode(fixture.testing, fixture.request("GET", "/apps/"+fixture.id+"/samples?"+query, "", 200), &page)
	return page.Samples, page.NextBefore
}

func TestIngestTypesAtomicityAndSecrets(testing *testing.T) {
	fixture := setup(testing)
	fixture.request("POST", "/ingest", `{"heartbeat":true,"streams":[{"name":"n","type":"number"},{"name":"t","type":"text"},{"name":"b","type":"boolean"},{"name":"l","type":"log"},{"name":"e","type":"event"}],"samples":[{"stream":"n","value":12.5},{"stream":"t","value":"hello"},{"stream":"b","value":true},{"stream":"l","value":{"message":"warning","severity":"warn"}},{"stream":"e","value":{"name":"deploy","fields":{"version":1}}}]}`, 200)
	for _, name := range []string{"n", "t", "b", "l", "e"} {
		samples, _ := fixture.history("stream=" + name)
		if len(samples) != 1 || samples[0].ID < 1 {
			testing.Fatalf("missing %s samples", name)
		}
	}
	fixture.request("POST", "/ingest", `{"instance_id":"must-not-exist","heartbeat":true,"health":{"status":"unhealthy"},"streams":[{"name":"new","type":"text"}],"samples":[{"stream":"n","value":"wrong"}]}`, 400)
	if app := fixture.app(); len(app.Instances) != 0 || app.Health != "unknown" {
		testing.Fatal("invalid batch changed state", app)
	}
	streams, err := fixture.service.Streams(fixture.id)
	if err != nil || len(streams) != 5 {
		testing.Fatal("invalid batch committed stream", streams, err)
	}
	fixture.request("POST", "/ingest", `{"streams":[{"name":"n","type":"text"}]}`, 409)
	for _, body := range []string{`{"samples":[{"stream":"b","value":1}]}`, `{"samples":[{"stream":"e","value":{"name":"x","fields":null}}]}`, `{"samples":[{"stream":"l","value":{"message":"x","severity":"bad"}}]}`, `{"samples":[{"stream":"n","value":null}]}`, `{"heartbeat":true,"extra":1}`, `{"samples":[{"stream":"n","value":1,"extra":1}]}`, `{"heartbeat":true} {}`, `null`} {
		fixture.request("POST", "/ingest", body, 400)
	}
	for _, path := range []string{"/apps", "/apps/" + fixture.id} {
		body := fixture.request("GET", path, "", 200)
		if strings.Contains(body, fixture.secret) || strings.Contains(body, "secret") {
			testing.Fatal("credential exposed")
		}
	}
	var stored []byte
	if err := fixture.db.QueryRow("SELECT secret FROM apps WHERE id=?", fixture.id).Scan(&stored); err != nil {
		testing.Fatal(err)
	}
	hash := sha256.Sum256([]byte(fixture.secret))
	if string(stored) != string(hash[:]) {
		testing.Fatal("credential is not hashed")
	}
	var rotated struct {
		Secret string `json:"secret"`
	}
	decode(testing, fixture.request("POST", "/apps/"+fixture.id+"/rotate-secret", "", 200), &rotated)
	fixture.request("POST", "/ingest", `{"heartbeat":true}`, 401)
	fixture.secret = rotated.Secret
	fixture.request("POST", "/ingest", `{"heartbeat":true}`, 200)
}

func TestConnectivityHealthAndRestart(testing *testing.T) {
	fixture := setup(testing)
	if app := fixture.app(); app.Connectivity != "unknown" || app.Health != "unknown" {
		testing.Fatal(app)
	}
	fixture.request("POST", "/ingest", `{"instance_id":"worker-a","health":{"status":"healthy"}}`, 200)
	if app := fixture.app(); app.Connectivity != "offline" || app.Health != "healthy" || app.LastHeartbeat != nil {
		testing.Fatal(app)
	}
	fixture.request("POST", "/ingest", `{"instance_id":"worker-a","heartbeat":true,"health":{"status":"unhealthy","message":"DB failed"}}`, 200)
	if app := fixture.app(); app.Connectivity != "online" || app.Health != "unhealthy" {
		testing.Fatal(app)
	}
	fixture.request("POST", "/ingest", `{"heartbeat":true,"health":{"status":"healthy"}}`, 200)
	fixture.now = fixture.now.Add(10 * time.Second)
	if app := fixture.app(); app.Connectivity != "offline" || app.Health != "unhealthy" {
		testing.Fatal(app)
	}
	fixture.request("POST", "/ingest", `{"instance_id":"worker-a","heartbeat":true,"health":{"status":"healthy"}}`, 200)
	if app := fixture.app(); app.Connectivity != "offline" {
		testing.Fatal("direct participant must remain offline", app)
	}
	fixture.request("POST", "/ingest", `{"heartbeat":true}`, 200)
	if app := fixture.app(); app.Connectivity != "online" || app.Health != "healthy" || len(app.Instances) != 1 {
		testing.Fatal(app)
	}
	fixture.service.Close()
	restartedStore, err := sqlite.Open(fixture.path)
	if err != nil {
		testing.Fatal(err)
	}
	restarted, err := appservice.New(restartedStore, realtime.NewHub(), func() time.Time { return fixture.now.Add(time.Minute) })
	if err != nil {
		restartedStore.Close()
		testing.Fatal(err)
	}
	defer restarted.Close()
	detail, err := restarted.Detail(fixture.id)
	app := detail.App
	if err != nil || app.Connectivity != "offline" || app.Health != "healthy" || len(app.Instances) != 1 {
		testing.Fatal(app, err)
	}
}

func TestHistoryCursorsFiltersRetention(testing *testing.T) {
	fixture := setup(testing)
	fixture.request("POST", "/ingest", `{"streams":[{"name":"logs","type":"log"},{"name":"number","type":"number"}],"samples":[{"stream":"logs","timestamp":"2035-01-01T00:00:00Z","value":{"message":"first","severity":"info"}},{"stream":"logs","timestamp":"2020-01-01T00:00:00Z","value":{"message":"second","severity":"warn"}},{"stream":"logs","value":{"message":"third","severity":"error"}},{"stream":"logs","value":{"message":"fourth","severity":"info"}}]}`, 200)
	samples, next := fixture.history("stream=logs&limit=2")
	if len(samples) != 2 || next == nil || samples[0].ID >= samples[1].ID {
		testing.Fatal(samples, next)
	}
	oldest := *next
	previous, none := fixture.history(fmt.Sprintf("stream=logs&limit=2&before=%d", oldest))
	if len(previous) != 2 || none != nil || previous[1].ID >= samples[0].ID {
		testing.Fatal(previous, none)
	}
	after, _ := fixture.history(fmt.Sprintf("stream=logs&after=%d&limit=1", previous[0].ID))
	if len(after) != 1 || after[0].ID != previous[1].ID {
		testing.Fatal(after)
	}
	filtered, _ := fixture.history("stream=logs&severity=warn,error")
	if len(filtered) != 2 {
		testing.Fatal(filtered)
	}
	samples, _ = fixture.history("stream=logs&since=" + url.QueryEscape(fixture.now.Add(time.Second).Format(time.RFC3339)))
	if len(samples) != 0 {
		testing.Fatal(samples)
	}
	for _, query := range []string{"stream=logs&before=1&after=2", "stream=logs&limit=1001", "stream=logs&before=garbage", "stream=logs&severity=bad", "stream=number&severity=info", "stream=logs&since=no", "stream=logs&severity=bad&since=9999-01-01T00%3A00%3A00Z"} {
		fixture.request("GET", "/apps/"+fixture.id+"/samples?"+query, "", 400)
	}
	if _, err := fixture.db.Exec(`WITH RECURSIVE batch(number) AS (SELECT 1 UNION ALL SELECT number+1 FROM batch WHERE number<1100) INSERT INTO samples(app_id,stream,timestamp,received_at,value) SELECT ?,'logs',?,?,'{"message":"bulk","severity":"info"}' FROM batch`, fixture.id, fixture.now.Format(time.RFC3339), fixture.now.UnixNano()); err != nil {
		testing.Fatal(err)
	}
	fixture.now = fixture.now.Add(8 * 24 * time.Hour)
	samples, _ = fixture.history("stream=logs")
	if len(samples) != 0 {
		testing.Fatal("retention uses received time", samples)
	}
	if err := fixture.service.Cleanup(context.Background()); err != nil {
		testing.Fatal(err)
	}
	var count int
	_ = fixture.db.QueryRow("SELECT count(*) FROM samples").Scan(&count)
	if count != 0 {
		testing.Fatal("cleanup left expired samples", count)
	}
}

func TestTabsSettingsLayoutsDeletion(testing *testing.T) {
	fixture := setup(testing)
	fixture.request("DELETE", "/tabs/general", "", 409)
	fixture.request("POST", "/tabs", `{"name":" "}`, 400)
	var tab domain.Tab
	decode(testing, fixture.request("POST", "/tabs", `{"name":"Services"}`, 201), &tab)
	fixture.request("PUT", "/tabs/"+tab.ID, `{"name":"Renamed"}`, 200)
	fixture.request("PUT", "/apps/"+fixture.id, `{"name":"Changed","tab_id":"`+tab.ID+`","tags":["one","two","one"],"heartbeat_timeout_seconds":20,"retention_days":1}`, 200)
	app := fixture.app()
	if app.Name != "Changed" || app.ID != fixture.id || len(app.Tags) != 2 {
		testing.Fatal(app)
	}
	fixture.request("PUT", "/apps/"+fixture.id, `{"name":"Changed","tab_id":"general"}`, 400)
	fixture.request("POST", "/ingest", `{"streams":[{"name":"cpu","type":"number"},{"name":"logs","type":"log"}],"samples":[{"stream":"cpu","value":2}]}`, 200)
	path := "/apps/" + fixture.id + "/layout"
	valid := `{"revision":0,"widgets":[{"id":"cpu-widget","stream":"cpu","kind":"line","x":0,"y":0,"w":6,"h":2}]}`
	var layout domain.Layout
	decode(testing, fixture.request("PUT", path, valid, 200), &layout)
	if layout.Revision != 1 {
		testing.Fatal(layout)
	}
	fixture.request("PUT", path, valid, 409)
	for _, body := range []string{`{"revision":1,"widgets":[{"id":"w","stream":"logs","kind":"line","x":0,"y":0,"w":1,"h":1}]}`, `{"revision":1,"widgets":[{"id":"w","stream":"cpu","kind":"value","x":11,"y":0,"w":2,"h":1}]}`, `{"revision":1,"widgets":[{"id":"w","stream":"cpu","kind":"gauge","x":0,"y":0,"w":2,"h":1,"min":3,"max":2}]}`} {
		fixture.request("PUT", path, body, 400)
	}
	fixture.request("GET", "/apps?tab_id=general", "", 200)
	filtered := fixture.request("GET", "/apps?tag=one", "", 200)
	if !strings.Contains(filtered, fixture.id) {
		testing.Fatal(filtered)
	}
	fixture.request("DELETE", "/apps/"+fixture.id, "", 204)
	fixture.request("GET", "/apps/"+fixture.id, "", 404)
	fixture.request("DELETE", "/tabs/"+tab.ID, "", 204)
	for _, table := range []string{"instances", "streams", "samples"} {
		var count int
		_ = fixture.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count)
		if count != 0 {
			testing.Fatal("cascade missing", table, count)
		}
	}
}

func TestCleanupUsesCurrentRetentionBetweenBatches(testing *testing.T) {
	fixture := setup(testing)
	received := fixture.now.Add(-8 * 24 * time.Hour)
	if _, err := fixture.db.Exec(`WITH RECURSIVE batch(number) AS (SELECT 1 UNION ALL SELECT number+1 FROM batch WHERE number<1100) INSERT INTO samples(app_id,stream,timestamp,received_at,value) SELECT ?,'cpu',?,?,'1' FROM batch`, fixture.id, received.Format(time.RFC3339), received.UnixNano()); err != nil {
		testing.Fatal(err)
	}
	if count, err := fixture.service.CleanupBatch(fixture.id); err != nil || count != 1000 {
		testing.Fatalf("first batch: removed %d, error %v", count, err)
	}
	fixture.request("PUT", "/apps/"+fixture.id, `{"name":"Example","tab_id":"general","tags":[],"heartbeat_timeout_seconds":10,"retention_days":30}`, 200)
	if count, err := fixture.service.CleanupBatch(fixture.id); err != nil || count != 0 {
		testing.Fatalf("new retention must preserve remaining samples: removed %d, error %v", count, err)
	}
	var remaining int
	if err := fixture.db.QueryRow("SELECT count(*) FROM samples WHERE app_id=?", fixture.id).Scan(&remaining); err != nil || remaining != 100 {
		testing.Fatalf("remaining samples: %d, error %v", remaining, err)
	}
	fixture.request("DELETE", "/apps/"+fixture.id, "", 204)
	if count, err := fixture.service.CleanupBatch(fixture.id); err != nil || count != 0 {
		testing.Fatalf("deleted App should be skipped: removed %d, error %v", count, err)
	}
}

func TestAuthenticationAndLimits(testing *testing.T) {
	fixture := setup(testing)
	for _, path := range []string{"/apps", "/ws", "/tabs"} {
		request := httptest.NewRequest("GET", path, nil)
		writer := httptest.NewRecorder()
		fixture.handler.ServeHTTP(writer, request)
		if writer.Code != 401 {
			testing.Fatal(path, writer.Code)
		}
	}
	fixture.request("GET", "/healthz", "", 200)
	fixture.request("POST", "/ingest", `{"heartbeat":true,"instance_id":""}`, 400)
	fixture.request("POST", "/ingest", `{"streams":[{"name":"text","type":"text"}],"samples":[{"stream":"text","value":"`+strings.Repeat("a", 16385)+`"}]}`, 400)
	fixture.request("POST", "/ingest", `{"heartbeat":true,"padding":"`+strings.Repeat("a", 1<<20)+`"}`, 413)
	samples := make([]domain.IngestSample, 1001)
	fixture.request("POST", "/ingest", domain.EncodeJSON(domain.IngestRequest{Heartbeat: true, Samples: samples}), 400)
	streams := make([]domain.Stream, 101)
	fixture.request("POST", "/ingest", domain.EncodeJSON(domain.IngestRequest{Heartbeat: true, Streams: streams}), 400)
	for index := 0; index < 256; index++ {
		_, err := fixture.db.Exec("INSERT INTO streams(app_id,name,definition) VALUES(?,?,?)", fixture.id, fmt.Sprint(index), domain.EncodeJSON(domain.Stream{Name: fmt.Sprint(index), Type: "number"}))
		if err != nil {
			testing.Fatal(err)
		}
	}
	fixture.request("POST", "/ingest", `{"streams":[{"name":"overflow","type":"text"}]}`, 400)
	transaction, err := fixture.db.Begin()
	if err != nil {
		testing.Fatal(err)
	}
	for index := 0; index < 1024; index++ {
		instance := domain.Instance{ID: fmt.Sprint(index), Health: "unknown"}
		if _, err = transaction.Exec("INSERT INTO instances(app_id,id,state) VALUES(?,?,?)", fixture.id, instance.ID, domain.EncodeJSON(instance)); err != nil {
			testing.Fatal(err)
		}
	}
	if err = transaction.Commit(); err != nil {
		testing.Fatal(err)
	}
	fixture.request("POST", "/ingest", `{"instance_id":"overflow","heartbeat":true}`, 400)
	fixture.request("POST", "/ingest", `{"instance_id":"0","heartbeat":true}`, 200)
}

func TestWebSocketReadyChangesAndRetention(testing *testing.T) {
	fixture := setup(testing)
	server := httptest.NewServer(fixture.handler)
	defer server.Close()
	header := http.Header{"X-Hh-Support-Key": []string{testProxySecret}}
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", header)
	if err != nil {
		testing.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	var frame map[string]json.RawMessage
	if err := connection.ReadJSON(&frame); err != nil || string(frame["type"]) != `"ready"` {
		testing.Fatal(frame, err)
	}
	fixture.request("POST", "/ingest", `{"heartbeat":true,"streams":[{"name":"cpu","type":"number"}],"samples":[{"stream":"cpu","value":1}]}`, 200)
	for _, kind := range []string{"changed", "samples", "state"} {
		if err := connection.ReadJSON(&frame); err != nil || string(frame["type"]) != domain.EncodeJSON(kind) {
			testing.Fatal(kind, frame, err)
		}
	}
	fixture.now = fixture.now.Add(8 * 24 * time.Hour)
	if err := fixture.service.Cleanup(context.Background()); err != nil {
		testing.Fatal(err)
	}
	if err := connection.ReadJSON(&frame); err != nil || string(frame["type"]) != `"changed"` || string(frame["app_id"]) != domain.EncodeJSON(fixture.id) {
		testing.Fatal("retention must invalidate live values", frame, err)
	}

}

func TestExpirySweepPublishesState(testing *testing.T) {
	fixture := setup(testing)
	fixture.request("POST", "/ingest", `{"heartbeat":true,"health":{"status":"unhealthy","message":"DB failed"}}`, 200)
	server := httptest.NewServer(fixture.handler)
	defer server.Close()
	header := http.Header{"X-Hh-Support-Key": []string{testProxySecret}}
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", header)
	if err != nil {
		testing.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	var frame map[string]json.RawMessage
	if err := connection.ReadJSON(&frame); err != nil {
		testing.Fatal(err)
	}
	expired := fixture.now.Add(-20 * time.Second)
	if _, err := fixture.db.Exec("UPDATE apps SET direct=? WHERE id=?", domain.EncodeJSON(domain.Instance{LastHeartbeat: &expired, Health: "unhealthy", HealthMessage: "DB failed"}), fixture.id); err != nil {
		testing.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() { fixture.service.Run(ctx); close(finished) }()
	defer func() { cancel(); <-finished }()
	if err := connection.ReadJSON(&frame); err != nil || string(frame["type"]) != `"state"` {
		testing.Fatal("expiry sweep must push state", frame, err)
	}
	var app domain.App
	decode(testing, string(frame["app"]), &app)
	if app.Connectivity != "offline" || app.Health != "unhealthy" || app.HealthMessage != "DB failed" {
		testing.Fatal(app)
	}
}
