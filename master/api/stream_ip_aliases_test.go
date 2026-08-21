package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func withTempAliasPath(t *testing.T) {
	t.Helper()
	old := observabilityAliasPath
	observabilityAliasPath = filepath.Join(t.TempDir(), "stream-ip-aliases.json")
	t.Cleanup(func() { observabilityAliasPath = old })
}

func TestObservabilityAliasLifecycle(t *testing.T) {
	withTempAliasPath(t)

	add := httptest.NewRequest(http.MethodPost, "/streamInfo/ip-aliases", strings.NewReader(`{"ip":"192.168.76.1","alias":"vm.minelive"}`))
	recorder := httptest.NewRecorder()
	addObservabilityAlias(recorder, add)
	if recorder.Code != http.StatusOK {
		t.Fatalf("add alias status = %d, body = %s", recorder.Code, recorder.Body)
	}

	list := httptest.NewRequest(http.MethodGet, "/streamInfo/ip-aliases", nil)
	recorder = httptest.NewRecorder()
	listObservabilityAliases(recorder, list)
	var payload struct {
		Aliases []observabilityAlias `json:"aliases"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Aliases) != 1 || payload.Aliases[0].Alias != "vm.minelive" {
		t.Fatalf("aliases = %#v", payload.Aliases)
	}

	remove := httptest.NewRequest(http.MethodDelete, "/streamInfo/ip-aliases", strings.NewReader(`{"ip":"192.168.76.1","alias":"vm.minelive"}`))
	recorder = httptest.NewRecorder()
	removeObservabilityAlias(recorder, remove)
	if recorder.Code != http.StatusOK {
		t.Fatalf("remove alias status = %d, body = %s", recorder.Code, recorder.Body)
	}

	recorder = httptest.NewRecorder()
	listObservabilityAliases(recorder, list)
	payload.Aliases = nil
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Aliases) != 0 {
		t.Fatalf("aliases after delete = %#v", payload.Aliases)
	}
}

func TestObservabilityAliasValidation(t *testing.T) {
	withTempAliasPath(t)
	for _, body := range []string{`{"ip":"nope","alias":"x"}`, `{"ip":"192.168.1.1","alias":"has space"}`} {
		req := httptest.NewRequest(http.MethodPost, "/streamInfo/ip-aliases", strings.NewReader(body))
		recorder := httptest.NewRecorder()
		addObservabilityAlias(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d", body, recorder.Code)
		}
	}
}
