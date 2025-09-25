package virsh

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const ROOTFOLDER = "/var/512SvMan"

func dirQCOW2() string { return filepath.Join(ROOTFOLDER, "qcow2") }
func dirISO() string   { return filepath.Join(ROOTFOLDER, "iso") }
func dirXML() string   { return filepath.Join(ROOTFOLDER, "xml") }

func EnsureDirs() error {
	for _, d := range []string{ROOTFOLDER, dirQCOW2(), dirISO(), dirXML()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return nil
}

// toAbsUnderRoot: keep absolute as-is; relative -> join to defaultDir.
func toAbsUnderRoot(defaultDir, nameOrPath string) string {
	if nameOrPath == "" {
		return defaultDir
	}
	if filepath.IsAbs(nameOrPath) {
		return nameOrPath
	}
	return filepath.Join(defaultDir, nameOrPath)
}

// Resolve disk & ISO paths under ROOTFOLDER if relative
func ResolveDiskPath(p string) string { return toAbsUnderRoot(dirQCOW2(), p) }
func ResolveISOPath(p string) string  { return toAbsUnderRoot(dirISO(), p) }

// qemu-img --output=json minimal struct
type qiInfo struct {
	Format string `json:"format"`
}

// DetectDiskFormat returns "qcow2" or "raw" (or other qemu formats if present).
// If the file doesn't exist, it infers from the extension.
func DetectDiskFormat(path string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		out, err := exec.Command("qemu-img", "info", "--output=json", path).Output()
		if err != nil {
			return "", fmt.Errorf("qemu-img info: %w", err)
		}
		var info qiInfo
		if err := json.Unmarshal(out, &info); err != nil {
			return "", fmt.Errorf("parse qemu-img info json: %w", err)
		}
		if info.Format == "" {
			return "", fmt.Errorf("could not detect disk format for %s", path)
		}
		return strings.ToLower(info.Format), nil
	}

	// Not exists: infer by extension
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".qcow2":
		return "qcow2", nil
	case ".img", ".raw":
		return "raw", nil
	default:
		// default to qcow2 if unknown
		return "qcow2", nil
	}
}

// EnsureDiskAndDetectFormat creates the disk if missing (using detected format)
// and returns the resulting format (e.g. "qcow2" or "raw").
func EnsureDiskAndDetectFormat(path string, sizeGB int) (string, error) {
	fmtStr, err := DetectDiskFormat(path)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(path); err == nil {
		return fmtStr, nil
	}

	if sizeGB <= 0 {
		return "", fmt.Errorf("disk %s does not exist and sizeGB <= 0", path)
	}

	// Create with chosen format
	args := []string{"create", "-f", fmtStr, path, fmt.Sprintf("%dG", sizeGB)}
	cmd := exec.Command("qemu-img", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("qemu-img create: %w", err)
	}
	return fmtStr, nil
}

// WriteDomainXMLToDisk: save vm XML under xml/<vm>.xml
func WriteDomainXMLToDisk(vmName, xml string) (string, error) {
	if err := EnsureDirs(); err != nil {
		return "", err
	}
	out := filepath.Join(dirXML(), fmt.Sprintf("%s.xml", vmName))
	if err := os.WriteFile(out, []byte(strings.TrimSpace(xml)+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("write xml %s: %w", out, err)
	}
	return out, nil
}
