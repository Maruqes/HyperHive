package master

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientFireAndConflict(t *testing.T) {
	conflict := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-HH-Support-Key") != "secret" || r.URL.Path != "/support-internal/automations/fire" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var request FireRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.AutomationID != "a1" || request.Snapshot["db.cpu"] != 97.0 {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		if conflict {
			http.Error(w, "automation is already running", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"run_id":"r1"}`))
	}))
	defer server.Close()
	client := New(server.URL+"/", "secret")
	request := FireRequest{AutomationID: "a1", Trigger: "condition", Snapshot: map[string]any{"db.cpu": 97.0}, TimeoutSeconds: 60}
	if runID, err := client.Fire(context.Background(), request); err != nil || runID != "r1" {
		t.Fatalf("fire: %q %v", runID, err)
	}
	conflict = true
	if _, err := client.Fire(context.Background(), request); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("conflict: %v", err)
	}
	if err := New(server.URL, "wrong").Notify(context.Background(), Notification{Title: "x"}); err == nil {
		t.Fatal("unauthorized notify succeeded")
	}
}
