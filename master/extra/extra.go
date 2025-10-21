package extra

import (
	"512SvMan/websocket"
	"context"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
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

type ExtraServiceServer struct {
	extraGrpc.UnimplementedExtraServiceServer
}

func SendWebsocketMessage(Type extraGrpc.WebSocketsMessageType, Message string) {
	websocketMsg := websocket.Message{
		Type: Type.String(),
		Data: Message,
	}
	websocket.BroadcastMessage(websocketMsg)
}

func (s *ExtraServiceServer) SendWebsocketMessage(ctx context.Context, req *extraGrpc.WebsocketMessage) (*extraGrpc.Empty, error) {
	SendWebsocketMessage(req.Type, req.Message)
	return &extraGrpc.Empty{}, nil
}
