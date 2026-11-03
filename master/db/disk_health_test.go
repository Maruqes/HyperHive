package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func useDiskHealthTestDB(t *testing.T) {
	t.Helper()
	previous, previousMain := DiskHealthDB, DB
	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	DiskHealthDB = conn
	// The feature must never touch data.db; a closed handle makes any use fail.
	mainConn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	mainConn.Close()
	DB = mainConn
	t.Cleanup(func() {
		conn.Close()
		DiskHealthDB, DB = previous, previousMain
	})
	if err := CreateDiskHealthTables(context.Background()); err != nil {
		t.Fatal(err)
	}
}

var diskT0 = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func intPtr(v int) *int { return &v }

func TestDiskHealthDisabled(t *testing.T) {
	previous := DiskHealthDB
	DiskHealthDB = nil
	t.Cleanup(func() { DiskHealthDB = previous })
	if _, err := ListDisks(context.Background(), "all"); !errors.Is(err, ErrDiskHealthDisabled) {
		t.Fatalf("ListDisks without db: %v", err)
	}
}

func TestDiskHealthUpsertRetireAndSamples(t *testing.T) {
	useDiskHealthTestDB(t)
	ctx := context.Background()

	seen := DiskSeen{Serial: "S1", Model: "Disk", MachineName: "a", Device: "/dev/sda", Transport: "sata", Rotational: true, SeenAt: diskT0}
	if err := UpsertDiskSeen(ctx, seen); err != nil {
		t.Fatal(err)
	}
	seen.SeenAt = diskT0.Add(6 * time.Hour)
	seen.Device = "/dev/sdb"
	if err := UpsertDiskSeen(ctx, seen); err != nil {
		t.Fatal(err)
	}
	d, err := GetDisk(ctx, "S1")
	if err != nil {
		t.Fatal(err)
	}
	if !d.FirstSeen.Equal(diskT0) || !d.LastSeen.Equal(diskT0.Add(6*time.Hour)) || d.Device != "/dev/sdb" || !d.Rotational {
		t.Fatalf("unexpected disk %+v", d)
	}
	if _, err := GetDisk(ctx, "missing"); !errors.Is(err, ErrDiskNotFound) {
		t.Fatalf("GetDisk missing: %v", err)
	}

	wear := diskT0.Add(1000 * time.Hour)
	if err := UpdateDiskScore(ctx, "S1", DiskScoreUpdate{SmartAvailable: true, Score: intPtr(70), Band: "watch", Reason: "a\nb", WearOutAt: &wear}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateDiskAlertState(ctx, "S1", "watch", `{"pendingSectors":1}`); err != nil {
		t.Fatal(err)
	}
	d, _ = GetDisk(ctx, "S1")
	if d.Score == nil || *d.Score != 70 || d.Band != "watch" || d.WearOutAt == nil || !d.WearOutAt.Equal(wear) || d.AlertCounters == "" {
		t.Fatalf("score not stored: %+v", d)
	}

	// Samples come back oldest first, filtered by since.
	for _, offset := range []time.Duration{12 * time.Hour, 0, 6 * time.Hour} {
		if err := InsertDiskSample(ctx, DiskHealthSample{Serial: "S1", CapturedAt: diskT0.Add(offset), Score: intPtr(90), PendingSectors: int64(offset.Hours())}); err != nil {
			t.Fatal(err)
		}
	}
	samples, err := GetDiskSamplesSince(ctx, "S1", diskT0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || samples[0].PendingSectors != 6 || samples[1].PendingSectors != 12 {
		t.Fatalf("unexpected samples %+v", samples)
	}
	latest, err := GetLatestDiskSamples(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if latest["S1"].PendingSectors != 12 {
		t.Fatalf("latest sample %+v", latest["S1"])
	}

	// Retire disks not seen since the cutoff, then un-retire on sight.
	n, err := RetireStaleDisks(ctx, diskT0.Add(7*time.Hour), diskT0.Add(8*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("retire: n=%d err=%v", n, err)
	}
	active, _ := ListDisks(ctx, "active")
	retired, _ := ListDisks(ctx, "retired")
	if len(active) != 0 || len(retired) != 1 || retired[0].RetiredAt == nil {
		t.Fatalf("active=%v retired=%v", active, retired)
	}
	seen.SeenAt = diskT0.Add(9 * time.Hour)
	if err := UpsertDiskSeen(ctx, seen); err != nil {
		t.Fatal(err)
	}
	d, _ = GetDisk(ctx, "S1")
	if d.RetiredAt != nil {
		t.Fatal("seen disk is still retired")
	}
	last, err := LastDiskHealthPoll(ctx)
	if err != nil || last == nil || !last.Equal(diskT0.Add(9*time.Hour)) {
		t.Fatalf("last poll %v %v", last, err)
	}
}

func TestDiskHealthDuplicateSamplesIgnored(t *testing.T) {
	useDiskHealthTestDB(t)
	ctx := context.Background()

	sample := DiskHealthSample{Serial: "S1", CapturedAt: diskT0, Score: intPtr(90), PendingSectors: 2}
	if err := InsertDiskSample(ctx, sample); err != nil {
		t.Fatal(err)
	}
	// A retried poll inserts the same (serial, captured_at) again.
	sample.PendingSectors = 9
	if err := InsertDiskSample(ctx, sample); err != nil {
		t.Fatalf("duplicate insert: %v", err)
	}
	samples, err := GetDiskSamplesSince(ctx, "S1", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || samples[0].PendingSectors != 2 {
		t.Fatalf("duplicate sample stored: %+v", samples)
	}

	// Table creation is idempotent with the unique index in place.
	if err := CreateDiskHealthTables(ctx); err != nil {
		t.Fatalf("second table creation: %v", err)
	}

	// An older database may already hold duplicates: creation removes them
	// before adding the unique index.
	if _, err := DiskHealthDB.ExecContext(ctx, `DROP INDEX idx_disk_health_samples_unique`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := DiskHealthDB.ExecContext(ctx, `INSERT INTO disk_health_samples (serial, captured_at) VALUES (?, ?)`,
			"S2", formatDiskHealthTime(diskT0)); err != nil {
			t.Fatal(err)
		}
	}
	if err := CreateDiskHealthTables(ctx); err != nil {
		t.Fatalf("migrate duplicates: %v", err)
	}
	if samples, _ := GetDiskSamplesSince(ctx, "S2", time.Time{}); len(samples) != 1 {
		t.Fatalf("duplicates not cleaned: %d", len(samples))
	}

	// Compaction still works after a duplicate insert.
	if err := CompactDiskHealth(ctx, diskT0.Add(100*24*time.Hour)); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if raw, _ := GetDiskSamplesSince(ctx, "S1", time.Time{}); len(raw) != 0 {
		t.Fatalf("raw samples left: %d", len(raw))
	}
	daily, err := GetDiskDailySince(ctx, "S1", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(daily) != 1 || daily[0].PendingSectors != 2 {
		t.Fatalf("compacted daily %+v", daily)
	}
}

func TestPurgeRetiredDisk(t *testing.T) {
	useDiskHealthTestDB(t)
	ctx := context.Background()
	if err := PurgeRetiredDisk(ctx, "missing"); !errors.Is(err, ErrDiskNotFound) {
		t.Fatalf("purge missing: %v", err)
	}
	if err := UpsertDiskSeen(ctx, DiskSeen{Serial: "S1", SeenAt: diskT0}); err != nil {
		t.Fatal(err)
	}
	if err := InsertDiskSample(ctx, DiskHealthSample{Serial: "S1", CapturedAt: diskT0}); err != nil {
		t.Fatal(err)
	}
	if err := PurgeRetiredDisk(ctx, "S1"); !errors.Is(err, ErrDiskNotRetired) {
		t.Fatalf("purge active: %v", err)
	}
	if samples, _ := GetDiskSamplesSince(ctx, "S1", time.Time{}); len(samples) != 1 {
		t.Fatal("refused purge deleted samples")
	}
	if _, err := RetireStaleDisks(ctx, diskT0.Add(time.Hour), diskT0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := CompactDiskHealth(ctx, diskT0.Add(100*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := PurgeRetiredDisk(ctx, "S1"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetDisk(ctx, "S1"); !errors.Is(err, ErrDiskNotFound) {
		t.Fatalf("disk survived purge: %v", err)
	}
	if daily, _ := GetDiskDailySince(ctx, "S1", time.Time{}); len(daily) != 0 {
		t.Fatal("daily rows survived purge")
	}
}

func TestCompactDiskHealth(t *testing.T) {
	useDiskHealthTestDB(t)
	ctx := context.Background()
	day := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	samples := []DiskHealthSample{
		{CapturedAt: day.Add(1 * time.Hour), Score: intPtr(90), TemperatureC: 40, PendingSectors: 1, PercentageUsed: 10, PowerOnHours: 100, SmartPassed: true},
		{CapturedAt: day.Add(7 * time.Hour), Score: intPtr(60), TemperatureC: 50, PendingSectors: 3, PercentageUsed: 11, PowerOnHours: 106, SmartPassed: true},
		{CapturedAt: day.Add(13 * time.Hour), Score: intPtr(75), TemperatureC: 45, PendingSectors: 2, PercentageUsed: 12, PowerOnHours: 112, SmartPassed: false},
		{CapturedAt: day.Add(19 * time.Hour), Score: intPtr(80), TemperatureC: 41, PendingSectors: 2, PercentageUsed: 13, PowerOnHours: 118, SmartPassed: true},
		// A sample on the next day.
		{CapturedAt: day.Add(25 * time.Hour), Score: intPtr(95), TemperatureC: 39, PercentageUsed: 14, PowerOnHours: 124, SmartPassed: true},
	}
	for _, s := range samples {
		s.Serial = "S1"
		if err := InsertDiskSample(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	// The cutoff lands between the second and third sample of the first day.
	now := day.Add(10*time.Hour + 90*24*time.Hour)
	if err := CompactDiskHealth(ctx, now); err != nil {
		t.Fatal(err)
	}
	daily, err := GetDiskDailySince(ctx, "S1", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(daily) != 1 || *daily[0].Score != 60 || daily[0].MaxTemperatureC != 50 || daily[0].PendingSectors != 3 || daily[0].PercentageUsed != 11 || daily[0].PowerOnHours != 106 {
		t.Fatalf("partial day %+v", daily)
	}
	raw, _ := GetDiskSamplesSince(ctx, "S1", time.Time{})
	if len(raw) != 3 {
		t.Fatalf("raw samples left: %d", len(raw))
	}

	// Rerun at the same time is a no-op.
	if err := CompactDiskHealth(ctx, now); err != nil {
		t.Fatal(err)
	}
	again, _ := GetDiskDailySince(ctx, "S1", time.Time{})
	if len(again) != 1 || *again[0].Score != *daily[0].Score || again[0].PendingSectors != daily[0].PendingSectors {
		t.Fatalf("second run changed rows: %+v", again)
	}

	// A later run merges the rest of the day: worst score kept, last values updated.
	now = day.Add(30*time.Hour + 90*24*time.Hour)
	if err := CompactDiskHealth(ctx, now); err != nil {
		t.Fatal(err)
	}
	daily, _ = GetDiskDailySince(ctx, "S1", time.Time{})
	if len(daily) != 2 {
		t.Fatalf("daily rows %+v", daily)
	}
	first := daily[0]
	if *first.Score != 60 || first.MaxTemperatureC != 50 || first.PendingSectors != 3 || first.PercentageUsed != 13 || first.PowerOnHours != 118 || !first.SmartPassed {
		t.Fatalf("merged day %+v", first)
	}
	if raw, _ := GetDiskSamplesSince(ctx, "S1", time.Time{}); len(raw) != 0 {
		t.Fatalf("raw samples left after full compaction: %d", len(raw))
	}

	// Daily rows older than two years are deleted.
	if err := CompactDiskHealth(ctx, day.Add(2*365*24*time.Hour+48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if daily, _ := GetDiskDailySince(ctx, "S1", time.Time{}); len(daily) != 0 {
		t.Fatalf("old daily rows kept: %+v", daily)
	}
}
