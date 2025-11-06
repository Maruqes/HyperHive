package services

import (
	"512SvMan/db"
	"512SvMan/info"
	"512SvMan/protocol"
	"context"
	"fmt"
	"time"

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

func historySinceDuration(duration time.Duration) time.Time {
	if duration <= 0 {
		return time.Time{}
	}
	return time.Now().Add(-duration)
}

func (s *InfoService) GetCPUHistory(machineName string, duration time.Duration) ([]db.CPUSnapshot, error) {
	return db.GetCPUSnapshotsSince(machineName, historySinceDuration(duration))
}

func (s *InfoService) GetMemHistory(machineName string, duration time.Duration) ([]db.MemSnapshot, error) {
	return db.GetMemSnapshotsSince(machineName, historySinceDuration(duration))
}

func (s *InfoService) GetDiskHistory(machineName string, duration time.Duration) ([]db.DiskSnapshot, error) {
	return db.GetDiskSnapshotsSince(machineName, historySinceDuration(duration))
}

func (s *InfoService) GetNetworkHistory(machineName string, duration time.Duration) ([]db.NetworkSnapshot, error) {
	return db.GetNetworkSnapshotsSince(machineName, historySinceDuration(duration))
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

func (s *InfoService) GetSlaveData() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		s.collectSlaveSnapshots()

		<-ticker.C
	}
}

func (s *InfoService) collectSlaveSnapshots() {
	machineNames := protocol.GetAllMachineNames()
	for _, machineName := range machineNames {
		s.collectMachineSnapshots(machineName)
	}
}

func (s *InfoService) collectMachineSnapshots(machineName string) {
	if info, err := s.GetCPUInfo(machineName); err != nil {
		logger.Debug(fmt.Sprintf("cpu snapshot skipped for %s: %v", machineName, err))
	} else if err := db.InsertCPUSnapshot(machineName, time.Now(), info); err != nil {
		logger.Error(fmt.Sprintf("failed to persist cpu snapshot for %s: %v", machineName, err))
	}

	if info, err := s.GetMemSummary(machineName); err != nil {
		logger.Debug(fmt.Sprintf("mem snapshot skipped for %s: %v", machineName, err))
	} else if err := db.InsertMemSnapshot(machineName, time.Now(), info); err != nil {
		logger.Error(fmt.Sprintf("failed to persist mem snapshot for %s: %v", machineName, err))
	}

	if info, err := s.GetDiskSummary(machineName); err != nil {
		logger.Debug(fmt.Sprintf("disk snapshot skipped for %s: %v", machineName, err))
	} else if err := db.InsertDiskSnapshot(machineName, time.Now(), info); err != nil {
		logger.Error(fmt.Sprintf("failed to persist disk snapshot for %s: %v", machineName, err))
	}

	if info, err := s.GetNetworkSummary(machineName); err != nil {
		logger.Debug(fmt.Sprintf("network snapshot skipped for %s: %v", machineName, err))
	} else if err := db.InsertNetworkSnapshot(machineName, time.Now(), info); err != nil {
		logger.Error(fmt.Sprintf("failed to persist network snapshot for %s: %v", machineName, err))
	}
}
