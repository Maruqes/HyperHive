package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	pciGrpc "github.com/Maruqes/512SvMan/api/proto/pci"
)

func TestWriteHostGPUs(t *testing.T) {
	for _, tc := range []struct {
		name          string
		response      *pciGrpc.HostGPUList
		status, count int
	}{
		{"empty", &pciGrpc.HostGPUList{}, 200, 0},
		{"populated", &pciGrpc.HostGPUList{Gpus: []*pciGrpc.HostGPU{{Address: "0000:01:00.0"}}}, 200, 1},
		{"missing response", nil, 502, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeHostGPUs(w, tc.response)
			if w.Code != tc.status {
				t.Fatalf("status: got %d want %d", w.Code, tc.status)
			}
			if tc.status != 200 {
				return
			}
			var body struct {
				Gpus []json.RawMessage `json:"gpus"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Gpus == nil || len(body.Gpus) != tc.count {
				t.Fatalf("unexpected inventory: %s", w.Body.String())
			}
		})
	}
}
