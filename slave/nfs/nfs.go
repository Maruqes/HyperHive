package nfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slave/extra"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
	"github.com/Maruqes/512SvMan/logger"
	"github.com/cavaliergopher/grab/v3"
)

type FolderMount struct {
	FolderPath string // shared folder, folder in host that will be shared via nfs
	Source     string // nfs source (ip:/path)
	Target     string // local mount point
}

const (
	monitorInterval         = 5 * time.Second
	monitorFailureThreshold = 3
	exportsDir              = "/etc/exports.d"
	exportsFile             = "/etc/exports.d/512svman.exports"
)

var CurrentMounts = []FolderMount{}
var CurrentMountsLock = &sync.RWMutex{}
var setfaclWarnOnce sync.Once

func ensureExportsLocation() error {
	if _, err := os.Stat(exportsDir); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := runCommand("ensure exports directory", "sudo", "mkdir", "-p", exportsDir); err != nil {
			return err
		}
	}

	if _, err := os.Stat(exportsFile); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := runCommand("ensure exports file", "sudo", "touch", exportsFile); err != nil {
			return err
		}
	}

	return nil
}

func isMounted(target string) bool {
	if strings.TrimSpace(target) == "" {
		return false
	}

	cmd := exec.Command("mountpoint", "-q", target)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			switch exitErr.ExitCode() {
			case 0:
				return true
			case 1, 32:
				// mountpoint uses 1 or 32 when the path is not a mount point
				return false
			}
		}
		logger.Error("mountpoint check failed:", target, err)
		return false
	}
	return true
}

func MonitorMounts() {
	for {
		CurrentMountsLock.RLock()
		mounts := append([]FolderMount(nil), CurrentMounts...)
		CurrentMountsLock.RUnlock()

		for _, mount := range mounts {
			//check if is mounted
			if !isMounted(mount.Target) {
				//if not try to mount 3 times with 5 seconds interval
				logger.Warn("NFS mount lost, attempting to remount:", mount.Target)
				success := false
				for i := 0; i < monitorFailureThreshold; i++ {
					err := MountSharedFolder(mount)
					if err == nil {
						success = true
						break
					}
					logger.Warn("remount attempt failed:", mount.Target, err)
					time.Sleep(monitorInterval)
				}
				if !success {
					logger.Error("Failed to remount NFS share after multiple attempts:", mount.Target)
				} else {
					logger.Info("Successfully remounted NFS share:", mount.Target)
				}
			}
		}
		time.Sleep(monitorInterval)
	}
}

func EnsureClientPrereqs() error {
	// Allow QEMU/libvirt contexts to use NFS storage
	if commandExists("setsebool") {
		if err := runCommand("enable virt_use_nfs", "sudo", "setsebool", "-P", "virt_use_nfs", "on"); err != nil {
			return err
		}
		if err := runCommand("enable virt_sandbox_use_nfs", "sudo", "setsebool", "-P", "virt_sandbox_use_nfs", "on"); err != nil {
			logger.Warn("Failed to enable virt_sandbox_use_nfs (may not exist on this system):", err)
		}
	}
	// Ensure libvirt lock/log services (advisory locks across hosts)
	_ = runCommand("enable virtlockd", "sudo", "systemctl", "enable", "--now", "virtlockd")
	_ = runCommand("enable virtlogd", "sudo", "systemctl", "enable", "--now", "virtlogd")
	return nil
}

func InstallNFS() error {
	resetCmd := fmt.Sprintf("mkdir -p %s && : > %s", exportsDir, exportsFile)
	if err := runCommand("reset NFS exports file", "sudo", "bash", "-lc", resetCmd); err != nil {
		return err
	}

	if err := runCommand("install nfs-utils", "sudo", "dnf", "-y", "install", "nfs-utils"); err != nil {
		return err
	}

	// Configure NFS server for multi-client VM workloads
	if err := tuneNFSServer(); err != nil {
		logger.Warn("Failed to tune NFS server (non-fatal):", err)
	}

	// Ensure the NFS server services are available when acting as a host
	if err := runCommand("enable nfs-server", "sudo", "systemctl", "enable", "--now", "nfs-server"); err != nil {
		return err
	}

	if err := EnsureClientPrereqs(); err != nil {
		return err
	}

	if err := runCommand("ensure exports file", "sudo", "touch", "/etc/exports"); err != nil {
		return err
	}

	logger.Info("NFS installed and nfs-server enabled")
	go MonitorMounts()
	return nil
}

