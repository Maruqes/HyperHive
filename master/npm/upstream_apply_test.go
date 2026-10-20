package npm

import (
	"512SvMan/db"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
)

type fakeUpstreamRuntime struct {
	applyCalls  int
	failCalls   map[int]bool
	unavailable bool
}

func (f *fakeUpstreamRuntime) Verify(context.Context) error {
	if f.unavailable {
		return errors.New("unavailable")
	}
	return nil
}
func (f *fakeUpstreamRuntime) Apply(_ context.Context, _ int64, _ []Upstream) error {
	f.applyCalls++
	if f.failCalls[f.applyCalls] {
		return errors.New("injected apply failure")
	}
	return nil
}

func upstreamTestDB(t *testing.T) *fakeUpstreamRuntime {
	t.Helper()
	oldDB := db.DB
	oldRuntime := upstreamCoordinator.runtime
	oldRecovery := upstreamCoordinator.recovery
	oldReady := upstreamCoordinator.ready
	var err error
	db.DB, err = sql.Open("sqlite3", filepath.Join(t.TempDir(), "upstreams.db")+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateNPMUpstreamTables(context.Background()); err != nil {
		t.Fatal(err)
	}
	f := &fakeUpstreamRuntime{failCalls: map[int]bool{}}
	upstreamCoordinator.runtime = f
	upstreamCoordinator.recovery = 0
	upstreamCoordinator.ready = true
	t.Cleanup(func() {
		db.DB.Close()
		db.DB = oldDB
		upstreamCoordinator.runtime = oldRuntime
		upstreamCoordinator.recovery = oldRecovery
		upstreamCoordinator.ready = oldReady
	})
	return f
}

func upstreamTestContext(t *testing.T) context.Context {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)
	return WithUpstreamNPMContext(context.Background(), server.URL, "fixture")
}

