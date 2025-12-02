package docker

import (
	"512SvMan/extra"
	"context"

	dockerGrpc "github.com/Maruqes/512SvMan/api/proto/docker"
	proto "github.com/Maruqes/512SvMan/api/proto/extra"
	"github.com/Maruqes/512SvMan/logger"
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

func ContainerLogs(ctx context.Context, conn *grpc.ClientConn, req *dockerGrpc.ContainerLogsRequest) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	logs, err := client.ContainerLogs(context.Background(), req)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		logger.Info("finished sending logs")
	}()
	extracontainerid := req.ContainerID
	for {
		msg, err := logs.Recv()
		if err != nil {
			return err
		}
		logger.Info(string(msg.Data))
		extra.SendWebsocketMessage(proto.WebSocketsMessageType_ContainerLogs, string(msg.Data), extracontainerid)
	}
}

func ContainerUpdate(conn *grpc.ClientConn, req *dockerGrpc.ContainerUpdateRequest) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.ContainerUpdate(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ContainerRename(conn *grpc.ClientConn, req *dockerGrpc.ContainerRenameRequest) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.ContainerRename(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ContainerExec(conn *grpc.ClientConn, req *dockerGrpc.ExecMsg) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.ContainerExec(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}
