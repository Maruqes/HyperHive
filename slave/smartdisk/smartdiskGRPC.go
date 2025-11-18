package smartdisk

import (
	"context"
	"fmt"
	"strings"

	smartdiskGrpc "github.com/Maruqes/512SvMan/api/proto/smartdisk"
)

type Service struct {
	smartdiskGrpc.UnimplementedSmartDiskServiceServer
}

func protoSelfTestToLocal(t smartdiskGrpc.SelfTestType) (SelfTestType, error) {
	switch t {
	case smartdiskGrpc.SelfTestType_SELF_TEST_TYPE_UNSPECIFIED:
		return SelfTestShort, nil
	case smartdiskGrpc.SelfTestType_SELF_TEST_TYPE_SHORT:
		return SelfTestShort, nil
	case smartdiskGrpc.SelfTestType_SELF_TEST_TYPE_EXTENDED:
		return SelfTestExtended, nil
	default:
		return "", fmt.Errorf("invalid self-test type: %v", t)
	}
}

func toProtoErrorLogATA(errors []ATAErrorEntry) []*smartdiskGrpc.ATAErrorEntry {
	if len(errors) == 0 {
		return nil
	}
	out := make([]*smartdiskGrpc.ATAErrorEntry, 0, len(errors))
	for _, e := range errors {
		out = append(out, &smartdiskGrpc.ATAErrorEntry{
			ErrorNumber:    e.ErrorNumber,
			LifetimeHours:  e.LifetimeHours,
			Lba:            e.LBA,
			LbaFirstError:  e.LBAFirstError,
			Status:         e.Status,
			ErrorMessage:   e.ErrorMessage,
			Operation:      e.Operation,
			SectorCount:    e.SectorCount,
			PoweredUpHours: e.PoweredUpHours,
		})
	}
	return out
}

func toProtoErrorLogNVMe(errors []NVMeErrorEntry) []*smartdiskGrpc.NVMeErrorEntry {
	if len(errors) == 0 {
		return nil
	}
	out := make([]*smartdiskGrpc.NVMeErrorEntry, 0, len(errors))
	for _, e := range errors {
		out = append(out, &smartdiskGrpc.NVMeErrorEntry{
			ErrorCount:          e.ErrorCount,
			Sqid:                e.SQID,
			Cid:                 e.CID,
			StatusField:         e.StatusField,
			ParamErrorLocation:  e.ParamErrLoc,
			Lba:                 e.LBA,
			Nsid:                e.NSID,
			Vs:                  e.VS,
			Trk:                 e.Trk,
			Message:             e.Message,
			SubmissionTimestamp: e.SubmissionTS,
		})
	}
	return out
}

func toProtoSelfTests(tests []SelfTestResult) []*smartdiskGrpc.SelfTestResult {
	if len(tests) == 0 {
		return nil
	}
	out := make([]*smartdiskGrpc.SelfTestResult, 0, len(tests))
	for _, t := range tests {
		out = append(out, &smartdiskGrpc.SelfTestResult{
			Type:             t.Type,
			Status:           t.Status,
			Passed:           t.Passed,
			RemainingPercent: t.RemainingPercent,
			LifetimeHours:    t.LifetimeHours,
		})
	}
	return out
}

