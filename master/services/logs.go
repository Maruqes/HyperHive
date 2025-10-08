package services

import "512SvMan/db"

type LogsService struct{}

func (ls *LogsService) GetLogs(limit int, level int) ([]db.LogEntry, error) {
	return db.GetLogs(limit, level)
}
