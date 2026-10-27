package integration

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"hyperhive/support/internal/domain"
)

func TestReplicateAppConfiguration(t *testing.T) {
	f := setup(t)
	f.request("POST", "/ingest", `{"instance_id":"worker","heartbeat":true,"health":{"status":"unhealthy","message":"source only"},"streams":[{"name":"cpu","type":"number","unit":"%","label":"CPU"},{"name":"text","type":"text"},{"name":"flag","type":"boolean"},{"name":"logs","type":"log"},{"name":"events","type":"event"}],"samples":[{"stream":"cpu","value":42}]}`, 200)
	f.request("POST", "/ingest", `{"heartbeat":true,"health":{"status":"degraded"}}`, 200)
	f.request("PUT", "/apps/"+f.id+"/layout", `{"revision":0,"widgets":[{"id":"cpu-widget","stream":"cpu","stream_type":"number","kind":"gauge","x":4,"y":9,"w":3,"h":3,"min":0,"max":100},{"id":"pending","stream":"future","stream_type":"text","kind":"table","x":0,"y":1,"w":4,"h":3}]}`, 200)
	var source domain.AppDetail
	decode(t, f.request("GET", "/apps/"+f.id, "", 200), &source)
	var tab domain.Tab
	decode(t, f.request("POST", "/tabs", `{"name":"Replicas"}`, 201), &tab)
	var created struct {
		App    domain.App `json:"app"`
		Secret string     `json:"secret"`
	}
	decode(t, f.request("POST", "/apps/"+f.id+"/replicate", fmt.Sprintf(`{"name":"Replica","tab_id":%q,"tags":[" new "]}`, tab.ID), 201), &created)
	if created.App.ID == f.id || created.Secret == f.secret || len(created.Secret) < 32 {
		t.Fatal("replica reused credentials", created.App.ID)
	}
	var replica domain.AppDetail
	decode(t, f.request("GET", "/apps/"+created.App.ID, "", 200), &replica)
	if replica.App.Name != "Replica" || replica.App.TabID != tab.ID || !reflect.DeepEqual(replica.App.Tags, []string{"new"}) || replica.App.Timeout != source.App.Timeout || replica.App.Retention != source.App.Retention {
		t.Fatal("incorrect replica settings", replica.App)
	}
	if len(replica.App.Instances) != 0 || replica.App.LastHeartbeat != nil || replica.App.Health != "unknown" || replica.App.Connectivity != "unknown" || replica.App.HealthMessage != "" {
		t.Fatal("replica inherited runtime state", replica.App)
	}
	if replica.Layout.Revision != 0 || !reflect.DeepEqual(source.Layout.Widgets, replica.Layout.Widgets) || !reflect.DeepEqual(source.Streams, replica.Streams) {
		t.Fatal("configuration was not fully copied", replica)
	}
	var history domain.History
	decode(t, f.request("GET", "/apps/"+created.App.ID+"/samples?stream=cpu", "", 200), &history)
	if len(history.Samples) != 0 {
		t.Fatal("replica inherited samples")
	}
	if err := f.store.AuthenticateApp(created.App.ID, f.secret); err == nil {
		t.Fatal("source secret authenticates replica")
	}
	if err := f.store.AuthenticateApp(created.App.ID, created.Secret); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/apps/"+f.id+"/replicate", strings.NewReader(`{"name":"Unauthorized"}`))
	writer := httptest.NewRecorder()
	f.handler.ServeHTTP(writer, request)
	if writer.Code != 401 {
		t.Fatalf("replication bypassed management authentication: %d", writer.Code)
	}
	f.request("PUT", "/apps/"+created.App.ID+"/layout", `{"revision":0,"widgets":[]}`, 200)
	f.request("POST", "/apps/"+created.App.ID+"/rotate-secret", "", 200)
	var unchanged domain.AppDetail
	decode(t, f.request("GET", "/apps/"+f.id, "", 200), &unchanged)
	if !reflect.DeepEqual(source, unchanged) {
		t.Fatal("replica edits changed source")
	}
	if err := f.store.AuthenticateApp(f.id, f.secret); err != nil {
		t.Fatal("replica rotation changed source secret", err)
	}
	// A replica is itself a reusable template, with inherited tags/tab when omitted.
	decode(t, f.request("POST", "/apps/"+created.App.ID+"/replicate", `{"name":"Next"}`, 201), &created)
	if created.App.TabID != tab.ID || !reflect.DeepEqual(created.App.Tags, []string{"new"}) {
		t.Fatal("omitted overrides did not inherit")
	}
	decode(t, f.request("POST", "/apps/"+f.id+"/replicate", `{"name":"No tags","tags":[]}`, 201), &created)
	if len(created.App.Tags) != 0 {
		t.Fatal("explicit empty tags did not clear tags")
	}
}

func TestReplicateAppValidationAndRollback(t *testing.T) {
	f := setup(t)
	f.request("POST", "/ingest", `{"streams":[{"name":"cpu","type":"number"}]}`, 200)
	f.request("POST", "/apps/missing/replicate", `{"name":"Copy"}`, 404)
	for _, body := range []string{`{}`, `null`, `{"name":"Copy","tab_id":"missing"}`, `{"name":"Copy","tags":[""]}`, `{"name":"Copy","secret":"forbidden"}`, `{"name":"Copy","retention_days":30}`} {
		f.request("POST", "/apps/"+f.id+"/replicate", body, 400)
	}
	f.request("GET", "/apps/"+f.id+"/replicate", "", 404)
	if _, err := f.db.Exec(`CREATE TRIGGER fail_replica_stream BEFORE INSERT ON streams BEGIN SELECT RAISE(ABORT, 'injected copy failure'); END`); err != nil {
		t.Fatal(err)
	}
	f.request("POST", "/apps/"+f.id+"/replicate", `{"name":"Rolled back"}`, 500)
	apps, err := f.service.Apps()
	if err != nil || len(apps) != 1 {
		t.Fatal("failed copy left a partial app", apps, err)
	}
	if _, err := f.db.Exec("DROP TRIGGER fail_replica_stream"); err != nil {
		t.Fatal(err)
	}
	// Retrying after a failed transaction must still succeed.
	body := f.request("POST", "/apps/"+f.id+"/replicate", `{"name":"Retry"}`, 201)
	if !json.Valid([]byte(body)) {
		t.Fatal("invalid creation response")
	}
}
