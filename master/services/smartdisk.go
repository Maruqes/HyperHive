package services

import (
	"512SvMan/protocol"
	"512SvMan/smartdisk"
	"context"
	"fmt"

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

func (s *SmartDiskService) StartForceReallocation(ctx context.Context, machineName, device string) (*smartdiskGrpc.ForceReallocProgress, error) {
	if device == "" {
		return nil, fmt.Errorf("device parameter is required")
	}

	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	// Start the reallocation job
	_, err := smartdisk.StartForceReallocation(ctx, conn.Connection, &smartdiskGrpc.ForceReallocRequest{
		Device: device,
	})
	if err != nil {
		return nil, err
	}

	// Return the current progress status
	return smartdisk.GetForceReallocationProgress(conn.Connection, &smartdiskGrpc.ForceReallocRequest{
		Device: device,
	})
}

func (s *SmartDiskService) GetForceReallocationProgress(machineName, device string) (*smartdiskGrpc.ForceReallocProgress, error) {
	if device == "" {
		return nil, fmt.Errorf("device parameter is required")
	}

	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	progress, err := smartdisk.GetForceReallocationProgress(conn.Connection, &smartdiskGrpc.ForceReallocRequest{
		Device: device,
	})
	if err != nil {
		return nil, err
	}
	return progress, nil
}

func (s *SmartDiskService) GetAllForceReallocationProgress(machineName string) ([]*smartdiskGrpc.ForceReallocProgress, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	return smartdisk.GetAllForceReallocationProgress(conn.Connection)
}

func (s *SmartDiskService) CancelForceReallocation(ctx context.Context, machineName, device string) (string, error) {
	if device == "" {
		return "", fmt.Errorf("device parameter is required")
	}

	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return "", fmt.Errorf("no connection found for machine: %s", machineName)
	}

	resp, err := smartdisk.CancelForceReallocation(ctx, conn.Connection, &smartdiskGrpc.ForceReallocRequest{
		Device: device,
	})
	if err != nil {
		return "", err
	}
	return resp.GetMessage(), nil
}
