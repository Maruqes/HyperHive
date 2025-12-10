package db

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	snapshotTimeLayout      = time.RFC3339Nano
	snapshotRetentionMonths = 3
)

var (
	protoMarshalOptions = protojson.MarshalOptions{
		EmitUnpopulated: true,
	}
	protoUnmarshalOptions = protojson.UnmarshalOptions{
		AllowPartial:   true,
		DiscardUnknown: true,
	}
)

type CPUSnapshot struct {
	ID          int
	MachineName string
	CapturedAt  time.Time
	Info        *infoGrpc.CPUCoreInfo
}

type MemSnapshot struct {
	ID          int
	MachineName string
	CapturedAt  time.Time
	Info        *infoGrpc.MemSummary
}

type DiskSnapshot struct {
	ID          int
	MachineName string
	CapturedAt  time.Time
	Info        *infoGrpc.DiskSummary
}

type NetworkSnapshot struct {
	ID          int
	MachineName string
	CapturedAt  time.Time
	Info        *infoGrpc.NetworkSummary
}

func marshalSnapshot(msg proto.Message) (string, error) {
	if msg == nil {
		return "null", nil
	}
	encoded, err := protoMarshalOptions.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func assignSnapshot(data string, target proto.Message) (bool, error) {
	if data == "" || data == "null" {
		return false, nil
	}
	if err := protoUnmarshalOptions.Unmarshal([]byte(data), target); err != nil {
		return false, err
	}
	return true, nil
}

func formatSnapshotTime(ts time.Time) string {
	if ts.IsZero() {
		ts = time.Now()
	}
	return ts.UTC().Format(snapshotTimeLayout)
}

func parseSnapshotTime(raw string) (time.Time, error) {
	return time.Parse(snapshotTimeLayout, raw)
}

func formatQueryTime(ts time.Time) string {
	if ts.IsZero() {
		return (time.Time{}).UTC().Format(snapshotTimeLayout)
	}
	return ts.UTC().Format(snapshotTimeLayout)
}

func createSnapshotTable(ctx context.Context, createStmt, indexStmt string) error {
	if _, err := DB.ExecContext(ctx, createStmt); err != nil {
		return err
	}
	if indexStmt == "" {
		return nil
	}
	_, err := DB.ExecContext(ctx, indexStmt)
	return err
}

func insertSnapshot(ctx context.Context, table, query, machineName string, capturedAt time.Time, payload string) error {
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, query, machineName, formatSnapshotTime(capturedAt), payload); err != nil {
		return err
	}

	cutoff := formatSnapshotTime(time.Now().AddDate(0, -snapshotRetentionMonths, 0))
	cleanupQuery := fmt.Sprintf(`DELETE FROM %s WHERE captured_at < ?`, table)
	if _, err := tx.ExecContext(ctx, cleanupQuery, cutoff); err != nil {
		return err
	}

	return tx.Commit()
}

func fetchSnapshots(ctx context.Context, query string, args []any, scanner func(id int, machineName, capturedAt, payload string) error) error {
	rows, err := DB.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id         int
			name       string
			capturedAt string
			payload    sql.NullString
		)
		if err := rows.Scan(&id, &name, &capturedAt, &payload); err != nil {
			return err
		}
		payloadStr := ""
		if payload.Valid {
			payloadStr = payload.String
		}
		if err := scanner(id, name, capturedAt, payloadStr); err != nil {
			return err
		}
	}
	return rows.Err()
}

func cpuSnapshotCollector(target *[]CPUSnapshot) func(int, string, string, string) error {
	return func(id int, machineName, capturedAt, payload string) error {
		ts, err := parseSnapshotTime(capturedAt)
		if err != nil {
			return err
		}
		snapshot := CPUSnapshot{
			ID:          id,
			MachineName: machineName,
			CapturedAt:  ts,
		}
		info := &infoGrpc.CPUCoreInfo{}
		ok, err := assignSnapshot(payload, info)
		if err != nil {
			return err
		}
		if ok {
			snapshot.Info = info
		}
		*target = append(*target, snapshot)
		return nil
	}
}

func memSnapshotCollector(target *[]MemSnapshot) func(int, string, string, string) error {
	return func(id int, machineName, capturedAt, payload string) error {
		ts, err := parseSnapshotTime(capturedAt)
		if err != nil {
			return err
		}
		snapshot := MemSnapshot{
			ID:          id,
			MachineName: machineName,
			CapturedAt:  ts,
		}
		info := &infoGrpc.MemSummary{}
		ok, err := assignSnapshot(payload, info)
		if err != nil {
			return err
		}
		if ok {
			snapshot.Info = info
		}
		*target = append(*target, snapshot)
		return nil
	}
}

func diskSnapshotCollector(target *[]DiskSnapshot) func(int, string, string, string) error {
	return func(id int, machineName, capturedAt, payload string) error {
		ts, err := parseSnapshotTime(capturedAt)
		if err != nil {
			return err
		}
		snapshot := DiskSnapshot{
			ID:          id,
			MachineName: machineName,
			CapturedAt:  ts,
		}
		info := &infoGrpc.DiskSummary{}
		ok, err := assignSnapshot(payload, info)
		if err != nil {
			return err
		}
		if ok {
			snapshot.Info = info
		}
		*target = append(*target, snapshot)
		return nil
	}
}

