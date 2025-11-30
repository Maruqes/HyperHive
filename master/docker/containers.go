package docker

import (
	"context"

	dockerGrpc "github.com/Maruqes/512SvMan/api/proto/docker"
	"google.golang.org/grpc"
)

func ContainerList(conn *grpc.ClientConn) (*dockerGrpc.ListOfContainers, error) {
	client := dockerGrpc.NewDockerServiceClient(conn)
	return client.ContainerList(context.Background(), &dockerGrpc.Empty{})
}

func ContainerCreateFunc(conn *grpc.ClientConn, req *dockerGrpc.ContainerCreate) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.ContainerCreateFunc(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ContainerRemove(conn *grpc.ClientConn, req *dockerGrpc.RemoveContainer) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.ContainerRemove(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ContainerStop(conn *grpc.ClientConn, req *dockerGrpc.ContainerId) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.ContainerStop(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ContainerStart(conn *grpc.ClientConn, req *dockerGrpc.ContainerId) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.ContainerStart(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ContainerRestart(conn *grpc.ClientConn, req *dockerGrpc.ContainerId) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.ContainerRestart(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}
func ContainerPause(conn *grpc.ClientConn, req *dockerGrpc.ContainerId) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.ContainerPause(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}
func ContainerUnPause(conn *grpc.ClientConn, req *dockerGrpc.ContainerId) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.ContainerUnPause(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ContainerKill(conn *grpc.ClientConn, req *dockerGrpc.KillContainer) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.ContainerKill(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}
