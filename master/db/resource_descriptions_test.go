package db

import (
	"context"
	"database/sql"
	"testing"
)

func TestResourceDescriptionsLifecycle(t *testing.T) {
	ctx := context.Background()
	originalDB := DB
	t.Cleanup(func() {
		DB = originalDB
	})

	var err error
	DB, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer DB.Close()

	if err := CreateResourceDescriptionsTable(ctx); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if err := SetResourceDescription(ctx, "npm_stream", 7, "database replication"); err != nil {
		t.Fatalf("set description: %v", err)
	}
	if err := SetResourceDescription(ctx, "npm_stream", 7, "primary database replication"); err != nil {
		t.Fatalf("update description: %v", err)
	}

	descriptions, err := GetResourceDescriptions(ctx, "npm_stream", []int{7, 8})
	if err != nil {
		t.Fatalf("get descriptions: %v", err)
	}
	if got := descriptions[7]; got != "primary database replication" {
		t.Fatalf("description = %q, want %q", got, "primary database replication")
	}
	if _, ok := descriptions[8]; ok {
		t.Fatal("unexpected description for unknown resource")
	}

	if err := DeleteResourceDescription(ctx, "npm_stream", 7); err != nil {
		t.Fatalf("delete description: %v", err)
	}
	descriptions, err = GetResourceDescriptions(ctx, "npm_stream", []int{7})
	if err != nil {
		t.Fatalf("get descriptions after delete: %v", err)
	}
	if _, ok := descriptions[7]; ok {
		t.Fatal("description was not deleted")
	}
}
