package vmdisk

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
)

type VMDisk struct {
	DiskPath      string
	FolderPath    string
	Format        string
	SizeGB        int64
	OccupiedBytes int64
	OccupiedGB    float64
}

type qemuImgInfo struct {
	Format      string `json:"format"`
	VirtualSize int64  `json:"virtual-size"`
	ActualSize  int64  `json:"actual-size"`
}

const bytesInGB = int64(1024 * 1024 * 1024)

var vmDiskNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func CreateVMDisk(basePath, name string, sizeGB int64, format string) (*VMDisk, error) {
	basePath = strings.TrimSpace(basePath)
	name = strings.TrimSpace(name)

	if err := validateVMDiskName(name); err != nil {
		return nil, err
	}
	if sizeGB <= 0 {
		return nil, fmt.Errorf("sizeGB must be greater than zero")
	}

	qemuFormat, extension, err := normalizeVMDiskFormat(format)
	if err != nil {
		return nil, err
	}
	if err := requireQemuImg(); err != nil {
		return nil, err
	}

	cleanBase, err := cleanExistingBasePath(basePath)
	if err != nil {
		return nil, err
	}

	folderPath := filepath.Join(cleanBase, "vm_disk_"+name)
	if err := ensurePathInsideBase(cleanBase, folderPath); err != nil {
		return nil, err
	}
	if _, err := os.Stat(folderPath); err == nil {
		return nil, fmt.Errorf("vm disk folder already exists: %s", folderPath)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat vm disk folder %s: %w", folderPath, err)
	}

	if err := os.Mkdir(folderPath, 0o777); err != nil {
		return nil, fmt.Errorf("create vm disk folder %s: %w", folderPath, err)
	}

	diskPath := filepath.Join(folderPath, name+extension)
	if err := ensurePathInsideBase(cleanBase, diskPath); err != nil {
		cleanupVMDiskCreate(folderPath, diskPath)
		return nil, err
	}

	cmd := exec.Command("qemu-img", "create", "-f", qemuFormat, diskPath, fmt.Sprintf("%dG", sizeGB))
	out, err := cmd.CombinedOutput()
	if err != nil {
		cleanupVMDiskCreate(folderPath, diskPath)
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return nil, fmt.Errorf("qemu-img create %s: %s", diskPath, msg)
		}
		return nil, fmt.Errorf("qemu-img create %s: %w", diskPath, err)
	}

	if err := os.Chmod(diskPath, 0o777); err != nil {
		cleanupVMDiskCreate(folderPath, diskPath)
		return nil, fmt.Errorf("chmod vm disk %s: %w", diskPath, err)
	}
	if err := os.Chmod(folderPath, 0o777); err != nil {
		cleanupVMDiskCreate(folderPath, diskPath)
		return nil, fmt.Errorf("chmod vm disk folder %s: %w", folderPath, err)
	}

	disk, err := diskInfoFromPath(diskPath, folderPath)
	if err != nil {
		return nil, err
	}

	logger.Info("VM disk created", "name", name, "format", disk.Format, "sizeGB", disk.SizeGB, "occupiedGB", disk.OccupiedGB, "path", diskPath)
	return disk, nil
}

func DeleteVMDisk(basePath, name string) (*VMDisk, error) {
	disk, err := findVMDisk(basePath, name)
	if err != nil {
		return nil, err
	}

	if err := os.Remove(disk.DiskPath); err != nil {
		return nil, fmt.Errorf("remove vm disk %s: %w", disk.DiskPath, err)
	}
	if err := os.Remove(disk.FolderPath); err != nil {
		return nil, fmt.Errorf("remove vm disk folder %s: %w", disk.FolderPath, err)
	}

	logger.Info("VM disk deleted", "name", name, "path", disk.DiskPath)
	return disk, nil
}

func GetVMDiskInfo(basePath, name string) (*VMDisk, error) {
	return findVMDisk(basePath, name)
}

func GrowVMDisk(basePath, name string, sizeGB int64) (*VMDisk, error) {
	if sizeGB <= 0 {
		return nil, fmt.Errorf("sizeGB must be greater than zero")
	}
	if err := requireQemuImg(); err != nil {
		return nil, err
	}

	disk, err := findVMDisk(basePath, name)
	if err != nil {
		return nil, err
	}

	info, err := readQemuImgInfo(disk.DiskPath)
	if err != nil {
		return nil, err
	}
	currentGB := ceilDiv(info.VirtualSize, bytesInGB)
	if sizeGB <= currentGB {
		return nil, fmt.Errorf("new sizeGB must be greater than current size %dGB", currentGB)
	}

	cmd := exec.Command("qemu-img", "resize", disk.DiskPath, fmt.Sprintf("%dG", sizeGB))
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return nil, fmt.Errorf("qemu-img resize %s: %s", disk.DiskPath, msg)
		}
		return nil, fmt.Errorf("qemu-img resize %s: %w", disk.DiskPath, err)
	}

	updated, err := readQemuImgInfo(disk.DiskPath)
	if err != nil {
		return nil, fmt.Errorf("verify resized vm disk: %w", err)
	}
	if updated.VirtualSize < sizeGB*bytesInGB {
		return nil, fmt.Errorf("qemu-img resize did not reach requested size, have %d bytes want %d bytes", updated.VirtualSize, sizeGB*bytesInGB)
	}

	applyQemuInfo(disk, updated)
	logger.Info("VM disk grown", "name", name, "sizeGB", disk.SizeGB, "occupiedGB", disk.OccupiedGB, "path", disk.DiskPath)
	return disk, nil
}

