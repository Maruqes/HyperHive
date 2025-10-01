package nfs

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Maruqes/512SvMan/logger"
)

type FolderMount struct {
	FolderPath string // shared folder, folder in host that will be shared via nfs
	Source     string // nfs source (ip:/path)
	Target     string // local mount point
}

type mountMonitorEntry struct {
	cancel context.CancelFunc
}

var (
	mountMonitorsMu sync.Mutex
	mountMonitors   = make(map[string]*mountMonitorEntry)
)

const (
	monitorInterval         = 5 * time.Second
	monitorFailureThreshold = 3
)

func InstallNFS() error {
	if err := runCommand("install nfs-utils", "sudo", "dnf", "-y", "install", "nfs-utils"); err != nil {
		return err
	}
	// Ensure the NFS server services are available when acting as a host
	if err := runCommand("enable nfs-server", "sudo", "systemctl", "enable", "--now", "nfs-server"); err != nil {
		return err
	}
	logger.Info("NFS installed and nfs-server enabled")
	return nil
}

func exportsEntry(path string) string {
	return fmt.Sprintf("%s *(rw,sync,no_subtree_check,no_root_squash)", path)
}

func escapeForSedLiteral(text string) string {
	replacer := strings.NewReplacer(
		`\\`, `\\\\`,
		`#`, `\#`,
		`[`, `\[`,
		`]`, `\]`,
		`^`, `\^`,
		`$`, `\$`,
		`.`, `\.`,
		`*`, `\*`,
		`"`, `\"`,
	)
	return replacer.Replace(text)
}

// CreateSharedFolder creates a directory and ensures it is exported via /etc/exports.
func CreateSharedFolder(folder FolderMount) error {
	if !filepath.IsAbs(folder.FolderPath) {
		return fmt.Errorf("folder path must be a full path and exist")
	}

	path := strings.TrimSpace(folder.FolderPath)
	if path == "" {
		return fmt.Errorf("folder path is required")
	}

	if err := runCommand("create share directory", "sudo", "mkdir", "-p", path); err != nil {
		return err
	}

	entry := exportsEntry(path)
	cmdStr := fmt.Sprintf("touch /etc/exports && (grep -Fxq %q /etc/exports || echo %q >> /etc/exports)", entry, entry)
	if err := runCommand("update /etc/exports", "sudo", "bash", "-lc", cmdStr); err != nil {
		return err
	}

	if err := runCommand("refresh nfs exports", "sudo", "exportfs", "-ra"); err != nil {
		return err
	}
	logger.Info("NFS share created: " + path)
	return nil
}

func RemoveSharedFolder(folder FolderMount) error {
	path := strings.TrimSpace(folder.FolderPath)
	if path == "" {
		return fmt.Errorf("folder path is required")
	}

	// 1) Remove any export line whose first field equals the path
	//    - Keeps comments/blank lines intact
	//    - Robust to different export options/spacing on the line
	filterCmd := fmt.Sprintf(`
set -euo pipefail
if [ -f /etc/exports ]; then
  tmp=$(mktemp)
  awk -v p='%s' 'BEGIN{OFS=FS=" "}{ if ($0 ~ /^[[:space:]]*#/ || NF==0) { print; next } if ($1!=p) { print } }' /etc/exports > "$tmp"
  sudo install -m 0644 "$tmp" /etc/exports
  rm -f "$tmp"
fi
`, escapeForSingleQuotes(path))
	if err := runCommand("filter /etc/exports", "sudo", "bash", "-lc", filterCmd); err != nil {
		return err
	}

	// 2) Unexport this path if it’s currently exported (ignores error if not exported)
	_ = runCommand("unexport path", "sudo", "exportfs", "-u", path)

	// 3) Re-apply exports
	if err := runCommand("refresh nfs exports", "sudo", "exportfs", "-ra"); err != nil {
		return err
	}

	// 4) Remove the directory (best-effort)
	if err := runCommand("remove share directory", "sudo", "rm", "-rf", path); err != nil {
		return err
	}
	logger.Info("NFS share removed:", path)
	return nil
}

