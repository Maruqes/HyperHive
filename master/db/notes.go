package db

import (
	"context"
	"database/sql"
	"errors"
)

type Note struct {
	ID     int    `json:"id"`
	Titulo string `json:"titulo"`
	Nota   string `json:"nota"`
}

func CreateNotesTable(ctx context.Context) error {
	const query = `
	CREATE TABLE IF NOT EXISTS notes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		titulo TEXT NOT NULL DEFAULT '',
		nota TEXT NOT NULL
	);
	`
	if _, err := DB.ExecContext(ctx, query); err != nil {
		return err
	}
	return ensureNotesTitleColumn(ctx)
}

func ensureNotesTitleColumn(ctx context.Context) error {
	rows, err := DB.QueryContext(ctx, `PRAGMA table_info(notes);`)
	if err != nil {
		return err
	}
	defer rows.Close()

	hasTitle := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == "titulo" {
			hasTitle = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if hasTitle {
		return nil
	}

	_, err = DB.ExecContext(ctx, `ALTER TABLE notes ADD COLUMN titulo TEXT NOT NULL DEFAULT '';`)
	return err
}

func AddNote(ctx context.Context, titulo, nota string) (*Note, error) {
	const query = `
	INSERT INTO notes (titulo, nota)
	VALUES (?, ?)
	RETURNING id, titulo, nota;
	`

	var note Note
	if err := DB.QueryRowContext(ctx, query, titulo, nota).Scan(&note.ID, &note.Titulo, &note.Nota); err != nil {
		return nil, err
	}
	return &note, nil
}

func GetAllNotes(ctx context.Context) ([]Note, error) {
	const query = `
	SELECT id, titulo, nota
	FROM notes
	ORDER BY id DESC;
	`

	rows, err := DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := []Note{}
	for rows.Next() {
		var note Note
		if err := rows.Scan(&note.ID, &note.Titulo, &note.Nota); err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return notes, nil
}

func GetNoteByID(ctx context.Context, id int) (*Note, error) {
	const query = `
	SELECT id, titulo, nota
	FROM notes
	WHERE id = ?;
	`

	var note Note
	err := DB.QueryRowContext(ctx, query, id).Scan(&note.ID, &note.Titulo, &note.Nota)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	return &note, nil
}

func UpdateNoteByID(ctx context.Context, id int, titulo, nota string) (*Note, error) {
	const query = `
	UPDATE notes
	SET titulo = ?, nota = ?
	WHERE id = ?
	RETURNING id, titulo, nota;
	`

	var note Note
	err := DB.QueryRowContext(ctx, query, titulo, nota, id).Scan(&note.ID, &note.Titulo, &note.Nota)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	return &note, nil
}
