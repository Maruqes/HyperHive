package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/vmsafety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type VMsAPI interface {
	GetVMHostBootSnapshot(context.Context, client.VMHostInput) (json.RawMessage, error)
	SetVMHostCoreIsolationPost(context.Context, client.VMCoreIsolationInput) (json.RawMessage, error)
	SetVMHostCoreIsolationPut(context.Context, client.VMCoreIsolationInput) (json.RawMessage, error)
	SetVMHostHugePagesPost(context.Context, client.VMHostHugePagesInput) (json.RawMessage, error)
	SetVMHostHugePagesPut(context.Context, client.VMHostHugePagesInput) (json.RawMessage, error)

	AttachVMDisk(context.Context, client.VMAttachDiskInput) (json.RawMessage, error)
	BackupVM(context.Context, client.VMBackupInput) (json.RawMessage, error)
	CloneVM(context.Context, client.VMCloneInput) (json.RawMessage, error)
	DisableVMAutomaticBackup(context.Context, client.VMIDInput) (json.RawMessage, error)
	SnapshotVMNFS(context.Context, int64) (json.RawMessage, error)

	ExportVM(context.Context, client.VMNameInput, string) (json.RawMessage, error)
	DownloadVMBackup(context.Context, client.VMIDInput, string) (json.RawMessage, error)
	GetVMSnapshot(context.Context, client.VMXMLInput) (json.RawMessage, error)
	StartVM(context.Context, client.VMNameInput) (json.RawMessage, error)
	ShutdownVM(context.Context, client.VMNameInput) (json.RawMessage, error)
	PauseVM(context.Context, client.VMNameInput) (json.RawMessage, error)
	ResumeVM(context.Context, client.VMNameInput) (json.RawMessage, error)
	ForceShutdownVM(context.Context, client.VMNameInput) (json.RawMessage, error)
	RestartVM(context.Context, client.VMNameInput) (json.RawMessage, error)
	SetVMCPUPinning(context.Context, client.VMPinningInput) (json.RawMessage, error)
	UpdateVMXML(context.Context, client.VMUpdateXMLInput) (json.RawMessage, error)
	UpdateVMCPUXML(context.Context, client.VMUpdateCPUXMLInput) (json.RawMessage, error)
	SetVMBallooning(context.Context, client.VMEnableInput) (json.RawMessage, error)
	SetVMHugePages(context.Context, client.VMEnableInput) (json.RawMessage, error)
	SetVMKVM(context.Context, client.VMKVMInput) (json.RawMessage, error)
	SetVMHyperV(context.Context, client.VMHyperVInput) (json.RawMessage, error)
	SetVMMachineType(context.Context, client.VMMachineTypeInput) (json.RawMessage, error)
	SetVMNetwork(context.Context, client.VMNetworkInput) (json.RawMessage, error)
	SetVMVNCPassword(context.Context, client.VMVNCPasswordInput) (json.RawMessage, error)
	AddVMVideo(context.Context, client.VMNameInput) (json.RawMessage, error)
	EditVM(context.Context, client.VMEditInput) (json.RawMessage, error)
	SetVMTunedProfile(context.Context, client.VMTunedInput) (json.RawMessage, error)
	SetVMIrqBalance(context.Context, client.VMIrqInput) (json.RawMessage, error)
	CreateVMXMLTemplate(context.Context, client.VMTemplateInput) (json.RawMessage, error)
	UpdateVMXMLTemplate(context.Context, client.VMUpdateTemplateInput) (json.RawMessage, error)
	GrowVMDisk(context.Context, client.VMGrowDiskInput) (json.RawMessage, error)
	ListVMs(context.Context, client.VMEmptyInput) (json.RawMessage, error)
	GetVM(context.Context, client.VMNameInput) (json.RawMessage, error)
	GetVMXML(context.Context, client.VMXMLInput) (json.RawMessage, error)
	GetVMCPUXML(context.Context, client.VMCPUXMLInput) (json.RawMessage, error)
	ListVMAttachedDisks(context.Context, client.VMNameInput) (json.RawMessage, error)
	GetVMVideo(context.Context, client.VMNameInput) (json.RawMessage, error)
	GetVMBallooning(context.Context, client.VMNameInput) (json.RawMessage, error)
	GetVMHugePages(context.Context, client.VMNameInput) (json.RawMessage, error)
	GetVMKVM(context.Context, client.VMNameInput) (json.RawMessage, error)
	GetVMHyperV(context.Context, client.VMNameInput) (json.RawMessage, error)
	GetVMCPUPinning(context.Context, client.VMNameInput) (json.RawMessage, error)
	ListVMMachineTypes(context.Context, client.VMHostInput) (json.RawMessage, error)
	GetVMCPUTopology(context.Context, client.VMHostInput) (json.RawMessage, error)
	GetVMTunedProfiles(context.Context, client.VMHostInput) (json.RawMessage, error)
	GetVMIrqBalance(context.Context, client.VMHostInput) (json.RawMessage, error)
	GetVMCoreIsolation(context.Context, client.VMHostInput) (json.RawMessage, error)
	GetVMHostHugePages(context.Context, client.VMHostInput) (json.RawMessage, error)
	ListVMBackups(context.Context, client.VMEmptyInput) (json.RawMessage, error)
	ListVMAutomaticBackups(context.Context, client.VMEmptyInput) (json.RawMessage, error)
	ListVMXMLTemplates(context.Context, client.VMEmptyInput) (json.RawMessage, error)
	GetVMXMLTemplate(context.Context, client.VMIDInput) (json.RawMessage, error)
	ListVMDisks(context.Context, client.VMEmptyInput) (json.RawMessage, error)
	GetVMCPUDisableFeatures(context.Context, client.VMCPUFeaturesInput) (json.RawMessage, error)
}
type vmOutput struct {
	Result json.RawMessage `json:"result"`
}

