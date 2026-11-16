package db

import (
	"context"
	"database/sql"
	"testing"
)

func TestGPUModesLifecycle(t *testing.T) {
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

	if err := CreateGPUModesTable(ctx); err != nil {
		t.Fatal(err)
	}
	vm := GPUMode{MachineName: "a", GPUAddress: "0000:01:00.0", Mode: "vm", VMName: "win", LastError: "vm win not found"}
	for _, mode := range []GPUMode{
		vm,
		{MachineName: "a", GPUAddress: "0000:02:00.0", Mode: "none"},
		{MachineName: "b", GPUAddress: "0000:01:00.0", Mode: "none"},
	} {
		if err := SaveGPUMode(ctx, mode); err != nil {
			t.Fatal(err)
		}
	}
	// Saving again replaces the row, including a cleared error and VM.
	host := GPUMode{MachineName: "a", GPUAddress: "0000:02:00.0", Mode: "host"}
	if err := SaveGPUMode(ctx, host); err != nil {
		t.Fatal(err)
	}

	got, err := GetGPUModes(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["0000:01:00.0"] != vm || got["0000:02:00.0"] != host {
		t.Fatalf("modes of a = %+v", got)
	}
}
