package virsh

import (
	"context"
	"fmt"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"github.com/Maruqes/512SvMan/logger"
	libvirt "libvirt.org/go/libvirt"
)

// vmNicCounters drops a device missing from the XML: without a MAC it has no stable key.
func vmNicCounters(stats []libvirt.DomainStatsNet, nics []*grpcVirsh.VmNic) []*grpcVirsh.VMNicCounters {
	macs := make(map[string]string, len(nics))
	for _, nic := range nics {
		macs[nic.GetTarget()] = nic.GetMac()
	}
	out := make([]*grpcVirsh.VMNicCounters, 0, len(stats))
	for _, stat := range stats {
		if mac := macs[stat.Name]; mac != "" {
			out = append(out, &grpcVirsh.VMNicCounters{Mac: mac, RxBytes: stat.RxBytes, TxBytes: stat.TxBytes})
		}
	}
	return out
}

func domainNicCounters(dom *libvirt.Domain, stats []libvirt.DomainStatsNet) ([]*grpcVirsh.VMNicCounters, error) {
	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return nil, fmt.Errorf("read domain xml: %w", err)
	}
	nics, err := nicsFromDomainXML(xmlDesc)
	if err != nil {
		return nil, err
	}
	return vmNicCounters(stats, nics), nil
}

func sumVMInterfaceStats(name string, nics []libvirt.DomainStatsNet) *grpcVirsh.VMInterfaceCounters {
	out := &grpcVirsh.VMInterfaceCounters{Name: name}
	for _, nic := range nics {
		out.RxBytes += nic.RxBytes
		out.TxBytes += nic.TxBytes
	}
	return out
}

// GetVMInterfaceStats reads every running VM in one libvirt call, plus one XML read per
// VM because DomainStatsNet only names the device; still cheap enough to poll.
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
		vm := sumVMInterfaceStats(name, stat.Net)
		// One unreadable domain must not cost the other VMs their rates.
		if vm.Nics, err = domainNicCounters(stat.Domain, stat.Net); err != nil {
			logger.Error(fmt.Sprintf("%s: nic counters: %v", name, err))
		}
		out.Vms = append(out.Vms, vm)
	}
	return out, nil
}
