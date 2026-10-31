package db

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestPowerScheduleStatusLifecycle(t *testing.T) {
	ctx := context.Background()
	originalDB := DB
	t.Cleanup(func() { DB = originalDB })
	var err error
	DB, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	DB.SetMaxOpenConns(1)
	defer DB.Close()
	if err := CreatePowerScheduleTables(ctx); err != nil {
		t.Fatal(err)
	}

	applied := time.Unix(1_800_000_000, 0)
	if err := RecordPowerScheduleApplied(ctx, PowerScheduleStatus{Machine: "a", Enabled: true, StopUnix: 10, WakeUnix: 20, JobID: "5", AppliedAt: applied}); err != nil {
		t.Fatal(err)
	}
	if err := RecordPowerScheduleFailure(ctx, "a", "atd down", applied.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := RecordPowerScheduleFailure(ctx, "b", "not connected", applied); err != nil {
		t.Fatal(err)
	}

	statuses, err := GetPowerScheduleStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 {
		t.Fatalf("statuses = %+v", statuses)
	}
	a := statuses[0]
	if a.JobID != "5" || a.StopUnix != 10 || !a.AppliedAt.Equal(applied) || a.LastError != "atd down" || !a.AttemptedAt.Equal(applied.Add(time.Hour)) {
		t.Fatalf("failure overwrote last success: %+v", a)
	}
	if b := statuses[1]; b.Enabled || b.JobID != "" || !b.AppliedAt.IsZero() {
		t.Fatalf("failure-only row: %+v", b)
	}

	if err := RecordPowerScheduleApplied(ctx, PowerScheduleStatus{Machine: "a", AppliedAt: applied.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	statuses, _ = GetPowerScheduleStatuses(ctx)
	if statuses[0].LastError != "" || statuses[0].Enabled {
		t.Fatalf("success did not clear error: %+v", statuses[0])
	}

	if err := SavePowerSchedule(ctx, PowerSchedule{Enabled: true, Start: "07:30", Stop: "00:15", TZ: "Europe/Lisbon", UpdatedAt: applied}); err != nil {
		t.Fatal(err)
	}
	s, err := GetPowerSchedule(ctx)
	if err != nil || !s.Enabled || s.Start != "07:30" || s.Stop != "00:15" || s.TZ != "Europe/Lisbon" {
		t.Fatalf("schedule = %+v, %v", s, err)
	}
}
