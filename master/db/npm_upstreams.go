package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrUpstreamConflict = errors.New("upstream revision, name, or reference conflict")

// NPMUpstreamRow keeps the NPM domain model out of the storage package.
type NPMUpstreamRow struct {
	ID            int64
	Name, Context string
	Revision      int64
	Definition    json.RawMessage
}

type NPMUpstreamBinding struct {
	Consumer string `json:"consumer_type"`
	ID       int    `json:"npm_id"`
	Path     string `json:"location_path"`
	PoolID   int64  `json:"upstream_id"`
	// Expected stores the last downstream payload for drift checks and compensation.
	Expected json.RawMessage `json:"expected"`
}

type NPMUpstreamOperation struct {
	ID            int64
	State         string
	Before, After json.RawMessage
}

func CreateNPMUpstreamTables(ctx context.Context) error {
	_, err := DB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS npm_upstreams (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 name TEXT NOT NULL UNIQUE COLLATE NOCASE,
 context TEXT NOT NULL CHECK(context IN ('http','stream')),
 revision INTEGER NOT NULL,
 definition BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS npm_upstream_bindings (
 consumer_type TEXT NOT NULL CHECK(consumer_type IN ('proxy','location','stream')),
 npm_id INTEGER NOT NULL,
 location_path TEXT NOT NULL DEFAULT '',
 upstream_id INTEGER NOT NULL REFERENCES npm_upstreams(id) ON DELETE RESTRICT,
 expected BLOB NOT NULL,
 PRIMARY KEY(consumer_type,npm_id,location_path)
);
CREATE TABLE IF NOT EXISTS npm_upstream_operations (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 state TEXT NOT NULL,
 before_state BLOB NOT NULL,
 after_state BLOB NOT NULL,
 created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);`)
	return err
}

func ListNPMUpstreams(ctx context.Context) ([]NPMUpstreamRow, error) {
	rows, err := DB.QueryContext(ctx, "SELECT id,name,context,revision,definition FROM npm_upstreams ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []NPMUpstreamRow{}
	for rows.Next() {
		var p NPMUpstreamRow
		if err := rows.Scan(&p.ID, &p.Name, &p.Context, &p.Revision, &p.Definition); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func ListNPMUpstreamBindings(ctx context.Context) ([]NPMUpstreamBinding, error) {
	rows, err := DB.QueryContext(ctx, "SELECT consumer_type,npm_id,location_path,upstream_id,expected FROM npm_upstream_bindings ORDER BY consumer_type,npm_id,location_path")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []NPMUpstreamBinding{}
	for rows.Next() {
		var b NPMUpstreamBinding
		if err := rows.Scan(&b.Consumer, &b.ID, &b.Path, &b.PoolID, &b.Expected); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// ReserveNPMUpstreamID allocates stable IDs even when an external apply later fails.
func ReserveNPMUpstreamID(ctx context.Context) (int64, error) {
	// A separate SQLite sequence prevents a failed create from publishing a pool row.
	if _, err := DB.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS npm_upstream_ids (id INTEGER PRIMARY KEY AUTOINCREMENT)"); err != nil {
		return 0, err
	}
	r, err := DB.ExecContext(ctx, "INSERT INTO npm_upstream_ids DEFAULT VALUES")
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func BeginNPMUpstreamOperation(ctx context.Context, before, after []byte) (int64, error) {
	r, err := DB.ExecContext(ctx, "INSERT INTO npm_upstream_operations(state,before_state,after_state) VALUES ('prepared',?,?)", before, after)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func SetNPMUpstreamOperationState(ctx context.Context, id int64, state string) error {
	_, err := DB.ExecContext(ctx, "UPDATE npm_upstream_operations SET state=? WHERE id=?", state, id)
	return err
}

func UpdateNPMUpstreamOperationCandidate(ctx context.Context, id int64, after []byte) error {
	_, err := DB.ExecContext(ctx, "UPDATE npm_upstream_operations SET after_state=? WHERE id=? AND state='prepared'", after, id)
	return err
}

func PendingNPMUpstreamOperations(ctx context.Context) ([]NPMUpstreamOperation, error) {
	rows, err := DB.QueryContext(ctx, "SELECT id,state,before_state,after_state FROM npm_upstream_operations WHERE state NOT IN ('applied','rolled_back') ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []NPMUpstreamOperation
	for rows.Next() {
		var o NPMUpstreamOperation
		if err := rows.Scan(&o.ID, &o.State, &o.Before, &o.After); err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

// CommitNPMUpstreamSnapshot publishes definitions, bindings and applied journal together.
func CommitNPMUpstreamSnapshot(ctx context.Context, op int64, pools []NPMUpstreamRow, bindings []NPMUpstreamBinding) error {
	return writeNPMUpstreamSnapshot(ctx, op, pools, bindings, "applied")
}
func RestoreNPMUpstreamSnapshot(ctx context.Context, op int64, pools []NPMUpstreamRow, bindings []NPMUpstreamBinding) error {
	return writeNPMUpstreamSnapshot(ctx, op, pools, bindings, "rolled_back")
}
func writeNPMUpstreamSnapshot(ctx context.Context, op int64, pools []NPMUpstreamRow, bindings []NPMUpstreamBinding, state string) error {
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM npm_upstream_bindings"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM npm_upstreams"); err != nil {
		return err
	}
	for _, p := range pools {
		if _, err = tx.ExecContext(ctx, "INSERT INTO npm_upstreams(id,name,context,revision,definition) VALUES(?,?,?,?,?)", p.ID, p.Name, p.Context, p.Revision, []byte(p.Definition)); err != nil {
			return fmt.Errorf("%w: %v", ErrUpstreamConflict, err)
		}
	}
	for _, b := range bindings {
		if _, err = tx.ExecContext(ctx, "INSERT INTO npm_upstream_bindings(consumer_type,npm_id,location_path,upstream_id,expected) VALUES(?,?,?,?,?)", b.Consumer, b.ID, b.Path, b.PoolID, []byte(b.Expected)); err != nil {
			return err
		}
	}
	r, err := tx.ExecContext(ctx, "UPDATE npm_upstream_operations SET state=? WHERE id=? AND state IN ('prepared','recovery_required','applied')", state, op)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}
