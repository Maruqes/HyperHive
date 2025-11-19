package smartdisk

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
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

	progress := &smartdiskGrpc.SelfTestProgress{
		Device:           req.GetDevice(),
		Status:           "idle",
		ProgressPercent:  0,
		RemainingPercent: 0,
	}

	// Prefer parsed self-test entries.
	for _, test := range info.SelfTests {
		if test.RemainingPercent > 0 || containsInProgress(test.Status) {
			pctDone := clampPercent(100 - test.RemainingPercent)
			progress = &smartdiskGrpc.SelfTestProgress{
				Device:           req.GetDevice(),
				Status:           test.Status,
				ProgressPercent:  pctDone,
				RemainingPercent: clampPercent(test.RemainingPercent),
			}
			return progress, nil
		}
	}

	// Fallback: if smartctl responded with a running test error, parse remaining from the message.
	if pct, ok := parseRemainingFromError(err); ok {
		progress.Status = "in progress"
		progress.RemainingPercent = pct
		progress.ProgressPercent = clampPercent(100 - pct)
	}

	return progress, nil
}

func (s *Service) CancelSelfTest(ctx context.Context, req *smartdiskGrpc.CancelSelfTestRequest) (*smartdiskGrpc.SelfTestResponse, error) {
	device, err := validateDevicePath(req.GetDevice())
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "smartctl", "-X", device)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("smartctl -X failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return &smartdiskGrpc.SelfTestResponse{Message: fmt.Sprintf("self-test cancelled for %s", device)}, nil
}

func (s *Service) StartForceReallocation(ctx context.Context, req *smartdiskGrpc.ForceReallocRequest) (*smartdiskGrpc.ForceReallocResponse, error) {
	progress, err := StartForceReallocation(req.GetDevice())
	if err != nil {
		return nil, err
	}
	return &smartdiskGrpc.ForceReallocResponse{Message: fmt.Sprintf("force reallocation started for %s (badblocks)", progress.Device)}, nil
}

func (s *Service) GetForceReallocationProgress(ctx context.Context, req *smartdiskGrpc.ForceReallocRequest) (*smartdiskGrpc.ForceReallocProgress, error) {
	progress := GetForceReallocationProgress(req.GetDevice())
	if progress == nil {
		return nil, fmt.Errorf("device not found")
	}
	return &smartdiskGrpc.ForceReallocProgress{
		Device:           progress.Device,
		Status:           progress.Status,
		ProgressPercent:  progress.ProgressPercent,
		CurrentBlock:     progress.CurrentBlock,
		TotalBlocks:      progress.TotalBlocks,
		ElapsedTime:      progress.ElapsedTime,
		ReadErrors:       progress.ReadErrors,
		WriteErrors:      progress.WriteErrors,
		CorruptionErrors: progress.CorruptionErrors,
		Message:          progress.Message,
		Error:            progress.Error,
	}, nil
}

func (s *Service) GetAllForceReallocationProgress(ctx context.Context, req *smartdiskGrpc.Empty) (*smartdiskGrpc.ForceReallocProgressList, error) {
	progressList := GetAllForceReallocationProgress()
	var protoList []*smartdiskGrpc.ForceReallocProgress
	for _, progress := range progressList {
		protoList = append(protoList, &smartdiskGrpc.ForceReallocProgress{
			Device:           progress.Device,
			Status:           progress.Status,
			ProgressPercent:  progress.ProgressPercent,
			CurrentBlock:     progress.CurrentBlock,
			TotalBlocks:      progress.TotalBlocks,
			ElapsedTime:      progress.ElapsedTime,
			ReadErrors:       progress.ReadErrors,
			WriteErrors:      progress.WriteErrors,
			CorruptionErrors: progress.CorruptionErrors,
			Message:          progress.Message,
			Error:            progress.Error,
		})
	}
	return &smartdiskGrpc.ForceReallocProgressList{Jobs: protoList}, nil
}

func (s *Service) CancelForceReallocation(ctx context.Context, req *smartdiskGrpc.ForceReallocRequest) (*smartdiskGrpc.ForceReallocResponse, error) {
	if err := CancelForceReallocation(req.GetDevice()); err != nil {
		return nil, err
	}
	return &smartdiskGrpc.ForceReallocResponse{
		Message: fmt.Sprintf("force reallocation cancelled for %s", req.GetDevice()),
	}, nil
}

func containsInProgress(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return strings.Contains(status, "in progress") || strings.Contains(status, "progress")
}

func parseRemainingFromError(err error) (int64, bool) {
	if err == nil {
		return 0, false
	}
	re := regexp.MustCompile(`([0-9]+)%\s*remaining`)
	m := re.FindStringSubmatch(err.Error())
	if len(m) < 2 {
		return 0, false
	}
	value, convErr := strconv.ParseInt(m[1], 10, 64)
	if convErr != nil {
		return 0, false
	}
	return clampPercent(value), true
}

func clampPercent(v int64) int64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