func TestUpstreamPersistenceRevisionAndCompensation(t *testing.T) {
	f := upstreamTestDB(t)
	ctx := upstreamTestContext(t)
	p, err := SaveUpstream(ctx, exampleUpstream(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID < 1 || p.Revision != 1 || p.ApplyStatus != "applied" || p.Generation == 0 {
		t.Fatalf("wrong result: %+v", p)
	}
	duplicate := exampleUpstream()
	duplicate.Name = "APP_POOL"
	if _, err := SaveUpstream(ctx, duplicate, 0); err == nil {
		t.Fatal("duplicate accepted")
	}
	p.Name = "renamed"
	if _, err := SaveUpstream(ctx, p, 9); err == nil {
		t.Fatal("stale revision accepted")
	}
	updated, err := SaveUpstream(ctx, p, 1)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != p.ID || updated.Revision != 2 {
		t.Fatal("unstable identity")
	}
	f.failCalls[f.applyCalls+1] = true
	updated.Members[0].Port = 9999
	if _, err := SaveUpstream(ctx, updated, 2); err == nil {
		t.Fatal("apply failure accepted")
	}
	saved, err := GetUpstream(ctx, p.ID)
	if err != nil || saved.Members[0].Port != 8080 || saved.Revision != 2 {
		t.Fatalf("last-good lost: %+v %v", saved, err)
	}
	pending, err := db.PendingNPMUpstreamOperations(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("rollback not journaled: %v %v", pending, err)
	}
	if err := InitializeUpstreams(ctx); err != nil {
		t.Fatal(err)
	}
	saved, err = GetUpstream(ctx, p.ID)
	if err != nil || saved.Name != "renamed" {
		t.Fatal("restart persistence failed")
	}
	if err := DeleteUpstream(ctx, p.ID, 2); err != nil {
		t.Fatal(err)
	}
	ps, err := ListUpstreams(ctx)
	if err != nil || len(ps) != 0 {
		t.Fatal("delete failed")
	}
}

func TestUpstreamRecoveryRequiredAndRestart(t *testing.T) {
	f := upstreamTestDB(t)
	ctx := upstreamTestContext(t)
	p, err := SaveUpstream(ctx, exampleUpstream(), 0)
	if err != nil {
		t.Fatal(err)
	}
	f.failCalls[2] = true
	f.failCalls[3] = true
	p.Name = "failed_change"
	_, err = SaveUpstream(ctx, p, 1)
	var typed *UpstreamError
	if !errors.As(err, &typed) || typed.RecoveryID == 0 {
		t.Fatalf("missing recovery ID: %v", err)
	}
	if _, err := SaveUpstream(ctx, exampleUpstream(), 0); err == nil {
		t.Fatal("mutation during recovery accepted")
	}
	f.failCalls = nil
	if err := InitializeUpstreams(ctx); err != nil {
		t.Fatal(err)
	}
	saved, err := GetUpstream(ctx, p.ID)
	if err != nil || saved.Name != "app_pool" {
		t.Fatal("last-good recovery failed")
	}
}

func TestUpstreamUnavailableDoesNotCreateJournal(t *testing.T) {
	f := upstreamTestDB(t)
	f.unavailable = true
	_, err := SaveUpstream(context.Background(), exampleUpstream(), 0)
	if err == nil {
		t.Fatal("unavailable adapter accepted")
	}
	pending, err := db.PendingNPMUpstreamOperations(context.Background())
	if err != nil || len(pending) != 0 {
		t.Fatalf("unavailable preflight poisoned journal: %v %v", pending, err)
	}
}

func TestUpstreamSelectionOmissionNullAndConflicts(t *testing.T) {
	pools := []Upstream{{ID: 42, Context: "http"}, {ID: 43, Context: "stream"}}
	for _, tc := range []struct {
		name, raw, host, context string
		old                      int64
		port                     int
		want                     int64
		fail                     bool
	}{
		{name: "omitted preserves", old: 42, context: "http", want: 42},
		{name: "explicit detach", raw: "null", old: 42, host: "127.0.0.1", port: 8080, context: "http"},
		{name: "detach requires target", raw: "null", old: 42, context: "http", fail: true},
		{name: "select", raw: "42", context: "http", want: 42},
		{name: "conflict", raw: "42", context: "http", host: "127.0.0.1", port: 80, fail: true},
		{name: "wrong context", raw: "43", context: "http", fail: true},
		{name: "invalid id", raw: `"42"`, context: "http", fail: true},
		{name: "raw marker", host: "hh_upstream_42", port: 80, context: "http", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, _, _, err := selectedPool(json.RawMessage(tc.raw), tc.old, tc.host, tc.port, tc.context, pools)
			if (err != nil) != tc.fail || !tc.fail && id != tc.want {
				t.Fatalf("id=%d error=%v", id, err)
			}
		})
	}
}

func TestUpstreamCommitFailureRestoresSnapshot(t *testing.T) {
	upstreamTestDB(t)
	ctx := upstreamTestContext(t)
	p, err := SaveUpstream(ctx, exampleUpstream(), 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.Exec(`CREATE TRIGGER fail_upstream_commit BEFORE UPDATE OF state ON npm_upstream_operations WHEN NEW.state='applied' BEGIN SELECT RAISE(ABORT,'injected commit failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	p.Name = "must_not_commit"
	if _, err := SaveUpstream(ctx, p, 1); err == nil {
		t.Fatal("commit failure accepted")
	}
	old, err := GetUpstream(ctx, p.ID)
	if err != nil || old.Name != "app_pool" || old.Revision != 1 {
		t.Fatalf("last-good definition lost: %+v %v", old, err)
	}
	pending, err := db.PendingNPMUpstreamOperations(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatal("compensation not committed")
	}
}

func TestUpstreamConcurrentBindAndDelete(t *testing.T) {
	for iteration := 0; iteration < 8; iteration++ {
		t.Run(fmt.Sprint(iteration), func(t *testing.T) {
			upstreamTestDB(t)
			var mutex sync.Mutex
			var consumers []Proxy
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mutex.Lock()
				defer mutex.Unlock()
				if r.Method == "GET" {
					if r.URL.Path == "/api/nginx/streams" {
						w.Write([]byte(`[]`))
					} else {
						if consumers == nil {
							w.Write([]byte(`[]`))
						} else {
							json.NewEncoder(w).Encode(consumers)
						}
					}
					return
				}
				if r.Method != "POST" {
					t.Errorf("unexpected mutation %s", r.Method)
					w.WriteHeader(500)
					return
				}
				var p Proxy
				if json.NewDecoder(r.Body).Decode(&p) != nil {
					t.Error("invalid payload")
				}
				p.ID = 7
				p.Enabled = true
				consumers = append(consumers, p)
				w.Write([]byte(`{"id":7}`))
			}))
			defer server.Close()
			ctx := WithUpstreamNPMContext(context.Background(), server.URL, "fixture")
			pool, err := SaveUpstream(ctx, exampleUpstream(), 0)
			if err != nil {
				t.Fatal(err)
			}
			selection, _ := json.Marshal(pool.ID)
			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(2)
			var bindErr, deleteErr error
			go func() {
				defer wg.Done()
				<-start
				_, bindErr = CreateProxy(server.URL, "fixture", Proxy{DomainNames: []string{"concurrent.test"}, ForwardScheme: "http", UpstreamID: selection, AccessListID: "0", Meta: map[string]any{}, Locations: []Location{}})
			}()
			go func() { defer wg.Done(); <-start; deleteErr = DeleteUpstream(ctx, pool.ID, 1) }()
			close(start)
			wg.Wait()
			if (bindErr == nil) == (deleteErr == nil) {
				t.Fatalf("expected one winner: bind=%v delete=%v", bindErr, deleteErr)
			}
			pools, err := db.ListNPMUpstreams(ctx)
			if err != nil {
				t.Fatal(err)
			}
			bindings, err := db.ListNPMUpstreamBindings(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(bindings) > 0 && len(pools) == 0 {
				t.Fatal("orphan reference")
			}
		})
	}
}

func TestUpstreamCreateCommitFailureCompletesWithoutDeletion(t *testing.T) {
	upstreamTestDB(t)
	ctx := upstreamTestContext(t)
	if _, err := db.DB.Exec(`CREATE TRIGGER fail_create_commit BEFORE UPDATE OF state ON npm_upstream_operations WHEN NEW.state='applied' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := SaveUpstream(ctx, exampleUpstream(), 0)
	var failure *UpstreamError
	if !errors.As(err, &failure) || failure.RecoveryID == 0 {
		t.Fatalf("missing recovery state: %v", err)
	}
	if _, err := db.DB.Exec("DROP TRIGGER fail_create_commit"); err != nil {
		t.Fatal(err)
	}
	if err := InitializeUpstreams(ctx); err != nil {
		t.Fatal(err)
	}
	pools, err := ListUpstreams(ctx)
	if err != nil || len(pools) != 1 || pools[0].Name != "app_pool" || pools[0].ApplyStatus != "applied" {
		t.Fatalf("journaled create not completed: %+v %v", pools, err)
	}
}
