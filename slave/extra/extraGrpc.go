package extra

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slave/env512"
	"strings"
	"time"

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

func (s *ExtraService) ShutDown(ctx context.Context, req *extraGrpc.RestartShutdownNow) (*extraGrpc.Empty, error) {
	err := shutdownPc(req.Now)
	if err != nil {
		return &extraGrpc.Empty{}, err
	}
	return &extraGrpc.Empty{}, nil
}

func (s *ExtraService) Restart(ctx context.Context, req *extraGrpc.RestartShutdownNow) (*extraGrpc.Empty, error) {
	err := restartPc(req.Now)
	if err != nil {
		return &extraGrpc.Empty{}, err
	}
	return &extraGrpc.Empty{}, nil
}

func SendWebsocketMessage(message, extra string, msgType extraGrpc.WebSocketsMessageType) error {
	if env512.Conn == nil {
		err := fmt.Errorf("gRPC connection not set")
		logger.Error("SendWebsocketMessage: %v", err)
		return err
	}
	h := extraGrpc.NewExtraServiceClient(env512.Conn)
	_, err := h.SendWebsocketMessage(context.Background(), &extraGrpc.WebsocketMessage{Message: message, Type: msgType, Extra: extra})
	if err != nil {
		logger.Error("SendWebsocketMessage: %v", err)
	}
	return err
}

// Severity mirrors the master's notification severities.
type Severity = extraGrpc.NotificationSeverity

const (
	SeverityInfo     = extraGrpc.NotificationSeverity_SEVERITY_INFO
	SeverityWarning  = extraGrpc.NotificationSeverity_SEVERITY_WARNING
	SeverityError    = extraGrpc.NotificationSeverity_SEVERITY_ERROR
	SeverityCritical = extraGrpc.NotificationSeverity_SEVERITY_CRITICAL
)

// Notification is an event sent to the master, which applies policy and delivery.
type Notification struct {
	Event    string // stable key, e.g. "health.smart"
	Severity Severity
	Title    string
	Body     string
	URL      string
	DedupKey string // repeats with the same key are suppressed by the master
}

const notifyTimeout = 15 * time.Second

// Notify sends n to the master. Failures are logged and returned; callers usually continue.
func Notify(n Notification) error {
	if env512.Conn == nil {
		err := fmt.Errorf("gRPC connection not set")
		logger.Error("Notify: %v", err)
		return err
	}
	if n.URL == "" {
		n.URL = "/"
	}
	msg := &extraGrpc.Notification{
		Title:    n.Title,
		Body:     n.Body,
		RelURL:   n.URL,
		Critical: n.Severity >= SeverityError, // older masters only read critical
		Severity: n.Severity,
		Event:    n.Event,
		DedupKey: n.DedupKey,
		Machine:  env512.MachineName,
	}
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	_, err := extraGrpc.NewExtraServiceClient(env512.Conn).SendNotifications(ctx, msg)
	if err != nil {
		logger.Error("Notify %s: %v", n.Event, err)
	}
	return err
}

// RunCmdLogged streams stdout/stderr to the logger and returns an error when the
// command fails or the context is canceled. Use exec.CommandContext to honor ctx.
func RunCmdLogged(ctx context.Context, cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	scan := func(r *bufio.Scanner, prefix string, collectErr *error) {
		for r.Scan() {
			line := strings.TrimRight(r.Text(), "\r")
			if line == "" {
				continue
			}
			if prefix != "" {
				logger.Info("%s%s", prefix, line)
			} else {
				logger.Info(line)
			}
		}
		if scanErr := r.Err(); scanErr != nil {
			logger.Error("cmd stream error: %v", scanErr)
			*collectErr = errors.Join(*collectErr, scanErr)
		}
	}

	errAgg := error(nil)
	stdoutScanner := bufio.NewScanner(stdout)
	stderrScanner := bufio.NewScanner(stderr)

	done := make(chan struct{})
	go func() {
		scan(stdoutScanner, "", &errAgg)
		done <- struct{}{}
	}()
	go func() {
		scan(stderrScanner, "stderr: ", &errAgg)
		done <- struct{}{}
	}()

	for i := 0; i < 2; i++ {
		<-done
	}

	waitErr := cmd.Wait()
	if ctxErr := ctx.Err(); ctxErr != nil {
		errAgg = errors.Join(errAgg, ctxErr)
	}
	if waitErr != nil {
		errAgg = errors.Join(errAgg, waitErr)
	}

	return errAgg
}
