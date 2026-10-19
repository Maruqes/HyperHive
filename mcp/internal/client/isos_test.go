package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestISODownloadSafetyBounds(t *testing.T) {
	valid := StartISODownloadInput{URL: "https://fixture.test/image.iso", ISOName: "x0.iso", NFSShareID: 1}
	if err := ValidateStartISODownload(valid); err != nil {
		t.Fatalf("safe leaf rejected: %v", err)
	}
	for _, name := range []string{"../outside.iso", "/absolute.iso", `a\b.iso`, "nul\x00.iso"} {
		input := valid
		input.ISOName = name
		if ValidateStartISODownload(input) == nil {
			t.Fatalf("unsafe filename accepted %q", name)
		}
	}
}

func TestISODownloadExactIntegerAndAcceptance(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	status := 202
	response := `{"status":"started","counter":9007199254740993}`
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		var body struct {
			URL, ISOName string
			NFSShareID   json.Number `json:"nfs_share_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NFSShareID != "9007199254740993" {
			t.Errorf("ID changed: %v %#v", err, body)
		}
		w.WriteHeader(status)
		fmt.Fprint(w, response)
	}))
	defer backend.Close()
	c, err := New(Config{BaseURL: backend.URL, Token: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	input := StartISODownloadInput{URL: "https://fixture.test/x.iso", ISOName: "x.iso", NFSShareID: 9007199254740993}
	accepted, err := c.StartISODownload(context.Background(), input)
	if err != nil || accepted["status"] != "started" || accepted["counter"] != json.Number("9007199254740993") {
		t.Fatalf("acceptance changed %v %#v", err, accepted)
	}
	for _, code := range []int{200, 201, 204, 400, 401, 403, 404, 409, 500} {
		mu.Lock()
		status = code
		before := calls
		mu.Unlock()
		if _, err := c.StartISODownload(context.Background(), input); err == nil {
			t.Fatalf("accepted HTTP %d", code)
		}
		mu.Lock()
		after := calls
		mu.Unlock()
		if after != before+1 {
			t.Fatal("request retried")
		}
	}
}