func findVMDisk(basePath, name string) (*VMDisk, error) {
	if err := validateVMDiskName(strings.TrimSpace(name)); err != nil {
		return nil, err
	}
	if err := requireQemuImg(); err != nil {
		return nil, err
	}

	cleanBase, err := cleanExistingBasePath(basePath)
	if err != nil {
		return nil, err
	}

	folderPath := filepath.Join(cleanBase, "vm_disk_"+name)
	if err := ensurePathInsideBase(cleanBase, folderPath); err != nil {
		return nil, err
	}
	info, err := os.Stat(folderPath)
	if err != nil {
		return nil, fmt.Errorf("stat vm disk folder %s: %w", folderPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("vm disk folder path is not a directory: %s", folderPath)
	}

	candidates := []string{
		filepath.Join(folderPath, name+".qcow2"),
		filepath.Join(folderPath, name+".raw"),
	}

	var matches []string
	for _, candidate := range candidates {
		if err := ensurePathInsideBase(cleanBase, candidate); err != nil {
			return nil, err
		}
		info, err := os.Stat(candidate)
		switch {
		case err == nil && !info.IsDir():
			matches = append(matches, candidate)
		case err == nil && info.IsDir():
			return nil, fmt.Errorf("vm disk path is a directory: %s", candidate)
		case errors.Is(err, os.ErrNotExist):
		default:
			return nil, fmt.Errorf("stat vm disk %s: %w", candidate, err)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("vm disk file not found in %s", folderPath)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("multiple vm disk files found in %s", folderPath)
	}

	qemuInfo, err := readQemuImgInfo(matches[0])
	if err != nil {
		return nil, err
	}
	return diskFromQemuInfo(matches[0], folderPath, qemuInfo), nil
}

func validateVMDiskName(name string) error {
	if !vmDiskNamePattern.MatchString(name) || name == "." || name == ".." {
		return fmt.Errorf("invalid disk name %q", name)
	}
	return nil
}

func normalizeVMDiskFormat(format string) (qemuFormat, extension string, err error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "qcow", "qcow2":
		return "qcow2", ".qcow2", nil
	case "raw":
		return "raw", ".raw", nil
	default:
		return "", "", fmt.Errorf("unsupported disk format %q, use qcow, qcow2 or raw", format)
	}
}

func requireQemuImg() error {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		return fmt.Errorf("qemu-img not found in PATH: %w", err)
	}
	return nil
}

func cleanExistingBasePath(basePath string) (string, error) {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		return "", fmt.Errorf("base path is required")
	}
	cleanBase, err := filepath.Abs(filepath.Clean(basePath))
	if err != nil {
		return "", fmt.Errorf("resolve base path: %w", err)
	}
	baseInfo, err := os.Stat(cleanBase)
	if err != nil {
		return "", fmt.Errorf("stat base path %s: %w", cleanBase, err)
	}
	if !baseInfo.IsDir() {
		return "", fmt.Errorf("base path %s is not a directory", cleanBase)
	}
	return cleanBase, nil
}

func ensurePathInsideBase(basePath, path string) error {
	rel, err := filepath.Rel(basePath, path)
	if err != nil {
		return fmt.Errorf("resolve relative path: %w", err)
	}
	if rel == "." || rel == "" {
		return nil
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return fmt.Errorf("path %s escapes base path %s", path, basePath)
	}
	return nil
}

func cleanupVMDiskCreate(folderPath, diskPath string) {
	if diskPath != "" {
		if err := os.Remove(diskPath); err != nil && !os.IsNotExist(err) {
			logger.Warn("failed to cleanup vm disk file", "path", diskPath, "error", err)
		}
	}
	if folderPath != "" {
		if err := os.Remove(folderPath); err != nil && !os.IsNotExist(err) {
			logger.Warn("failed to cleanup vm disk folder", "path", folderPath, "error", err)
		}
	}
}

func readQemuImgInfo(path string) (*qemuImgInfo, error) {
	cmd := exec.Command("qemu-img", "info", "--output=json", path)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("qemu-img info %s: %s", path, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("qemu-img info %s: %w", path, err)
	}

	var info qemuImgInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("parse qemu-img info json: %w", err)
	}
	if info.VirtualSize <= 0 {
		return nil, fmt.Errorf("qemu-img info %s returned invalid virtual size", path)
	}
	return &info, nil
}

func diskInfoFromPath(diskPath, folderPath string) (*VMDisk, error) {
	qemuInfo, err := readQemuImgInfo(diskPath)
	if err != nil {
		return nil, err
	}
	return diskFromQemuInfo(diskPath, folderPath, qemuInfo), nil
}

func diskFromQemuInfo(diskPath, folderPath string, info *qemuImgInfo) *VMDisk {
	disk := &VMDisk{
		DiskPath:   diskPath,
		FolderPath: folderPath,
	}
	applyQemuInfo(disk, info)
	return disk
}

func applyQemuInfo(disk *VMDisk, info *qemuImgInfo) {
	disk.Format = strings.ToLower(info.Format)
	disk.SizeGB = ceilDiv(info.VirtualSize, bytesInGB)
	disk.OccupiedBytes = info.ActualSize
	disk.OccupiedGB = bytesToGB(info.ActualSize)
}

func bytesToGB(bytes int64) float64 {
	if bytes <= 0 {
		return 0
	}
	return float64(bytes) / float64(bytesInGB)
}

func ceilDiv(n, d int64) int64 {
	if d <= 0 {
		return 0
	}
	if n <= 0 {
		return 0
	}
	return (n + d - 1) / d
}