func tuneNFSServer() error {
	// Increase NFS server thread count for better multi-client performance
	// Default is usually 8, we increase to 32 for VM workloads
	nfsdThreads := `# Increased for VM workloads with multiple clients
RPCNFSDCOUNT=32
`
	tmpFile, err := os.CreateTemp("", "nfs-config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(nfsdThreads); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	// Write to /etc/nfs.conf.d/ if it exists, otherwise /etc/sysconfig/nfs
	configPaths := []string{
		"/etc/nfs.conf.d/512svman.conf",
		"/etc/sysconfig/nfs",
	}

	configured := false
	for _, configPath := range configPaths {
		dir := filepath.Dir(configPath)
		if _, err := os.Stat(dir); err == nil {
			if err := runCommand("configure nfs server threads",
				"sudo", "bash", "-c",
				fmt.Sprintf("grep -q RPCNFSDCOUNT %s 2>/dev/null || cat %s >> %s",
					escapeForSingleQuotes(configPath),
					escapeForSingleQuotes(tmpPath),
					escapeForSingleQuotes(configPath))); err != nil {
				continue
			}
			configured = true
			logger.Info("NFS server thread count increased to 32")
			break
		}
	}

	if !configured {
		logger.Warn("Could not find NFS server config file to tune thread count")
	}

	return nil
}

func exportsEntry(path string) string {
	// FAST profile (performance > durabilidade)
	// - async: confirma antes de gravar em disco → muito mais rápido
	// - no_subtree_check: evita overhead em lookups
	// - no_root_squash: permite qemu/libvirt operar como root (mais simples/compat)
	// - insecure: permite portas >1024 (útil em alguns clientes)
	// - sec=sys: autenticação UNIX simples
	return fmt.Sprintf("%s *(rw,async,no_subtree_check,no_root_squash,insecure,sec=sys)", path)
}

func allowSELinuxForNFS(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("selinux path is required")
	}

	if !commandExists("getenforce") {
		return nil
	}

	modeOut, err := exec.Command("getenforce").Output()
	if err != nil {
		logger.Warn("SELinux detection failed, skipping adjustments:", err)
		return nil
	}
	mode := strings.TrimSpace(string(modeOut))
	if strings.EqualFold(mode, "disabled") {
		return nil
	}

	if err := ensureNFSSELinuxBoolean(); err != nil {
		return err
	}

	if err := labelNFSMountSource(path); err != nil {
		return err
	}

	return nil
}

var safeName = regexp.MustCompile(`^[\p{L}\p{N}._-]+$`)

func IsSafePath(path string) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return errors.New("path must be absolute")
	}

	// Reject any non-ASCII or control/shell-unsafe ASCII characters.
	for _, r := range clean {
		switch {
		case r < 32 || r == 127: // control chars + DEL
			return fmt.Errorf("control or non-printable char detected: U+%04X", r)
		case r > 127:
			return fmt.Errorf("non-ASCII character detected: %q", r)
		case strings.ContainsRune("<>|;&$`'\"\\!()[]{}?*^~#", r):
			return fmt.Errorf("disallowed shell character: %q", r)
		}
	}

	// Split into directory components (NOT SplitList)
	parts := strings.Split(clean, string(os.PathSeparator))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			continue
		}
		if !safeName.MatchString(part) {
			return fmt.Errorf("unsafe characters in component %q", part)
		}
	}
	return nil
}

func IsFolderBeingShared(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}

	data, err := os.ReadFile(exportsFile)
	if err != nil {
		return false
	}

	lines := strings.Split(string(data), "\n")
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		fields := strings.Fields(trim)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == path {
			return true
		}
	}
	return false
}

// CreateSharedFolder creates a directory and ensures it is exported via exportsFile.
func CreateSharedFolder(folder FolderMount) error {
	if err := IsSafePath(folder.FolderPath); err != nil {
		return fmt.Errorf("invalid folder path: %w", err)
	}

	path := strings.TrimSpace(folder.FolderPath)
	if path == "" {
		return fmt.Errorf("folder path is required")
	}

	if err := runCommand("create share directory", "sudo", "mkdir", "-p", path); err != nil {
		return err
	}

	if err := ensureOpenPermissions(path, true); err != nil {
		return err
	}

	if err := allowSELinuxForNFS(path); err != nil {
		return err
	}

	if err := ensureExportsLocation(); err != nil {
		return err
	}

	entry := exportsEntry(path)
	cmdStr := fmt.Sprintf("(grep -Fxq %q %q 2>/dev/null || echo %q >> %q)", entry, exportsFile, entry, exportsFile)
	if err := runCommand("update NFS exports", "sudo", "bash", "-lc", cmdStr); err != nil {
		return err
	}

	if err := runCommand("refresh nfs exports", "sudo", "exportfs", "-ra"); err != nil {
		return err
	}
	logger.Info("NFS share created: " + path)
	return nil
}

