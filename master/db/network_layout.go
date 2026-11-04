package db

import (
	"context"
	"fmt"
)

// NetworkPosition is where a piece of the network map was moved to. Pieces inside a box
// are relative to that box, so they follow it.
type NetworkPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func CreateNetworkLayoutTable(ctx context.Context) error {
	_, err := DB.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS network_layout (
		node_id TEXT PRIMARY KEY,
		x REAL NOT NULL,
		y REAL NOT NULL
	) WITHOUT ROWID`)
	return err
}

func GetNetworkLayout(ctx context.Context) (map[string]NetworkPosition, error) {
	rows, err := DB.QueryContext(ctx, "SELECT node_id, x, y FROM network_layout")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	positions := map[string]NetworkPosition{}
	for rows.Next() {
		var id string
		var position NetworkPosition
		if err := rows.Scan(&id, &position.X, &position.Y); err != nil {
			return nil, err
		}
		positions[id] = position
	}
	return positions, rows.Err()
}

// SaveNetworkLayout only touches the given pieces, so two open pages cannot undo each other's other moves.
func SaveNetworkLayout(ctx context.Context, positions map[string]NetworkPosition) error {
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for id, position := range positions {
		if _, err := tx.ExecContext(ctx, `
		INSERT INTO network_layout (node_id, x, y) VALUES (?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET x = excluded.x, y = excluded.y`, id, position.X, position.Y); err != nil {
			return fmt.Errorf("save position of %s: %w", id, err)
		}
	}
	return tx.Commit()
}

func ResetNetworkLayout(ctx context.Context) error {
	_, err := DB.ExecContext(ctx, "DELETE FROM network_layout")
	return err
}
