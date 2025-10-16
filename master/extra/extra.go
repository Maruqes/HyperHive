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

func (s *ExtraServiceServer) SendWebsocketMessage(ctx context.Context, req *extraGrpc.WebsocketMessage) (*extraGrpc.Empty, error) {
	websocketMsg := websocket.Message{
		Type: req.Type.String(),
		Data: req.Message,
	}
	websocket.BroadcastMessage(websocketMsg)
	return &extraGrpc.Empty{}, nil
}
