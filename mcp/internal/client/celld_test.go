package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCelldOptionalQueryValues(t *testing.T) {
	zero := int64(0)
	limit := 0
	download := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.RawQuery; got != "download=false&from=0&limit=0&to=0" {
			t.Errorf("explicit query values lost: %s", got)
		}
		fmt.Fprint(w, `{"records":[],"from":0,"to":0,"truncated":false,"incomplete":true}`)
	}))
	defer backend.Close()
	api, err := New(Config{BaseURL: backend.URL, Token: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := api.GetCelldLogs(context.Background(), CelldObservationInput{CelldFleetIDInput: CelldFleetIDInput{ID: "fleet_one"}, From: &zero, To: &zero, Limit: &limit, Download: &download})
	if err != nil || result["from"] != json.Number("0") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestCelldExportResponseRejections(t *testing.T) {
	for _, tc := range []struct{ name, contentType, body, truncated string }{
		{"wrong media", "application/json", `{"records":[]}`, ""},
		{"invalid record", "application/x-ndjson", `{"id":"r"}`, ""},
		{"trailing data", "application/x-ndjson", `{"id":"r","node":"n","generation":"g","received_ms":1} {}`, ""},
		{"bad header", "application/x-ndjson", "", "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				if tc.truncated != "" {
					w.Header().Set("X-Observability-Truncated", tc.truncated)
				}
				fmt.Fprint(w, tc.body)
			}))
			defer backend.Close()
			api, err := New(Config{BaseURL: backend.URL, Token: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			yes := true
			_, err = api.GetCelldEvents(context.Background(), CelldObservationInput{CelldFleetIDInput: CelldFleetIDInput{ID: "fleet_one"}, Download: &yes})
			if err == nil {
				t.Fatal("invalid export accepted")
			}
		})
	}
}
