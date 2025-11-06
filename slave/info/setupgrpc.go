package info

import (
	"context"

	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
)

type INFOService struct {
	infoGrpc.UnimplementedInfoServer
}

func processStructToProto(proc *ProcessStruct) *infoGrpc.ProcessStruct {
	if proc == nil {
		return nil
	}

	return &infoGrpc.ProcessStruct{
		Pid:            proc.PID,
		Name:           proc.Name,
		Status:         proc.Status,
		Username:       proc.Username,
		CpuPercent:     proc.CPUPercent,
		MemoryPercent:  proc.MemoryPercent,
		MemoryRss:      proc.MemoryRSS,
		MemoryVms:      proc.MemoryVMS,
		NumThreads:     proc.NumThreads,
		NumFds:         proc.NumFDs,
		CreateTime:     proc.CreateTime,
		ReadBytes:      proc.ReadBytes,
		WriteBytes:     proc.WriteBytes,
		ReadCount:      proc.ReadCount,
		WriteCount:     proc.WriteCount,
		NetBytesSent:   proc.NetBytesSent,
		NetBytesRecv:   proc.NetBytesRecv,
		NetPacketsSent: proc.NetPacketsSent,
		NetPacketsRecv: proc.NetPacketsRecv,
		NetConnections: proc.NetConnections,
		CmdLine:        proc.CmdLine,
		Cwd:            proc.Cwd,
		Nice:           proc.Nice,
		NumCtxSwitches: proc.NumCtxSwitches,
	}
}

func (s *INFOService) GetCPUInfo(ctx context.Context, req *infoGrpc.Empty) (*infoGrpc.CPUCoreInfo, error) {
	info, err := CPUInfo.GetCPUInfo()
	if err != nil {
		return nil, err
	}
	var cores []*infoGrpc.Core

	for _, core := range info.Cores {
		cores = append(cores, &infoGrpc.Core{
			Temp:  core.Temp,
			Usage: core.Usage,
		})
	}

	return &infoGrpc.CPUCoreInfo{Cores: cores}, nil
}

func (s *INFOService) GetMemSummary(ctx context.Context, req *infoGrpc.Empty) (*infoGrpc.MemSummary, error) {
	info, err := MemInfo.GetMemSummary()
	if err != nil {
		return nil, err
	}

	return &infoGrpc.MemSummary{
		UsedPercent: info.UsedPercent,
		UsedMb:      int32(info.UsedMB),
		FreePercent: info.FreePercent,
		FreeMb:      int32(info.FreeMB),
		TotalMb:     int32(info.TotalMB),
	}, nil
}

func (s *INFOService) GetDiskSummary(ctx context.Context, req *infoGrpc.Empty) (*infoGrpc.DiskSummary, error) {
	info, err := DiskInfo.GetDiskSummary()
	if err != nil {
		return nil, err
	}

	var disks []*infoGrpc.DiskStruct
	for _, disk := range info.Disks {
		disks = append(disks, &infoGrpc.DiskStruct{
			Device:      disk.Device,
			MountPoint:  disk.MountPoint,
			Fstype:      disk.Fstype,
			Total:       disk.Total,
			Free:        disk.Free,
			Used:        disk.Used,
			UsedPercent: disk.UsedPercent,
			Opts:        disk.Opts,
		})
	}

	var ioStats []*infoGrpc.DiskIOStruct
	for _, io := range info.IO {
		ioStats = append(ioStats, &infoGrpc.DiskIOStruct{
			Device:           io.Device,
			ReadCount:        io.ReadCount,
			WriteCount:       io.WriteCount,
			ReadBytes:        io.ReadBytes,
			WriteBytes:       io.WriteBytes,
			ReadTime:         io.ReadTime,
			WriteTime:        io.WriteTime,
			IopsInProgress:   io.IopsInProgress,
			IoTime:           io.IoTime,
			WeightedIo:       io.WeightedIO,
			MergedReadCount:  io.MergedReadCount,
			MergedWriteCount: io.MergedWriteCount,
		})
	}

	return &infoGrpc.DiskSummary{
		Disks: disks,
		Usage: info.Usage,
		Io:    ioStats,
	}, nil
}

func (s *INFOService) GetNetworkSummary(ctx context.Context, req *infoGrpc.Empty) (*infoGrpc.NetworkSummary, error) {
	info, err := NetworkInfo.GetNetworkSummary()
	if err != nil {
		return nil, err
	}

	// Map network interfaces
	var interfaces []*infoGrpc.NetworkInterfaceStruct
	for _, iface := range info.Interfaces {
		interfaces = append(interfaces, &infoGrpc.NetworkInterfaceStruct{
			Name:         iface.Name,
			Mtu:          int32(iface.MTU),
			HardwareAddr: iface.HardwareAddr,
			Flags:        iface.Flags,
			Addrs:        iface.Addrs,
		})
	}

	// Map network stats
	var stats []*infoGrpc.NetworkStatsStruct
	for _, stat := range info.Stats {
		stats = append(stats, &infoGrpc.NetworkStatsStruct{
			Name:        stat.Name,
			BytesSent:   stat.BytesSent,
			BytesRecv:   stat.BytesRecv,
			PacketsSent: stat.PacketsSent,
			PacketsRecv: stat.PacketsRecv,
		})
	}

	return &infoGrpc.NetworkSummary{
		Interfaces: interfaces,
		Stats:      stats,
		Usage:      info.Usage,
	}, nil
}

func (s *INFOService) GetProcesses(ctx context.Context, req *infoGrpc.Empty) (*infoGrpc.ProcessList, error) {
	processes, err := ProcessInfo.GetProcesses()
	if err != nil {
		return nil, err
	}

	protoProcesses := make([]*infoGrpc.ProcessStruct, 0, len(processes))
	for i := range processes {
		protoProcesses = append(protoProcesses, processStructToProto(&processes[i]))
	}

	return &infoGrpc.ProcessList{Processes: protoProcesses}, nil
}

func (s *INFOService) GetProcessByPID(ctx context.Context, req *infoGrpc.ProcessPIDRequest) (*infoGrpc.ProcessStruct, error) {
	proc, err := ProcessInfo.GetProcessByPID(req.GetPid())
	if err != nil {
		return nil, err
	}

	return processStructToProto(proc), nil
}

func (s *INFOService) KillProcess(ctx context.Context, req *infoGrpc.ProcessPIDRequest) (*infoGrpc.Ok, error) {
	if err := ProcessInfo.KillProcess(req.GetPid()); err != nil {
		return nil, err
	}

	return &infoGrpc.Ok{Resp: "OK"}, nil
}

func (s *INFOService) TerminateProcess(ctx context.Context, req *infoGrpc.ProcessPIDRequest) (*infoGrpc.Ok, error) {
	if err := ProcessInfo.TerminateProcess(req.GetPid()); err != nil {
		return nil, err
	}

	return &infoGrpc.Ok{Resp: "OK"}, nil
}

func (s *INFOService) StressCPU(ctx context.Context, req *infoGrpc.StressCPUParams) (*infoGrpc.Empty, error) {
	err := CPUInfo.StressTestCPU(ctx, int(req.NumSeconds), int(req.NumVCPU))
	if err != nil {
		return nil, err
	}

	return &infoGrpc.Empty{}, nil
}

func (s *INFOService) TestRamMEM(ctx context.Context, req *infoGrpc.TestRamMEMParams) (*infoGrpc.Ok, error) {
	resp, err := MemInfo.SressTestMem(ctx, int(req.NumGigs), int(req.NumOfPasses))
	if err != nil {
		return nil, err
	}

	return &infoGrpc.Ok{Resp: resp}, nil
}
