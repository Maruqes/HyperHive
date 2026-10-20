package hyperhivemcp

import (
	"context"
	"encoding/json"
	"github.com/Maruqes/HyperHive/mcp/internal/client"
)

func (api configuredAPI) CreateVM(ctx context.Context, in client.VMCreateInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.CreateVM(ctx, in)
}
func (api configuredAPI) SetVMAutostart(ctx context.Context, in client.VMAutostartInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMAutostart(ctx, in)
}
func (api configuredAPI) SetVMLive(ctx context.Context, in client.VMEnableInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.SetVMLive(ctx, in)
}
func (api configuredAPI) CreateVMAutomaticBackup(ctx context.Context, in client.VMScheduleSettings) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.CreateVMAutomaticBackup(ctx, in)
}
func (api configuredAPI) UpdateVMAutomaticBackup(ctx context.Context, in client.VMUpdateScheduleInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.UpdateVMAutomaticBackup(ctx, in)
}
func (api configuredAPI) EnableVMAutomaticBackup(ctx context.Context, in client.VMIDInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.EnableVMAutomaticBackup(ctx, in)
}
func (api configuredAPI) AddVMSSHKey(ctx context.Context, in client.VMSSHInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.AddVMSSHKey(ctx, in)
}

func (api configuredAPI) RemoveVMISOs(ctx context.Context, in client.VMNameInput) (json.RawMessage, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.RemoveVMISOs(ctx, in)
}
