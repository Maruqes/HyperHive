package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
)

const NotesPath = "/notes/"

// NoteObject preserves API fields and exact JSON numbers, including future fields.
type NoteObject map[string]any

func validateNoteObject(note NoteObject) error {
	if note == nil {
		return errors.New("HyperHive API returned an invalid note")
	}
	id, ok := note["id"].(json.Number)
	if !ok {
		return errors.New("HyperHive API returned an invalid note ID")
	}
	n, err := id.Int64()
	if err != nil || n <= 0 {
		return errors.New("HyperHive API returned an invalid note ID")
	}
	if _, ok := note["titulo"].(string); !ok {
		return errors.New("HyperHive API returned an invalid note title")
	}
	if _, ok := note["nota"].(string); !ok {
		return errors.New("HyperHive API returned an invalid note body")
	}
	return nil
}
func decodeNotesJSON(raw []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(dst) != nil {
		return errors.New("HyperHive API returned invalid Notes JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("HyperHive API returned trailing Notes data")
	}
	return nil
}
func (c *Client) ListNotes(ctx context.Context) ([]NoteObject, error) {
	var raw json.RawMessage
	if err := c.getJSON(ctx, NotesPath, &raw); err != nil {
		return nil, err
	}
	var notes []NoteObject
	if err := decodeNotesJSON(raw, &notes); err != nil {
		return nil, err
	}
	if notes == nil {
		return nil, errors.New("HyperHive API returned an invalid Notes array")
	}
	for _, note := range notes {
		if err := validateNoteObject(note); err != nil {
			return nil, err
		}
	}
	return notes, nil
}

// NoteIDInput selects an existing note by its positive database integer ID.
type NoteIDInput struct {
	ID int64 `json:"id" jsonschema:"Positive note ID"`
}

func ValidateNoteID(id int64) error {
	if id <= 0 {
		return errors.New("id must be a positive integer")
	}
	return nil
}
func (c *Client) noteRequest(ctx context.Context, method, path string, body any) (NoteObject, error) {
	var raw json.RawMessage
	if err := c.request(ctx, method, path, body, &raw); err != nil {
		return nil, err
	}
	var note NoteObject
	if err := decodeNotesJSON(raw, &note); err != nil {
		return nil, err
	}
	if err := validateNoteObject(note); err != nil {
		return nil, err
	}
	return note, nil
}
func (c *Client) GetNote(ctx context.Context, input NoteIDInput) (NoteObject, error) {
	if err := ValidateNoteID(input.ID); err != nil {
		return nil, err
	}
	return c.noteRequest(ctx, "GET", NotesPath+strconv.FormatInt(input.ID, 10), nil)
}

// NoteContent requires both complete fields; whitespace is preserved on the wire.
type NoteContent struct {
	Titulo string `json:"titulo" jsonschema:"Required nonblank note title, preserved unchanged"`
	Nota   string `json:"nota" jsonschema:"Required nonblank note body, preserved unchanged"`
}

func ValidateNoteContent(input NoteContent) error {
	if strings.TrimSpace(input.Titulo) == "" {
		return errors.New("titulo must not be blank")
	}
	if strings.TrimSpace(input.Nota) == "" {
		return errors.New("nota must not be blank")
	}
	return nil
}
func (c *Client) CreateNote(ctx context.Context, input NoteContent) (NoteObject, error) {
	if err := ValidateNoteContent(input); err != nil {
		return nil, err
	}
	return c.noteRequest(ctx, "POST", NotesPath, input)
}

// EditNoteInput replaces both title and body, including for the PATCH alias.
type EditNoteInput struct {
	ID     int64  `json:"id" jsonschema:"Positive note ID"`
	Titulo string `json:"titulo" jsonschema:"Required nonblank replacement title"`
	Nota   string `json:"nota" jsonschema:"Required nonblank replacement body"`
}

func ValidateEditNote(input EditNoteInput) error {
	if err := ValidateNoteID(input.ID); err != nil {
		return err
	}
	return ValidateNoteContent(NoteContent{Titulo: input.Titulo, Nota: input.Nota})
}
func (c *Client) ReplaceNote(ctx context.Context, input EditNoteInput) (NoteObject, error) {
	if err := ValidateEditNote(input); err != nil {
		return nil, err
	}
	return c.noteRequest(ctx, "PUT", NotesPath+strconv.FormatInt(input.ID, 10), NoteContent{Titulo: input.Titulo, Nota: input.Nota})
}

// PatchNote uses the API PATCH alias, which requires full replacement content.
func (c *Client) PatchNote(ctx context.Context, input EditNoteInput) (NoteObject, error) {
	if err := ValidateEditNote(input); err != nil {
		return nil, err
	}
	return c.noteRequest(ctx, "PATCH", NotesPath+strconv.FormatInt(input.ID, 10), NoteContent{Titulo: input.Titulo, Nota: input.Nota})
}
