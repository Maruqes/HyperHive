package tools

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/Maruqes/HyperHive/mcp/internal/vmsafety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strconv"
	"strings"
)

type vmWriteOutput struct {
	Result            json.RawMessage `json:"result"`
	BackupPath        string          `json:"backup_path"`
	AutomaticRollback bool            `json:"automatic_rollback"`
}
type vmSnapshot func(context.Context) (map[string]json.RawMessage, error)

func vmSnapshotDomain(ctx context.Context, api VMsAPI, name, machine string) (map[string]json.RawMessage, error) {
	raw, err := api.GetVM(ctx, client.VMNameInput{VMName: name})
	if err != nil {
		return nil, err
	}
	if client.ValidateVMResponse("GetVM", raw) != nil {
		return nil, errors.New("invalid VM identity snapshot")
	}
	var vm struct {
		Name    string `json:"name"`
		Machine string `json:"machineName"`
	}
	json.Unmarshal(raw, &vm)
	if vm.Name != name || (machine != "" && machine != vm.Machine) {
		return nil, errors.New("VM target host or name differs from the current identity; no change made")
	}
	oldXML, err := api.GetVMXML(ctx, client.VMXMLInput{VMName: name, MachineName: vm.Machine})
	if err != nil {
		return nil, err
	}
	if client.ValidateVMResponse("GetVMXML", oldXML) != nil {
		return nil, errors.New("invalid VM domain snapshot")
	}
	var domain struct {
		XML     string `json:"vm_xml"`
		Machine string `json:"machine_name"`
	}
	json.Unmarshal(oldXML, &domain)
	var doc struct {
		XMLName xml.Name `xml:"domain"`
		Name    string   `xml:"name"`
	}
	if xml.Unmarshal([]byte(domain.XML), &doc) != nil || doc.Name != name || domain.Machine != vm.Machine {
		return nil, errors.New("VM domain snapshot identity mismatch; no change made")
	}
	secure, err := api.GetVMSnapshot(ctx, client.VMXMLInput{VMName: name, MachineName: vm.Machine})
	if err != nil {
		return nil, err
	}
	if client.ValidateVMResponse("GetVMSnapshot", secure) != nil {
		return nil, errors.New("secure VM snapshot unavailable; no change made")
	}
	var identity struct {
		Name    string `json:"name"`
		Machine string `json:"machine_name"`
	}
	json.Unmarshal(secure, &identity)
	if identity.Name != name || identity.Machine != vm.Machine {
		return nil, errors.New("secure VM snapshot identity mismatch; no change made")
	}
	return map[string]json.RawMessage{"vm": raw, "domain_xml": oldXML, "secure_domain": secure}, nil
}
func vmExtraSnapshot(ctx context.Context, snapshots map[string]json.RawMessage, key, kind string, read func(context.Context) (json.RawMessage, error)) error {
	raw, err := read(ctx)
	if err != nil {
		return err
	}
	if client.ValidateVMResponse(kind, raw) != nil {
		return errors.New("invalid VM setting snapshot; no change made")
	}
	if string(raw) == "null" {
		snapshots[key] = vmInventoryRecord(raw)
	} else {
		snapshots[key] = raw
	}
	return nil
}
func addVMWrite[In any](api VMsAPI, gate *safety.Gate, store vmsafety.Store, tool *mcp.Tool, write func(context.Context, In) (json.RawMessage, error), snapshot func(context.Context, In) (string, map[string]json.RawMessage, error)) ToolDefinition {
	tool.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{"result": map[string]any{}, "backup_path": map[string]any{"type": "string"}, "automatic_rollback": map[string]any{"type": "boolean"}}, "required": []string{"result", "backup_path", "automatic_rollback"}, "additionalProperties": false}
	return defineToolWithLevel(tool, func(risk safety.Level) mcp.ToolHandlerFor[In, any] {
		return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			if err := vmArguments(req); err != nil {
				return nil, nil, err
			}
			if err := client.ValidateVMWrite(in); err != nil {
				return nil, nil, err
			}
			unlock, err := gate.Lock(ctx)
			if err != nil {
				return nil, nil, err
			}
			defer unlock()
			target, snapshots, err := snapshot(ctx, in)
			if err != nil {
				return nil, nil, err
			}
			path, err := store.Save(ctx, vmsafety.Record{Tool: tool.Name, Target: target, Risk: risk, Snapshots: snapshots})
			if err != nil {
				return nil, nil, err
			}
			result, err := write(ctx, in)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("VM backend change failed after backup. Retained backup: %s. No automatic rollback was attempted. %s", path, err)}}}, nil, nil
			}
			return vmResult(vmWriteOutput{Result: result, BackupPath: path, AutomaticRollback: false})
		}
	})
}
func defineUpdateVMXML(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("update_vm_xml", "POST", "/virsh/updatevmxml/{vm_name}", "Replace the full domain definition; guest must be shut off. Domain name and actual UUID must match. A secure inactive/current XML snapshot is saved before mutation. No automatic rollback."), api.UpdateVMXML, func(ctx context.Context, in client.VMUpdateXMLInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, in.MachineName)
		if err != nil {
			return "", nil, err
		}
		var before struct {
			UUID string `json:"uuid"`
		}
		json.Unmarshal(snapshots["secure_domain"], &before)
		var after struct {
			UUID string `xml:"uuid"`
		}
		if xml.Unmarshal([]byte(in.VMXML), &after) != nil || before.UUID == "" || before.UUID != after.UUID {
			return "", nil, errors.New("replacement XML must preserve the actual existing UUID; no change made")
		}
		return in.VMName, snapshots, nil
	})
}

