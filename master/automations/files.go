// Package automations runs HH Automation scripts on the master host and manages
// their files, the shared npm workspace and the generated SDK. Config and
// trigger state live in HH Support (see docs/automations.md).
package automations

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxFileSize bounds one script file read or written through the API.
	MaxFileSize = 1 << 20
	maxFiles    = 500
)

var (
	ErrNotFound     = errors.New("not found")
	ErrInvalidPath  = errors.New("invalid file path")
	idPattern       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	defaultPackage  = "{\n  \"name\": \"hh-automations\",\n  \"private\": true,\n  \"type\": \"module\",\n  \"dependencies\": {}\n}\n"
	starterMainFile = `// HH Automation entry point. It runs on the master host as root.
// Globals: hh (HyperHive API client), ctx (run context) and log.
export default async function ({ ctx, hh, log }: HHAutomationArgs) {
  log.info(` + "`run ${ctx.runId} started by ${ctx.trigger}`" + `, ctx.snapshot);
  // const vms = await hh.api.virsh.getGetallvms();
}
`
)

// Dir is the automation workspace: shared package.json/node_modules, .hh runtime
// files and scripts/<id>/.
func Dir() string {
	if dir := strings.TrimSpace(os.Getenv("HH_AUTOMATIONS_DIR")); dir != "" {
		return dir
	}
	return "/opt/hyperhive/automations"
}

func runtimeDir() string         { return filepath.Join(Dir(), ".hh") }
func scriptsDir() string         { return filepath.Join(Dir(), "scripts") }
func packagePath() string        { return filepath.Join(Dir(), "package.json") }
func ScriptDir(id string) string { return filepath.Join(scriptsDir(), id) }

// ValidID rejects automation IDs that are not Support tokens.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// EnsureWorkspace creates the private workspace and the default package.json.
func EnsureWorkspace() error {
	for _, dir := range []string{Dir(), runtimeDir(), scriptsDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create automation directory %s: %w", dir, err)
		}
	}
	if _, err := os.Stat(packagePath()); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(packagePath(), []byte(defaultPackage), 0o600); err != nil {
			return fmt.Errorf("write default package.json: %w", err)
		}
	}
	return nil
}

// CreateStarter writes main.ts for a new automation unless it already has files.
func CreateStarter(id string) error {
	if !ValidID(id) {
		return ErrInvalidPath
	}
	if err := EnsureWorkspace(); err != nil {
		return err
	}
	dir := ScriptDir(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create script directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(dir, "main.ts"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("create main.ts: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(starterMainFile); err != nil {
		return fmt.Errorf("write main.ts: %w", err)
	}
	return nil
}

// EntryFile returns the script entry: main.ts, else main.js, else main.mjs.
func EntryFile(id string) (string, error) {
	for _, name := range []string{"main.ts", "main.js", "main.mjs"} {
		candidate := filepath.Join(ScriptDir(id), name)
		if info, err := os.Lstat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("automation has no main.ts, main.js or main.mjs")
}

type FileInfo struct {
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

// cleanPath validates a slash-separated path relative to a script directory.
func cleanPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	cleaned := path.Clean(name)
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || cleaned != name || cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." || len(cleaned) > 512 {
		return "", ErrInvalidPath
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "node_modules" || strings.HasPrefix(part, ".") {
			return "", ErrInvalidPath
		}
	}
	return cleaned, nil
}

// openScriptRoot confines file access to the script directory (no symlink escapes).
func openScriptRoot(id string, create bool) (*os.Root, error) {
	if !ValidID(id) {
		return nil, ErrInvalidPath
	}
	if create {
		if err := os.MkdirAll(ScriptDir(id), 0o700); err != nil {
			return nil, fmt.Errorf("create script directory: %w", err)
		}
	}
	root, err := os.OpenRoot(ScriptDir(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return root, err
}

func ListFiles(id string) ([]FileInfo, error) {
	root, err := openScriptRoot(id, false)
	if errors.Is(err, ErrNotFound) {
		return []FileInfo{}, nil
	} else if err != nil {
		return nil, err
	}
	defer root.Close()
	files := []FileInfo{}
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name != "." && (entry.Name() == "node_modules" || strings.HasPrefix(entry.Name(), ".")) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if len(files) >= maxFiles {
			return fs.SkipAll
		}
		files = append(files, FileInfo{Path: name, Size: info.Size(), ModifiedAt: info.ModTime().UTC()})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, err
}

func ReadFile(id, name string) (string, error) {
	name, err := cleanPath(name)
	if err != nil {
		return "", err
	}
	root, err := openScriptRoot(id, false)
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotFound
	} else if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", ErrInvalidPath
	}
	if info.Size() > MaxFileSize {
		return "", fmt.Errorf("file is larger than %d bytes", MaxFileSize)
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("file is not UTF-8 text")
	}
	return string(data), nil
}

// WriteFile replaces a script file atomically, creating parent directories.
func WriteFile(id, name, content string) (FileInfo, error) {
	name, err := cleanPath(name)
	if err != nil {
		return FileInfo{}, err
	}
	if len(content) > MaxFileSize || !utf8.ValidString(content) {
		return FileInfo{}, fmt.Errorf("content must be UTF-8 text up to %d bytes", MaxFileSize)
	}
	root, err := openScriptRoot(id, true)
	if err != nil {
		return FileInfo{}, err
	}
	defer root.Close()
	if dir := path.Dir(name); dir != "." {
		if err := root.MkdirAll(dir, 0o700); err != nil {
			return FileInfo{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return FileInfo{}, ErrInvalidPath
	}
	temp := path.Join(path.Dir(name), fmt.Sprintf(".%s.%d.tmp", path.Base(name), time.Now().UnixNano()))
	if err := root.WriteFile(temp, []byte(content), 0o600); err != nil {
		return FileInfo{}, fmt.Errorf("write %s: %w", name, err)
	}
	if err := root.Rename(temp, name); err != nil {
		_ = root.Remove(temp)
		return FileInfo{}, fmt.Errorf("replace %s: %w", name, err)
	}
	info, err := root.Stat(name)
	if err != nil {
		return FileInfo{}, err
	}
	return FileInfo{Path: name, Size: info.Size(), ModifiedAt: info.ModTime().UTC()}, nil
}

func DeleteFile(id, name string) error {
	name, err := cleanPath(name)
	if err != nil {
		return err
	}
	root, err := openScriptRoot(id, false)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if info.IsDir() {
		return ErrInvalidPath
	}
	return root.Remove(name)
}

// RemoveScripts deletes an automation's whole script directory.
func RemoveScripts(id string) error {
	if !ValidID(id) {
		return ErrInvalidPath
	}
	return os.RemoveAll(ScriptDir(id))
}

func ReadPackage() (string, error) {
	if err := EnsureWorkspace(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(packagePath())
	return string(data), err
}

// WritePackage replaces the shared package.json; it must be a JSON object.
func WritePackage(content string) error {
	if len(content) > MaxFileSize || !isJSONObject(content) {
		return fmt.Errorf("package.json must be a JSON object up to %d bytes", MaxFileSize)
	}
	if err := EnsureWorkspace(); err != nil {
		return err
	}
	temp := packagePath() + ".tmp"
	if err := os.WriteFile(temp, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write package.json: %w", err)
	}
	return os.Rename(temp, packagePath())
}

func isJSONObject(content string) bool {
	var value map[string]any
	return json.Unmarshal([]byte(content), &value) == nil && value != nil
}
