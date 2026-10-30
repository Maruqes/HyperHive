package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"512SvMan/db"
	"512SvMan/nots"

	"github.com/go-chi/chi/v5"
)

func TestFirebaseDeviceRegistration(t *testing.T) {
	var saved []db.FirebaseDevice
	var deleted []string
	store := firebaseDeviceStore{
		save:   func(_ *http.Request, d db.FirebaseDevice) error { saved = append(saved, d); return nil },
		delete: func(_ *http.Request, token string) error { deleted = append(deleted, token); return nil },
	}
	for _, tc := range []struct {
		body   string
		auth   bool
		status int
	}{
		{`{"projectId":"example-project","token":"device-one"}`, false, 401},
		{`{"projectId":"example-project","token":"bad token"}`, true, 400},
		{`{"projectId":"x","token":"device-one"}`, true, 400},
		{`{"projectId":"example-project","token":"device-one","extra":1}`, true, 400},
		{`{"projectId":"example-project","token":"device-one"}`, true, 200},
	} {
		w := httptest.NewRecorder()
		store.register(w, firebaseRequest(tc.body, tc.auth))
		if w.Code != tc.status {
			t.Fatalf("%s: got %d want %d", tc.body, w.Code, tc.status)
		}
	}
	if len(saved) != 1 || saved[0].Token != "device-one" || saved[0].UserID != 1 || saved[0].ProjectID != "example-project" {
		t.Fatalf("saved: %+v", saved)
	}
	w := httptest.NewRecorder()
	store.unregister(w, firebaseRequest(`{"token":"device-one"}`, true))
	if w.Code != 200 || len(deleted) != 1 || deleted[0] != "device-one" {
		t.Fatalf("unregister: %d %v", w.Code, deleted)
	}
}

func TestNotificationRulesHandlers(t *testing.T) {
	var saved []nots.Rule
	store := notificationRuleStore{
		list: func(context.Context) ([]nots.Rule, error) {
			return []nots.Rule{nots.DefaultRule("metrics"), nots.DefaultRule("operation")}, nil
		},
		save: func(_ context.Context, r nots.Rule) error { saved = append(saved, r); return nil },
	}
	router := chi.NewRouter()
	router.Get("/notification/rules", store.listRules)
	router.Put("/notification/rules/{category}", store.putRule)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/notification/rules", nil))
	var listed []notificationRuleDTO
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || len(listed) != 2 || listed[0].MinPushSeverity != "error" || listed[1].MinPushSeverity != "info" || listed[0].Description == "" {
		t.Fatalf("list: %s", w.Body.String())
	}

	for _, tc := range []struct {
		category, body string
		status         int
	}{
		{"nope", `{"enabled":true,"minPushSeverity":"error"}`, 404},
		{"metrics", `{"enabled":true}`, 400},
		{"metrics", `{"enabled":true,"minPushSeverity":"urgent"}`, 400},
		{"metrics", `{"enabled":true,"minPushSeverity":"error","quietStart":"22:00"}`, 400},
		{"metrics", `{"enabled":false,"minPushSeverity":"warning","quietStart":"22:00","quietEnd":"07:00"}`, 200},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("PUT", "/notification/rules/"+tc.category, strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatalf("%s %s: got %d want %d (%s)", tc.category, tc.body, w.Code, tc.status, w.Body.String())
		}
	}
	if len(saved) != 1 || saved[0].Enabled || saved[0].MinPushSeverity != nots.SeverityWarning || saved[0].QuietEnd != "07:00" {
		t.Fatalf("saved: %+v", saved)
	}
}
