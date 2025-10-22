package info

import (
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

type NetworkInfoStruct struct{}

var NetworkInfo NetworkInfoStruct

// NetworkInterfaceStruct contains detailed network interface information
type NetworkInterfaceStruct struct {
	Name         string   // Interface name (e.g., eth0, wlan0)
	MTU          int      // Maximum Transmission Unit
	HardwareAddr string   // MAC address
	Flags        []string // Interface flags (up, broadcast, multicast, etc.)
	Addrs        []string // IP addresses assigned to this interface
}

// NetworkStatsStruct contains network I/O statistics
type NetworkStatsStruct struct {
	Name        string // Interface name
	BytesSent   uint64 // Total bytes sent
	BytesRecv   uint64 // Total bytes received
	PacketsSent uint64 // Total packets sent
	PacketsRecv uint64 // Total packets received
}

// NetworkConnectionStruct contains information about network connections
type NetworkConnectionStruct struct {
	Fd     uint32 // File descriptor
	Family uint32 // Address family (AF_INET, AF_INET6)
	Type   uint32 // Socket type (SOCK_STREAM, SOCK_DGRAM)
	Laddr  string // Local address
	Raddr  string // Remote address
	Status string // Connection status (ESTABLISHED, LISTEN, etc.)
	Pid    int32  // Process ID
}

// GetInterfaces returns detailed information about all network interfaces
func (n *NetworkInfoStruct) GetInterfaces() ([]NetworkInterfaceStruct, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var networkInterfaces []NetworkInterfaceStruct

	for _, iface := range interfaces {
		var addrs []string
		for _, addr := range iface.Addrs {
			addrs = append(addrs, addr.Addr)
		}

		netInterface := NetworkInterfaceStruct{
			Name:         iface.Name,
			MTU:          iface.MTU,
			HardwareAddr: iface.HardwareAddr,
			Flags:        iface.Flags,
			Addrs:        addrs,
		}

		networkInterfaces = append(networkInterfaces, netInterface)
	}

	return networkInterfaces, nil
}

// GetInterfaceStats returns I/O statistics for all network interfaces
func (n *NetworkInfoStruct) GetInterfaceStats() ([]NetworkStatsStruct, error) {
	stats, err := net.IOCounters(true) // true = per interface
	if err != nil {
		return nil, err
	}

	var networkStats []NetworkStatsStruct

	for _, stat := range stats {
		netStat := NetworkStatsStruct{
			Name:        stat.Name,
			BytesSent:   stat.BytesSent,
			BytesRecv:   stat.BytesRecv,
			PacketsSent: stat.PacketsSent,
			PacketsRecv: stat.PacketsRecv,
		}

		networkStats = append(networkStats, netStat)
	}

	return networkStats, nil
}

// GetInterfaceUsage returns network usage statistics (can be used for bandwidth monitoring)
func (n *NetworkInfoStruct) GetInterfaceUsage() (map[string]uint64, error) {
	stats, err := net.IOCounters(true)
	if err != nil {
		return nil, err
	}

	usage := make(map[string]uint64)

	for _, stat := range stats {
		// Total bytes (sent + received) as usage metric
		usage[stat.Name] = stat.BytesSent + stat.BytesRecv
	}

	return usage, nil
}

type ProcessInfoStruct struct{}

var ProcessInfo ProcessInfoStruct

// ProcessStruct contains detailed process information
type ProcessStruct struct {
	PID            int32   // Process ID
	Name           string  // Process name
	Status         string  // Process status (running, sleeping, etc.)
	Username       string  // User running the process
	CPUPercent     float64 // CPU usage percentage
	MemoryPercent  float32 // Memory usage percentage
	MemoryRSS      uint64  // Resident Set Size (physical memory)
	MemoryVMS      uint64  // Virtual Memory Size
	NumThreads     int32   // Number of threads
	NumFDs         int32   // Number of file descriptors
	CreateTime     int64   // Process creation time (Unix timestamp)
	ReadBytes      uint64  // Bytes read from disk
	WriteBytes     uint64  // Bytes written to disk
	ReadCount      uint64  // Number of read operations
	WriteCount     uint64  // Number of write operations
	NetBytesSent   uint64  // Network bytes sent
	NetBytesRecv   uint64  // Network bytes received
	NetPacketsSent uint64  // Network packets sent
	NetPacketsRecv uint64  // Network packets received
	NetConnections int32   // Number of network connections
	CmdLine        string  // Command line
	Cwd            string  // Current working directory
	Nice           int32   // Process nice value
	NumCtxSwitches int64   // Number of context switches
}

// GetProcesses returns detailed information about all running processes
func (p *ProcessInfoStruct) GetProcesses() ([]ProcessStruct, error) {
	pids, err := process.Pids()
	if err != nil {
		return nil, err
	}

	var processes []ProcessStruct

	for _, pid := range pids {
		proc, err := process.NewProcess(pid)
		if err != nil {
			continue // Skip processes we can't access
		}

		procInfo := ProcessStruct{PID: pid}

		// Get process name
		if name, err := proc.Name(); err == nil {
			procInfo.Name = name
		}

		// Get status
		if status, err := proc.Status(); err == nil {
			if len(status) > 0 {
				procInfo.Status = status[0]
			}
		}

		// Get username
		if username, err := proc.Username(); err == nil {
			procInfo.Username = username
		}

		// Get CPU percent
		if cpuPercent, err := proc.CPUPercent(); err == nil {
			procInfo.CPUPercent = cpuPercent
		}

		// Get memory percent
		if memPercent, err := proc.MemoryPercent(); err == nil {
			procInfo.MemoryPercent = memPercent
		}

		// Get memory info
		if memInfo, err := proc.MemoryInfo(); err == nil {
			procInfo.MemoryRSS = memInfo.RSS
			procInfo.MemoryVMS = memInfo.VMS
		}

		// Get number of threads
		if numThreads, err := proc.NumThreads(); err == nil {
			procInfo.NumThreads = numThreads
		}

		// Get number of file descriptors
		if numFDs, err := proc.NumFDs(); err == nil {
			procInfo.NumFDs = numFDs
		}

		// Get create time
		if createTime, err := proc.CreateTime(); err == nil {
			procInfo.CreateTime = createTime
		}

		// Get I/O counters (disk read/write)
		if ioCounters, err := proc.IOCounters(); err == nil {
			procInfo.ReadBytes = ioCounters.ReadBytes
			procInfo.WriteBytes = ioCounters.WriteBytes
			procInfo.ReadCount = ioCounters.ReadCount
			procInfo.WriteCount = ioCounters.WriteCount
		}

		// Get network connections
		if connections, err := proc.Connections(); err == nil {
			procInfo.NetConnections = int32(len(connections))
			// Note: Per-process network I/O bytes is not directly available
			// in most systems without custom monitoring or packet capture
		}

		// Get command line
		if cmdline, err := proc.Cmdline(); err == nil {
			procInfo.CmdLine = cmdline
		}

		// Get current working directory
		if cwd, err := proc.Cwd(); err == nil {
			procInfo.Cwd = cwd
		}

		// Get nice value
		if nice, err := proc.Nice(); err == nil {
			procInfo.Nice = nice
		}

		// Get context switches
		if ctxSwitches, err := proc.NumCtxSwitches(); err == nil {
			procInfo.NumCtxSwitches = ctxSwitches.Voluntary + ctxSwitches.Involuntary
		}

		processes = append(processes, procInfo)
	}

	return processes, nil
}

// GetProcessByPID returns detailed information about a specific process
func (p *ProcessInfoStruct) GetProcessByPID(pid int32) (*ProcessStruct, error) {
	proc, err := process.NewProcess(pid)
	if err != nil {
		return nil, err
	}

	procInfo := &ProcessStruct{PID: pid}

	if name, err := proc.Name(); err == nil {
		procInfo.Name = name
	}

	if status, err := proc.Status(); err == nil {
		if len(status) > 0 {
			procInfo.Status = status[0]
		}
	}

	if username, err := proc.Username(); err == nil {
		procInfo.Username = username
	}

	if cpuPercent, err := proc.CPUPercent(); err == nil {
		procInfo.CPUPercent = cpuPercent
	}

	if memPercent, err := proc.MemoryPercent(); err == nil {
		procInfo.MemoryPercent = memPercent
	}

	if memInfo, err := proc.MemoryInfo(); err == nil {
		procInfo.MemoryRSS = memInfo.RSS
		procInfo.MemoryVMS = memInfo.VMS
	}

	if numThreads, err := proc.NumThreads(); err == nil {
		procInfo.NumThreads = numThreads
	}

	if numFDs, err := proc.NumFDs(); err == nil {
		procInfo.NumFDs = numFDs
	}

	if createTime, err := proc.CreateTime(); err == nil {
		procInfo.CreateTime = createTime
	}

	if ioCounters, err := proc.IOCounters(); err == nil {
		procInfo.ReadBytes = ioCounters.ReadBytes
		procInfo.WriteBytes = ioCounters.WriteBytes
		procInfo.ReadCount = ioCounters.ReadCount
		procInfo.WriteCount = ioCounters.WriteCount
	}

	if connections, err := proc.Connections(); err == nil {
		procInfo.NetConnections = int32(len(connections))
	}

	if cmdline, err := proc.Cmdline(); err == nil {
		procInfo.CmdLine = cmdline
	}

	if cwd, err := proc.Cwd(); err == nil {
		procInfo.Cwd = cwd
	}

	if nice, err := proc.Nice(); err == nil {
		procInfo.Nice = nice
	}

	if ctxSwitches, err := proc.NumCtxSwitches(); err == nil {
		procInfo.NumCtxSwitches = ctxSwitches.Voluntary + ctxSwitches.Involuntary
	}

	return procInfo, nil
}

// KillProcess terminates a process by PID
func (p *ProcessInfoStruct) KillProcess(pid int32) error {
	proc, err := process.NewProcess(pid)
	if err != nil {
		return err
	}

	return proc.Kill()
}

// TerminateProcess gracefully terminates a process by PID
func (p *ProcessInfoStruct) TerminateProcess(pid int32) error {
	proc, err := process.NewProcess(pid)
	if err != nil {
		return err
	}

	return proc.Terminate()
}

