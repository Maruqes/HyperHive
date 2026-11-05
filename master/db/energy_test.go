package db

import (
	"context"
	"database/sql"
	"math"
	"testing"
)

func openEnergyFixture(t *testing.T) {
	t.Helper()
	original := DB
	var err error
	DB, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	DB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		DB.Close()
		DB = original
	})
	if err := CreateEnergyTables(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAddEnergyUsageAddsRepeatWrites(t *testing.T) {
	openEnergyFixture(t)
	ctx := context.Background()
	for _, add := range []struct {
		machine string
		vm      string
		wh      float64
		cost    float64
	}{
		{machine: "node-a", vm: "", wh: 100, cost: 2},
		{machine: "node-a", vm: "", wh: 50, cost: 1},
		{machine: "node-a", vm: "vm1", wh: 30, cost: 0.6},
		{machine: "node-a", vm: "vm1", wh: 20, cost: 0.4},
	} {
		if err := AddEnergyUsage(ctx, "2026-11", add.machine, add.vm, add.wh, add.cost); err != nil {
			t.Fatal(err)
		}
	}
	if err := AddEnergyUsage(ctx, "2026-10", "node-a", "vm1", 999, 99); err != nil {
		t.Fatal(err)
	}

	rows, err := GetEnergyUsageForMonth(ctx, "2026-11")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	byVM := map[string]EnergyUsageRow{}
	for _, row := range rows {
		byVM[row.VM] = row
	}
	if host := byVM[""]; !energyAlmostEqual(host.Wh, 150) || !energyAlmostEqual(host.Cost, 3) {
		t.Fatalf("host row = %+v", host)
	}
	if guest := byVM["vm1"]; !energyAlmostEqual(guest.Wh, 50) || !energyAlmostEqual(guest.Cost, 1) {
		t.Fatalf("guest row = %+v", guest)
	}
}

func TestEnergyPriceChangeKeepsEarlierCost(t *testing.T) {
	openEnergyFixture(t)
	ctx := context.Background()

	if err := SetEnergyPrice(ctx, 0.20); err != nil {
		t.Fatal(err)
	}
	if err := AddEnergyUsage(ctx, "2026-11", "node-a", "vm1", 1000, 0.20); err != nil {
		t.Fatal(err)
	}
	if err := SetEnergyPrice(ctx, 0.50); err != nil {
		t.Fatal(err)
	}
	if err := AddEnergyUsage(ctx, "2026-11", "node-a", "vm2", 1000, 0.50); err != nil {
		t.Fatal(err)
	}

	price, set, err := GetEnergyPrice(ctx)
	if err != nil || !set || !energyAlmostEqual(price, 0.50) {
		t.Fatalf("price = %v set=%v err=%v", price, set, err)
	}
	rows, err := GetEnergyUsageForMonth(ctx, "2026-11")
	if err != nil {
		t.Fatal(err)
	}
	costs := map[string]float64{}
	for _, row := range rows {
		costs[row.VM] = row.Cost
	}
	if !energyAlmostEqual(costs["vm1"], 0.20) || !energyAlmostEqual(costs["vm2"], 0.50) {
		t.Fatalf("costs = %+v", costs)
	}
}

func TestDeleteEnergyUsageBeforeRemovesWholeMonths(t *testing.T) {
	openEnergyFixture(t)
	ctx := context.Background()
	for _, month := range []string{"2026-08", "2026-09", "2026-10", "2026-11"} {
		if err := AddEnergyUsage(ctx, month, "node-a", "", 10, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetEnergyPrice(ctx, 0.30); err != nil {
		t.Fatal(err)
	}
	if err := DeleteEnergyUsageBefore(ctx, "2026-11"); err != nil {
		t.Fatal(err)
	}
	for _, month := range []string{"2026-08", "2026-09", "2026-10"} {
		rows, err := GetEnergyUsageForMonth(ctx, month)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 0 {
			t.Fatalf("%s kept %+v", month, rows)
		}
	}
	rows, err := GetEnergyUsageForMonth(ctx, "2026-11")
	if err != nil || len(rows) != 1 {
		t.Fatalf("2026-11 = %+v, %v", rows, err)
	}
	if price, set, err := GetEnergyPrice(ctx); err != nil || !set || !energyAlmostEqual(price, 0.30) {
		t.Fatalf("price after retention = %v set=%v err=%v", price, set, err)
	}
}

func TestGetEnergyPriceUnset(t *testing.T) {
	openEnergyFixture(t)
	price, set, err := GetEnergyPrice(context.Background())
	if err != nil || set || price != 0 {
		t.Fatalf("price = %v set=%v err=%v", price, set, err)
	}
}

func energyAlmostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
