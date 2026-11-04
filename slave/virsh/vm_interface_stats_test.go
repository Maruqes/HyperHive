package virsh

import (
	"testing"

	libvirt "libvirt.org/go/libvirt"
)

func TestSumVMInterfaceStatsAddsEveryNIC(t *testing.T) {
	got := sumVMInterfaceStats("web", []libvirt.DomainStatsNet{{RxBytes: 10, TxBytes: 1}, {RxBytes: 5, TxBytes: 2}})
	if got.GetName() != "web" || got.GetRxBytes() != 15 || got.GetTxBytes() != 3 {
		t.Fatalf("unexpected sum: %+v", got)
	}
}

const twoNicDomainXML = `<domain><devices>
<interface type='network'><mac address='52:54:00:00:00:01'/><source network='512rede'/><target dev='vnet3'/><model type='virtio'/></interface>
<interface type='bridge'><mac address='52:54:00:00:00:02'/><source bridge='br0'/><target dev='vnet4'/><model type='e1000'/></interface>
</devices></domain>`

func TestNicsFromDomainXML(t *testing.T) {
	nics, err := nicsFromDomainXML(twoNicDomainXML)
	if err != nil {
		t.Fatal(err)
	}
	if len(nics) != 2 {
		t.Fatalf("nics = %+v", nics)
	}
	first, second := nics[0], nics[1]
	if first.GetMac() != "52:54:00:00:00:01" || first.GetNetwork() != "512rede" || first.GetTarget() != "vnet3" || first.GetModel() != "virtio" {
		t.Fatalf("first nic = %+v", first)
	}
	if second.GetMac() != "52:54:00:00:00:02" || second.GetNetwork() != "br0" || second.GetTarget() != "vnet4" || second.GetModel() != "e1000" {
		t.Fatalf("second nic = %+v", second)
	}
	if network, err := networkFromDomainXML(twoNicDomainXML); err != nil || network != "512rede" {
		t.Fatalf("network = %q, %v", network, err)
	}
	if _, err := nicsFromDomainXML("<domain"); err == nil {
		t.Fatal("invalid XML must fail")
	}
}

func TestVMNicCountersMapsDeviceToMAC(t *testing.T) {
	nics, err := nicsFromDomainXML(twoNicDomainXML)
	if err != nil {
		t.Fatal(err)
	}
	got := vmNicCounters([]libvirt.DomainStatsNet{{Name: "vnet4", RxBytes: 7, TxBytes: 8}, {Name: "vnet9", RxBytes: 1}}, nics)
	if len(got) != 1 || got[0].GetMac() != "52:54:00:00:00:02" || got[0].GetRxBytes() != 7 || got[0].GetTxBytes() != 8 {
		t.Fatalf("counters = %+v", got)
	}
}
