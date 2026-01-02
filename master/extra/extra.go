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
	if err := nots.SendGlobalNotification(req.Title, req.Body, req.RelURL, req.Critical); err != nil {
		logger.Errorf("nao foi possivel enviar a not %s: %s", req.Title, req.Body)
	}

	return &extraGrpc.Empty{}, nil
}

func (s *ExtraServiceServer) SendWebsocketMessage(ctx context.Context, req *extraGrpc.WebsocketMessage) (*extraGrpc.Empty, error) {
	SendWebsocketMessage(req.Type, req.Message, req.Extra)
	return &extraGrpc.Empty{}, nil
}
