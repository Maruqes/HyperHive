package docker

import (
	"context"

	dockerGrpc "github.com/Maruqes/512SvMan/api/proto/docker"
	"google.golang.org/grpc"
)

func ImageList(conn *grpc.ClientConn) (*dockerGrpc.ListOfImages, error) {
	client := dockerGrpc.NewDockerServiceClient(conn)
	return client.ImageList(context.Background(), &dockerGrpc.Empty{})
}

func ImageDownload(conn *grpc.ClientConn, req *dockerGrpc.DownloadImage) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.ImageDownload(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ImageRemove(conn *grpc.ClientConn, req *dockerGrpc.Remove) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.ImageRemove(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}
