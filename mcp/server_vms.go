package hyperhivemcp

import (
	"context"
	"encoding/json"
	"github.com/Maruqes/HyperHive/mcp/internal/client"
)

func (api configuredAPI) ListVMs(ctx context.Context, in client.VMEmptyInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListVMs(ctx, in)
}
func (api configuredAPI) GetVM(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVM(ctx, in)
}
func (api configuredAPI) GetVMXML(ctx context.Context, in client.VMXMLInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMXML(ctx, in)
}
func (api configuredAPI) GetVMCPUXML(ctx context.Context, in client.VMCPUXMLInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMCPUXML(ctx, in)
}
func (api configuredAPI) ListVMAttachedDisks(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListVMAttachedDisks(ctx, in)
}
func (api configuredAPI) GetVMVideo(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMVideo(ctx, in)
}
func (api configuredAPI) GetVMBallooning(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMBallooning(ctx, in)
}
func (api configuredAPI) GetVMHugePages(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMHugePages(ctx, in)
}
func (api configuredAPI) GetVMKVM(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMKVM(ctx, in)
}
func (api configuredAPI) GetVMHyperV(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMHyperV(ctx, in)
}
func (api configuredAPI) GetVMCPUPinning(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMCPUPinning(ctx, in)
}
func (api configuredAPI) ListVMMachineTypes(ctx context.Context, in client.VMHostInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListVMMachineTypes(ctx, in)
}
func (api configuredAPI) GetVMCPUTopology(ctx context.Context, in client.VMHostInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMCPUTopology(ctx, in)
}
func (api configuredAPI) GetVMTunedProfiles(ctx context.Context, in client.VMHostInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMTunedProfiles(ctx, in)
}
func (api configuredAPI) GetVMIrqBalance(ctx context.Context, in client.VMHostInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMIrqBalance(ctx, in)
}
func (api configuredAPI) GetVMCoreIsolation(ctx context.Context, in client.VMHostInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMCoreIsolation(ctx, in)
}
func (api configuredAPI) GetVMHostHugePages(ctx context.Context, in client.VMHostInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMHostHugePages(ctx, in)
}
func (api configuredAPI) ListVMBackups(ctx context.Context, in client.VMEmptyInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListVMBackups(ctx, in)
}
func (api configuredAPI) ListVMAutomaticBackups(ctx context.Context, in client.VMEmptyInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListVMAutomaticBackups(ctx, in)
}
func (api configuredAPI) ListVMXMLTemplates(ctx context.Context, in client.VMEmptyInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListVMXMLTemplates(ctx, in)
}
func (api configuredAPI) GetVMXMLTemplate(ctx context.Context, in client.VMIDInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMXMLTemplate(ctx, in)
}
func (api configuredAPI) ListVMDisks(ctx context.Context, in client.VMEmptyInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListVMDisks(ctx, in)
}
func (api configuredAPI) GetVMCPUDisableFeatures(ctx context.Context, in client.VMCPUFeaturesInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMCPUDisableFeatures(ctx, in)
}
func (api configuredAPI) StartVM(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.StartVM(ctx, in)
}
func (api configuredAPI) ShutdownVM(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ShutdownVM(ctx, in)
}
func (api configuredAPI) PauseVM(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.PauseVM(ctx, in)
}
func (api configuredAPI) ResumeVM(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ResumeVM(ctx, in)
}
func (api configuredAPI) ForceShutdownVM(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ForceShutdownVM(ctx, in)
}
func (api configuredAPI) RestartVM(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.RestartVM(ctx, in)
}
func (api configuredAPI) SetVMCPUPinning(ctx context.Context, in client.VMPinningInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMCPUPinning(ctx, in)
}
func (api configuredAPI) UpdateVMXML(ctx context.Context, in client.VMUpdateXMLInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.UpdateVMXML(ctx, in)
}
func (api configuredAPI) UpdateVMCPUXML(ctx context.Context, in client.VMUpdateCPUXMLInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.UpdateVMCPUXML(ctx, in)
}
func (api configuredAPI) SetVMBallooning(ctx context.Context, in client.VMEnableInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMBallooning(ctx, in)
}
func (api configuredAPI) SetVMHugePages(ctx context.Context, in client.VMEnableInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMHugePages(ctx, in)
}
func (api configuredAPI) SetVMKVM(ctx context.Context, in client.VMKVMInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMKVM(ctx, in)
}
func (api configuredAPI) SetVMHyperV(ctx context.Context, in client.VMHyperVInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMHyperV(ctx, in)
}
func (api configuredAPI) SetVMMachineType(ctx context.Context, in client.VMMachineTypeInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMMachineType(ctx, in)
}
func (api configuredAPI) SetVMNetwork(ctx context.Context, in client.VMNetworkInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMNetwork(ctx, in)
}
func (api configuredAPI) SetVMVNCPassword(ctx context.Context, in client.VMVNCPasswordInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMVNCPassword(ctx, in)
}
func (api configuredAPI) AddVMVideo(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.AddVMVideo(ctx, in)
}
func (api configuredAPI) EditVM(ctx context.Context, in client.VMEditInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.EditVM(ctx, in)
}
func (api configuredAPI) SetVMTunedProfile(ctx context.Context, in client.VMTunedInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMTunedProfile(ctx, in)
}
func (api configuredAPI) SetVMIrqBalance(ctx context.Context, in client.VMIrqInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMIrqBalance(ctx, in)
}
func (api configuredAPI) CreateVMXMLTemplate(ctx context.Context, in client.VMTemplateInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.CreateVMXMLTemplate(ctx, in)
}
func (api configuredAPI) UpdateVMXMLTemplate(ctx context.Context, in client.VMUpdateTemplateInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.UpdateVMXMLTemplate(ctx, in)
}
func (api configuredAPI) GrowVMDisk(ctx context.Context, in client.VMGrowDiskInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GrowVMDisk(ctx, in)
}

