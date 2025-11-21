package services

import (
	"512SvMan/protocol"
	"512SvMan/smartdisk"
	"context"
	"fmt"
	"strings"

	smartdiskGrpc "github.com/Maruqes/512SvMan/api/proto/smartdisk"
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
