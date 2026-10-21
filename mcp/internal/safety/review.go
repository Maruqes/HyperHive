package safety

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Summarize binds the review to all exact arguments while displaying only
// bounded target identifiers and field sizes, never arbitrary configuration.
func Summarize(raw json.RawMessage) (summary, target, digest string, err error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var args map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&args) != nil || args == nil {
		return "", "", "", errors.New("invalid MCP argument object")
	}
	canonical, err := json.Marshal(args)
	if err != nil {
		return "", "", "", errors.New("invalid MCP arguments")
	}
	hash := sha256.Sum256(canonical)
	digest = hex.EncodeToString(hash[:])
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var fields, targets []string
	for _, key := range keys {
		value, _ := json.Marshal(args[key])
		display := fmt.Sprintf("%s=[redacted; %d bytes]", key, len(value))
		switch args[key].(type) {
		case json.Number, bool:
			display = key + "=" + string(value)
		}
		switch key {
		case "id", "vm_name", "machine_name", "nfs_share_id", "nfs_id", "backup_id", "port", "alias", "ip", "domain", "publicDomain", "privateDomain", "new_name", "name", "servers", "slavesnames", "domain_names":
			if len(value) <= 256 {
				display = key + "=" + string(value)
				targets = append(targets, display)
			}
		}
		fields = append(fields, display)
	}
	if len(targets) == 0 {
		targets = []string{"new resource or operation-scoped target; see exact argument digest"}
	}
	return strings.Join(fields, "; "), strings.Join(targets, "; "), digest, nil
}

type Review struct {
	Version         int       `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
	Tool            string    `json:"tool"`
	Area            string    `json:"area"`
	Level           Level     `json:"level"`
	Target          string    `json:"target"`
	ArgumentsSHA256 string    `json:"arguments_sha256"`
	Arguments       string    `json:"redacted_arguments"`
	ExpectedImpact  string    `json:"expected_impact"`
	Checks          []string  `json:"validated_checks"`
	Outcome         string    `json:"outcome"`
}

// ReviewStore never deletes VM recovery artifacts. Review capacity is bounded;
// an operator archives records outside this directory before admitting more.
type ReviewStore struct {
	Dir   string
	Limit int
	mu    sync.Mutex
}

func (s *ReviewStore) Save(ctx context.Context, record Review) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if record.Level != Medium || record.Tool == "" || record.Target == "" || len(record.ArgumentsSHA256) != 64 || record.ExpectedImpact == "" || len(record.Checks) == 0 {
		return "", errors.New("incomplete MCP preflight; no operation dispatched")
	}
	dir := s.Dir
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", errors.New("MCP review directory unavailable")
		}
		dir = filepath.Join(cache, "hyperhive", "mcp", "reviews")
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("MCP review directory must be absolute")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", errors.New("could not create MCP review directory; no operation dispatched")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("MCP review directory must be private (0700) and not a symlink")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", errors.New("could not inspect MCP review capacity")
	}
	limit := s.Limit
	if limit <= 0 {
		limit = 10000
	}
	if len(entries) >= limit {
		return "", errors.New("MCP review capacity reached; archive review records before retrying; no operation dispatched")
	}
	record.Version = 1
	record.CreatedAt = time.Now().UTC()
	record.Outcome = "preflight_passed"
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil || len(data) > 65536 {
		return "", errors.New("MCP review exceeds record limits")
	}
	f, err := os.CreateTemp(dir, "review-*.json")
	if err != nil {
		return "", errors.New("could not create private MCP review")
	}
	path := f.Name()
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return "", errors.New("could not persist MCP review; no operation dispatched")
	}
	d, err := os.Open(dir)
	if err != nil {
		return "", errors.New("could not persist MCP review directory")
	}
	syncErr := d.Sync()
	closeErr = d.Close()
	if syncErr != nil || closeErr != nil {
		return "", errors.New("could not persist MCP review directory")
	}
	return path, ctx.Err()
}

// Complete is best effort: the already-persisted preflight remains authoritative
// if a crash or I/O failure prevents recording the final backend outcome.
func (s *ReviewStore) Complete(path, outcome string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var record Review
	if json.Unmarshal(data, &record) != nil {
		return
	}
	record.Outcome = outcome
	data, err = json.MarshalIndent(record, "", "  ")
	if err != nil {
		return
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".outcome-*")
	if err != nil {
		return
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return
	}
	if os.Rename(f.Name(), path) != nil {
		return
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		d.Sync()
		d.Close()
	}
}
