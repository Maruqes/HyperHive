package db

import (
	"context"
	"database/sql"
	"errors"
)

// EnergyUsageRow is one month's accumulated energy for a VM or, when VM is
// empty, for the host itself.
type EnergyUsageRow struct {
	Month   string  `json:"month"`
	Machine string  `json:"machine"`
	VM      string  `json:"vm"`
	Wh      float64 `json:"wh"`
	Cost    float64 `json:"cost"`
}

func CreateEnergyTables(ctx context.Context) error {
	const query = `
	CREATE TABLE IF NOT EXISTS energy_usage (
		month TEXT NOT NULL,
		machine TEXT NOT NULL,
		vm TEXT NOT NULL DEFAULT '',
		wh REAL NOT NULL DEFAULT 0,
		cost REAL NOT NULL DEFAULT 0,
		PRIMARY KEY (month, machine, vm)
	);
	CREATE TABLE IF NOT EXISTS energy_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		price_per_kwh REAL NOT NULL DEFAULT 0
	);
	`
	_, err := DB.ExecContext(ctx, query)
	return err
}

// AddEnergyUsage adds one interval to the month/machine/vm bucket. Cost is the
// caller's computed snapshot, so a later price change never rewrites it.
func AddEnergyUsage(ctx context.Context, month, machine, vm string, wh, cost float64) error {
	const query = `
	INSERT INTO energy_usage (month, machine, vm, wh, cost)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(month, machine, vm) DO UPDATE SET
		wh = wh + excluded.wh,
		cost = cost + excluded.cost;
	`
	_, err := DB.ExecContext(ctx, query, month, machine, vm, wh, cost)
	return err
}

func GetEnergyPrice(ctx context.Context) (price float64, set bool, err error) {
	const query = `SELECT price_per_kwh FROM energy_settings WHERE id = 1;`
	err = DB.QueryRowContext(ctx, query).Scan(&price)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return price, true, nil
}

func SetEnergyPrice(ctx context.Context, price float64) error {
	const query = `
	INSERT INTO energy_settings (id, price_per_kwh)
	VALUES (1, ?)
	ON CONFLICT(id) DO UPDATE SET price_per_kwh = excluded.price_per_kwh;
	`
	_, err := DB.ExecContext(ctx, query, price)
	return err
}

func GetEnergyUsageForMonth(ctx context.Context, month string) ([]EnergyUsageRow, error) {
	const query = `
	SELECT month, machine, vm, wh, cost
	FROM energy_usage
	WHERE month = ?
	ORDER BY machine, vm;
	`
	rows, err := DB.QueryContext(ctx, query, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []EnergyUsageRow{}
	for rows.Next() {
		var row EnergyUsageRow
		if err := rows.Scan(&row.Month, &row.Machine, &row.VM, &row.Wh, &row.Cost); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// GetEnergyMonthlyTotals sums the host rows of every month from fromMonth
// (YYYY-MM, inclusive). VM rows are a subset of the host rows and are left out.
func GetEnergyMonthlyTotals(ctx context.Context, fromMonth string) ([]EnergyUsageRow, error) {
	const query = `
	SELECT month, SUM(wh), SUM(cost)
	FROM energy_usage
	WHERE vm = '' AND month >= ?
	GROUP BY month
	ORDER BY month;
	`
	rows, err := DB.QueryContext(ctx, query, fromMonth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []EnergyUsageRow{}
	for rows.Next() {
		var row EnergyUsageRow
		if err := rows.Scan(&row.Month, &row.Wh, &row.Cost); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// DeleteEnergyUsageBefore drops whole months older than month (YYYY-MM string
// comparison). Settings are kept.
func DeleteEnergyUsageBefore(ctx context.Context, month string) error {
	const query = `DELETE FROM energy_usage WHERE month < ?;`
	_, err := DB.ExecContext(ctx, query, month)
	return err
}
