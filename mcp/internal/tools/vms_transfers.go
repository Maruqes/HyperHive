package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/Maruqes/HyperHive/mcp/internal/vmsafety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"path/filepath"
)

func defineExportVM(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMTransfer(gate, store, vmTool("export_vm", "GET", "/virsh/export/{vm_name}", "Export a shut-off VM disk to a new private artifact on the MCP server filesystem. Backend synchronizes NFS mounts. Returns only after the full file is saved, with byte count and SHA-256; requires secure prior VM state and a recorded Medium preflight. No automatic rollback."), func(in client.VMNameInput) error { return client.ValidateVMName(in.VMName) }, func(ctx context.Context, in client.VMNameInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		return in.VMName, snapshots, err
	}, api.ExportVM)
}

func defineDownloadVMBackup(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMTransfer(gate, store, vmTool("download_vm_backup", "GET", "/virsh/downloadbackup/{backup_id}", "Download an existing VM backup disk to a new private artifact on the MCP server filesystem. Returns only after the full file is saved, with byte count and SHA-256. Does not restore or modify any guest."), func(in client.VMIDInput) error { return client.ValidateVMID(in.ID) }, func(ctx context.Context, in client.VMIDInput) (string, map[string]json.RawMessage, error) {
		raw, err := api.ListVMBackups(ctx, client.VMEmptyInput{})
		if err != nil {
			return "", nil, err
		}
		if client.ValidateVMResponse("ListVMBackups", raw) != nil {
			return "", nil, errors.New("invalid backup inventory")
		}
		var records []struct {
			DB struct {
				ID int64 `json:"Id"`
			} `json:"db_res"`
		}
		if json.Unmarshal(raw, &records) != nil {
			return "", nil, errors.New("invalid backup inventory")
		}
		for _, record := range records {
			if record.DB.ID == in.ID {
				return fmt.Sprintf("backup %d", in.ID), map[string]json.RawMessage{"backups": raw}, nil
			}
		}
		return "", nil, errors.New("backup not found")
	}, api.DownloadVMBackup)
}

func addVMTransfer[In any](gate *safety.Gate, store vmsafety.Store, tool *mcp.Tool, validate func(In) error, snapshot func(context.Context, In) (string, map[string]json.RawMessage, error), transfer func(context.Context, In, string) (json.RawMessage, error)) ToolDefinition {
	// Both tools create a retained local artifact; export also synchronizes NFS.
	tool.Annotations.ReadOnlyHint = false
	tool.Annotations.IdempotentHint = false
	tool.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{"result": map[string]any{}, "backup_path": map[string]any{"type": "string"}, "automatic_rollback": map[string]any{"type": "boolean"}}, "required": []string{"result", "backup_path", "automatic_rollback"}}
	return defineToolWithLevel(tool, func(risk safety.Level) mcp.ToolHandlerFor[In, any] {
		return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			if err := vmArguments(req); err != nil {
				return nil, nil, err
			}
			if err := validate(in); err != nil {
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
			result, err := transfer(ctx, in, filepath.Dir(path))
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "VM transfer failed. Retained prior-state record: " + path + ". No automatic rollback. " + err.Error()}}}, nil, nil
			}
			return vmResult(vmWriteOutput{Result: result, BackupPath: path, AutomaticRollback: false})
		}
	})
}
