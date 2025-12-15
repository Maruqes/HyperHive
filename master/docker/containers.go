package docker

import (
	"512SvMan/extra"
	"context"
	"errors"
	"io"

	dockerGrpc "github.com/Maruqes/512SvMan/api/proto/docker"
	proto "github.com/Maruqes/512SvMan/api/proto/extra"
	"github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func ContainerLogs(ctx context.Context, conn *grpc.ClientConn, req *dockerGrpc.ContainerLogsRequest, streamID string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if streamID == "" {
		streamID = req.ContainerID
	}

	client := dockerGrpc.NewDockerServiceClient(conn)
	logs, err := client.ContainerLogs(ctx, req)
	if err != nil {
		return err
	}

	for {
		msg, err := logs.Recv()
		if err != nil {
			// Treat cancellations/timeouts as a clean shutdown instead of an error
			if errors.Is(err, io.EOF) {
				return nil
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			if st, ok := status.FromError(err); ok && (st.Code() == codes.Canceled || st.Code() == codes.DeadlineExceeded) {
				return nil
			}
			return err
		}
		logger.Info(string(msg.Data))
		extra.SendWebsocketMessage(proto.WebSocketsMessageType_ContainerLogs, string(msg.Data), streamID)
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

func StartAlwaysContainers(conn *grpc.ClientConn) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.StartAlwaysContainers(context.Background(), &dockerGrpc.Empty{})
	if err != nil {
		return err
	}
	return nil
}
