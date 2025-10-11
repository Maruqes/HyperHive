package extra

import (
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