package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"hyperhive/support/internal/domain"
	"hyperhive/support/internal/master"
	"hyperhive/support/internal/realtime"
	"hyperhive/support/internal/service"
	"hyperhive/support/internal/storage/sqlite"
)

type fakeMaster struct {
	mu            sync.Mutex
	fires         []master.FireRequest
	notifications []master.Notification
}

func (fake *fakeMaster) Fire(_ context.Context, request master.FireRequest) (string, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.fires = append(fake.fires, request)
	return "run", nil
}
func (fake *fakeMaster) Notify(_ context.Context, notification master.Notification) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.notifications = append(fake.notifications, notification)
	return nil
}
func (fake *fakeMaster) counts() (int, int) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return len(fake.fires), len(fake.notifications)
}

type automationFixture struct {
	t      *testing.T
	app    *service.Service
	master *fakeMaster
	clock  time.Time
	appID  string
	secret string
	path   string
}

func newAutomationFixture(t *testing.T) *automationFixture {
	f := &automationFixture{t: t, master: &fakeMaster{}, clock: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), path: filepath.Join(t.TempDir(), "support.db")}
	f.open()
	app, secret, err := f.app.SaveApp("", domain.AppSettings{Name: "DB", TabID: "general"})
	if err != nil {
		t.Fatal(err)
	}
	f.appID, f.secret = app.ID, secret
	f.ingest(`{"heartbeat":true,"streams":[{"name":"cpu","type":"number"}]}`)
	return f
}

func (f *automationFixture) open() {
	store, err := sqlite.Open(f.path)
	if err != nil {
		f.t.Fatal(err)
	}
	f.app, err = service.New(store, realtime.NewHub(), func() time.Time { return f.clock })
	if err != nil {
		f.t.Fatal(err)
	}
	f.app.SetMaster(f.master)
	application := f.app
	f.t.Cleanup(func() { application.Close() })
}

func (f *automationFixture) ingest(body string) {
	f.t.Helper()
	var request domain.IngestRequest
	if err := json.Unmarshal([]byte(body), &request); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.app.Ingest(context.Background(), f.appID, f.secret, request); err != nil {
		f.t.Fatal(err)
	}
}

func (f *automationFixture) cpu(value int, advance time.Duration) {
	f.t.Helper()
	f.clock = f.clock.Add(advance)
	f.ingest(`{"heartbeat":true,"samples":[{"stream":"cpu","value":` + jsonNumber(value) + `}]}`)
}

func jsonNumber(value int) string { data, _ := json.Marshal(value); return string(data) }

