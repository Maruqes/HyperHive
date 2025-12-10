package services

import (
	"context"

	"512SvMan/db"
)

type LogsService struct{}

func (ls *LogsService) GetLogs(ctx context.Context, limit int, level int) ([]db.LogEntry, error) {
	return db.GetLogs(ctx, limit, level)
}
