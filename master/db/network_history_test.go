package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func openNetworkHistoryFixture(t *testing.T) {
	t.Helper()
	conn, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := createNetworkHistoryTable(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	previous := NetworkHistoryDB
	NetworkHistoryDB = conn
	t.Cleanup(func() { NetworkHistoryDB = previous; conn.Close() })
}

func TestNetworkHistoryRecordMergesSamplesInEveryTier(t *testing.T) {
	openNetworkHistoryFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 4, 12, 0, 5, 0, time.UTC)
	for _, sample := range []map[string]NetworkHistorySample{
		{"vm:a:web": {100, 10, 10, 1}, "1.1.1.1:80": {50, 5, 5, 2}},
		{"vm:a:web": {200, 20, 40, 0.5}},
		{},
	} {
		if err := RecordNetworkHistory(ctx, at, sample); err != nil {
			t.Fatal(err)
		}
		at = at.Add(10 * time.Second)
	}
	for _, resolution := range []int64{60, 900, 21600} {
		got, err := GetNetworkHistory(ctx, []string{"vm:a:web"}, resolution, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("resolution %d: %+v", resolution, got)
		}
		want := NetworkHistorySample{300, 30, 40, 1}
		if got[0].NetworkHistorySample != want {
			t.Fatalf("resolution %d: got %+v, want %+v", resolution, got[0].NetworkHistorySample, want)
		}
	}
	// Several keys add up, for a line with more than one endpoint.
	got, err := GetNetworkHistory(ctx, []string{"vm:a:web", "1.1.1.1:80"}, 60, 0)
	if err != nil || len(got) != 1 || got[0].Sent != 350 || got[0].PeakSent != 45 {
		t.Fatalf("summed keys = %+v, %v", got, err)
	}
}

func TestNetworkHistoryIdleBucketExistsAndUnsampledDoesNot(t *testing.T) {
	openNetworkHistoryFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	if err := RecordNetworkHistory(ctx, at, nil); err != nil {
		t.Fatal(err)
	}
	got, err := GetNetworkHistory(ctx, []string{"vm:a:idle"}, 60, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Bucket != at.Unix() || got[0].Sent != 0 {
		t.Fatalf("idle = %+v", got)
	}
}

func TestNetworkHistoryCleanKeepsEachTierForItsRetention(t *testing.T) {
	openNetworkHistoryFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour)
	if err := RecordNetworkHistory(ctx, old, map[string]NetworkHistorySample{"k": {Sent: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := CleanNetworkHistory(ctx, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetNetworkHistory(ctx, []string{"k"}, 60, 0); len(got) != 0 {
		t.Fatalf("1m tier kept %+v", got)
	}
	if got, _ := GetNetworkHistory(ctx, []string{"k"}, 900, 0); len(got) != 1 {
		t.Fatalf("15m tier lost its row: %+v", got)
	}
}