func (f *automationFixture) waitFires(fires, notifications int) {
	f.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		gotFires, gotNotifications := f.master.counts()
		if gotFires == fires && gotNotifications == notifications {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("fires=%d notifications=%d, want %d %d", gotFires, gotNotifications, fires, notifications)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func intPtr(value int) *int    { return &value }
func boolPtr(value bool) *bool { return &value }

func (f *automationFixture) create(condition string, cooldown, forSeconds, maxRuns int) domain.Automation {
	f.t.Helper()
	created, err := f.app.CreateAutomation(domain.AutomationInput{Name: "High CPU", Condition: condition})
	if err != nil {
		f.t.Fatal(err)
	}
	if created.Enabled || created.Timeout != 600 {
		f.t.Fatalf("defaults: %+v", created)
	}
	updated, err := f.app.UpdateAutomation(created.ID, domain.AutomationInput{Name: "High CPU", Condition: condition, Enabled: boolPtr(true), Cooldown: intPtr(cooldown), For: intPtr(forSeconds), Timeout: intPtr(30), MaxRunsPerHour: intPtr(maxRuns)})
	if err != nil {
		f.t.Fatal(err)
	}
	return updated
}

func TestAutomationEdgeTriggerAndRearm(t *testing.T) {
	f := newAutomationFixture(t)
	f.cpu(50, time.Second)
	automation := f.create("app('db').cpu >= 95", 0, 0, 10)
	f.cpu(97, time.Second)
	f.waitFires(1, 0)
	f.cpu(98, time.Second)
	f.cpu(99, time.Second)
	f.waitFires(1, 0)
	f.cpu(10, time.Second)
	f.cpu(96, time.Second)
	f.waitFires(2, 0)
	if f.master.fires[0].Snapshot["DB.cpu"] != 97.0 || f.master.fires[0].TimeoutSeconds != 30 || f.master.fires[0].Trigger != "condition" {
		t.Fatalf("fire request: %+v", f.master.fires[0])
	}
	got, err := f.app.Automation(automation.ID)
	if err != nil || got.State.LastFiredAt == nil || got.State.Armed {
		t.Fatalf("state: %+v %v", got.State, err)
	}
}

func TestAutomationBaselineDoesNotFireWhenAlreadyTrue(t *testing.T) {
	f := newAutomationFixture(t)
	f.cpu(97, time.Second)
	f.create("app('db').cpu >= 95", 0, 0, 10)
	f.cpu(98, time.Second)
	f.waitFires(0, 0)
	f.cpu(10, time.Second)
	f.cpu(98, time.Second)
	f.waitFires(1, 0)
}

func TestAutomationForCooldownAndRateCap(t *testing.T) {
	f := newAutomationFixture(t)
	f.cpu(10, time.Second)
	automation := f.create("app('db').cpu >= 95", 60, 30, 2)
	f.cpu(97, time.Second)
	f.cpu(97, 20*time.Second)
	f.waitFires(0, 0)
	f.cpu(97, 11*time.Second)
	f.waitFires(1, 0)
	f.cpu(10, time.Second)
	f.cpu(97, time.Second)
	f.cpu(97, 31*time.Second) // for satisfied, cooldown (60s) not yet
	f.waitFires(1, 0)
	f.cpu(97, 30*time.Second)
	f.waitFires(2, 0)
	f.cpu(10, time.Second)
	f.cpu(97, 61*time.Second)
	f.cpu(97, 31*time.Second)
	f.waitFires(2, 1)
	got, _ := f.app.Automation(automation.ID)
	if got.Enabled || got.State.DisabledReason == "" {
		t.Fatalf("expected auto-disable: %+v", got)
	}
}

func TestAutomationStaleValueAndPersistence(t *testing.T) {
	f := newAutomationFixture(t)
	f.cpu(10, time.Second)
	automation := f.create("app('db').cpu >= 95", 0, 0, 10)
	result, err := f.app.TestCondition("app('db').cpu < 95")
	if err != nil || result.Value == nil || !*result.Value {
		t.Fatalf("test condition: %+v %v", result, err)
	}
	f.clock = f.clock.Add(10 * time.Minute) // heartbeat timeout is 120s
	result, _ = f.app.TestCondition("app('db').cpu < 95")
	if *result.Value || result.Error == "" {
		t.Fatalf("stale value must be false with error: %+v", result)
	}
	result, _ = f.app.TestCondition("app('db').connectivity == 'offline'")
	if !*result.Value {
		t.Fatalf("offline: %+v", result)
	}
	if _, err := f.app.SaveAutomationSettings(domain.AutomationSettings{Paused: true}); err != nil {
		t.Fatal(err)
	}
	f.app.Close() // closed again by its cleanup; Close is idempotent
	f.open()
	got, err := f.app.Automation(automation.ID)
	if err != nil || !got.Enabled || got.Condition != automation.Condition || !f.app.AutomationSettings().Paused {
		t.Fatalf("reload: %+v %v", got, err)
	}
	if err := f.app.DeleteAutomation(automation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.Automation(automation.ID); err == nil {
		t.Fatal("deleted automation still present")
	}
}

func TestConditionCatalog(t *testing.T) {
	f := newAutomationFixture(t)
	f.cpu(42, time.Second)
	f.ingest(`{"heartbeat":true,"streams":[{"name":"disk.used","type":"number","unit":"%"}],"samples":[{"stream":"disk.used","value":7}]}`)
	catalog, err := f.app.ConditionCatalog()
	if err != nil || len(catalog.Apps) != 1 {
		t.Fatalf("catalog: %+v %v", catalog, err)
	}
	app := catalog.Apps[0]
	if app.Name != "DB" || app.Connectivity != "online" || len(app.Streams) != 2 {
		t.Fatalf("app: %+v", app)
	}
	if cpu := app.Streams[0]; cpu.Name != "cpu" || cpu.Type != "number" || cpu.Value != float64(42) {
		t.Fatalf("cpu: %+v", cpu)
	}
	if disk := app.Streams[1]; disk.Name != "disk.used" || disk.Unit != "%" || disk.Value != float64(7) {
		t.Fatalf("disk: %+v", disk)
	}
	f.clock = f.clock.Add(10 * time.Minute)
	catalog, _ = f.app.ConditionCatalog()
	if value := catalog.Apps[0].Streams[0].Value; value != nil {
		t.Fatalf("stale value listed: %v", value)
	}
}

func TestAppValues(t *testing.T) {
	f := newAutomationFixture(t)
	f.cpu(97, time.Second)
	values, err := f.app.AppValues("db", "")
	if err != nil || values.Name != "DB" || values.Connectivity != "online" || values.Streams["cpu"] != float64(97) {
		t.Fatalf("values: %+v %v", values, err)
	}
	f.cpu(12, time.Second)
	if values, _ = f.app.AppValues("DB", ""); values.Streams["cpu"] != float64(12) {
		t.Fatalf("values must follow the latest sample: %+v", values)
	}
	var problem *domain.Error
	if _, err := f.app.AppValues("nope", ""); !errors.As(err, &problem) || problem.Kind != domain.MissingResource {
		t.Fatalf("unknown App: %v", err)
	}
	if _, err := f.app.AppValues("db", "missing"); !errors.As(err, &problem) || problem.Kind != domain.MissingResource {
		t.Fatalf("unknown instance: %v", err)
	}
	if _, err := f.app.AppValues(" ", ""); !errors.As(err, &problem) || problem.Kind != domain.InvalidInput {
		t.Fatalf("empty app: %v", err)
	}
	f.clock = f.clock.Add(10 * time.Minute)
	if values, _ = f.app.AppValues("db", ""); values.Connectivity != "offline" || len(values.Streams) != 0 {
		t.Fatalf("stale values listed: %+v", values)
	}
}