func (api configuredAPI) GetVMSnapshot(ctx context.Context, in client.VMXMLInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMSnapshot(ctx, in)
}

func (api configuredAPI) ExportVM(ctx context.Context, in client.VMNameInput, dir string) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ExportVM(ctx, in, dir)
}
func (api configuredAPI) DownloadVMBackup(ctx context.Context, in client.VMIDInput, dir string) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.DownloadVMBackup(ctx, in, dir)
}

func (api configuredAPI) AttachVMDisk(ctx context.Context, in client.VMAttachDiskInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.AttachVMDisk(ctx, in)
}
func (api configuredAPI) BackupVM(ctx context.Context, in client.VMBackupInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.BackupVM(ctx, in)
}
func (api configuredAPI) CloneVM(ctx context.Context, in client.VMCloneInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.CloneVM(ctx, in)
}
func (api configuredAPI) DisableVMAutomaticBackup(ctx context.Context, in client.VMIDInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.DisableVMAutomaticBackup(ctx, in)
}
func (api configuredAPI) SnapshotVMNFS(ctx context.Context, id int64) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SnapshotVMNFS(ctx, id)
}

func (api configuredAPI) GetVMHostBootSnapshot(ctx context.Context, in client.VMHostInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetVMHostBootSnapshot(ctx, in)
}
func (api configuredAPI) SetVMHostCoreIsolationPost(ctx context.Context, in client.VMCoreIsolationInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMHostCoreIsolationPost(ctx, in)
}
func (api configuredAPI) SetVMHostCoreIsolationPut(ctx context.Context, in client.VMCoreIsolationInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMHostCoreIsolationPut(ctx, in)
}
func (api configuredAPI) SetVMHostHugePagesPost(ctx context.Context, in client.VMHostHugePagesInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMHostHugePagesPost(ctx, in)
}
func (api configuredAPI) SetVMHostHugePagesPut(ctx context.Context, in client.VMHostHugePagesInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMHostHugePagesPut(ctx, in)
}
