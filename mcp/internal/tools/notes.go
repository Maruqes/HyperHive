package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NotesAPI exposes only reviewed Notes operations.
type NotesAPI interface {
	PatchNote(context.Context, client.EditNoteInput) (client.NoteObject, error)
	ReplaceNote(context.Context, client.EditNoteInput) (client.NoteObject, error)
	CreateNote(context.Context, client.NoteContent) (client.NoteObject, error)
	ListNotes(context.Context) ([]client.NoteObject, error)
	GetNote(context.Context, client.NoteIDInput) (client.NoteObject, error)
}

type notesObjectOutput struct {
	Result client.NoteObject `json:"result"`
}

type notesListOutput struct {
	Result []client.NoteObject `json:"result"`
}

func notesTool(name, method, path, description string) *mcp.Tool {
	destructive := method == "PUT" || method == "PATCH"
	return &mcp.Tool{Name: name, Description: description, Meta: mcp.Meta{"hyperhive/endpoint": map[string]any{"method": method, "path": path}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: method == "GET", DestructiveHint: &destructive, IdempotentHint: method != "POST"}}
}

// addNotesTool keeps integer precision in both result representations.
func addNotesTool[In, Out any](server *mcp.Server, tool *mcp.Tool, handler func(context.Context, In) (Out, error)) {
	schema, err := jsonschema.For[Out](nil)
	if err != nil {
		panic(err)
	}
	tool.OutputSchema = schema
	addExactTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, any, error) {
		var args map[string]json.RawMessage
		// The SDK may supply no raw bytes when arguments are omitted. Its
		// typed validation still rejects omitted inputs for required tools.
		if len(bytes.TrimSpace(req.Params.Arguments)) != 0 {
			if json.Unmarshal(req.Params.Arguments, &args) != nil || args == nil {
				return nil, nil, errors.New("Notes arguments must be a JSON object")
			}
		}
		for _, value := range args {
			if string(value) == "null" {
				return nil, nil, errors.New("Notes arguments must not contain null")
			}
		}
		out, err := handler(ctx, input)
		if err != nil {
			return nil, nil, err
		}
		data, err := json.Marshal(out)
		if err != nil {
			return nil, nil, errors.New("could not encode Notes result")
		}
		return &mcp.CallToolResult{StructuredContent: json.RawMessage(data), Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
	})
}

// RegisterNotes registers the permitted, named Notes operations.
func RegisterNotes(server *mcp.Server, api NotesAPI) {
	addNotesTool(server, notesTool("patch_note", "PATCH", client.NotesPath+"{id}", "Replace both title and body using the API PATCH alias. Calls PATCH /notes/{id}. This is a full replacement, not a partial merge: positive id, nonblank titulo and nota are all required. Overwrites both fields; returns the updated note or HTTP 404. No automatic retries."), func(ctx context.Context, input client.EditNoteInput) (notesObjectOutput, error) {
		if err := client.ValidateEditNote(input); err != nil {
			return notesObjectOutput{}, err
		}
		result, err := api.PatchNote(ctx, input)
		return notesObjectOutput{result}, err
	})
	addNotesTool(server, notesTool("replace_note", "PUT", client.NotesPath+"{id}", "Replace both title and body of an existing note. Calls PUT /notes/{id}. Positive id, nonblank titulo and nota are all required; this overwrites both fields. Returns the updated note, or HTTP 404 if missing. No automatic retries."), func(ctx context.Context, input client.EditNoteInput) (notesObjectOutput, error) {
		if err := client.ValidateEditNote(input); err != nil {
			return notesObjectOutput{}, err
		}
		result, err := api.ReplaceNote(ctx, input)
		return notesObjectOutput{result}, err
	})
	addNotesTool(server, notesTool("create_note", "POST", client.NotesPath, "Create a note with required nonblank titulo and nota. Calls POST /notes/; returns the created note. Values are preserved unchanged. No automatic retries."), func(ctx context.Context, input client.NoteContent) (notesObjectOutput, error) {
		if err := client.ValidateNoteContent(input); err != nil {
			return notesObjectOutput{}, err
		}
		result, err := api.CreateNote(ctx, input)
		return notesObjectOutput{result}, err
	})
	addNotesTool(server, notesTool("get_note", "GET", client.NotesPath+"{id}", "Get a note by positive integer ID. Calls GET /notes/{id}; missing notes return HTTP 404."), func(ctx context.Context, input client.NoteIDInput) (notesObjectOutput, error) {
		if err := client.ValidateNoteID(input.ID); err != nil {
			return notesObjectOutput{}, err
		}
		result, err := api.GetNote(ctx, input)
		return notesObjectOutput{result}, err
	})
	addNotesTool(server, notesTool("list_notes", "GET", client.NotesPath, "List all notes ordered by descending ID. Calls GET /notes/. Takes no arguments."), func(ctx context.Context, _ struct{}) (notesListOutput, error) {
		result, err := api.ListNotes(ctx)
		return notesListOutput{result}, err
	})
}
