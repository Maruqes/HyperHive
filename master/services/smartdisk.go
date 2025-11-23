package services

import (
	"512SvMan/db"
	"512SvMan/protocol"
	"512SvMan/smartdisk"
	"context"
	"fmt"
	"strings"
	"time"

	smartdiskGrpc "github.com/Maruqes/512SvMan/api/proto/smartdisk"
	"github.com/Maruqes/512SvMan/logger"
)

type SmartDiskService struct{}

func (s *SmartDiskService) GetSmartInfo(machineName, device string) (*smartdiskGrpc.SmartDiskInfo, error) {
	if device == "" {
		return nil, fmt.Errorf("device parameter is required")
	}

	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	return smartdisk.GetSmartInfo(conn.Connection, &smartdiskGrpc.SmartInfoRequest{Device: device})
}

func (s *SmartDiskService) RunSelfTest(ctx context.Context, machineName, device string, testType smartdiskGrpc.SelfTestType) (string, error) {
	if device == "" {
		return "", fmt.Errorf("device parameter is required")
	}

	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return "", fmt.Errorf("no connection found for machine: %s", machineName)
	}

	resp, err := smartdisk.RunSelfTest(ctx, conn.Connection, &smartdiskGrpc.SelfTestRequest{
		Device: device,
		Type:   testType,
	})
	if err != nil {
		return "", err
	}
	return resp.GetMessage(), nil
}

func (s *SmartDiskService) GetSelfTestProgress(machineName, device string) (*smartdiskGrpc.SelfTestProgress, error) {
	if device == "" {
		return nil, fmt.Errorf("device parameter is required")
	}

	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	return smartdisk.GetSelfTestProgress(conn.Connection, &smartdiskGrpc.SmartInfoRequest{
		Device: device,
	})
}

func (s *SmartDiskService) CancelSelfTest(ctx context.Context, machineName, device string) (string, error) {
	if device == "" {
		return "", fmt.Errorf("device parameter is required")
	}

	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return "", fmt.Errorf("no connection found for machine: %s", machineName)
	}

	resp, err := smartdisk.CancelSelfTest(ctx, conn.Connection, &smartdiskGrpc.CancelSelfTestRequest{
		Device: device,
	})
	if err != nil {
		return "", err
	}
	return resp.GetMessage(), nil
}

func (s *SmartDiskService) StartFullWipe(ctx context.Context, machineName, device string) (string, error) {
	device = strings.TrimSpace(device)
	if device == "" {
		return "", fmt.Errorf("device parameter is required")
	}
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return "", fmt.Errorf("no connection found for machine: %s", machineName)
	}
	resp, err := smartdisk.StartFullWipe(ctx, conn.Connection, &smartdiskGrpc.ForceReallocRequest{Device: device})
	if err != nil {
		return "", err
	}
	return resp.GetMessage(), nil
}

func (s *SmartDiskService) StartNonDestructiveRealloc(ctx context.Context, machineName, device string) (string, error) {
	device = strings.TrimSpace(device)
	if device == "" {
		return "", fmt.Errorf("device parameter is required")
	}
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return "", fmt.Errorf("no connection found for machine: %s", machineName)
	}
	resp, err := smartdisk.StartNonDestructiveRealloc(ctx, conn.Connection, &smartdiskGrpc.ForceReallocRequest{Device: device})
	if err != nil {
		return "", err
	}
	return resp.GetMessage(), nil
}

func (s *SmartDiskService) GetReallocStatus(machineName, device string) (*smartdiskGrpc.ForceReallocStatus, error) {
	device = strings.TrimSpace(device)
	if device == "" {
		return nil, fmt.Errorf("device parameter is required")
	}
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return smartdisk.GetReallocStatus(conn.Connection, &smartdiskGrpc.ForceReallocStatusRequest{Device: device})
}

func (s *SmartDiskService) ListReallocStatus(machineName string) ([]*smartdiskGrpc.ForceReallocStatus, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}
	resp, err := smartdisk.ListReallocStatus(conn.Connection, &smartdiskGrpc.ListReallocStatusRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetStatuses(), nil
}

func (s *SmartDiskService) CancelRealloc(ctx context.Context, machineName, device string) (string, error) {
	device = strings.TrimSpace(device)
	if device == "" {
		return "", fmt.Errorf("device parameter is required")
	}
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return "", fmt.Errorf("no connection found for machine: %s", machineName)
	}
	resp, err := smartdisk.CancelRealloc(ctx, conn.Connection, &smartdiskGrpc.ForceReallocRequest{Device: device})
	if err != nil {
		return "", err
	}
	return resp.GetMessage(), nil
}

func (s *SmartDiskService) DoAutomaticTest() {
	go func() {

		for {
			s.runDueSchedules()
			time.Sleep(20 * time.Second)
		}
	}()
}

func (s *SmartDiskService) runDueSchedules() {
	now := time.Now()

	schedules, err := db.GetDueSchedules(now)
	if err != nil {
		logger.Error("DoAutomaticTest: failed to get due schedules: %v", err)
		return
	}

	for _, sch := range schedules {
		s.runSchedule(now, sch)
	}
}

func (s *SmartDiskService) runSchedule(now time.Time, sch db.SmartDiskSchedule) {
	if sch.Device == "" || sch.MachineName == "" {
		logger.Error("DoAutomaticTest: schedule %d missing device or machine", sch.ID)
		return
	}

	conn := protocol.GetConnectionByMachineName(sch.MachineName)
	if conn == nil || conn.Connection == nil {
		logger.Error("DoAutomaticTest: no connection for machine %s (schedule id=%d)", sch.MachineName, sch.ID)
		return
	}

	var protoType smartdiskGrpc.SelfTestType
	switch strings.ToLower(strings.TrimSpace(sch.TestType)) {
	case "short":
		protoType = smartdiskGrpc.SelfTestType_SELF_TEST_TYPE_SHORT
	case "long", "extended":
		protoType = smartdiskGrpc.SelfTestType_SELF_TEST_TYPE_EXTENDED
	default:
		protoType = smartdiskGrpc.SelfTestType_SELF_TEST_TYPE_SHORT
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := smartdisk.RunSelfTest(ctx, conn.Connection, &smartdiskGrpc.SelfTestRequest{
		Device: sch.Device,
		Type:   protoType,
	})
	if err != nil {
		logger.Error(
			"DoAutomaticTest: failed to start test for device=%s on machine=%s: %v",
			sch.Device, sch.MachineName, err,
		)
		return
	}

	if err := db.UpdateLastRun(sch.ID, now); err != nil {
		logger.Error("DoAutomaticTest: failed to update last_run for schedule id=%d: %v", sch.ID, err)
		return
	}

	logger.Info(
		"DoAutomaticTest: scheduled test started for device=%s on machine=%s (schedule id=%d)",
		sch.Device, sch.MachineName, sch.ID,
	)
}
