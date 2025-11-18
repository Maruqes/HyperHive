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