// Escapes a string for safe inclusion inside single quotes in a POSIX shell.
// Example: abc'def -> 'abc'"'"'def'
func escapeForSingleQuotes(s string) string {
	return strings.ReplaceAll(s, `'`, `'"'"'`)
}

// MountSharedFolder mounts an exported folder from the network.
func MountSharedFolder(folder FolderMount) error {
	source := strings.TrimSpace(folder.Source)
	target := strings.TrimSpace(folder.Target)
	if source == "" {
		return fmt.Errorf("source is required")
	}
	if target == "" {
		return fmt.Errorf("target is required")
	}

	if err := runCommand("ensure mount directory", "sudo", "mkdir", "-p", target); err != nil {
		return err
	}

	mountOptions := []string{"_netdev", "soft", "timeo=10", "retrans=2", "nofail", "vers=4"}
	// Use aggressive timeouts so that we notice server failures quickly.
	if err := runCommand("mount nfs share", "sudo", "mount", "-t", "nfs", "-o", strings.Join(mountOptions, ","), source, target); err != nil {
		return err
	}

	startMountMonitor(source, target)
	logger.Info("NFS share mounted: " + source + " -> " + target)
	return nil
}

func UnmountSharedFolder(folder FolderMount) error {
	target := strings.TrimSpace(folder.Target)
	if target == "" {
		return fmt.Errorf("target is required")
	}

	monitorWasRunning := stopMountMonitor(target)
	if err := runCommand("unmount nfs share", "sudo", "umount", target); err != nil {
		if monitorWasRunning {
			startMountMonitor(folder.Source, target)
		}
		return err
	}
	logger.Info("NFS share unmounted: " + target)
	return nil
}

func startMountMonitor(source, target string) {
	host, err := extractNFSServer(source)
	if err != nil {
		logger.Warn("unable to start NFS monitor", "source", source, "error", err)
		return
	}

	mountMonitorsMu.Lock()
	if existing := mountMonitors[target]; existing != nil {
		existing.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	entry := &mountMonitorEntry{cancel: cancel}
	mountMonitors[target] = entry
	mountMonitorsMu.Unlock()

	go monitorNFSMount(ctx, entry, host, source, target)
}

func stopMountMonitor(target string) bool {
	mountMonitorsMu.Lock()
	entry := mountMonitors[target]
	if entry != nil {
		delete(mountMonitors, target)
	}
	mountMonitorsMu.Unlock()
	if entry != nil {
		entry.cancel()
		return true
	}
	return false
}

func monitorNFSMount(ctx context.Context, entry *mountMonitorEntry, host, source, target string) {
	defer clearMountMonitor(target, entry)

	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()

	failureCount := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if isNFSServerReachable(host) {
				failureCount = 0
				continue
			}
			failureCount++
			if failureCount < monitorFailureThreshold {
				continue
			}

			logger.Warn("nfs server unreachable; attempting automatic unmount", "source", source, "target", target)
			if err := runCommand("auto unmount nfs share", "sudo", "umount", "-f", target); err != nil {
				logger.Error("automatic nfs unmount failed", "target", target, "error", err)
				failureCount = monitorFailureThreshold - 1
				continue
			}
			logger.Info("nfs share automatically unmounted", "source", source, "target", target)
			return
		}
	}
}

func clearMountMonitor(target string, entry *mountMonitorEntry) {
	mountMonitorsMu.Lock()
	if mountMonitors[target] == entry {
		delete(mountMonitors, target)
	}
	mountMonitorsMu.Unlock()
}

func extractNFSServer(source string) (string, error) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return "", fmt.Errorf("empty nfs source")
	}
	idx := strings.Index(trimmed, ":/")
	if idx == -1 {
		return "", fmt.Errorf("invalid nfs source %q", source)
	}
	host := strings.Trim(trimmed[:idx], "[]")
	if host == "" {
		return "", fmt.Errorf("invalid nfs host in %q", source)
	}
	return host, nil
}

func isNFSServerReachable(host string) bool {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.Dial("tcp", net.JoinHostPort(host, "2049"))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func runCommand(desc string, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s: no command provided", desc)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logger.Error(desc + " failed: " + err.Error())
		return fmt.Errorf("%s: %w", desc, err)
	}
	logger.Info(desc + " succeeded")
	return nil
}
