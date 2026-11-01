package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/Maruqes/HyperHive/mcp/internal/vmsafety"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AutomationsAPI covers HH Automations. Scripts run as root on the master
// host, so writes that can start or change code require human approval.
// Deleting automations, files or runs stays in the HyperHive UI.
type AutomationsAPI interface {
	ListAutomations(context.Context) (json.RawMessage, error)
	GetAutomation(context.Context, string) (json.RawMessage, error)
	CreateAutomation(context.Context, client.CreateAutomationInput) (json.RawMessage, error)
	ReplaceAutomation(context.Context, string, client.AutomationConfig) (json.RawMessage, error)
	EvaluateAutomationCondition(context.Context, client.EvaluateConditionInput) (json.RawMessage, error)
	RunAutomation(context.Context, string) (json.RawMessage, error)
	ListAutomationRuns(context.Context, client.AutomationRunsInput) (json.RawMessage, error)
	GetAutomationRun(context.Context, string) (json.RawMessage, error)
	ListAutomationFiles(context.Context, string) (json.RawMessage, error)
	ReadAutomationFile(context.Context, client.AutomationFileInput) (json.RawMessage, error)
	WriteAutomationFile(context.Context, client.WriteAutomationFileInput) (json.RawMessage, error)
	ReadAutomationPackage(context.Context) (json.RawMessage, error)
	WriteAutomationPackage(context.Context, client.WriteAutomationPackageInput) (json.RawMessage, error)
	InstallAutomationPackages(context.Context) (json.RawMessage, error)
}

type automationOutput struct {
	Result any    `json:"result"`
	Backup string `json:"backup,omitempty" jsonschema:"private MCP-server path of the pre-change record"`
}

func automationTool(name, method, path, description string) *mcp.Tool {
	readOnly := method == "GET" || name == "evaluate_automation_condition"
	destructive := method == "PUT"
	schema, err := jsonschema.For[automationOutput](nil)
	if err != nil {
		panic(err)
	}
	return &mcp.Tool{Name: name, Description: description, OutputSchema: schema, Meta: mcp.Meta{"hyperhive/endpoint": map[string]any{"method": method, "path": path}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: &destructive, IdempotentHint: method == "GET" || method == "PUT"}}
}

// addAutomationTool returns the API JSON unchanged in both result forms.
// Inputs are validated by their Validate methods before approval and dispatch.
func addAutomationTool[In any](tool *mcp.Tool, handler func(context.Context, In) (json.RawMessage, error)) ToolDefinition {
	return defineTool(tool, func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, any, error) {
		raw, err := handler(ctx, input)
		return automationResult(raw, "", err)
	})
}

// saveAutomationBackup persists the actual pre-change state before a write.
func saveAutomationBackup(ctx context.Context, store vmsafety.Store, tool, target string, risk safety.Level, snapshot json.RawMessage) (string, error) {
	path, err := store.Save(ctx, vmsafety.Record{Tool: tool, Target: target, Risk: vmsafety.Risk(risk), Snapshots: map[string]json.RawMessage{"before": snapshot}})
	if err != nil {
		return "", errors.New("could not save the pre-change record; no change made")
	}
	return path, nil
}

func defineListAutomations(api AutomationsAPI) ToolDefinition {
	return addAutomationTool(automationTool("list_automations", "GET", client.AutomationsPath+"/", "List HH Automations with their Support config (condition, enabled, cooldown/for/timeout, hourly cap), live trigger state (condition value, last error, armed, last fired, disabled reason, value snapshot), running run ID, last run, global pause and Node availability. Calls GET /automations. Takes no arguments."), func(ctx context.Context, _ struct{}) (json.RawMessage, error) {
		return api.ListAutomations(ctx)
	})
}

func defineGetAutomation(api AutomationsAPI) ToolDefinition {
	return addAutomationTool(automationTool("get_automation", "GET", client.AutomationsPath+"/{id}", "Get one HH Automation's config, trigger state and last run. Calls GET /automations/{id}."), func(ctx context.Context, in client.AutomationIDInput) (json.RawMessage, error) {
		return api.GetAutomation(ctx, in.ID)
	})
}

func defineCreateAutomation(api AutomationsAPI) ToolDefinition {
	return addAutomationTool(automationTool("create_automation", "POST", client.AutomationsPath+"/", "Create an HH Automation. It always starts disabled with a starter main.ts; nothing runs until it is enabled (update_automation) or run (run_automation). Calls POST /automations. Condition syntax: app('name').stream >= 95, app('name','instance').health == 'degraded', app('name')['disk.used'], connectivity 'online'|'offline'. Returns the created automation. No automatic retries."), func(ctx context.Context, in client.CreateAutomationInput) (json.RawMessage, error) {
		return api.CreateAutomation(ctx, in)
	})
}

