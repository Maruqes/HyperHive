package db

import (
	"context"
	"database/sql"
	"testing"
)

func TestNetworkLayoutLifecycle(t *testing.T) {
	ctx := context.Background()
	original := DB
	t.Cleanup(func() { DB = original })
	var err error
	DB, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer DB.Close()
	DB.SetMaxOpenConns(1)

	if err := CreateNetworkLayoutTable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := SaveNetworkLayout(ctx, map[string]NetworkPosition{"vm:a:web": {X: 1, Y: 2}, "section:proxy": {X: -3.5, Y: 4}}); err != nil {
		t.Fatal(err)
	}
	// A later save moves only what it names.
	if err := SaveNetworkLayout(ctx, map[string]NetworkPosition{"vm:a:web": {X: 9, Y: 8}}); err != nil {
		t.Fatal(err)
	}
	got, err := GetNetworkLayout(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["vm:a:web"] != (NetworkPosition{X: 9, Y: 8}) || got["section:proxy"] != (NetworkPosition{X: -3.5, Y: 4}) {
		t.Fatalf("layout = %+v", got)
	}

	if err := ResetNetworkLayout(ctx); err != nil {
		t.Fatal(err)
	}
	got, err = GetNetworkLayout(ctx)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("layout after reset = %+v, %v", got, err)
	}
}
