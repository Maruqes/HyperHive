package extra

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"slave/env512"
	"strings"
	"sync"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
	"github.com/Maruqes/512SvMan/logger"
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

func SendWebsocketMessage(message string, msgType extraGrpc.WebSocketsMessageType) error {
	if env512.Conn == nil {
		return fmt.Errorf("gRPC connection not set")
	}
	h := extraGrpc.NewExtraServiceClient(env512.Conn)
	_, err := h.SendWebsocketMessage(context.Background(), &extraGrpc.WebsocketMessage{Message: message, Type: msgType})
	if err != nil {
		logger.Error("SendWebsocketMessage: %v", err)
	}
	return err
}

func ExecWithOutToSocketCMD(ctx context.Context, msgType extraGrpc.WebSocketsMessageType, cmd *exec.Cmd) []error {
	var (
		errors   []error
		errorsMu sync.Mutex
		wg       sync.WaitGroup
	)

	type streamMessage struct {
		line  string
		isErr bool
	}

	var (
		msgCh            = make(chan streamMessage, 1024)
		senderWG         sync.WaitGroup
		dropWarnOnce     sync.Once
		stderrLines      []string
		stderrLinesMu    sync.Mutex
		maxStoredStderr  = 200
	)

	//helper function
	appendErr := func(err error) {
		if err == nil {
			return
		}
		errorsMu.Lock()
		errors = append(errors, err)
		errorsMu.Unlock()
	}

	//std out
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return []error{err}
	}
	//std err
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return []error{err}
	}

	//start command
	if err := cmd.Start(); err != nil {
		return []error{err}
	}

	splitCRLF := func(data []byte, atEOF bool) (advance int, token []byte, splitErr error) {
		for i := 0; i < len(data); i++ {
			if data[i] == '\n' || data[i] == '\r' {
				advance = i + 1
				if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
					advance++
				}
				return advance, data[:i], nil
			}
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	}

	senderWG.Add(1)
	go func() {
		defer senderWG.Done()
		for msg := range msgCh {
			if msg.line == "" {
				continue
			}
			if err := SendWebsocketMessage(msg.line, msgType); err != nil {
				appendErr(fmt.Errorf("websocket send failed: %w", err))
			}
		}
	}()

	stream := func(r io.Reader, isErr bool) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Split(splitCRLF)
		for scanner.Scan() {
			line := strings.TrimRight(scanner.Text(), "\r")
			if len(line) == 0 {
				continue
			}
			logger.Info(line)
			if isErr {
				stderrLinesMu.Lock()
				stderrLines = append(stderrLines, line)
				if overflow := len(stderrLines) - maxStoredStderr; overflow > 0 {
					stderrLines = stderrLines[overflow:]
				}
				stderrLinesMu.Unlock()
			}
			msg := streamMessage{line: line, isErr: isErr}
			select {
			case msgCh <- msg:
			default:
				dropWarnOnce.Do(func() {
					logger.Warn("dropping websocket messages due to backpressure")
				})
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			wrappedErr := fmt.Errorf("stream read error: %w", scanErr)
			logger.Error("%v", wrappedErr)
			appendErr(wrappedErr)
		}
	}

	wg.Add(2)
	go stream(stdout, false)
	go stream(stderr, true)

	waitDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				if killErr := cmd.Process.Kill(); killErr != nil {
					appendErr(fmt.Errorf("failed to kill process: %w", killErr))
				}
			}
		case <-waitDone:
		}
	}()

	waitErr := cmd.Wait()
	close(waitDone)

	wg.Wait()
	close(msgCh)
	senderWG.Wait()

	if waitErr != nil {
		stderrLinesMu.Lock()
		for _, line := range stderrLines {
			appendErr(fmt.Errorf("stderr: %s", line))
		}
		stderrLinesMu.Unlock()
		appendErr(waitErr)
	}
	if err := ctx.Err(); err != nil {
		appendErr(err)
	}

	return errors
}

func ExecWithOutToSocket(ctx context.Context, msgType extraGrpc.WebSocketsMessageType, command string, args ...string) []error {
	cmd := exec.CommandContext(ctx, command, args...)
	return ExecWithOutToSocketCMD(ctx, msgType, cmd)
}
