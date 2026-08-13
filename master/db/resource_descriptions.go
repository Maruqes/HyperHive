package db

import (
	"context"
)

// CreateResourceDescriptionsTable ensures descriptions can be attached to any resource type.
func CreateResourceDescriptionsTable(ctx context.Context) error {
	const query = `
	CREATE TABLE IF NOT EXISTS resource_descriptions (
		resource_type TEXT NOT NULL,
		resource_id INTEGER NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (resource_type, resource_id)
	);
	`
	_, err := DB.ExecContext(ctx, query)
	return err
}

func SetResourceDescription(ctx context.Context, resourceType string, resourceID int, description string) error {
	const query = `
	INSERT INTO resource_descriptions (resource_type, resource_id, description)
	VALUES (?, ?, ?)
	ON CONFLICT(resource_type, resource_id) DO UPDATE SET description = excluded.description;
	`
	_, err := DB.ExecContext(ctx, query, resourceType, resourceID, description)
	return err
}

func GetResourceDescriptions(ctx context.Context, resourceType string, resourceIDs []int) (map[int]string, error) {
	descriptions := make(map[int]string, len(resourceIDs))
	if len(resourceIDs) == 0 {
		return descriptions, nil
	}

	query := "SELECT resource_id, description FROM resource_descriptions WHERE resource_type = ? AND resource_id IN (" + placeholders(len(resourceIDs)) + ")"
	args := make([]any, 0, len(resourceIDs)+1)
	args = append(args, resourceType)
	for _, id := range resourceIDs {
		args = append(args, id)
	}

	rows, err := DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var description string
		if err := rows.Scan(&id, &description); err != nil {
			return nil, err
		}
		descriptions[id] = description
	}
	return descriptions, rows.Err()
}

func DeleteResourceDescription(ctx context.Context, resourceType string, resourceID int) error {
	_, err := DB.ExecContext(ctx, "DELETE FROM resource_descriptions WHERE resource_type = ? AND resource_id = ?", resourceType, resourceID)
	return err
}

func placeholders(count int) string {
	if count == 1 {
		return "?"
	}
	result := "?"
	for i := 1; i < count; i++ {
		result += ", ?"
	}
	return result
}
