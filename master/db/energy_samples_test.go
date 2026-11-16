package db

import (
	"context"
	"testing"
)

func TestEnergySamplesBucketsSumAndRetention(t *testing.T) {
	openEnergyFixture(t)
	ctx := context.Background()

	// 7210 and 7250 share every bucket; 9100 only shares the hourly one.
	for _, ts := range []int64{7210, 7250, 9100} {
		if err := AddEnergySamples(ctx, []EnergySample{
			{Ts: ts, Kind: "host", Machine: "node-a", Wh: 2, RunningS: 10},
			{Ts: ts, Kind: "vm", Machine: "node-a", Name: "vm1", Wh: 1, RunningS: 10},
		}); err != nil {
			t.Fatal(err)
		}
	}

	for step, want := range map[int64][]EnergySample{
		300:  {{Ts: 7200, Wh: 4, RunningS: 20}, {Ts: 9000, Wh: 2, RunningS: 10}},
		1800: {{Ts: 7200, Wh: 4, RunningS: 20}, {Ts: 9000, Wh: 2, RunningS: 10}},
		3600: {{Ts: 7200, Wh: 6, RunningS: 30}},
	} {
		got, err := GetEnergySamples(ctx, step, 0, "host", "node-a", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("step %d: %+v, want %d buckets", step, got, len(want))
		}
		for i := range want {
			if got[i].Ts != want[i].Ts || got[i].Wh != want[i].Wh || got[i].RunningS != want[i].RunningS {
				t.Fatalf("step %d bucket %d = %+v, want %+v", step, i, got[i], want[i])
			}
		}
	}

	all, err := GetEnergySamples(ctx, 300, 9000, "", "", "")
	if err != nil || len(all) != 2 {
		t.Fatalf("unfiltered from 9000 = %+v, %v", all, err)
	}
	vms, err := GetEnergySamples(ctx, 300, 0, "vm", "", "vm1")
	if err != nil || len(vms) != 2 || vms[0].Wh != 2 {
		t.Fatalf("vm rows = %+v, %v", vms, err)
	}

	totals, err := GetEnergySampleTotals(ctx, 300, 0)
	if err != nil || len(totals) != 2 {
		t.Fatalf("totals = %+v, %v", totals, err)
	}
	for _, total := range totals {
		if total.Ts != 7200 || total.RunningS != 30 {
			t.Fatalf("total = %+v", total)
		}
	}

	if err := DeleteEnergySamplesBefore(ctx, 300, 9000); err != nil {
		t.Fatal(err)
	}
	left, _ := GetEnergySamples(ctx, 300, 0, "", "", "")
	other, _ := GetEnergySamples(ctx, 1800, 0, "", "", "")
	if len(left) != 2 || len(other) != 4 {
		t.Fatalf("after delete: step 300 has %d rows, step 1800 has %d", len(left), len(other))
	}
}
