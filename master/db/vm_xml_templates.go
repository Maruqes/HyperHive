package db

import (
	"context"
	"database/sql"
)

type VMXMLTemplate struct {
	Id          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	XML         string `json:"xml"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func CreateVMXMLTemplatesTable(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS vm_xml_templates (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		description TEXT NOT NULL DEFAULT '',
		xml TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := DB.ExecContext(ctx, query)
	return err
}

func AddVMXMLTemplate(ctx context.Context, name, description, xmlContent string) (int64, error) {
	query := `
	INSERT INTO vm_xml_templates (name, description, xml)
	VALUES (?, ?, ?);
	`
	res, err := DB.ExecContext(ctx, query, name, description, xmlContent)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func UpdateVMXMLTemplate(ctx context.Context, id int, name, description, xmlContent string) error {
	query := `
	UPDATE vm_xml_templates
	SET name = ?, description = ?, xml = ?, updated_at = CURRENT_TIMESTAMP
	WHERE id = ?;
	`
	_, err := DB.ExecContext(ctx, query, name, description, xmlContent, id)
	return err
}

func DeleteVMXMLTemplate(ctx context.Context, id int) error {
	query := `
	DELETE FROM vm_xml_templates
	WHERE id = ?;
	`
	_, err := DB.ExecContext(ctx, query, id)
	return err
}

func GetVMXMLTemplateByID(ctx context.Context, id int) (*VMXMLTemplate, error) {
	const query = `
	SELECT id, name, description, xml, created_at, updated_at
	FROM vm_xml_templates
	WHERE id = ?;
	`
	var tmpl VMXMLTemplate
	err := DB.QueryRowContext(ctx, query, id).Scan(
		&tmpl.Id,
		&tmpl.Name,
		&tmpl.Description,
		&tmpl.XML,
		&tmpl.CreatedAt,
		&tmpl.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &tmpl, nil
}

func GetAllVMXMLTemplates(ctx context.Context) ([]VMXMLTemplate, error) {
	const query = `
	SELECT id, name, description, xml, created_at, updated_at
	FROM vm_xml_templates
	ORDER BY name ASC, id ASC;
	`
	rows, err := DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []VMXMLTemplate
	for rows.Next() {
		var tmpl VMXMLTemplate
		if err := rows.Scan(
			&tmpl.Id,
			&tmpl.Name,
			&tmpl.Description,
			&tmpl.XML,
			&tmpl.CreatedAt,
			&tmpl.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, tmpl)
	}
	return out, rows.Err()
}