func networkSnapshotCollector(target *[]NetworkSnapshot) func(int, string, string, string) error {
	return func(id int, machineName, capturedAt, payload string) error {
		ts, err := parseSnapshotTime(capturedAt)
		if err != nil {
			return err
		}
		snapshot := NetworkSnapshot{
			ID:          id,
			MachineName: machineName,
			CapturedAt:  ts,
		}
		info := &infoGrpc.NetworkSummary{}
		ok, err := assignSnapshot(payload, info)
		if err != nil {
			return err
		}
		if ok {
			snapshot.Info = info
		}
		*target = append(*target, snapshot)
		return nil
	}
}

func CreateCPUSnapshotsTable(ctx context.Context) error {
	const createStmt = `
	CREATE TABLE IF NOT EXISTS cpu_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		machine_name TEXT NOT NULL,
		captured_at DATETIME NOT NULL,
		payload TEXT NOT NULL
	);
	`
	const indexStmt = `
	CREATE INDEX IF NOT EXISTS idx_cpu_snapshots_machine_captured
	ON cpu_snapshots(machine_name, captured_at);
	`
	return createSnapshotTable(ctx, createStmt, indexStmt)
}

func CreateMemSnapshotsTable(ctx context.Context) error {
	const createStmt = `
	CREATE TABLE IF NOT EXISTS mem_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		machine_name TEXT NOT NULL,
		captured_at DATETIME NOT NULL,
		payload TEXT NOT NULL
	);
	`
	const indexStmt = `
	CREATE INDEX IF NOT EXISTS idx_mem_snapshots_machine_captured
	ON mem_snapshots(machine_name, captured_at);
	`
	return createSnapshotTable(ctx, createStmt, indexStmt)
}

func CreateDiskSnapshotsTable(ctx context.Context) error {
	const createStmt = `
	CREATE TABLE IF NOT EXISTS disk_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		machine_name TEXT NOT NULL,
		captured_at DATETIME NOT NULL,
		payload TEXT NOT NULL
	);
	`
	const indexStmt = `
	CREATE INDEX IF NOT EXISTS idx_disk_snapshots_machine_captured
	ON disk_snapshots(machine_name, captured_at);
	`
	return createSnapshotTable(ctx, createStmt, indexStmt)
}

func CreateNetworkSnapshotsTable(ctx context.Context) error {
	const createStmt = `
	CREATE TABLE IF NOT EXISTS network_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		machine_name TEXT NOT NULL,
		captured_at DATETIME NOT NULL,
		payload TEXT NOT NULL
	);
	`
	const indexStmt = `
	CREATE INDEX IF NOT EXISTS idx_network_snapshots_machine_captured
	ON network_snapshots(machine_name, captured_at);
	`
	return createSnapshotTable(ctx, createStmt, indexStmt)
}

func InsertCPUSnapshot(ctx context.Context, machineName string, capturedAt time.Time, info *infoGrpc.CPUCoreInfo) error {
	payload, err := marshalSnapshot(info)
	if err != nil {
		return err
	}
	const query = `
	INSERT INTO cpu_snapshots (machine_name, captured_at, payload)
	VALUES (?, ?, ?);
	`
	return insertSnapshot(ctx, "cpu_snapshots", query, machineName, capturedAt, payload)
}

func InsertMemSnapshot(ctx context.Context, machineName string, capturedAt time.Time, info *infoGrpc.MemSummary) error {
	payload, err := marshalSnapshot(info)
	if err != nil {
		return err
	}
	const query = `
	INSERT INTO mem_snapshots (machine_name, captured_at, payload)
	VALUES (?, ?, ?);
	`
	return insertSnapshot(ctx, "mem_snapshots", query, machineName, capturedAt, payload)
}

func InsertDiskSnapshot(ctx context.Context, machineName string, capturedAt time.Time, info *infoGrpc.DiskSummary) error {
	payload, err := marshalSnapshot(info)
	if err != nil {
		return err
	}
	const query = `
	INSERT INTO disk_snapshots (machine_name, captured_at, payload)
	VALUES (?, ?, ?);
	`
	return insertSnapshot(ctx, "disk_snapshots", query, machineName, capturedAt, payload)
}

func InsertNetworkSnapshot(ctx context.Context, machineName string, capturedAt time.Time, info *infoGrpc.NetworkSummary) error {
	payload, err := marshalSnapshot(info)
	if err != nil {
		return err
	}
	const query = `
	INSERT INTO network_snapshots (machine_name, captured_at, payload)
	VALUES (?, ?, ?);
	`
	return insertSnapshot(ctx, "network_snapshots", query, machineName, capturedAt, payload)
}

