package docker

import (
	"context"
	"time"

	dockerGrpc "github.com/Maruqes/512SvMan/api/proto/docker"
	"google.golang.org/grpc"
)

const listContainersTimeout = 5 * time.Second

func ListContainers(ctx context.Context, conn *grpc.ClientConn) (*dockerGrpc.ContainerList, error) {
	ctx, cancel := context.WithTimeout(ctx, listContainersTimeout)
	defer cancel()
	return dockerGrpc.NewDockerServiceClient(conn).ListContainers(ctx, &dockerGrpc.Empty{})
}
