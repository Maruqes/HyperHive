package tools

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/Maruqes/HyperHive/mcp/internal/vmsafety"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type VMsAPI interface {
	RemoveVMISOs(context.Context, client.VMNameInput) (json.RawMessage, error)
	CreateVM(context.Context, client.VMCreateInput) (json.RawMessage, error)
	SetVMAutostart(context.Context, client.VMAutostartInput) (json.RawMessage, error)
	SetVMLive(context.Context, client.VMEnableInput) (json.RawMessage, error)
	CreateVMAutomaticBackup(context.Context, client.VMScheduleSettings) (json.RawMessage, error)
	UpdateVMAutomaticBackup(context.Context, client.VMUpdateScheduleInput) (json.RawMessage, error)
	EnableVMAutomaticBackup(context.Context, client.VMIDInput) (json.RawMessage, error)
	AddVMSSHKey(context.Context, client.VMSSHInput) (json.RawMessage, error)
	ListISOs(context.Context) ([]client.ISOObject, error)

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

func vmTool(name, method, path, description string) *mcp.Tool {
	destructive := method != "GET"
	return &mcp.Tool{Name: name, Description: description + " Calls " + method + " " + path + ". No automatic retries.", Meta: mcp.Meta{"hyperhive/endpoint": map[string]any{"method": method, "path": path}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: method == "GET", DestructiveHint: &destructive, IdempotentHint: method == "GET"}}
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
func addVMRead[In any](tool *mcp.Tool, read func(context.Context, In) (json.RawMessage, error)) ToolDefinition {
	tool.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{"result": map[string]any{}}, "required": []string{"result"}, "additionalProperties": false}
	return defineTool(tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
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
func defineListVMs(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("list_vms", "GET", "/virsh/getallvms", "Return the complete API JSON, preserving exact integers and backend fields."), api.ListVMs)
}

func defineGetVM(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm", "GET", "/virsh/getvmbyname/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVM)
}

func defineGetVMXML(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_xml", "GET", "/virsh/vmxml/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMXML)
}

func defineGetVMCPUXML(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_cpu_xml", "GET", "/virsh/cpuxml/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields. The backend requires machine_name in a GET JSON body."), api.GetVMCPUXML)
}

func defineListVMAttachedDisks(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("list_vm_attached_disks", "GET", "/virsh/vm_disk/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.ListVMAttachedDisks)
}

func defineGetVMVideo(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_video", "GET", "/virsh/novncvideo/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMVideo)
}

func defineGetVMBallooning(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_ballooning", "GET", "/virsh/ballooning/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMBallooning)
}

func defineGetVMHugepages(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_hugepages", "GET", "/virsh/hugepages/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMHugePages)
}

func defineGetVMKvm(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_kvm", "GET", "/virsh/kvm/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMKVM)
}

func defineGetVMHyperv(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_hyperv", "GET", "/virsh/hyperv/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMHyperV)
}

func defineGetVMCPUPinning(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_cpu_pinning", "GET", "/virsh/cpupinning/{vm_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMCPUPinning)
}

func defineListVMMachineTypes(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("list_vm_machine_types", "GET", "/virsh/machine_types/{machine_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.ListVMMachineTypes)
}

func defineGetVMCPUTopology(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_cpu_topology", "GET", "/virsh/cputopology/{machine_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMCPUTopology)
}

func defineGetVMTunedProfiles(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_tuned_profiles", "GET", "/virsh/tunedadm/{machine_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMTunedProfiles)
}

func defineGetVMIrqbalance(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_irqbalance", "GET", "/virsh/irqbalance/{machine_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMIrqBalance)
}

func defineGetVMCoreIsolation(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_core_isolation", "GET", "/virsh/coreisolation/{machine_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMCoreIsolation)
}

func defineGetVMHostHugepages(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_host_hugepages", "GET", "/virsh/hosthugepages/{machine_name}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMHostHugePages)
}

func defineListVMBackups(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("list_vm_backups", "GET", "/virsh/backups", "Return the complete API JSON, preserving exact integers and backend fields."), api.ListVMBackups)
}

func defineListVMAutomaticBackups(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("list_vm_automatic_backups", "GET", "/virsh/autobak", "Return the complete API JSON, preserving exact integers and backend fields."), api.ListVMAutomaticBackups)
}

