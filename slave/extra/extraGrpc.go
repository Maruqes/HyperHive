package extra

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"slave/env512"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
	"github.com/Maruqes/512SvMan/logger"
	"github.com/creack/pty"
)

type ExtraService struct {
	extraGrpc.UnimplementedExtraServiceServer
}

func (s *ExtraService) CheckForUpdates(ctx context.Context, req *extraGrpc.Empty) (*extraGrpc.AllUpdates, error) {
	updates, err := CheckForUpdates()
	if err != nil {
		return nil, err
	}
	res := &extraGrpc.AllUpdates{}
	for _, update := range updates {
		res.Updates = append(res.Updates, &extraGrpc.UpdateInfo{
			Name:    update.Name,
			Version: update.Version,
		})
	}
	return res, nil
}

func (s *ExtraService) PerformUpdate(ctx context.Context, req *extraGrpc.UpdateRequest) (*extraGrpc.Empty, error) {
	err := PerformUpdate(req.Name, req.RestartAfter)
	if err != nil {
		return &extraGrpc.Empty{}, err
	}
	return &extraGrpc.Empty{}, nil
}

func SendWebsocketMessage(message string) error {
	if env512.Conn == nil {
		return fmt.Errorf("gRPC connection not set")
	}
	h := extraGrpc.NewExtraServiceClient(env512.Conn)
	_, err := h.SendWebsocketMessage(context.Background(), &extraGrpc.WebsocketMessage{Message: message})
	if err != nil {
		logger.Error("SendWebsocketMessage: %v", err)
	}
	return err
}

func ExecWithOutToSocket(ctx context.Context, msgType extraGrpc.WebSocketsMessageType, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	defer ptmx.Close()

	reader := bufio.NewReader(ptmx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			line, err := reader.ReadString('\r')
			if len(line) > 0 {
				logger.Info(line)
				_ = SendWebsocketMessage(line)
			}
			if err != nil {
				continue
			}
			for reader.Buffered() > 0 {
				rest, _ := reader.ReadString('\r')
				if len(rest) > 0 {
					logger.Info(rest)
					_ = SendWebsocketMessage(rest)
				}
			}
			logger.Error("read error: %v", err)
			_ = SendWebsocketMessage(fmt.Sprintf("read error: %v", err))
		}
	}()

	select {
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		return ctx.Err()
	case <-done:
	}

	cmd.Wait()
	return nil
}
