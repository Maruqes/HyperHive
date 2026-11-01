package hyperhivemcp

import (
	"context"
	"encoding/json"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
)

func (api configuredAPI) automationClient() (*client.Client, error) {
	return client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
}

func (api configuredAPI) ListAutomations(ctx context.Context) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.ListAutomations(ctx)
}

func (api configuredAPI) GetAutomation(ctx context.Context, id string) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.GetAutomation(ctx, id)
}

func (api configuredAPI) CreateAutomation(ctx context.Context, input client.CreateAutomationInput) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.CreateAutomation(ctx, input)
}

func (api configuredAPI) ReplaceAutomation(ctx context.Context, id string, config client.AutomationConfig) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.ReplaceAutomation(ctx, id, config)
}

func (api configuredAPI) EvaluateAutomationCondition(ctx context.Context, input client.EvaluateConditionInput) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.EvaluateAutomationCondition(ctx, input)
}

func (api configuredAPI) RunAutomation(ctx context.Context, id string) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.RunAutomation(ctx, id)
}

func (api configuredAPI) ListAutomationRuns(ctx context.Context, input client.AutomationRunsInput) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.ListAutomationRuns(ctx, input)
}

func (api configuredAPI) GetAutomationRun(ctx context.Context, runID string) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.GetAutomationRun(ctx, runID)
}

func (api configuredAPI) ListAutomationFiles(ctx context.Context, id string) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.ListAutomationFiles(ctx, id)
}

func (api configuredAPI) ReadAutomationFile(ctx context.Context, input client.AutomationFileInput) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.ReadAutomationFile(ctx, input)
}

func (api configuredAPI) WriteAutomationFile(ctx context.Context, input client.WriteAutomationFileInput) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.WriteAutomationFile(ctx, input)
}

func (api configuredAPI) ReadAutomationPackage(ctx context.Context) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.ReadAutomationPackage(ctx)
}

func (api configuredAPI) WriteAutomationPackage(ctx context.Context, input client.WriteAutomationPackageInput) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.WriteAutomationPackage(ctx, input)
}

func (api configuredAPI) InstallAutomationPackages(ctx context.Context) (json.RawMessage, error) {
	c, err := api.automationClient()
	if err != nil {
		return nil, err
	}
	return c.InstallAutomationPackages(ctx)
}
