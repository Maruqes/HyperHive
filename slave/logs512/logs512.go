package logs512

import (
	"context"
	"fmt"
	stdlog "log"
	"slave/env512"

	logsGrpc "github.com/Maruqes/512SvMan/api/proto/logsserve"
	"github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
)

var stream logsGrpc.LogsServe_RecordLogClient

func LogMessage(urgency int, msg string, fields ...interface{}) {
	content := msg
	if len(fields) > 0 {
		for _, field := range fields {
			content += " " + fmt.Sprint(field)
		}
	}

	if stream == nil {
		stdlog.Printf("[logs512] stream not ready (urgency %d): %s", urgency, content)
		return
	}

	var entry logsGrpc.Log
	entry.MachineName = env512.MachineName
	entry.LogType = int32(urgency)
	entry.Content = content

	if err := stream.Send(&entry); err != nil {
		stdlog.Printf("[logs512] failed to send log (urgency %d): %v -- %s", urgency, err, content)
	}
}

func StartLogs(conn *grpc.ClientConn) {
	logsClient := logsGrpc.NewLogsServeClient(conn)

	streamC, err := logsClient.RecordLog(context.Background())
	if err != nil {
		logger.Error("Error starting log stream: %v", err)
		return
	}
	stream = streamC
}
