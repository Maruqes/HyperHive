package services

import (
	"512SvMan/info"
	"512SvMan/protocol"
	"context"
	"fmt"

	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
	"github.com/Maruqes/512SvMan/logger"
)

type InfoService struct{}

func (s *InfoService) GetCPUInfo(machineName string) (*infoGrpc.CPUCoreInfo, error) {
	conStruct := protocol.GetConnectionByMachineName(machineName)
	if conStruct == nil || conStruct.Connection == nil {
		return nil, fmt.Errorf("slave %s not found or not connected", machineName)
	}

	return info.GetCPUInfo(conStruct.Connection, &infoGrpc.Empty{})
}

func (s *InfoService) GetMemSummary(machineName string) (*infoGrpc.MemSummary, error) {
	conStruct := protocol.GetConnectionByMachineName(machineName)
	if conStruct == nil || conStruct.Connection == nil {
		return nil, fmt.Errorf("slave %s not found or not connected", machineName)
	}

	return info.GetMemSummary(conStruct.Connection, &infoGrpc.Empty{})
}

func (s *InfoService) GetDiskSummary(machineName string) (*infoGrpc.DiskSummary, error) {
	conStruct := protocol.GetConnectionByMachineName(machineName)
	if conStruct == nil || conStruct.Connection == nil {
		return nil, fmt.Errorf("slave %s not found or not connected", machineName)
	}

	return info.GetDiskSummary(conStruct.Connection, &infoGrpc.Empty{})
}

func (s *InfoService) GetNetworkSummary(machineName string) (*infoGrpc.NetworkSummary, error) {
	conStruct := protocol.GetConnectionByMachineName(machineName)
	if conStruct == nil || conStruct.Connection == nil {
		return nil, fmt.Errorf("slave %s not found or not connected", machineName)
	}

	return info.GetNetworkSummary(conStruct.Connection, &infoGrpc.Empty{})
}

func (s *InfoService) StressCPU(ctx context.Context, machineName string, params *infoGrpc.StressCPUParams) (*infoGrpc.Empty, error) {
	conStruct := protocol.GetConnectionByMachineName(machineName)
	if conStruct == nil || conStruct.Connection == nil {
		return nil, fmt.Errorf("slave %s not found or not connected", machineName)
	}

	return info.StressCPU(ctx, conStruct.Connection, params)
}

func (s *InfoService) TestRamMEM(ctx context.Context, machineName string, params *infoGrpc.TestRamMEMParams) (string, error) {
	conStruct := protocol.GetConnectionByMachineName(machineName)
	if conStruct == nil || conStruct.Connection == nil {
		return "", fmt.Errorf("slave %s not found or not connected", machineName)
	}

	go func() {
		newCTX := context.WithoutCancel(context.Background())
		_, err := info.TestRamMEM(newCTX, conStruct.Connection, params)
		if err != nil {
			logger.Error("MEM-TEST ERROR: " + err.Error())
			return
		}

		logger.Warn("TEST RAM FINISH SEE INFO LOG")
	}()

	return "TestRam is going it may take 10 mins or some hours (1/2/3) see logs to check results", nil
}
