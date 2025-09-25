package virsh

import (
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

// Ensure base folders exist.
func EnsureDirs() error {
	for _, d := range []string{ROOTFOLDER, dirQCOW2(), dirISO(), dirXML()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return nil
}

// If p is empty -> default under ROOTFOLDER; if relative -> join to ROOTFOLDER; if absolute -> keep.
func toAbsUnderRoot(defaultDir, nameOrPath, defaultExt string) string {
	if nameOrPath == "" {
		if defaultExt != "" && !strings.HasSuffix(nameOrPath, defaultExt) {
			return filepath.Join(defaultDir, "default"+defaultExt)
		}
		return filepath.Join(defaultDir, "default")
	}
	if filepath.IsAbs(nameOrPath) {
		return nameOrPath
	}
	// treat bare names (e.g., "debian-kde.qcow2") as relative to defaultDir
	if defaultExt != "" && !strings.HasSuffix(nameOrPath, defaultExt) && !strings.Contains(nameOrPath, ".") {
		nameOrPath = nameOrPath + defaultExt
	}
	return filepath.Join(defaultDir, nameOrPath)
}

// Ensure a qcow2 exists (create if missing). sizeGB used only if needs to create.
func EnsureQCOW2(path string, sizeGB int) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if sizeGB <= 0 {
		return fmt.Errorf("qcow2 %s does not exist and sizeGB <= 0", path)
	}
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", path, fmt.Sprintf("%dG", sizeGB))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Write the domain XML to file (for audit/debug) under xml/<name>.xml
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

// Resolve disk & ISO paths: allow empty/relative -> place under ROOTFOLDER
func ResolveDiskPath(diskPath string) string {
	if diskPath == "" {
		return filepath.Join(dirQCOW2(), "default.qcow2")
	}
	if filepath.IsAbs(diskPath) {
		return diskPath
	}
	return filepath.Join(dirQCOW2(), diskPath)
}
func ResolveISOPath(isoPath string) string {
	if isoPath == "" {
		// optional; caller should validate existence later if required
		return filepath.Join(dirISO(), "default.iso")
	}
	if filepath.IsAbs(isoPath) {
		return isoPath
	}
	return filepath.Join(dirISO(), isoPath)
}
