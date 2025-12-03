package docker

import (
	"context"

	dockerGrpc "github.com/Maruqes/512SvMan/api/proto/docker"
	"google.golang.org/grpc"
)

func VolumeList(conn *grpc.ClientConn) (*dockerGrpc.ListVolumesResponse, error) {
	client := dockerGrpc.NewDockerServiceClient(conn)
	return client.VolumeList(context.Background(), &dockerGrpc.Empty{})
}

func VolumeCreateBindMount(conn *grpc.ClientConn, req *dockerGrpc.VolumeCreateRequest) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.VolumeCreateBindMount(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func VolumeRemove(conn *grpc.ClientConn, req *dockerGrpc.VolumeRemoveRequest) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.VolumeRemove(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}
