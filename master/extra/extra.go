package extra

import (
	"512SvMan/nots"
	"512SvMan/websocket"
	"time"

	"context"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
	"github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
)

func CheckForUpdates(conn *grpc.ClientConn, empty *extraGrpc.Empty) (*extraGrpc.AllUpdates, error) {
	client := extraGrpc.NewExtraServiceClient(conn)
	return client.CheckForUpdates(context.Background(), empty)
}

func PerformUpdate(conn *grpc.ClientConn, update *extraGrpc.UpdateRequest) (*extraGrpc.Empty, error) {
	client := extraGrpc.NewExtraServiceClient(conn)
	return client.PerformUpdate(context.Background(), update)
}

func ShutDown(conn *grpc.ClientConn, update *extraGrpc.RestartShutdownNow) (*extraGrpc.Empty, error) {
	client := extraGrpc.NewExtraServiceClient(conn)
	return client.ShutDown(context.Background(), update)
}

func Restart(conn *grpc.ClientConn, update *extraGrpc.RestartShutdownNow) (*extraGrpc.Empty, error) {
	client := extraGrpc.NewExtraServiceClient(conn)
	return client.Restart(context.Background(), update)
}

type ExtraServiceServer struct {
	extraGrpc.UnimplementedExtraServiceServer
}

var funcCall []func()

func RegisterCallFunction(f func()) {
	funcCall = append(funcCall, f)
}
func StartHeartbeat(interval time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			for _, f := range funcCall {
				f()
			}
		}
	}()
}

func SendWebsocketMessage(Type extraGrpc.WebSocketsMessageType, Message, Extra string) {
	websocketMsg := websocket.Message{
		Type:  Type.String(),
		Data:  Message,
		Extra: Extra,
	}
	websocket.BroadcastMessage(websocketMsg)
}

func (s *ExtraServiceServer) SendNotifications(ctx context.Context, req *extraGrpc.Notification) (*extraGrpc.Empty, error) {
	if err := nots.Notify(ctx, notificationFromSlave(req)); err != nil {
		logger.Errorf("slave notification %q: %v", req.Title, err)
	}

	return &extraGrpc.Empty{}, nil
}

// notificationFromSlave maps the gRPC message; older slaves only send critical.
func notificationFromSlave(req *extraGrpc.Notification) nots.Notification {
	n := nots.Notification{
		Event:    req.Event,
		Title:    req.Title,
		Body:     req.Body,
		URL:      req.RelURL,
		Machine:  req.Machine,
		DedupKey: req.DedupKey,
	}
	switch req.Severity {
	case extraGrpc.NotificationSeverity_SEVERITY_INFO:
		n.Severity = nots.SeverityInfo
	case extraGrpc.NotificationSeverity_SEVERITY_WARNING:
		n.Severity = nots.SeverityWarning
	case extraGrpc.NotificationSeverity_SEVERITY_ERROR:
		n.Severity = nots.SeverityError
	case extraGrpc.NotificationSeverity_SEVERITY_CRITICAL:
		n.Severity = nots.SeverityCritical
	default:
		n.Severity = nots.SeverityInfo
		if req.Critical {
			n.Severity = nots.SeverityError
		}
	}
	if n.Event == "" {
		n.Event = nots.CategoryLegacy
	}
	return n
}

func (s *ExtraServiceServer) SendWebsocketMessage(ctx context.Context, req *extraGrpc.WebsocketMessage) (*extraGrpc.Empty, error) {
	SendWebsocketMessage(req.Type, req.Message, req.Extra)
	return &extraGrpc.Empty{}, nil
}