func vmTool(name, method, path, description string, risk vmsafety.Risk) *mcp.Tool {
	destructive := method != "GET"
	return &mcp.Tool{Name: name, Description: fmt.Sprintf("Risk: %s. %s Calls %s %s. No automatic retries.", risk, description, method, path), Meta: mcp.Meta{"hyperhive/risk": string(risk), "hyperhive/endpoint": map[string]any{"method": method, "path": path}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: method == "GET", DestructiveHint: &destructive, IdempotentHint: method == "GET"}}
}
func vmResult(value any) (*mcp.CallToolResult, any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, nil, errors.New("could not encode VM result")
	}
	return &mcp.CallToolResult{StructuredContent: json.RawMessage(data), Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
}
func vmArguments(req *mcp.CallToolRequest) error {
	var args map[string]json.RawMessage
	if req == nil || req.Params == nil || (len(req.Params.Arguments) > 0 && json.Unmarshal(req.Params.Arguments, &args) != nil) {
		return errors.New("invalid VM arguments")
	}
	if len(req.Params.Arguments) > 0 && args == nil {
		return errors.New("VM arguments must be an object")
	}
	for _, raw := range args {
		if string(raw) == "null" {
			return errors.New("VM arguments cannot be null")
		}
	}
	return nil
}
func addVMRead[In any](server *mcp.Server, tool *mcp.Tool, read func(context.Context, In) (json.RawMessage, error)) {
	tool.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{"result": map[string]any{}}, "required": []string{"result"}, "additionalProperties": false}
	addExactTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		if err := vmArguments(req); err != nil {
			return nil, nil, err
		}
		result, err := read(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		return vmResult(vmOutput{result})
	})
}
func RegisterVMs(server *mcp.Server, api VMsAPI, backupDir string) {
	gate := &vmsafety.Gate{}
	store := vmsafety.Store{Dir: backupDir}
	registerVMWrites(server, api, gate, store)
	registerVMTransfers(server, api, gate, store)
	registerVMMore(server, api, gate, store)
	registerVMHostWrites(server, api, gate, store)
	addVMRead(server, vmTool("list_vms", "GET", "/virsh/getallvms", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.ListVMs)
	addVMRead(server, vmTool("get_vm", "GET", "/virsh/getvmbyname/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVM)
	addVMRead(server, vmTool("get_vm_xml", "GET", "/virsh/vmxml/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMXML)
	addVMRead(server, vmTool("get_vm_cpu_xml", "GET", "/virsh/cpuxml/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields. The backend requires machine_name in a GET JSON body.", vmsafety.Low), api.GetVMCPUXML)
	addVMRead(server, vmTool("list_vm_attached_disks", "GET", "/virsh/vm_disk/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.ListVMAttachedDisks)
	addVMRead(server, vmTool("get_vm_video", "GET", "/virsh/novncvideo/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMVideo)
	addVMRead(server, vmTool("get_vm_ballooning", "GET", "/virsh/ballooning/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMBallooning)
	addVMRead(server, vmTool("get_vm_hugepages", "GET", "/virsh/hugepages/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMHugePages)
	addVMRead(server, vmTool("get_vm_kvm", "GET", "/virsh/kvm/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMKVM)
	addVMRead(server, vmTool("get_vm_hyperv", "GET", "/virsh/hyperv/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMHyperV)
	addVMRead(server, vmTool("get_vm_cpu_pinning", "GET", "/virsh/cpupinning/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMCPUPinning)
	addVMRead(server, vmTool("list_vm_machine_types", "GET", "/virsh/machine_types/{machine_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.ListVMMachineTypes)
	addVMRead(server, vmTool("get_vm_cpu_topology", "GET", "/virsh/cputopology/{machine_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMCPUTopology)
	addVMRead(server, vmTool("get_vm_tuned_profiles", "GET", "/virsh/tunedadm/{machine_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMTunedProfiles)
	addVMRead(server, vmTool("get_vm_irqbalance", "GET", "/virsh/irqbalance/{machine_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMIrqBalance)
	addVMRead(server, vmTool("get_vm_core_isolation", "GET", "/virsh/coreisolation/{machine_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMCoreIsolation)
	addVMRead(server, vmTool("get_vm_host_hugepages", "GET", "/virsh/hosthugepages/{machine_name}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMHostHugePages)
	addVMRead(server, vmTool("list_vm_backups", "GET", "/virsh/backups", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.ListVMBackups)
	addVMRead(server, vmTool("list_vm_automatic_backups", "GET", "/virsh/autobak", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.ListVMAutomaticBackups)
	addVMRead(server, vmTool("list_vm_xml_templates", "GET", "/virsh/xmltemplates/", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.ListVMXMLTemplates)
	addVMRead(server, vmTool("get_vm_xml_template", "GET", "/virsh/xmltemplates/{id}", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMXMLTemplate)
	addVMRead(server, vmTool("list_vm_disks", "GET", "/vm-disk/list", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.ListVMDisks)
	addVMRead(server, vmTool("get_vm_cpu_disable_features", "GET", "/virsh/getcpudisablefeatures", "Return the complete API JSON, preserving exact integers and backend fields.", vmsafety.Low), api.GetVMCPUDisableFeatures)
}
