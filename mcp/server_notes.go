package hyperhivemcp

import (
	"context"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
)

func (api configuredAPI) ListNotes(ctx context.Context) ([]client.NoteObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListNotes(ctx)
}

func (api configuredAPI) GetNote(ctx context.Context, input client.NoteIDInput) (client.NoteObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetNote(ctx, input)
}

func (api configuredAPI) CreateNote(ctx context.Context, input client.NoteContent) (client.NoteObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.CreateNote(ctx, input)
}

func (api configuredAPI) ReplaceNote(ctx context.Context, input client.EditNoteInput) (client.NoteObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ReplaceNote(ctx, input)
}

func (api configuredAPI) PatchNote(ctx context.Context, input client.EditNoteInput) (client.NoteObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.PatchNote(ctx, input)
}
