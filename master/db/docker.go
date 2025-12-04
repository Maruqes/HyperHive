package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type DockerRepo struct {
	ID          int
	MachineName string
	Name        string
	FolderToRun string
	EnvVars     map[string]string
}

func CreateDockerRepoTable() error {
	const query = `
	CREATE TABLE IF NOT EXISTS docker_repos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		machine_name TEXT NOT NULL,
		name TEXT NOT NULL,
		folder_to_run TEXT NOT NULL,
		env_vars TEXT NOT NULL DEFAULT '{}',
		UNIQUE(machine_name, name)
	);
	`
	if _, err := DB.Exec(query); err != nil {
		return err
	}

	// Ensure env_vars column exists for older installations.
	if _, err := DB.Exec(`ALTER TABLE docker_repos ADD COLUMN env_vars TEXT NOT NULL DEFAULT '{}'`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return err
		}
	}
	return nil
}

func UpsertDockerRepo(machineName, name, folderToRun string, envVars map[string]string) error {
	env, err := marshalEnvVars(envVars)
	if err != nil {
		return fmt.Errorf("encode env vars: %w", err)
	}

	const query = `
	INSERT INTO docker_repos (machine_name, name, folder_to_run, env_vars)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(machine_name, name) DO UPDATE SET
		folder_to_run = excluded.folder_to_run,
		env_vars = excluded.env_vars;
	`
	_, err = DB.Exec(query, machineName, name, folderToRun, env)
	if err != nil {
		return fmt.Errorf("upsert docker repo: %w", err)
	}
	return nil
}

func GetDockerRepo(machineName, name string) (*DockerRepo, error) {
	const query = `
	SELECT id, machine_name, name, folder_to_run, env_vars
	FROM docker_repos
	WHERE machine_name = ? AND name = ?
	LIMIT 1;
	`
	var (
		repo   DockerRepo
		envRaw sql.NullString
	)
	err := DB.QueryRow(query, machineName, name).Scan(&repo.ID, &repo.MachineName, &repo.Name, &repo.FolderToRun, &envRaw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get docker repo: %w", err)
	}

	if envRaw.Valid {
		repo.EnvVars, err = unmarshalEnvVars(envRaw.String)
		if err != nil {
			return nil, fmt.Errorf("decode env vars: %w", err)
		}
	} else {
		repo.EnvVars = map[string]string{}
	}

	return &repo, nil
}

func DeleteDockerRepo(machineName, name string) error {
	const query = `
	DELETE FROM docker_repos
	WHERE machine_name = ? AND name = ?;
	`
	if _, err := DB.Exec(query, machineName, name); err != nil {
		return fmt.Errorf("delete docker repo: %w", err)
	}
	return nil
}

func marshalEnvVars(env map[string]string) (string, error) {
	if env == nil {
		env = map[string]string{}
	}
	bytes, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func unmarshalEnvVars(raw string) (map[string]string, error) {
	if raw == "" {
		return map[string]string{}, nil
	}
	var env map[string]string
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil, err
	}
	if env == nil {
		env = map[string]string{}
	}
	return env, nil
}
