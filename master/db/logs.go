package db

import "database/sql"

/*
CREATE TABLE IF NOT EXISTS logs (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  ts            TEXT NOT NULL,           -- RFC3339
  machine       TEXT NOT NULL,
  level         INTEGER NOT NULL,        -- 0=info,1=error,2=warn,3=debug
  content       TEXT NOT NULL
);
*/

func CreateLogsTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS logs (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		ts            TEXT NOT NULL,           -- RFC3339
		level         INTEGER NOT NULL,        -- 0=info,1=error,2=warn,3=debug
		content       TEXT NOT NULL
	);
	`
	_, err := DB.Exec(query)
	return err
}

func InsertLog(ts string, level int, content string) error {
	query := `
	INSERT INTO logs (ts, level, content)
	VALUES (?, ?, ?);
	`
	_, err := DB.Exec(query, ts, level, content)
	return err
}

type LogEntry struct {
	ID      int    `json:"id"`
	TS      string `json:"ts"`
	Level   int    `json:"level"`
	Content string `json:"content"`
}

func GetLogs(limit int, level int) ([]LogEntry, error) {
	//level 0=info,1=error,2=warn,3=debug
	//if 0 gets 0123
	//if 1 gets 123
	//if 2 gets 23
	//if 3 gets 3
	var rows *sql.Rows
	var err error
	if level == 0 {
		rows, err = DB.Query("SELECT id, ts, level, content FROM logs ORDER BY ts DESC LIMIT ?", limit)
	} else {
		rows, err = DB.Query("SELECT id, ts, level, content FROM logs WHERE level >= ? ORDER BY ts DESC LIMIT ?", level, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var log LogEntry
		if err := rows.Scan(&log.ID, &log.TS, &log.Level, &log.Content); err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return logs, nil
}
