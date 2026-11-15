package services

import (
	"testing"

	pciGrpc "github.com/Maruqes/512SvMan/api/proto/pci"
)

func TestAdoptGPUMode(t *testing.T) {
	for _, tc := range []struct {
		name     string
		gpu      *pciGrpc.HostGPU
		mode, vm string
		warns    bool
	}{
		{"in one vm", &pciGrpc.HostGPU{AttachedToVms: []string{"win"}, ActualMode: "host"}, "vm", "win", false},
		{"in two vms", &pciGrpc.HostGPU{AttachedToVms: []string{"a", "b"}, ActualMode: "vm"}, "none", "", true},
		{"used by the host", &pciGrpc.HostGPU{ActualMode: "host"}, "host", "", false},
		{"on vfio without a vm", &pciGrpc.HostGPU{ActualMode: "vm"}, "none", "", false},
		{"no driver", &pciGrpc.HostGPU{ActualMode: "none"}, "none", "", false},
		{"slave too old to report", &pciGrpc.HostGPU{}, "none", "", false},
	} {
		mode, vm, warning := adoptGPUMode(tc.gpu)
		if mode != tc.mode || vm != tc.vm || (warning != "") != tc.warns {
			t.Errorf("%s: got %s %q warning=%q", tc.name, mode, vm, warning)
		}
	}
}