func GetCPUSnapshots(ctx context.Context, machineName string, limit int) ([]CPUSnapshot, error) {
	if limit <= 0 {
		limit = 100
	}
	const query = `
	SELECT id, machine_name, captured_at, payload
	FROM cpu_snapshots
	WHERE machine_name = ?
	ORDER BY captured_at DESC
	LIMIT ?;
	`
	var snapshots []CPUSnapshot
	err := fetchSnapshots(ctx, query, []any{machineName, limit}, cpuSnapshotCollector(&snapshots))
	return snapshots, err
}

func GetMemSnapshots(ctx context.Context, machineName string, limit int) ([]MemSnapshot, error) {
	if limit <= 0 {
		limit = 100
	}
	const query = `
	SELECT id, machine_name, captured_at, payload
	FROM mem_snapshots
	WHERE machine_name = ?
	ORDER BY captured_at DESC
	LIMIT ?;
	`
	var snapshots []MemSnapshot
	err := fetchSnapshots(ctx, query, []any{machineName, limit}, memSnapshotCollector(&snapshots))
	return snapshots, err
}

func GetDiskSnapshots(ctx context.Context, machineName string, limit int) ([]DiskSnapshot, error) {
	if limit <= 0 {
		limit = 100
	}
	const query = `
	SELECT id, machine_name, captured_at, payload
	FROM disk_snapshots
	WHERE machine_name = ?
	ORDER BY captured_at DESC
	LIMIT ?;
	`
	var snapshots []DiskSnapshot
	err := fetchSnapshots(ctx, query, []any{machineName, limit}, diskSnapshotCollector(&snapshots))
	return snapshots, err
}

func GetNetworkSnapshots(ctx context.Context, machineName string, limit int) ([]NetworkSnapshot, error) {
	if limit <= 0 {
		limit = 100
	}
	const query = `
	SELECT id, machine_name, captured_at, payload
	FROM network_snapshots
	WHERE machine_name = ?
	ORDER BY captured_at DESC
	LIMIT ?;
	`
	var snapshots []NetworkSnapshot
	err := fetchSnapshots(ctx, query, []any{machineName, limit}, networkSnapshotCollector(&snapshots))
	return snapshots, err
}

func sampleEvenly[T any](items []T, n int) []T {
	if n <= 0 || len(items) <= n {
		return items
	}

	out := make([]T, 0, n)
	step := float64(len(items)-1) / float64(n-1)

	for i := 0; i < n; i++ {
		idx := int(math.Round(float64(i) * step))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(items) {
			idx = len(items) - 1
		}
		out = append(out, items[idx])
	}

	return out
}

func GetDiskSnapshotsSince(ctx context.Context, machineName string, since time.Time, numberOfRows int) ([]DiskSnapshot, error) {
	const query = `
		SELECT id, machine_name, captured_at, payload
		FROM disk_snapshots
		WHERE machine_name = ? AND captured_at >= ?
		ORDER BY captured_at ASC;
	`
	var snapshots []DiskSnapshot
	err := fetchSnapshots(
		ctx,
		query,
		[]any{machineName, formatQueryTime(since)},
		diskSnapshotCollector(&snapshots),
	)
	if err != nil {
		return nil, err
	}

	return sampleEvenly(snapshots, numberOfRows), nil
}
func GetCPUSnapshotsSince(ctx context.Context, machineName string, since time.Time, numberOfRows int) ([]CPUSnapshot, error) {
	const query = `
		SELECT id, machine_name, captured_at, payload
		FROM cpu_snapshots
		WHERE machine_name = ? AND captured_at >= ?
		ORDER BY captured_at ASC;
	`
	var snapshots []CPUSnapshot
	err := fetchSnapshots(ctx, query, []any{machineName, formatQueryTime(since)}, cpuSnapshotCollector(&snapshots))
	if err != nil {
		return nil, err
	}
	return sampleEvenly(snapshots, numberOfRows), nil
}

func GetMemSnapshotsSince(ctx context.Context, machineName string, since time.Time, numberOfRows int) ([]MemSnapshot, error) {
	const query = `
		SELECT id, machine_name, captured_at, payload
		FROM mem_snapshots
		WHERE machine_name = ? AND captured_at >= ?
		ORDER BY captured_at ASC;
	`
	var snapshots []MemSnapshot
	err := fetchSnapshots(ctx, query, []any{machineName, formatQueryTime(since)}, memSnapshotCollector(&snapshots))
	if err != nil {
		return nil, err
	}
	return sampleEvenly(snapshots, numberOfRows), nil
}

func GetNetworkSnapshotsSince(ctx context.Context, machineName string, since time.Time, numberOfRows int) ([]NetworkSnapshot, error) {
	const query = `
		SELECT id, machine_name, captured_at, payload
		FROM network_snapshots
		WHERE machine_name = ? AND captured_at >= ?
		ORDER BY captured_at ASC;
	`
	var snapshots []NetworkSnapshot
	err := fetchSnapshots(ctx, query, []any{machineName, formatQueryTime(since)}, networkSnapshotCollector(&snapshots))
	if err != nil {
		return nil, err
	}
	return sampleEvenly(snapshots, numberOfRows), nil
}
