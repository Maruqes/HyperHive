package virsh

import (
	"context"
	"fmt"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	libvirt "libvirt.org/go/libvirt"
)

func sumVMInterfaceStats(name string, nics []libvirt.DomainStatsNet) *grpcVirsh.VMInterfaceCounters {
	out := &grpcVirsh.VMInterfaceCounters{Name: name}
	for _, nic := range nics {
		out.RxBytes += nic.RxBytes
		out.TxBytes += nic.TxBytes
	}
	return out
}

// GetVMInterfaceStats reads every running VM in one libvirt call, cheap enough to poll.
func (s *SlaveVirshService) GetVMInterfaceStats(_ context.Context, _ *grpcVirsh.Empty) (*grpcVirsh.VMInterfaceStatsResponse, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return nil, fmt.Errorf("connect to libvirt: %w", err)
	}
	defer conn.Close()
	stats, err := conn.GetAllDomainStats(nil, libvirt.DOMAIN_STATS_INTERFACE, libvirt.CONNECT_GET_ALL_DOMAINS_STATS_ACTIVE)
	if err != nil {
		return nil, fmt.Errorf("read VM interface stats: %w", err)
	}
	out := &grpcVirsh.VMInterfaceStatsResponse{}
	for _, stat := range stats {
		defer stat.Domain.Free()
		name, err := stat.Domain.GetName()
		if err != nil {
			return nil, fmt.Errorf("read VM name: %w", err)
		}
		out.Vms = append(out.Vms, sumVMInterfaceStats(name, stat.Net))
	}
	return out, nil
}