func defineListVMXMLTemplates(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("list_vm_xml_templates", "GET", "/virsh/xmltemplates/", "Return the complete API JSON, preserving exact integers and backend fields."), api.ListVMXMLTemplates)
}

func defineGetVMXMLTemplate(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_xml_template", "GET", "/virsh/xmltemplates/{id}", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMXMLTemplate)
}

func defineListVMDisks(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("list_vm_disks", "GET", "/vm-disk/list", "Return the complete API JSON, preserving exact integers and backend fields."), api.ListVMDisks)
}

func defineGetVMCPUDisableFeatures(api VMsAPI) ToolDefinition {
	return addVMRead(vmTool("get_vm_cpu_disable_features", "GET", "/virsh/getcpudisablefeatures", "Return the complete API JSON, preserving exact integers and backend fields."), api.GetVMCPUDisableFeatures)
}

// RegisterVMs is the complete tool and safety catalog for this area.
// Keep names and explicit levels here; definitions contain handlers and schemas.
func RegisterVMs(server *Registry, api VMsAPI, backupDir string) {
	server = server.Area("vms")
	gate := server.gate
	store := vmsafety.Store{Dir: backupDir}
	RegisterTool(server, "add_vm_ssh_key", defineAddVMSSHKey(api, gate, store), safety.VeryHigh)
	RegisterTool(server, "add_vm_video", defineAddVMVideo(api, gate, store), safety.High)
	RegisterTool(server, "attach_vm_disk", defineAttachVMDisk(api, gate, store), safety.High)
	RegisterTool(server, "backup_vm", defineBackupVM(api, gate, store), safety.High)
	RegisterTool(server, "clone_vm", defineCloneVM(api, gate, store), safety.High)
	RegisterTool(server, "create_vm", defineCreateVM(api, gate, store), safety.High)
	RegisterTool(server, "create_vm_automatic_backup", defineCreateVMAutomaticBackup(api, gate, store), safety.VeryHigh)
	RegisterTool(server, "create_vm_xml_template", defineCreateVMXMLTemplate(api, gate, store), safety.Medium)
	RegisterTool(server, "disable_vm_automatic_backup", defineDisableVMAutomaticBackup(api, gate, store), safety.Medium)
	RegisterTool(server, "download_vm_backup", defineDownloadVMBackup(api, gate, store), safety.Low)
	RegisterTool(server, "edit_vm", defineEditVM(api, gate, store), safety.High)
	RegisterTool(server, "enable_vm_automatic_backup", defineEnableVMAutomaticBackup(api, gate, store), safety.VeryHigh)
	RegisterTool(server, "export_vm", defineExportVM(api, gate, store), safety.Medium)
	RegisterTool(server, "force_shutdown_vm", defineForceShutdownVM(api, gate, store), safety.VeryHigh)
	RegisterTool(server, "get_vm", defineGetVM(api), safety.Low)
	RegisterTool(server, "get_vm_ballooning", defineGetVMBallooning(api), safety.Low)
	RegisterTool(server, "get_vm_core_isolation", defineGetVMCoreIsolation(api), safety.Low)
	RegisterTool(server, "get_vm_cpu_disable_features", defineGetVMCPUDisableFeatures(api), safety.Low)
	RegisterTool(server, "get_vm_cpu_pinning", defineGetVMCPUPinning(api), safety.Low)
	RegisterTool(server, "get_vm_cpu_topology", defineGetVMCPUTopology(api), safety.Low)
	RegisterTool(server, "get_vm_cpu_xml", defineGetVMCPUXML(api), safety.Low)
	RegisterTool(server, "get_vm_host_hugepages", defineGetVMHostHugepages(api), safety.Low)
	RegisterTool(server, "get_vm_hugepages", defineGetVMHugepages(api), safety.Low)
	RegisterTool(server, "get_vm_hyperv", defineGetVMHyperv(api), safety.Low)
	RegisterTool(server, "get_vm_irqbalance", defineGetVMIrqbalance(api), safety.Low)
	RegisterTool(server, "get_vm_kvm", defineGetVMKvm(api), safety.Low)
	RegisterTool(server, "get_vm_tuned_profiles", defineGetVMTunedProfiles(api), safety.Low)
	RegisterTool(server, "get_vm_video", defineGetVMVideo(api), safety.Low)
	RegisterTool(server, "get_vm_xml", defineGetVMXML(api), safety.Low)
	RegisterTool(server, "get_vm_xml_template", defineGetVMXMLTemplate(api), safety.Low)
	RegisterTool(server, "grow_vm_disk", defineGrowVMDisk(api, gate, store), safety.High)
	RegisterTool(server, "list_vm_attached_disks", defineListVMAttachedDisks(api), safety.Low)
	RegisterTool(server, "list_vm_automatic_backups", defineListVMAutomaticBackups(api), safety.Low)
	RegisterTool(server, "list_vm_backups", defineListVMBackups(api), safety.Low)
	RegisterTool(server, "list_vm_disks", defineListVMDisks(api), safety.Low)
	RegisterTool(server, "list_vm_machine_types", defineListVMMachineTypes(api), safety.Low)
	RegisterTool(server, "list_vm_xml_templates", defineListVMXMLTemplates(api), safety.Low)
	RegisterTool(server, "list_vms", defineListVMs(api), safety.Low)
	RegisterTool(server, "pause_vm", definePauseVM(api, gate, store), safety.High)
	RegisterTool(server, "remove_vm_isos", defineRemoveVMISOs(api, gate, store), safety.High)
	RegisterTool(server, "replace_vm_host_core_isolation", defineReplaceVMHostCoreIsolation(api, gate, store), safety.VeryHigh)
	RegisterTool(server, "replace_vm_host_hugepages", defineReplaceVMHostHugepages(api, gate, store), safety.VeryHigh)
	RegisterTool(server, "restart_vm", defineRestartVM(api, gate, store), safety.VeryHigh)
	RegisterTool(server, "resume_vm", defineResumeVM(api, gate, store), safety.Medium)
	RegisterTool(server, "set_vm_autostart", defineSetVMAutostart(api, gate, store), safety.Medium)
	RegisterTool(server, "set_vm_ballooning", defineSetVMBallooning(api, gate, store), safety.High)
	RegisterTool(server, "set_vm_cpu_pinning", defineSetVMCPUPinning(api, gate, store), safety.High)
	RegisterTool(server, "set_vm_host_core_isolation", defineSetVMHostCoreIsolation(api, gate, store), safety.VeryHigh)
	RegisterTool(server, "set_vm_host_hugepages", defineSetVMHostHugepages(api, gate, store), safety.VeryHigh)
	RegisterTool(server, "set_vm_hugepages", defineSetVMHugepages(api, gate, store), safety.High)
	RegisterTool(server, "set_vm_hyperv", defineSetVMHyperv(api, gate, store), safety.High)
	RegisterTool(server, "set_vm_irqbalance", defineSetVMIrqbalance(api, gate, store), safety.High)
	RegisterTool(server, "set_vm_kvm", defineSetVMKvm(api, gate, store), safety.High)
	RegisterTool(server, "set_vm_live", defineSetVMLive(api, gate, store), safety.High)
	RegisterTool(server, "set_vm_machine_type", defineSetVMMachineType(api, gate, store), safety.High)
	RegisterTool(server, "set_vm_network", defineSetVMNetwork(api, gate, store), safety.High)
	RegisterTool(server, "set_vm_tuned_profile", defineSetVMTunedProfile(api, gate, store), safety.High)
	RegisterTool(server, "set_vm_vnc_password", defineSetVMVNCPassword(api, gate, store), safety.High)
	RegisterTool(server, "shutdown_vm", defineShutdownVM(api, gate, store), safety.High)
	RegisterTool(server, "start_vm", defineStartVM(api, gate, store), safety.Medium)
	RegisterTool(server, "update_vm_automatic_backup", defineUpdateVMAutomaticBackup(api, gate, store), safety.VeryHigh)
	RegisterTool(server, "update_vm_cpu_xml", defineUpdateVMCPUXML(api, gate, store), safety.High)
	RegisterTool(server, "update_vm_xml", defineUpdateVMXML(api, gate, store), safety.VeryHigh)
	RegisterTool(server, "update_vm_xml_template", defineUpdateVMXMLTemplate(api, gate, store), safety.High)
}
