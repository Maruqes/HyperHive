package info

import (
	"512SvMan/nots"
	"512SvMan/protocol"
	"context"
	"fmt"
	"time"

	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
	"github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
)

func GetCPUInfo(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.CPUCoreInfo, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetCPUInfo(context.Background(), empty)
}

func GetMemSummary(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.MemSummary, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetMemSummary(context.Background(), empty)
}

func GetDiskSummary(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.DiskSummary, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetDiskSummary(context.Background(), empty)
}

func GetNetworkSummary(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.NetworkSummary, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetNetworkSummary(context.Background(), empty)
}

func GetProcesses(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.ProcessList, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetProcesses(context.Background(), empty)
}

func GetProcessByPID(conn *grpc.ClientConn, req *infoGrpc.ProcessPIDRequest) (*infoGrpc.ProcessStruct, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetProcessByPID(context.Background(), req)
}

func KillProcess(conn *grpc.ClientConn, req *infoGrpc.ProcessPIDRequest) (*infoGrpc.Ok, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.KillProcess(context.Background(), req)
}

func TerminateProcess(conn *grpc.ClientConn, req *infoGrpc.ProcessPIDRequest) (*infoGrpc.Ok, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.TerminateProcess(context.Background(), req)
}

func StressCPU(ctx context.Context, conn *grpc.ClientConn, params *infoGrpc.StressCPUParams) (*infoGrpc.Empty, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.StressCPU(ctx, params)
}

func TestRamMEM(ctx context.Context, conn *grpc.ClientConn, params *infoGrpc.TestRamMEMParams) (*infoGrpc.Ok, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.TestRamMEM(ctx, params)
}

func LoopNots() {
	notCpu := func(cpu *infoGrpc.CPUCoreInfo, machine string) {
		if cpu == nil {
			return
		}
		for _, core := range cpu.Cores {
			if core.Temp >= 85 {
				msg := fmt.Sprintf("CPU core high temperature on %s: %d°C", machine, core.Temp)
				nots.SendGlobalNotification("High CPU temperature", msg, "/", true)
			} else if core.Temp >= 78 {
				msg := fmt.Sprintf("CPU core temperature warning on %s: %d°C", machine, core.Temp)
				nots.SendGlobalNotification("CPU temperature", msg, "/", false)
			}

			if core.Usage >= 90 {
				msg := fmt.Sprintf("CPU usage high on %s: %d%%", machine, core.Usage)
				nots.SendGlobalNotification("High CPU usage", msg, "/", true)
			} else if core.Usage >= 80 {
				msg := fmt.Sprintf("CPU usage warning on %s: %d%%", machine, core.Usage)
				nots.SendGlobalNotification("CPU usage", msg, "/", false)
			}
		}
	}

	notMem := func(mem *infoGrpc.MemSummary, machine string) {

		if mem == nil {
			return
		}

		// Prefer explicit UsedPercent from the protobuf
		used := mem.UsedPercent

		if used >= 85 {
			msg := fmt.Sprintf("Memory critical on %s: %.0f%% used (%d MB used of %d MB)", machine, used, mem.UsedMb, mem.TotalMb)
			nots.SendGlobalNotification("Memory critical", msg, "/", true)
		} else if used >= 70 {
			msg := fmt.Sprintf("Memory high on %s: %.0f%% used (%d MB used of %d MB)", machine, used, mem.UsedMb, mem.TotalMb)
			nots.SendGlobalNotification("Memory usage", msg, "/", false)
		}
	}

	notDisk := func(disk *infoGrpc.DiskSummary, machine string) {
		if disk == nil {
			return
		}

		for k, v := range disk.Usage {
			if v >= 85 {
				msg := fmt.Sprintf("Disk %s critical usage on %s: %.0f%%", k, machine, v)
				nots.SendGlobalNotification("Disk critical usage", msg, "/", true)
			} else if v >= 70 {
				msg := fmt.Sprintf("Disk %s high usage on %s: %.0f%%", k, machine, v)
				nots.SendGlobalNotification("Disk usage", msg, "/", false)
			}
		}

		// Check individual disks
		for _, d := range disk.Disks {
			if d == nil {
				continue
			}
			name := d.MountPoint
			if name == "" {
				name = d.Device
			}

			// temperature checks
			if d.TemperatureC >= 55 {
				msg := fmt.Sprintf("Disk %s critical temperature on %s: %d°C", name, machine, d.TemperatureC)
				nots.SendGlobalNotification("Disk critical temperature", msg, "/", true)
			} else if d.TemperatureC >= 45 {
				msg := fmt.Sprintf("Disk %s high temperature on %s: %d°C", name, machine, d.TemperatureC)
				nots.SendGlobalNotification("Disk temperature", msg, "/", false)
			}
		}
	}

	notAll := func(con protocol.ConnectionsStruct) {
		if con.Connection == nil {
			return
		}
		cpu, err := GetCPUInfo(con.Connection, &infoGrpc.Empty{})
		if err != nil {
			logger.Errorf("%v", err)
			return
		}

		disk, err := GetDiskSummary(con.Connection, &infoGrpc.Empty{})
		if err != nil {
			logger.Errorf("%v", err)
			return
		}

		mem, err := GetMemSummary(con.Connection, &infoGrpc.Empty{})
		if err != nil {
			logger.Errorf("%v", err)
			return
		}

		machine := con.MachineName
		notCpu(cpu, machine)
		notMem(mem, machine)
		notDisk(disk, machine)
	}

	checkFunc := func() {
		conns := protocol.GetConnectionsSnapshot()
		if conns == nil {
			return
		}
		for _, con := range conns {
			go notAll(con)
		}
	}

	go func() {
		for {
			checkFunc()
			time.Sleep(10 * time.Minute)
		}
	}()
}
