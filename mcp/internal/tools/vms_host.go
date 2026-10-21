package tools

import (
	"context"
	"encoding/json"
	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/Maruqes/HyperHive/mcp/internal/vmsafety"
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
func defineSetVMHostCoreIsolation(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("set_vm_host_core_isolation", "POST", "/virsh/coreisolation/{machine_name}", "Replace host CPU isolation boot arguments for ALL kernels; affects all host workloads and can require a reboot. Saves exact grubby info for every kernel, current cmdline, default kernel and previous isolation state. No automatic rollback or reboot."), api.SetVMHostCoreIsolationPost, func(ctx context.Context, in client.VMCoreIsolationInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmHostPrior(ctx, api, in.MachineName, "GetVMCoreIsolation")
		return in.MachineName, snapshots, err
	})
}

func defineReplaceVMHostCoreIsolation(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("replace_vm_host_core_isolation", "PUT", "/virsh/coreisolation/{machine_name}", "Replace host CPU isolation boot arguments for ALL kernels; affects all host workloads and can require a reboot. Saves exact grubby info for every kernel, current cmdline, default kernel and previous isolation state. No automatic rollback or reboot."), api.SetVMHostCoreIsolationPut, func(ctx context.Context, in client.VMCoreIsolationInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmHostPrior(ctx, api, in.MachineName, "GetVMCoreIsolation")
		return in.MachineName, snapshots, err
	})
}

func defineSetVMHostHugepages(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("set_vm_host_hugepages", "POST", "/virsh/hosthugepages/{machine_name}", "Replace host hugepage boot arguments for ALL kernels; affects host memory availability and all workloads and can require a reboot. Saves exact grubby info for every kernel, current cmdline, default kernel and previous hugepage state. No automatic rollback or reboot."), api.SetVMHostHugePagesPost, func(ctx context.Context, in client.VMHostHugePagesInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmHostPrior(ctx, api, in.MachineName, "GetVMHostHugePages")
		return in.MachineName, snapshots, err
	})
}

func defineReplaceVMHostHugepages(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("replace_vm_host_hugepages", "PUT", "/virsh/hosthugepages/{machine_name}", "Replace host hugepage boot arguments for ALL kernels; affects host memory availability and all workloads and can require a reboot. Saves exact grubby info for every kernel, current cmdline, default kernel and previous hugepage state. No automatic rollback or reboot."), api.SetVMHostHugePagesPut, func(ctx context.Context, in client.VMHostHugePagesInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmHostPrior(ctx, api, in.MachineName, "GetVMHostHugePages")
		return in.MachineName, snapshots, err
	})
}
