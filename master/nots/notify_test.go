package nots

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"512SvMan/db"
)

type fakeChannel struct {
	name string
	mu   sync.Mutex
	got  []Notification
}

func (f *fakeChannel) Name() string { return f.name }
func (f *fakeChannel) Send(_ context.Context, n Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, n)
	return nil
}
func (f *fakeChannel) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.got) }

type fakeHistory struct{ saved []Notification }

func (f *fakeHistory) Save(_ context.Context, n Notification, _ time.Time) error {
	f.saved = append(f.saved, n)
	return nil
}

type fakeRules map[string]Rule

func (f fakeRules) Rule(_ context.Context, category string) (Rule, bool, error) {
	r, ok := f[category]
	return r, ok, nil
}

func newTestNotifier(rules fakeRules, now time.Time) (*Notifier, *fakeHistory, *fakeChannel, *fakeChannel) {
	h, ws, push := &fakeHistory{}, &fakeChannel{name: "ws"}, &fakeChannel{name: "push"}
	x := NewNotifier(h, rules, ws, push)
	x.now = func() time.Time { return now }
	return x, h, ws, push
}

func TestDefaultPolicyBySeverityAndCategory(t *testing.T) {
	for _, tc := range []struct {
		event    string
		severity Severity
		push     bool
	}{
		{"metrics.cpu_temperature", SeverityWarning, false},
		{"metrics.cpu_temperature", SeverityError, true},
		{"operation.backup", SeverityInfo, true}, // finished operations push at low priority
		{"health.smart", SeverityWarning, false},
		{"connection.lost", SeverityCritical, true},
		{"", SeverityInfo, false}, // legacy
	} {
		x, h, ws, push := newTestNotifier(nil, time.Now())
		if err := x.Notify(context.Background(), Notification{Event: tc.event, Severity: tc.severity, Title: "t"}); err != nil {
			t.Fatal(err)
		}
		x.Wait()
		if len(h.saved) != 1 || ws.count() != 1 || (push.count() == 1) != tc.push {
			t.Fatalf("%s/%s: history=%d ws=%d push=%d", tc.event, tc.severity, len(h.saved), ws.count(), push.count())
		}
	}
}

func TestDisabledRuleKeepsHistoryOnly(t *testing.T) {
	x, h, ws, push := newTestNotifier(fakeRules{"health": {Category: "health", Enabled: false}}, time.Now())
	_ = x.Notify(context.Background(), Notification{Event: "health.smart", Severity: SeverityCritical, Title: "t"})
	x.Wait()
	if len(h.saved) != 1 || ws.count() != 0 || push.count() != 0 {
		t.Fatalf("history=%d ws=%d push=%d", len(h.saved), ws.count(), push.count())
	}
}

func TestQuietHoursHoldBackNonCriticalPush(t *testing.T) {
	night := time.Date(2026, 10, 30, 23, 30, 0, 0, time.Local)
	rule := Rule{Category: "metrics", Enabled: true, MinPushSeverity: SeverityError, QuietStart: "22:00", QuietEnd: "07:00"}
	x, _, ws, push := newTestNotifier(fakeRules{"metrics": rule}, night)
	_ = x.Notify(context.Background(), Notification{Event: "metrics.memory", Severity: SeverityError, Title: "t"})
	x.Wait()
	if ws.count() != 1 || push.count() != 0 {
		t.Fatalf("error pushed during quiet hours: ws=%d push=%d", ws.count(), push.count())
	}
	_ = x.Notify(context.Background(), Notification{Event: "metrics.memory", Severity: SeverityCritical, Title: "t"})
	x.Wait()
	if push.count() != 1 {
		t.Fatal("critical held back by quiet hours")
	}
}

func TestQuietHoursWindow(t *testing.T) {
	r := Rule{QuietStart: "22:00", QuietEnd: "07:00"}
	at := func(h, m int) time.Time { return time.Date(2026, 1, 1, h, m, 0, 0, time.Local) }
	if !r.inQuietHours(at(23, 0)) || !r.inQuietHours(at(6, 59)) || r.inQuietHours(at(7, 0)) || r.inQuietHours(at(12, 0)) {
		t.Fatal("overnight window wrong")
	}
	r = Rule{QuietStart: "12:00", QuietEnd: "13:00"}
	if !r.inQuietHours(at(12, 30)) || r.inQuietHours(at(13, 0)) {
		t.Fatal("daytime window wrong")
	}
}

func TestDedupSuppressesRepeatsInsideWindow(t *testing.T) {
	now := time.Now()
	x, h, _, _ := newTestNotifier(nil, now)
	n := Notification{Event: "health.smart", Severity: SeverityWarning, Title: "t", DedupKey: "smart:node1:/dev/sda"}
	_ = x.Notify(context.Background(), n)
	_ = x.Notify(context.Background(), n)
	x.now = func() time.Time { return now.Add(25 * time.Hour) }
	_ = x.Notify(context.Background(), n)
	x.Wait()
	if len(h.saved) != 2 {
		t.Fatalf("saved %d, want 2", len(h.saved))
	}
}

func TestRuleValidate(t *testing.T) {
	if err := (Rule{Category: "nope"}).Validate(); err == nil {
		t.Fatal("unknown category accepted")
	}
	if err := (Rule{Category: "metrics", QuietStart: "22:00"}).Validate(); err == nil {
		t.Fatal("half quiet window accepted")
	}
	if err := (Rule{Category: "metrics", QuietStart: "25:00", QuietEnd: "07:00"}).Validate(); err == nil {
		t.Fatal("bad time accepted")
	}
	if err := (Rule{Category: "metrics", QuietStart: "22:00", QuietEnd: "07:00"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestParseSeverity(t *testing.T) {
	for in, want := range map[string]Severity{"info": SeverityInfo, "warn": SeverityWarning, "Warning": SeverityWarning, "error": SeverityError, "critical": SeverityCritical} {
		if got, err := ParseSeverity(in); err != nil || got != want {
			t.Fatalf("%s: %v %v", in, got, err)
		}
	}
	if _, err := ParseSeverity("urgent"); err == nil {
		t.Fatal("invalid severity accepted")
	}
}

type fakeFCM struct {
	mu   sync.Mutex
	errs map[string]error
	sent []string
}

func (f *fakeFCM) SendNotification(_ context.Context, token string, _ Notification) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, token)
	return "id", f.errs[token]
}

func TestFCMRemovesUnregisteredTokensOnly(t *testing.T) {
	sender := &fakeFCM{errs: map[string]error{
		"gone":  &FirebaseError{Status: http.StatusBadRequest, Unregistered: true},
		"flaky": errors.New("timeout"),
	}}
	var mu sync.Mutex
	var removed []string
	devices := []db.FirebaseDevice{{Token: "ok"}, {Token: "gone"}, {Token: "flaky"}}
	_ = sendToDevices(context.Background(), sender, devices, Notification{}, func(_ context.Context, token string) error {
		mu.Lock()
		defer mu.Unlock()
		removed = append(removed, token)
		return nil
	})
	if len(sender.sent) != 3 || len(removed) != 1 || removed[0] != "gone" {
		t.Fatalf("sent=%v removed=%v", sender.sent, removed)
	}
}
