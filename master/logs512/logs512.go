package logs512

import (
	"512SvMan/db"
	"512SvMan/env512"
	"512SvMan/extra"
	"fmt"
	"io"
	"time"

	proto "github.com/Maruqes/512SvMan/api/proto/extra"
	logsGrpc "github.com/Maruqes/512SvMan/api/proto/logsserve"
	"github.com/Maruqes/512SvMan/logger"
)

type LogsServer struct {
	logsGrpc.UnimplementedLogsServeServer
}

func (s *LogsServer) RecordLog(stream logsGrpc.LogsServe_RecordLogServer) error {
	for {
		entry, err := stream.Recv()
		if err == io.EOF {
			// client done sending
			return stream.SendAndClose(&logsGrpc.LogAck{Received: true})
		}
		if err != nil {
			return err
		}
		msg := ""
		if env512.Mode == "dev" {
			msg = fmt.Sprintf("[%s] %s", entry.MachineName, entry.Content)
		} else {
			//use a json format
			msg = fmt.Sprintf(`{"machine_name":"%s","content":"%s"}`, entry.MachineName, entry.Content)
		}
		switch entry.LogType {
		case 0:
			logger.Info(msg)
		case 1:
			logger.Error(msg)
		case 2:
			logger.Warn(msg)
		case 3:
			logger.Debug(msg)
		default:
			logger.Info(msg)
		}
	}
}

func LoggerCallBack(urgency int, msg string, fields ...interface{}) {
	finalMsg := fmt.Sprintf(msg, fields...)
	extra.SendWebsocketMessage(proto.WebSocketsMessageType_Logs, finalMsg, fmt.Sprintf("%d", urgency))
	msg = finalMsg
	fields = nil
	db.InsertLog(time.Now().Format(time.RFC3339), urgency, msg)
}
