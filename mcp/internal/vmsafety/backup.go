package vmsafety

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Record contains the actual pre-change responses, not an invented rollback
// command. Snapshots can contain credentials and must never be logged.
type Record struct {
	Version   int                        `json:"version"`
	CreatedAt time.Time                  `json:"created_at"`
	Tool      string                     `json:"tool"`
	Target    string                     `json:"target"`
	Risk      Risk                       `json:"risk"`
	Snapshots map[string]json.RawMessage `json:"snapshots"`
}

// Store writes retained recovery records on the MCP server's filesystem.
// Dir is operator configuration, never a tool argument. Empty selects a
// per-user cache directory; records are never automatically removed.
type Store struct{ Dir string }

func (s Store) Save(ctx context.Context, record Record) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if record.Tool == "" || record.Target == "" || len(record.Snapshots) == 0 {
		return "", errors.New("VM backup requires a tool, target and nonempty snapshots")
	}
	for key, snapshot := range record.Snapshots {
		if key == "" || !json.Valid(snapshot) || bytes.Equal(bytes.TrimSpace(snapshot), []byte("null")) {
			return "", errors.New("VM backup contains an invalid snapshot")
		}
	}
	dir := s.Dir
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", errors.New("VM backup directory is unavailable; no change made")
		}
		dir = filepath.Join(cache, "hyperhive", "mcp", "vm-backups")
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("VM backup directory must be an absolute path")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", errors.New("could not create VM backup directory; no change made")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("VM backup directory must be a private directory (0700), not a symlink; no change made")
	}
	// Random directory names avoid target names becoming paths and prevent
	// one backup from replacing another, including simultaneous invocations.
	batch, err := os.MkdirTemp(dir, "change-")
	if err != nil {
		return "", errors.New("could not reserve VM backup location; no change made")
	}
	record.Version = 1
	record.CreatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", errors.New("could not encode VM backup; no change made")
	}
	path := filepath.Join(batch, "before.json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", errors.New("could not write VM backup; no change made")
	}
	_, writeErr := file.Write(append(data, '\n'))
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return "", errors.New("could not persist VM backup; no change made")
	}
	// Persist the new file and both directory entries before a mutation can run.
	for _, p := range []string{batch, dir} {
		d, err := os.Open(p)
		if err != nil {
			return "", errors.New("could not persist VM backup directory; no change made")
		}
		syncErr := d.Sync()
		closeErr := d.Close()
		if syncErr != nil || closeErr != nil {
			return "", errors.New("could not persist VM backup directory; no change made")
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return path, nil
}
