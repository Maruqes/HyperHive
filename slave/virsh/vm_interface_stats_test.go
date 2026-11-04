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