func SyncSharedFolder(folder []FolderMount) error {
	unique := make(map[string]struct{})
	var paths []string

	for _, mount := range folder {
		path := strings.TrimSpace(mount.FolderPath)
		if path == "" {
			return fmt.Errorf("folder path is required")
		}
		if err := IsSafePath(path); err != nil {
			return fmt.Errorf("invalid folder path %q: %w", path, err)
		}
		if _, exists := unique[path]; exists {
			continue
		}

		if err := runCommand(fmt.Sprintf("ensure share directory %s", path), "sudo", "mkdir", "-p", path); err != nil {
			return err
		}

		if err := ensureOpenPermissions(path, true); err != nil {
			return err
		}

		if err := allowSELinuxForNFS(path); err != nil {
			return err
		}

		unique[path] = struct{}{}
		paths = append(paths, path)
	}

	sort.Strings(paths)

	if err := ensureExportsLocation(); err != nil {
		return err
	}

	entries := make([]string, len(paths))
	for i, path := range paths {
		entries[i] = exportsEntry(path)
	}

	content := strings.Join(entries, "\n")
	if len(content) > 0 {
		content += "\n"
	}

	tmpFile, err := os.CreateTemp("", "svman-exports-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return err
	}

	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := runCommand("ensure exports directory", "sudo", "mkdir", "-p", exportsDir); err != nil {
		return err
	}

	if err := runCommand("sync exports file", "sudo", "install", "-m", "0644", tmpPath, exportsFile); err != nil {
		return err
	}

	if err := runCommand("refresh nfs exports", "sudo", "exportfs", "-ra"); err != nil {
		return err
	}

	logger.Info("NFS exports synchronized", "count", len(paths))
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
file='%s'
if [ -f "$file" ]; then
  tmp=$(mktemp)
  awk -v p='%s' 'BEGIN{OFS=FS=" "}{ if ($0 ~ /^[[:space:]]*#/ || NF==0) { print; next } if ($1!=p) { print } }' "$file" > "$tmp"
  install -m 0644 "$tmp" "$file"
  rm -f "$tmp"
fi
`, escapeForSingleQuotes(exportsFile), escapeForSingleQuotes(path))
	if err := runCommand("filter NFS exports", "sudo", "bash", "-lc", filterCmd); err != nil {
		return err
	}

	// 2) Unexport this path if it’s currently exported (ignores error if not exported)
	_ = runCommand("unexport path", "sudo", "exportfs", "-u", path)

	// 3) Re-apply exports
	if err := runCommand("refresh nfs exports", "sudo", "exportfs", "-ra"); err != nil {
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

func ensureOpenPermissions(path string, recursive bool) error {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return fmt.Errorf("path is required")
	}

	chownArgs := []string{"sudo", "chown"}
	chmodArgs := []string{"sudo", "chmod"}
	if recursive {
		chownArgs = append(chownArgs, "-R")
		chmodArgs = append(chmodArgs, "-R")
	}
	// Use qemu:qemu ownership instead of root:root for better QEMU compatibility
	chownArgs = append(chownArgs, "qemu:qemu", clean)
	if err := runCommand("set owner qemu:qemu "+clean, chownArgs...); err != nil {
		// Fallback to root if qemu user doesn't exist
		logger.Warn("Failed to set qemu:qemu ownership, trying root:root", err)
		chownArgs = []string{"sudo", "chown"}
		if recursive {
			chownArgs = append(chownArgs, "-R")
		}
		chownArgs = append(chownArgs, "root:root", clean)
		if err := runCommand("set owner root:root "+clean, chownArgs...); err != nil {
			return err
		}
	}

	chmodArgs = append(chmodArgs, "0777", clean)
	if err := runCommand("set mode 0777 "+clean, chmodArgs...); err != nil {
		return err
	}
	if err := applyWorldWritableACL(clean, recursive); err != nil {
		return err
	}
	return nil
}

func applyWorldWritableACL(path string, recursive bool) error {
	if !commandExists("setfacl") {
		setfaclWarnOnce.Do(func() {
			logger.Warn("setfacl not available, skipping ACL adjustments for shared folder permissions")
		})
		return nil
	}

	args := []string{"sudo", "setfacl"}
	if recursive {
		args = append(args, "-R")
	}
	args = append(args, "-m", "u::rwx,g::rwx,o::rwx,mask::rwx", path)
	if err := runCommand("set ACL "+path, args...); err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat path for ACL %s: %w", path, err)
	}
	if !info.IsDir() {
		return nil
	}

	if recursive {
		escaped := escapeForSingleQuotes(path)
		script := fmt.Sprintf("find '%s' -type d -exec setfacl -m d:u::rwx,d:g::rwx,d:o::rwx,d:mask::rwx {} +", escaped)
		if err := runCommand("set default ACL "+path, "sudo", "bash", "-lc", script); err != nil {
			return err
		}
	} else {
		if err := runCommand("set default ACL "+path, "sudo", "setfacl", "-d", "-m", "u::rwx,g::rwx,o::rwx,mask::rwx", path); err != nil {
			return err
		}
	}

	return nil
}

func MountSharedFolder(folder FolderMount) error {
	source := strings.TrimSpace(folder.Source)
	target := strings.TrimSpace(folder.Target)
	if source == "" || target == "" {
		return fmt.Errorf("source and target are required")
	}

	opts := []string{
		"rw",
		"async",
		"hard",
		"proto=tcp",
		"vers=4.2",
		"rsize=1048576",
		"wsize=1048576",
		"nconnect=8",
		"timeo=600",
		"retrans=2",
		"noatime",
		"nodiratime",
		"_netdev",
		"actimeo=3",
		"lookupcache=positive",
	}

	ensureMountedWithOpts := func(remount bool) error {
		if remount {
			// Try remount in place first
			if err := runCommand("remount nfs share with correct opts",
				"sudo", "mount", "-t", "nfs4", "-o", "remount,"+strings.Join(opts, ","), source, target); err == nil {
				return nil
			}
			// Fall back to full unmount + mount if remount failed
			_ = runCommand("unmount nfs share", "sudo", "umount", "-f", target)
		}
		// Fresh mount
		if err := runCommand("mount nfs share",
			"sudo", "mount", "-t", "nfs4", "-o", strings.Join(opts, ","), source, target); err != nil {
			return err
		}
		return nil
	}

	if isMounted(target) {
		// Validate current mount options; remount if needed
		mtab, _ := os.ReadFile("/proc/mounts")
		targetEsc := strings.ReplaceAll(target, " ", "\\040") // how /proc/mounts escapes spaces
		lines := strings.Split(string(mtab), "\n")
		needsRemount := false
		for _, ln := range lines {
			if ln == "" {
				continue
			}
			fields := strings.Fields(ln)
			if len(fields) < 4 {
				continue
			}
			mp := fields[1]
			fsType := fields[2]
			if mp == targetEsc && strings.HasPrefix(fsType, "nfs") {
				cur := "," + fields[3] + ","
				// MUST NOT have local_lock=posix (causes corruption with multiple clients)
				if strings.Contains(cur, ",local_lock=posix,") {
					needsRemount = true
					logger.Warn("Found dangerous local_lock=posix, remounting with safe options")
					break
				}
				// MUST NOT have nconnect (can cause issues with locking)
				if strings.Contains(cur, ",nconnect=") {
					needsRemount = true
					logger.Warn("Found nconnect option, remounting without it")
					break
				}
				// Should have sync for data integrity
				if !strings.Contains(cur, ",sync,") {
					needsRemount = true
					logger.Warn("Missing sync option, remounting with it")
					break
				}
				// good enough
				break
			}
		}
		if !needsRemount {
			logger.Info("mount nfs share kept (already mounted with correct opts):", source, "->", target)
			return nil
		}
		logger.Warn("remounting NFS with multi-client-safe opts:", target)
		return ensureMountedWithOpts(true)
	}

	// Ensure mount point exists
	if err := runCommand("ensure mount directory", "sudo", "mkdir", "-p", target); err != nil {
		return err
	}

	// First-time mount with correct options
	if err := ensureMountedWithOpts(false); err != nil {
		return err
	}

	logger.Info("NFS LOCK")
	CurrentMountsLock.Lock()
	CurrentMounts = append(CurrentMounts, folder)
	CurrentMountsLock.Unlock()
	logger.Info("NFS UNLOCK")

	logger.Info("NFS share mounted: " + source + " -> " + target)
	return nil
}

func UnmountSharedFolder(folder FolderMount) error {
	target := strings.TrimSpace(folder.Target)
	if target == "" {
		return fmt.Errorf("target is required")
	}

	// If already not mounted, just cleanup state.
	if !isMounted(target) {
		CurrentMountsLock.Lock()
		for i, m := range CurrentMounts {
			if m.Target == target {
				CurrentMounts = append(CurrentMounts[:i], CurrentMounts[i+1:]...)
				break
			}
		}
		CurrentMountsLock.Unlock()
		logger.Info("NFS share already unmounted: " + target)
		return nil
	}

	// Always force unmount first; some NFS mounts report success yet remain busy.
	if err := runCommand("force unmount nfs share (-f)", "sudo", "umount", "-f", target); err != nil {
		logger.Warn("force unmount (-f) failed, attempting lazy force:", err)
	}

	// If still mounted, lazy-force unmount as a last resort.
	if isMounted(target) {
		if err := runCommand("lazy force unmount nfs share (-fl)", "sudo", "umount", "-f", "-l", target); err != nil {
			logger.Warn("lazy force unmount (-fl) failed", err)
		}
	}

	// Verify it is really unmounted
	if isMounted(target) {
		return fmt.Errorf("failed to unmount %s (still mounted)", target)
	}

	logger.Info("NFS share unmounted: " + target)

	CurrentMountsLock.Lock()
	for i, m := range CurrentMounts {
		if m.Target == target {
			CurrentMounts = append(CurrentMounts[:i], CurrentMounts[i+1:]...)
			break
		}
	}
	CurrentMountsLock.Unlock()

	return nil
}

func runCommand(desc string, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s: no command provided", desc)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	if err := cmd.Run(); err != nil {
		stdoutStr := strings.TrimSpace(stdoutBuf.String())
		stderrStr := strings.TrimSpace(stderrBuf.String())
		logger.Error(desc + " failed: " + err.Error())
		if stderrStr != "" {
			logger.Error(desc + " stderr: " + stderrStr)
		}
		if stdoutStr != "" {
			logger.Error(desc + " stdout: " + stdoutStr)
		}

		var details []string
		if stderrStr != "" {
			details = append(details, "stderr: "+stderrStr)
		}
		if stdoutStr != "" {
			details = append(details, "stdout: "+stdoutStr)
		}
		if len(details) > 0 {
			return fmt.Errorf("%s: %s: %w", desc, strings.Join(details, "; "), err)
		}
		return fmt.Errorf("%s: %w", desc, err)
	}
	logger.Info(desc + " succeeded")
	return nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func ensureNFSSELinuxBoolean() error {
	if !commandExists("setsebool") {
		logger.Warn("setsebool binary not available, skipping SELinux boolean for NFS exports")
		return nil
	}
	if err := runCommand("enable nfs_export_all_rw", "sudo", "setsebool", "-P", "nfs_export_all_rw", "on"); err != nil {
		return err
	}
	// Allow the NFS daemon to modify files on behalf of anonymous users
	if err := runCommand("enable nfsd_anon_write", "sudo", "setsebool", "-P", "nfsd_anon_write", "on"); err != nil {
		return err
	}
	return nil
}

func labelNFSMountSource(path string) error {
	if !commandExists("semanage") || !commandExists("restorecon") {
		logger.Warn("semanage or restorecon missing, skipping SELinux labeling for", path)
		return nil
	}
	pattern := fmt.Sprintf("%s(/.*)?", strings.TrimRight(path, "/"))
	escapedPattern := escapeForSingleQuotes(pattern)
	cmd := fmt.Sprintf("semanage fcontext -a -t virt_image_t '%s' || semanage fcontext -m -t virt_image_t '%s'", escapedPattern, escapedPattern)
	if err := runCommand("label selinux context for share", "sudo", "bash", "-lc", cmd); err != nil {
		return err
	}
	if err := runCommand("restore selinux context", "sudo", "restorecon", "-Rv", path); err != nil {
		return err
	}
	return nil
}

func downloadFile(ctx context.Context, url, destPath string) error {
	//if file exists return error
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("file already exists: %s", destPath)
	}

	client := grab.NewClient()

	if ctx == nil {
		ctx = context.Background()
	}

	req, err := grab.NewRequest(destPath, url)
	if err != nil {
		return err
	}
	// bind request to ctx so it cancels the transfer
	req = req.WithContext(ctx)

	fmt.Printf("Downloading %v...\n", req.URL())
	resp := client.Do(req)

	if resp.HTTPResponse != nil {
		fmt.Printf("  %v\n", resp.HTTPResponse.Status)
	}

	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()

Loop:
	for {
		select {
		case <-ctx.Done():
			// context canceled; grab will stop the request via req.WithContext
			fmt.Fprintf(os.Stderr, "Download canceled\n")
			return ctx.Err()
		case <-t.C:
			extra.SendWebsocketMessage(
				fmt.Sprintf("Download progress: %v / %v bytes (%.2f%%)  - %.2f MB/s",
					resp.BytesComplete(),
					resp.Size(),
					100*resp.Progress(),
					float64(resp.BytesPerSecond())/1024/1024),
				extraGrpc.WebSocketsMessageType_DownloadIso,
			)
		case <-resp.Done:
			break Loop
		}
	}

	// check for errors
	if err := resp.Err(); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	fmt.Printf("Download saved to %v\n", resp.Filename)
	return nil
}

// check if folder exists, if not return error
func DownloadISO(ctx context.Context, url, isoName, downloadFolder string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("url is required")
	}
	if isoName == "" {
		return "", fmt.Errorf("isoName is required")
	}
	if downloadFolder == "" {
		return "", fmt.Errorf("downloadFolder is required")
	}

	if err := IsSafePath(downloadFolder); err != nil {
		return "", fmt.Errorf("invalid download folder path: %w", err)
	}

	if downloadFolder[len(downloadFolder)-1] == '/' {
		downloadFolder = downloadFolder[:len(downloadFolder)-1]
	}

	if _, err := os.Stat(downloadFolder); os.IsNotExist(err) {
		return "", fmt.Errorf("download folder does not exist: %s", downloadFolder)
	}

	isoPath := filepath.Join(downloadFolder, isoName)

	err := downloadFile(ctx, url, isoPath)
	if err != nil {
		return "", fmt.Errorf("failed to download ISO: %w", err)
	}
	return isoPath, nil
}

type SharedFolderStatus struct {
	Working         bool
	SpaceOccupiedGB int64
	SpaceFreeGB     int64
	SpaceTotalGB    int64
}

func GetSharedFolderStatus(folder FolderMount) (*SharedFolderStatus, error) {
	path := strings.TrimSpace(folder.FolderPath)
	if path == "" {
		return nil, fmt.Errorf("folder path is required")
	}

	var status SharedFolderStatus

	if !isMounted(folder.Target) {
		status.Working = false
		return &status, nil
	}
	status.Working = true

	var statfs syscall.Statfs_t
	if err := syscall.Statfs(path, &statfs); err != nil {
		return nil, fmt.Errorf("failed to get filesystem stats: %w", err)
	}

	const bytesInGB = 1024 * 1024 * 1024
	status.SpaceTotalGB = int64(statfs.Blocks * uint64(statfs.Bsize) / bytesInGB)
	status.SpaceFreeGB = int64(statfs.Bfree * uint64(statfs.Bsize) / bytesInGB)
	status.SpaceOccupiedGB = status.SpaceTotalGB - status.SpaceFreeGB

	return &status, nil
}

type FolderContent struct {
	Files []string
	Dirs  []string
}

func GetFolderContentList(folderPath string) (*FolderContent, error) {
	path := strings.TrimSpace(folderPath)
	if path == "" {
		return nil, fmt.Errorf("folder path is required")
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var content FolderContent
	for _, e := range entries {
		full := filepath.Join(path, e.Name())
		if e.IsDir() {
			content.Dirs = append(content.Dirs, full)
		} else {
			content.Files = append(content.Files, full)
		}
	}

	return &content, nil
}

func CanFindFileOrDir(folderPath string) (bool, error) {
	path := strings.TrimSpace(folderPath)
	if path == "" {
		return false, fmt.Errorf("folder path is required")
	}

	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to stat path: %w", err)
	}
	return true, nil
}
