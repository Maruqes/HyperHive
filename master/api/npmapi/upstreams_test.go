package npmapi

import (
	"512SvMan/db"
	"512SvMan/npm"
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestUpstreamHTTPContracts(t *testing.T) {
	oldDB := db.DB
	var err error
	db.DB, err = sql.Open("sqlite3", filepath.Join(t.TempDir(), "api.db")+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close(); db.DB = oldDB })
	ctx := context.Background()
	if err := db.CreateNPMUpstreamTables(ctx); err != nil {
		t.Fatal(err)
	}
	if err := npm.InitializeUpstreams(ctx); err != nil {
		t.Fatal(err)
	}
	p := npm.Upstream{ID: 42, Name: "api_pool", Context: "http", Method: "round_robin", Revision: 3, ApplyStatus: "applied", Generation: 6, Members: []npm.UpstreamMember{{IP: "127.0.0.1", Port: 8080}}, References: []npm.UpstreamReference{}}
	raw, _ := json.Marshal(p)
	op, err := db.BeginNPMUpstreamOperation(ctx, []byte(`{}`), raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CommitNPMUpstreamSnapshot(ctx, op, []db.NPMUpstreamRow{{ID: 42, Name: p.Name, Context: p.Context, Revision: 3, Definition: raw}}, []db.NPMUpstreamBinding{{Consumer: "proxy", ID: 7, PoolID: 42, Expected: json.RawMessage(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	SetupUpstreamAPI(router)
	for _, tc := range []struct {
		method, path, body, match, want string
		status                          int
	}{
		{method: "GET", path: "/upstream/list", status: 200, want: `"references":[{"consumer_type":"proxy","npm_id":7}]`},
		{method: "GET", path: "/upstream/42", status: 200, want: `"revision":3`},
		{method: "GET", path: "/upstream/999", status: 404, want: "upstream_not_found"},
		{method: "GET", path: "/upstream/not-an-id", status: 400, want: "invalid_upstream_id"},
		{method: "POST", path: "/upstream/create", body: `{"name":"bad","loadbalancer_type":"round_robin","server_arr":[{"ip":"127.0.0.1","port":80,"settings_string":"weight=1;"}]}`, status: 400, want: "settings"},
		{method: "POST", path: "/upstream/create", body: `{"unknown":true}`, status: 400, want: "invalid_upstream_body"},
		{method: "POST", path: "/upstream/create", body: `{} {}`, status: 400, want: "invalid_upstream_body"},
		{method: "POST", path: "/upstream/create", body: `{"name":"api_pool","loadbalancer_type":"round_robin","server_arr":[{"ip":"127.0.0.1","port":80,"settings_string":""}]}`, status: 409, want: "upstream_name_conflict"},
		{method: "PUT", path: "/upstream/42", body: `{"name":"api_pool","revision":2,"loadbalancer_type":"round_robin","server_arr":[{"ip":"127.0.0.1","port":80,"settings_string":""}]}`, status: 409, want: "upstream_revision_conflict"},
		{method: "PUT", path: "/upstream/42", body: `{"name":"api_pool"}`, status: 400, want: "expected_revision"},
		{method: "DELETE", path: "/upstream/42", status: 400, want: "If-Match"},
		{method: "DELETE", path: "/upstream/42", match: `"3"`, status: 409, want: "upstream_referenced"},
		{method: "POST", path: "/upstream/create", body: strings.Repeat(" ", 65537) + `{}`, status: 400, want: "invalid_upstream_body"},
	} {
		t.Run(tc.method+tc.path+tc.want, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.match != "" {
				r.Header.Set("If-Match", tc.match)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Content-Type") != "application/json" {
				t.Fatal("unstructured response")
			}
			if tc.path == "/upstream/42" && tc.method == "GET" && w.Header().Get("ETag") != `"3"` {
				t.Fatal("missing revision ETag")
			}
		})
	}
}
