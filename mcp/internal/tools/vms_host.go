package tools

import (
	"context"
	"encoding/json"
	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/vmsafety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func vmHostPrior(ctx context.Context, api VMsAPI, machine, kind string) (map[string]json.RawMessage, error) {
	boot, err := api.GetVMHostBootSnapshot(ctx, client.VMHostInput{MachineName: machine})
	if err != nil {
		return nil, err
	}
	if err = client.ValidateVMHostBootSnapshot(boot, machine); err != nil {
		return nil, err
	}
	snapshots := map[string]json.RawMessage{"host_boot": boot}
	getter := api.GetVMCoreIsolation
	if kind == "GetVMHostHugePages" {
		getter = api.GetVMHostHugePages
	}
	if err = vmExtraSnapshot(ctx, snapshots, "host_setting", kind, func(ctx context.Context) (json.RawMessage, error) {
		return getter(ctx, client.VMHostInput{MachineName: machine})
	}); err != nil {
		return nil, err
	}
	return snapshots, nil
}
func registerVMHostWrites(server *mcp.Server, api VMsAPI, gate *vmsafety.Gate, store vmsafety.Store) {
	for _, variant := range []struct {
		name, method string
		write        func(context.Context, client.VMCoreIsolationInput) (json.RawMessage, error)
	}{{"set_vm_host_core_isolation", "POST", api.SetVMHostCoreIsolationPost}, {"replace_vm_host_core_isolation", "PUT", api.SetVMHostCoreIsolationPut}} {
		addVMWrite(server, api, gate, store, vmTool(variant.name, variant.method, "/virsh/coreisolation/{machine_name}", "Replace host CPU isolation boot arguments for ALL kernels; affects all host workloads and can require a reboot. Saves exact grubby info for every kernel, current cmdline, default kernel and previous isolation state. No automatic rollback or reboot.", vmsafety.VeryHigh), vmsafety.VeryHigh, variant.write, func(ctx context.Context, in client.VMCoreIsolationInput) (string, map[string]json.RawMessage, error) {
			snapshots, err := vmHostPrior(ctx, api, in.MachineName, "GetVMCoreIsolation")
			return in.MachineName, snapshots, err
		})
	}
	for _, variant := range []struct {
		name, method string
		write        func(context.Context, client.VMHostHugePagesInput) (json.RawMessage, error)
	}{{"set_vm_host_hugepages", "POST", api.SetVMHostHugePagesPost}, {"replace_vm_host_hugepages", "PUT", api.SetVMHostHugePagesPut}} {
		addVMWrite(server, api, gate, store, vmTool(variant.name, variant.method, "/virsh/hosthugepages/{machine_name}", "Replace host hugepage boot arguments for ALL kernels; affects host memory availability and all workloads and can require a reboot. Saves exact grubby info for every kernel, current cmdline, default kernel and previous hugepage state. No automatic rollback or reboot.", vmsafety.VeryHigh), vmsafety.VeryHigh, variant.write, func(ctx context.Context, in client.VMHostHugePagesInput) (string, map[string]json.RawMessage, error) {
			snapshots, err := vmHostPrior(ctx, api, in.MachineName, "GetVMHostHugePages")
			return in.MachineName, snapshots, err
		})
	}
}