func defineStartVM(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("start_vm", "POST", "/virsh/startvm/{vm_name}", "Start the VM. A private prior-state record is saved before mutation. No automatic rollback."), api.StartVM, func(ctx context.Context, in client.VMNameInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineShutdownVM(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("shutdown_vm", "POST", "/virsh/shutdownvm/{vm_name}", "Request guest shutdown; services will stop. A private prior-state record is saved before mutation. No automatic rollback."), api.ShutdownVM, func(ctx context.Context, in client.VMNameInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func definePauseVM(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("pause_vm", "POST", "/virsh/pausevm/{vm_name}", "Pause guest execution. A private prior-state record is saved before mutation. No automatic rollback."), api.PauseVM, func(ctx context.Context, in client.VMNameInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineResumeVM(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("resume_vm", "POST", "/virsh/resumevm/{vm_name}", "Resume guest execution. A private prior-state record is saved before mutation. No automatic rollback."), api.ResumeVM, func(ctx context.Context, in client.VMNameInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineForceShutdownVM(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("force_shutdown_vm", "POST", "/virsh/forceshutdownvm/{vm_name}", "Force stop the VM; unsaved guest data may be lost. A private prior-state record is saved before mutation. No automatic rollback."), api.ForceShutdownVM, func(ctx context.Context, in client.VMNameInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineRestartVM(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("restart_vm", "POST", "/virsh/restartvm/{vm_name}", "Force stop then start the VM; unsaved guest data may be lost. A private prior-state record is saved before mutation. No automatic rollback."), api.RestartVM, func(ctx context.Context, in client.VMNameInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineSetVMCPUPinning(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("set_vm_cpu_pinning", "POST", "/virsh/cpupinning/{vm_name}", "Replace CPU pinning and topology; physical core ranges and hyper-threading are specified below. A private prior-state record is saved before mutation. No automatic rollback."), api.SetVMCPUPinning, func(ctx context.Context, in client.VMPinningInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		if err = vmExtraSnapshot(ctx, snapshots, "setting", "GetVMCPUPinning", func(ctx context.Context) (json.RawMessage, error) {
			return api.GetVMCPUPinning(ctx, client.VMNameInput{VMName: in.VMName})
		}); err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineUpdateVMCPUXML(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("update_vm_cpu_xml", "POST", "/virsh/updatecpuxml/{vm_name}", "Replace the CPU XML definition on the named host; CPU content is omitted from picker; size shown and the full argument digest binds approval. A private prior-state record is saved before mutation. No automatic rollback."), api.UpdateVMCPUXML, func(ctx context.Context, in client.VMUpdateCPUXMLInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, in.MachineName)
		if err != nil {
			return "", nil, err
		}
		if err = vmExtraSnapshot(ctx, snapshots, "cpu_xml", "GetVMCPUXML", func(ctx context.Context) (json.RawMessage, error) {
			return api.GetVMCPUXML(ctx, client.VMCPUXMLInput{VMName: in.VMName, MachineName: in.MachineName})
		}); err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineSetVMBallooning(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("set_vm_ballooning", "POST", "/virsh/ballooning/{vm_name}", "Replace memory ballooning settings; guest must be shut off. A private prior-state record is saved before mutation. No automatic rollback."), api.SetVMBallooning, func(ctx context.Context, in client.VMEnableInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		if err = vmExtraSnapshot(ctx, snapshots, "setting", "GetVMBallooning", func(ctx context.Context) (json.RawMessage, error) {
			return api.GetVMBallooning(ctx, client.VMNameInput{VMName: in.VMName})
		}); err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineSetVMHugepages(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("set_vm_hugepages", "POST", "/virsh/hugepages/{vm_name}", "Replace guest hugepage settings; guest must be shut off. A private prior-state record is saved before mutation. No automatic rollback."), api.SetVMHugePages, func(ctx context.Context, in client.VMEnableInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		if err = vmExtraSnapshot(ctx, snapshots, "setting", "GetVMHugePages", func(ctx context.Context) (json.RawMessage, error) {
			return api.GetVMHugePages(ctx, client.VMNameInput{VMName: in.VMName})
		}); err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineSetVMKvm(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("set_vm_kvm", "POST", "/virsh/kvm/{vm_name}", "Replace KVM hidden setting; guest must be shut off. A private prior-state record is saved before mutation. No automatic rollback."), api.SetVMKVM, func(ctx context.Context, in client.VMKVMInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		if err = vmExtraSnapshot(ctx, snapshots, "setting", "GetVMKVM", func(ctx context.Context) (json.RawMessage, error) {
			return api.GetVMKVM(ctx, client.VMNameInput{VMName: in.VMName})
		}); err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineSetVMHyperv(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("set_vm_hyperv", "POST", "/virsh/hyperv/{vm_name}", "Replace Hyper-V feature setting; guest must be shut off. A private prior-state record is saved before mutation. No automatic rollback."), api.SetVMHyperV, func(ctx context.Context, in client.VMHyperVInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		if err = vmExtraSnapshot(ctx, snapshots, "setting", "GetVMHyperV", func(ctx context.Context) (json.RawMessage, error) {
			return api.GetVMHyperV(ctx, client.VMNameInput{VMName: in.VMName})
		}); err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineSetVMMachineType(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("set_vm_machine_type", "POST", "/virsh/machine_type/{vm_name}", "Replace guest machine type; guest must be shut off. A private prior-state record is saved before mutation. No automatic rollback."), api.SetVMMachineType, func(ctx context.Context, in client.VMMachineTypeInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineSetVMNetwork(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("set_vm_network", "POST", "/virsh/change_vm_network/{vm_name}", "Replace guest network with default or 512rede; may interrupt connectivity. A private prior-state record is saved before mutation. No automatic rollback."), api.SetVMNetwork, func(ctx context.Context, in client.VMNetworkInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineSetVMVNCPassword(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("set_vm_vnc_password", "POST", "/virsh/change_vnc_password/{vm_name}", "Replace VNC password; guest must be shut off. Password omitted from picker. A private prior-state record is saved before mutation. No automatic rollback."), api.SetVMVNCPassword, func(ctx context.Context, in client.VMVNCPasswordInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineAddVMVideo(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("add_vm_video", "POST", "/virsh/novncvideo/add/{vm_name}", "Add managed noVNC graphics/video/audio devices; guest must be shut off. A private prior-state record is saved before mutation. No automatic rollback."), api.AddVMVideo, func(ctx context.Context, in client.VMNameInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		if err = vmExtraSnapshot(ctx, snapshots, "setting", "GetVMVideo", func(ctx context.Context) (json.RawMessage, error) {
			return api.GetVMVideo(ctx, client.VMNameInput{VMName: in.VMName})
		}); err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineEditVM(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("edit_vm", "POST", "/virsh/editvm/{vm_name}", "Replace VM resource settings; zero or omitted fields preserve existing values; positive disk size grows only. Guest must be shut off. A private prior-state record is saved before mutation. No automatic rollback."), api.EditVM, func(ctx context.Context, in client.VMEditInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		if in.DiskSizeGB > 0 {
			if err := vmValidatePrimaryDiskSnapshot(snapshots); err != nil {
				return "", nil, err
			}
		}
		return in.VMName, snapshots, nil
	})
}

func defineSetVMTunedProfile(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("set_vm_tuned_profile", "POST", "/virsh/tunedadm/{machine_name}", "Change host tuned profile; affects all workloads on this machine. A private prior-state record is saved before mutation. No automatic rollback."), api.SetVMTunedProfile, func(ctx context.Context, in client.VMTunedInput) (string, map[string]json.RawMessage, error) {
		snapshots := map[string]json.RawMessage{}
		err := vmExtraSnapshot(ctx, snapshots, "host_setting", "GetVMTunedProfiles", func(ctx context.Context) (json.RawMessage, error) {
			return api.GetVMTunedProfiles(ctx, client.VMHostInput{MachineName: in.MachineName})
		})
		return in.MachineName, snapshots, err
	})
}

func defineSetVMIrqbalance(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("set_vm_irqbalance", "POST", "/virsh/irqbalance/{machine_name}", "Enable/start or disable/stop host irqbalance service; affects all workloads. A private prior-state record is saved before mutation. No automatic rollback."), api.SetVMIrqBalance, func(ctx context.Context, in client.VMIrqInput) (string, map[string]json.RawMessage, error) {
		snapshots := map[string]json.RawMessage{}
		err := vmExtraSnapshot(ctx, snapshots, "host_setting", "GetVMIrqBalance", func(ctx context.Context) (json.RawMessage, error) {
			return api.GetVMIrqBalance(ctx, client.VMHostInput{MachineName: in.MachineName})
		})
		return in.MachineName, snapshots, err
	})
}

func defineCreateVMXMLTemplate(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("create_vm_xml_template", "POST", "/virsh/xmltemplates/", "Create a reusable VM XML template. XML omitted from summaries; size shown and the full argument digest binds the invocation. A private prior-state record is saved before mutation. No automatic rollback."), api.CreateVMXMLTemplate, func(ctx context.Context, in client.VMTemplateInput) (string, map[string]json.RawMessage, error) {
		snapshots := map[string]json.RawMessage{}
		err := vmExtraSnapshot(ctx, snapshots, "templates", "ListVMXMLTemplates", func(ctx context.Context) (json.RawMessage, error) {
			return api.ListVMXMLTemplates(ctx, client.VMEmptyInput{})
		})
		return in.Name, snapshots, err
	})
}

func defineUpdateVMXMLTemplate(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("update_vm_xml_template", "PUT", "/virsh/xmltemplates/{id}", "Replace name, description and XML of the template. XML omitted from summaries; size shown and the full argument digest binds the invocation. A private prior-state record is saved before mutation. No automatic rollback."), api.UpdateVMXMLTemplate, func(ctx context.Context, in client.VMUpdateTemplateInput) (string, map[string]json.RawMessage, error) {
		snapshots := map[string]json.RawMessage{}
		err := vmExtraSnapshot(ctx, snapshots, "templates", "GetVMXMLTemplate", func(ctx context.Context) (json.RawMessage, error) {
			return api.GetVMXMLTemplate(ctx, client.VMIDInput{ID: in.ID})
		})
		return strconv.FormatInt(in.ID, 10), snapshots, err
	})
}

func defineGrowVMDisk(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("grow_vm_disk", "POST", "/vm-disk/{id}/grow", "Grow a VM disk to size_gb; cannot shrink. This cannot be undone by the saved metadata record. A private prior-state record is saved before mutation. No automatic rollback."), api.GrowVMDisk, func(ctx context.Context, in client.VMGrowDiskInput) (string, map[string]json.RawMessage, error) {
		return vmSnapshotDisk(ctx, api, in.ID)
	})
}

func vmSnapshotDisk(ctx context.Context, api VMsAPI, id int64) (string, map[string]json.RawMessage, error) {
	raw, err := api.ListVMDisks(ctx, client.VMEmptyInput{})
	if err != nil {
		return "", nil, err
	}
	if client.ValidateVMResponse("ListVMDisks", raw) != nil {
		return "", nil, errors.New("invalid disk inventory snapshot")
	}
	var disks []json.RawMessage
	json.Unmarshal(raw, &disks)
	for _, disk := range disks {
		var old struct {
			ID          int64  `json:"id"`
			AttachedVM  string `json:"attached_vm_name"`
			StatusError string `json:"status_error"`
		}
		if json.Unmarshal(disk, &old) != nil {
			return "", nil, errors.New("invalid disk snapshot")
		}
		if old.ID != id {
			continue
		}
		if old.StatusError != "" {
			return "", nil, errors.New("actual old disk metadata unavailable; no change made")
		}
		snapshots := map[string]json.RawMessage{"disk": disk}
		if old.AttachedVM != "" {
			domain, err := vmSnapshotDomain(ctx, api, old.AttachedVM, "")
			if err != nil {
				return "", nil, err
			}
			for k, v := range domain {
				snapshots[k] = v
			}
		}
		return strconv.FormatInt(id, 10), snapshots, nil
	}
	return "", nil, errors.New("disk not found; no change made")
}

// VM inventory can succeed after its disk inspection fails. For a disk resize,
// require the actual previous size and the same file selected by EditVm from
// inactive XML; XML alone does not contain a virtual disk's previous size.
func vmValidatePrimaryDiskSnapshot(snapshots map[string]json.RawMessage) error {
	bad := errors.New("actual old primary disk metadata unavailable or inconsistent; no change made")
	var vm struct {
		Size int32  `json:"diskSizeGB"`
		Path string `json:"diskPath"`
	}
	if json.Unmarshal(snapshots["vm"], &vm) != nil || vm.Size <= 0 || strings.TrimSpace(vm.Path) == "" {
		return bad
	}
	var secure struct {
		XML string `json:"inactive_xml"`
	}
	if json.Unmarshal(snapshots["secure_domain"], &secure) != nil {
		return bad
	}
	var domain struct {
		Disks []struct {
			Device string `xml:"device,attr"`
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
		} `xml:"devices>disk"`
	}
	if xml.Unmarshal([]byte(secure.XML), &domain) != nil {
		return bad
	}
	for _, disk := range domain.Disks {
		device := strings.TrimSpace(disk.Device)
		if device == "" {
			device = "disk"
		}
		path := strings.TrimSpace(disk.Source.File)
		if device == "disk" && path != "" {
			if path != vm.Path {
				return bad
			}
			return nil
		}
	}
	return bad
}