func toProtoSmartDiskInfo(info *SmartDiskInfo, smartctlErr error) *smartdiskGrpc.SmartDiskInfo {
	if info == nil {
		return nil
	}

	pb := &smartdiskGrpc.SmartDiskInfo{
		Device:                  info.Device,
		Model:                   info.Model,
		Serial:                  info.Serial,
		Firmware:                info.Firmware,
		CapacityBytes:           info.CapacityBytes,
		PowerOnHours:            info.PowerOnHours,
		PowerCycleCount:         info.PowerCycleCount,
		TemperatureC:            info.TemperatureC,
		TemperatureMax:          info.TemperatureMax,
		TemperatureMin:          info.TemperatureMin,
		SmartPassed:             info.SmartPassed,
		ReallocatedSectors:      info.ReallocatedSectors,
		ReallocatedEventCount:   info.ReallocatedEventCount,
		PendingSectors:          info.PendingSectors,
		OfflineUncorrectable:    info.OfflineUncorrectable,
		RawReadErrorRate:        info.RawReadErrorRate,
		SeekErrorRate:           info.SeekErrorRate,
		SpinRetryCount:          info.SpinRetryCount,
		SpinUpTimeMs:            info.SpinUpTime,
		StartStopCount:          info.StartStopCount,
		LoadCycleCount:          info.LoadCycleCount,
		CrcErrorCount:           info.CRCErrorCount,
		UncorrectableReadErrors: info.UncorrectableReadErrs,
		CommandTimeouts:         info.CommandTimeouts,
		WriteErrorRate:          info.WriteErrorRate,
		EndToEndErrors:          info.EndToEndErrors,
		ReportedUncorrectable:   info.ReportedUncorrectable,
		HighFlyWrites:           info.HighFlyWrites,
		AirflowTemperatureC:     info.AirflowTemperatureC,
		HardwareEccRecovered:    info.HardwareECCRecovered,
		MediaErrors:             info.MediaErrors,
		PercentageUsed:          info.PercentageUsed,
		AvailableSpare:          info.AvailableSpare,
		AvailableSpareThreshold: info.AvailableSpareThreshold,
		CriticalWarning:         info.CriticalWarning,
		DataUnitsRead:           info.DataUnitsRead,
		DataUnitsWritten:        info.DataUnitsWritten,
		HostReadCommands:        info.HostReadCommands,
		HostWriteCommands:       info.HostWriteCommands,
		UnsafeShutdowns:         info.UnsafeShutdowns,
		ErrorLogCount:           info.ErrorLogCount,
		DeviceErrorCount:        info.DeviceErrorCount,
		LastAtaErrors:           toProtoErrorLogATA(info.LastATAErrors),
		LastNvmeErrors:          toProtoErrorLogNVMe(info.LastNVMeErrors),
		SelfTests:               toProtoSelfTests(info.SelfTests),
		HealthStatus:            info.HealthStatus,
		PhysicalProblemRisk:     info.PhysicalProblemRisk,
		RecommendedAction:       info.RecommendedAction,
	}

	if smartctlErr != nil {
		pb.SmartctlError = smartctlErr.Error()
	}

	return pb
}

func (s *Service) GetSmartInfo(ctx context.Context, req *smartdiskGrpc.SmartInfoRequest) (*smartdiskGrpc.SmartDiskInfo, error) {
	info, err := GetSmartInfo(req.GetDevice())
	if info == nil {
		return nil, err
	}

	return toProtoSmartDiskInfo(info, err), nil
}

func (s *Service) RunSelfTest(ctx context.Context, req *smartdiskGrpc.SelfTestRequest) (*smartdiskGrpc.SelfTestResponse, error) {
	testType, err := protoSelfTestToLocal(req.GetType())
	if err != nil {
		return nil, err
	}
	if req.GetDevice() == "" {
		return nil, fmt.Errorf("device must not be empty")
	}

	if err := RunSelfTest(req.GetDevice(), testType); err != nil {
		return nil, err
	}

	desc := "short"
	if testType == SelfTestExtended {
		desc = "extended"
	}

	return &smartdiskGrpc.SelfTestResponse{
		Message: fmt.Sprintf("%s self-test started for %s", desc, req.GetDevice()),
	}, nil
}

func (s *Service) GetSelfTestProgress(ctx context.Context, req *smartdiskGrpc.SmartInfoRequest) (*smartdiskGrpc.SelfTestProgress, error) {
	info, err := GetSmartInfo(req.GetDevice())
	if err != nil && info == nil {
		return nil, err
	}
	// If smartctl returned warnings but we still parsed info, continue.
	progress := &smartdiskGrpc.SelfTestProgress{
		Device:           req.GetDevice(),
		Status:           "idle",
		ProgressPercent:  0,
		RemainingPercent: 0,
	}

	for _, test := range info.SelfTests {
		if test.RemainingPercent > 0 || containsInProgress(test.Status) {
			pct := int64(100 - test.RemainingPercent)
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			progress = &smartdiskGrpc.SelfTestProgress{
				Device:           req.GetDevice(),
				Status:           test.Status,
				ProgressPercent:  pct,
				RemainingPercent: test.RemainingPercent,
				TestType:         test.Type,
				LifetimeHours:    test.LifetimeHours,
			}
			break
		}
	}

	return progress, nil
}

func containsInProgress(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return strings.Contains(status, "in progress") || strings.Contains(status, "progress")
}
