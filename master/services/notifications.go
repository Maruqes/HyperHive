package services

import (
	"context"

	"512SvMan/nots"

	"github.com/Maruqes/512SvMan/logger"
)

// notifyOperation reports the outcome of a long operation. Delivery failures are only
// logged because the operation itself already finished.
func notifyOperation(ctx context.Context, event string, severity nots.Severity, machine, title, body string) {
	if err := nots.Notify(ctx, nots.Notification{Event: event, Severity: severity, Machine: machine, Title: title, Body: body}); err != nil {
		logger.Errorf("notify %s: %v", event, err)
	}
}