func defineUpdateAutomation(api AutomationsAPI, store vmsafety.Store) ToolDefinition {
	return defineToolWithLevel(automationTool("update_automation", "PUT", client.AutomationsPath+"/{id}", "Change an HH Automation's config; only the fields given change (the tool reads the current config and sends a full PUT /automations/{id}). Enabling it lets its condition start root runs on the master host. Saves the previous config privately on the MCP server first and returns that path. No automatic retries."), func(level safety.Level) mcp.ToolHandlerFor[client.UpdateAutomationInput, any] {
		return func(ctx context.Context, _ *mcp.CallToolRequest, in client.UpdateAutomationInput) (*mcp.CallToolResult, any, error) {
			current, err := api.GetAutomation(ctx, in.ID)
			if err != nil {
				return nil, nil, err
			}
			config, err := client.MergeAutomation(current, in)
			if err != nil {
				return nil, nil, err
			}
			backup, err := saveAutomationBackup(ctx, store, "update_automation", "automation:"+in.ID, level, current)
			if err != nil {
				return nil, nil, err
			}
			result, err := api.ReplaceAutomation(ctx, in.ID, config)
			return automationResult(result, backup, err)
		}
	})
}

func automationResult(raw json.RawMessage, backup string, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		if backup != "" {
			return nil, nil, errors.New(err.Error() + "; pre-change record: " + backup)
		}
		return nil, nil, err
	}
	data, err := json.Marshal(automationOutput{Result: raw, Backup: backup})
	if err != nil {
		return nil, nil, errors.New("could not encode automation result")
	}
	return &mcp.CallToolResult{StructuredContent: json.RawMessage(data), Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
}

func defineEvaluateAutomationCondition(api AutomationsAPI) ToolDefinition {
	return addAutomationTool(automationTool("evaluate_automation_condition", "POST", client.AutomationsPath+"/evaluate", "Evaluate a Condition to run against current HH Support data without firing or running anything. Calls POST /automations/evaluate. Returns value (errors evaluate to false), error and the snapshot of values read."), func(ctx context.Context, in client.EvaluateConditionInput) (json.RawMessage, error) {
		return api.EvaluateAutomationCondition(ctx, in)
	})
}

func defineRunAutomation(api AutomationsAPI) ToolDefinition {
	return addAutomationTool(automationTool("run_automation", "POST", client.AutomationsPath+"/{id}/run", "Start a manual run of an HH Automation: its script runs as root on the master host with its configured timeout. Calls POST /automations/{id}/run and returns run_id immediately (HTTP 409 when it is already running). Read progress with get_automation_run. No automatic retries."), func(ctx context.Context, in client.AutomationIDInput) (json.RawMessage, error) {
		return api.RunAutomation(ctx, in.ID)
	})
}

func defineListAutomationRuns(api AutomationsAPI) ToolDefinition {
	return addAutomationTool(automationTool("list_automation_runs", "GET", client.AutomationsPath+"/runs", "List HH Automation runs newest first (trigger, status running|succeeded|failed|timeout|killed|interrupted, exit code, times, snapshot), without output. Calls GET /automations/runs."), func(ctx context.Context, in client.AutomationRunsInput) (json.RawMessage, error) {
		return api.ListAutomationRuns(ctx, in)
	})
}

func defineGetAutomationRun(api AutomationsAPI) ToolDefinition {
	return addAutomationTool(automationTool("get_automation_run", "GET", client.AutomationsPath+"/runs/{run_id}", "Get one HH Automation run with its output (stdout+stderr, up to 1 MiB; written when the run ends). Calls GET /automations/runs/{run_id}."), func(ctx context.Context, in client.AutomationRunInput) (json.RawMessage, error) {
		return api.GetAutomationRun(ctx, in.RunID)
	})
}

func defineListAutomationFiles(api AutomationsAPI) ToolDefinition {
	return addAutomationTool(automationTool("list_automation_files", "GET", client.AutomationsPath+"/{id}/files", "List an HH Automation's script files (path, size, modified time). The entry is main.ts, else main.js, else main.mjs. Calls GET /automations/{id}/files."), func(ctx context.Context, in client.AutomationIDInput) (json.RawMessage, error) {
		return api.ListAutomationFiles(ctx, in.ID)
	})
}

func defineReadAutomationFile(api AutomationsAPI) ToolDefinition {
	return addAutomationTool(automationTool("read_automation_file", "GET", client.AutomationsPath+"/{id}/files/content", "Read one HH Automation script file. Calls GET /automations/{id}/files/content?path=."), func(ctx context.Context, in client.AutomationFileInput) (json.RawMessage, error) {
		return api.ReadAutomationFile(ctx, in)
	})
}

