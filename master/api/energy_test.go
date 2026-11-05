package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"512SvMan/db"
	"512SvMan/services"

	"github.com/go-chi/chi/v5"
)

func openEnergyAPIDB(t *testing.T) {
	t.Helper()
	previous := db.DB
	fixture, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	fixture.SetMaxOpenConns(1)
	db.DB = fixture
	t.Cleanup(func() {
		fixture.Close()
		db.DB = previous
	})
	if err := db.CreateEnergyTables(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestEnergyPriceHandler(t *testing.T) {
	openEnergyAPIDB(t)
	previous := services.Energy
	services.Energy = services.NewEnergyService()
	t.Cleanup(func() { services.Energy = previous })

	router := chi.NewRouter()
	setupEnergyAPI(router)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}

	for _, tc := range []struct {
		body string
		want float64
	}{
		{`{"pricePerKWh":0}`, 0},
		{`{"pricePerKWh":0.25}`, 0.25},
		{`{"pricePerKWh":10}`, 10},
	} {
		rec := do(http.MethodPut, "/energy/price", tc.body)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s status %d: %s", tc.body, rec.Code, rec.Body)
		}
		var response map[string]float64
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response["pricePerKWh"] != tc.want {
			t.Fatalf("response = %v, want %v", response, tc.want)
		}
	}

	for _, body := range []string{
		`{`,
		`{}`,
		`{"pricePerKWh":null}`,
		`{"pricePerKWh":-0.1}`,
		`{"pricePerKWh":10.1}`,
		`{"pricePerKWh":"0.1"}`,
	} {
		if rec := do(http.MethodPut, "/energy/price", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s status %d, want 400", body, rec.Code)
		}
	}

	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		price := value
		if err := validateEnergyPrice(&price); err == nil {
			t.Fatalf("validated %v as a price", value)
		}
	}
}

func TestEnergySummaryHandlerShape(t *testing.T) {
	openEnergyAPIDB(t)
	previous := services.Energy
	services.Energy = services.NewEnergyService()
	t.Cleanup(func() { services.Energy = previous })

	ctx := context.Background()
	month := time.Now().Format("2006-01")
	for _, row := range []struct {
		machine string
		vm      string
		wh      float64
		cost    float64
	}{
		{"node-a", "", 600, 0.15},
		{"node-a", "vm1", 300, 0.075},
		{"node-b", "", 400, 0.1},
	} {
		if err := db.AddEnergyUsage(ctx, month, row.machine, row.vm, row.wh, row.cost); err != nil {
			t.Fatal(err)
		}
	}

	router := chi.NewRouter()
	setupEnergyAPI(router)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/energy/summary", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", rec.Code, rec.Body)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"pricePerKWh", "totals", "machines", "vms"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("missing %q in %s", key, rec.Body)
		}
	}
	if body["pricePerKWh"] != nil {
		t.Fatalf("unset price = %v", body["pricePerKWh"])
	}

	totals := body["totals"].(map[string]any)
	for _, key := range []string{"liveWatts", "monthKWh", "monthCost"} {
		if _, ok := totals[key]; !ok {
			t.Fatalf("missing totals.%s in %s", key, rec.Body)
		}
	}
	if totals["monthKWh"] != 1.0 || math.Abs(totals["monthCost"].(float64)-0.25) > 1e-9 {
		t.Fatalf("totals = %+v", totals)
	}

	machines := body["machines"].([]any)
	if len(machines) != 2 {
		t.Fatalf("machines = %+v", machines)
	}
	machine := machines[0].(map[string]any)
	for _, key := range []string{"machine", "liveWatts", "monthKWh", "monthCost", "cpuEstimate"} {
		if _, ok := machine[key]; !ok {
			t.Fatalf("missing machine.%s in %s", key, rec.Body)
		}
	}
	if machine["machine"] != "node-a" || machine["cpuEstimate"] != "" {
		t.Fatalf("machine = %+v", machine)
	}

	vms := body["vms"].([]any)
	if len(vms) != 1 {
		t.Fatalf("vms = %+v", vms)
	}
	vm := vms[0].(map[string]any)
	for _, key := range []string{"name", "liveWatts", "monthKWh", "monthCost"} {
		if _, ok := vm[key]; !ok {
			t.Fatalf("missing vm.%s in %s", key, rec.Body)
		}
	}
	if vm["name"] != "vm1" || math.Abs(vm["monthKWh"].(float64)-0.3) > 1e-9 {
		t.Fatalf("vm = %+v", vm)
	}
}

func TestEnergyRoutesRequireAuth(t *testing.T) {
	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		setupEnergyAPI(r)
	})
	for _, request := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/energy/summary"},
		{http.MethodPut, "/energy/price"},
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(request.method, request.path, strings.NewReader(`{}`)))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status %d, want 401", request.method, request.path, rec.Code)
		}
	}
}