func defineWriteAutomationFile(api AutomationsAPI, store vmsafety.Store) ToolDefinition {
	return defineToolWithLevel(automationTool("write_automation_file", "PUT", client.AutomationsPath+"/{id}/files/content", "Create or replace one HH Automation script file with the complete content given. The script runs as root on the master host; globals hh (master REST client: hh.api.<area>.<fn>() or hh.request(method, path, {query, body})), ctx and log are available, and `export default async function ({ ctx, hh, log })` is awaited. Saves the previous content privately on the MCP server first and returns that path. Calls PUT /automations/{id}/files/content?path=. No automatic retries."), func(level safety.Level) mcp.ToolHandlerFor[client.WriteAutomationFileInput, any] {
		return func(ctx context.Context, _ *mcp.CallToolRequest, in client.WriteAutomationFileInput) (*mcp.CallToolResult, any, error) {
			previous, err := api.ReadAutomationFile(ctx, client.AutomationFileInput{ID: in.ID, Path: in.Path})
			if err != nil {
				if !strings.Contains(err.Error(), "HTTP 404") {
					return nil, nil, err
				}
				previous = json.RawMessage(`{"exists":false}`)
			}
			backup, err := saveAutomationBackup(ctx, store, "write_automation_file", "automation:"+in.ID+":"+in.Path, level, previous)
			if err != nil {
				return nil, nil, err
			}
			result, err := api.WriteAutomationFile(ctx, in)
			return automationResult(result, backup, err)
		}
	})
}

func defineReadAutomationPackage(api AutomationsAPI) ToolDefinition {
	return addAutomationTool(automationTool("read_automation_package", "GET", client.AutomationsPath+"/package", "Read the package.json shared by all HH Automations. Calls GET /automations/package. Takes no arguments."), func(ctx context.Context, _ struct{}) (json.RawMessage, error) {
		return api.ReadAutomationPackage(ctx)
	})
}

func defineWriteAutomationPackage(api AutomationsAPI, store vmsafety.Store) ToolDefinition {
	return defineToolWithLevel(automationTool("write_automation_package", "PUT", client.AutomationsPath+"/package", "Replace the package.json shared by all HH Automations (a JSON object); changing dependencies affects every automation after the next install. Saves the previous content privately on the MCP server first. Calls PUT /automations/package. No automatic retries."), func(level safety.Level) mcp.ToolHandlerFor[client.WriteAutomationPackageInput, any] {
		return func(ctx context.Context, _ *mcp.CallToolRequest, in client.WriteAutomationPackageInput) (*mcp.CallToolResult, any, error) {
			previous, err := api.ReadAutomationPackage(ctx)
			if err != nil {
				return nil, nil, err
			}
			backup, err := saveAutomationBackup(ctx, store, "write_automation_package", "automation-package", level, previous)
			if err != nil {
				return nil, nil, err
			}
			result, err := api.WriteAutomationPackage(ctx, in)
			return automationResult(result, backup, err)
		}
	})
}

func defineInstallAutomationPackages(api AutomationsAPI) ToolDefinition {
	return addAutomationTool(automationTool("install_automation_packages", "POST", client.AutomationsPath+"/package/install", "Run npm install as root in the shared HH Automations workspace (installs Node.js and npm with dnf first when missing). Calls POST /automations/package/install and returns run_id immediately; read the log with get_automation_run. No automatic retries."), func(ctx context.Context, _ client.InstallAutomationPackagesInput) (json.RawMessage, error) {
		return api.InstallAutomationPackages(ctx)
	})
}

// RegisterAutomations is the complete tool and safety catalog for this area.
// There are deliberately no tools that delete automations, files or runs.
func RegisterAutomations(server *Registry, api AutomationsAPI, backupDir string) {
	server = server.Area("automations")
	store := vmsafety.Store{Dir: backupDir}
	RegisterTool(server, "create_automation", defineCreateAutomation(api), safety.Medium)
	RegisterTool(server, "evaluate_automation_condition", defineEvaluateAutomationCondition(api), safety.Low)
	RegisterTool(server, "get_automation", defineGetAutomation(api), safety.Low)
	RegisterTool(server, "get_automation_run", defineGetAutomationRun(api), safety.Low)
	RegisterTool(server, "install_automation_packages", defineInstallAutomationPackages(api), safety.High)
	RegisterTool(server, "list_automation_files", defineListAutomationFiles(api), safety.Low)
	RegisterTool(server, "list_automation_runs", defineListAutomationRuns(api), safety.Low)
	RegisterTool(server, "list_automations", defineListAutomations(api), safety.Low)
	RegisterTool(server, "read_automation_file", defineReadAutomationFile(api), safety.Low)
	RegisterTool(server, "read_automation_package", defineReadAutomationPackage(api), safety.Low)
	RegisterTool(server, "run_automation", defineRunAutomation(api), safety.VeryHigh)
	RegisterTool(server, "update_automation", defineUpdateAutomation(api, store), safety.High)
	RegisterTool(server, "write_automation_file", defineWriteAutomationFile(api, store), safety.High)
	RegisterTool(server, "write_automation_package", defineWriteAutomationPackage(api, store), safety.High)
}
