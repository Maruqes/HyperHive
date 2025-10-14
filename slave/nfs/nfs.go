package nfs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Maruqes/512SvMan/logger"
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

func listFilesInDir(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	files, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	return files, nil
}

func isMounted(target string) bool {
	_, err := listFilesInDir(target)
	if err != nil {
		// If we can't read the directory, assume it's not mounted
		logger.Error("cannot read mount target:", target, err)
		return false
	}
	// Use the mountpoint command to check if the target is a mount point
	out, err := exec.Command("mountpoint", "-q", target).CombinedOutput()
	if err != nil {
		logger.Error("mountpoint check failed:", err, string(out))
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
					time.Sleep(monitorInterval)
				}
				if !success {
					logger.Error("Failed to remount NFS share after multiple attempts:", mount.Target)
					err := UnmountSharedFolder(mount)
					if err != nil {
						logger.Error("Failed to unmount NFS share:", mount.Target, err)
					}
					logger.Error("NFS share unmounted to prevent further issues:", mount.Target)
				} else {
					logger.Info("Successfully remounted NFS share:", mount.Target)
				}
			}
		}
		time.Sleep(monitorInterval)
	}
}

func InstallNFS() error {
	resetCmd := fmt.Sprintf("mkdir -p %s && : > %s", exportsDir, exportsFile)
	if err := runCommand("reset NFS exports file", "sudo", "bash", "-lc", resetCmd); err != nil {
		return err
	}

	if err := runCommand("install nfs-utils", "sudo", "dnf", "-y", "install", "nfs-utils"); err != nil {
		return err
	}
	// Ensure the NFS server services are available when acting as a host
	if err := runCommand("enable nfs-server", "sudo", "systemctl", "enable", "--now", "nfs-server"); err != nil {
		return err
	}
	logger.Info("NFS installed and nfs-server enabled")
	go MonitorMounts()
	return nil
}

func detectQemuIDs() (uid, gid string) {
	candidates := [][2]string{
		{"qemu", "qemu"},
		{"libvirt-qemu", "kvm"},
		{"libvirt-qemu", "libvirt-qemu"},
	}
	for _, c := range candidates {
		uOut, uErr := exec.Command("id", "-u", c[0]).Output()
		gOut, gErr := exec.Command("getent", "group", c[1]).Output()
		if uErr == nil && gErr == nil && len(uOut) > 0 && len(gOut) > 0 {
			uid := strings.TrimSpace(string(uOut))
			// getent group line: name:x:GID:members
			parts := strings.Split(strings.TrimSpace(string(gOut)), ":")
			if len(parts) >= 3 {
				gid := parts[2]
				return uid, gid
			}
		}
	}
	// fallback: nfsnobody
	return "65534", "65534"
}

func exportsEntry(path string) string {
	uid, gid := detectQemuIDs()
	// all_squash -> mapeia TODOS os clientes para anonuid/anongid
	// anonuid/anongid -> usa UID/GID do qemu do servidor
	return fmt.Sprintf("%s *(rw,sync,all_squash,anonuid=%s,anongid=%s,no_subtree_check)", path, uid, gid)
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

	if err := ensureVirtUseNFSBoolean(); err != nil {
		return err
	}

	if err := labelNFSMountSource(path); err != nil {
		return err
	}

	if err := ensureNFSGeneratorPolicy(); err != nil {
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

// CreateSharedFolder creates a directory (0777) and ensures it is exported via exportsFile.
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

	// Give full read/write/execute permissions to everyone (owner/group/others)
	// antes tinhas chmod 777; é melhor apertar
	uid, gid := detectQemuIDs()
	if err := runCommand("set share owner to qemu", "sudo", "chown", "-R", fmt.Sprintf("%s:%s", uid, gid), path); err != nil {
		return err
	}
	if err := runCommand("set share perms", "sudo", "chmod", "-R", "770", path); err != nil {
		return err
	}

	if err := allowSELinuxForNFS(path); err != nil {
		return err
	}

	entry := exportsEntry(path)
	cmdStr := fmt.Sprintf("mkdir -p %s && touch %s && (grep -Fxq %q %s || echo %q >> %s)", exportsDir, exportsFile, entry, exportsFile, entry, exportsFile)
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

		if err := allowSELinuxForNFS(path); err != nil {
			return err
		}

		unique[path] = struct{}{}
		paths = append(paths, path)
	}

	sort.Strings(paths)

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

func isRemoteFS(path string) bool {
	b, _ := os.ReadFile("/proc/self/mountinfo")
	for _, ln := range strings.Split(string(b), "\n") {
		if ln == "" {
			continue
		}
		parts := strings.Split(ln, " - ")
		if len(parts) != 2 {
			continue
		}
		left := strings.Fields(parts[0])
		right := strings.Fields(parts[1])
		if len(left) < 5 || len(right) < 1 {
			continue
		}
		mountpoint := left[4]
		fstype := right[0]
		if strings.HasPrefix(path, mountpoint) {
			switch fstype {
			case "nfs", "nfs4", "cifs", "fuse.sshfs":
				return true
			}
		}
	}
	return false
}

func givePermissionsToEveryone(folder FolderMount) error {
	path := strings.TrimSpace(folder.Target)
	if path == "" {
		return fmt.Errorf("target is required")
	}

	// Em NFS, não tentar chown/chmod recursivo
	if isRemoteFS(path) {
		if err := ensureVirtUseNFSBoolean(); err != nil {
			return err
		}
		// teste de escrita (como qemu se existir, senão como current)
		// nota: pode falhar se o export ainda não foi ajustado
		testCmd := fmt.Sprintf("echo ok | tee %s >/dev/null", filepath.Join(path, ".svman_write_test"))
		// tenta com qemu
		if exec.Command("bash", "-lc", "id -u qemu >/dev/null 2>&1").Run() == nil {
			_ = runCommand("write test (qemu)", "sudo", "-u", "qemu", "bash", "-lc", testCmd)
		} else {
			_ = runCommand("write test", "bash", "-lc", testCmd)
		}
		return nil
	}

	// FS local: aqui sim, podes abrir permissões conforme já tinhas
	if err := runCommand("give permissions to everyone", "sudo", "chmod", "777", path); err != nil {
		return err
	}
	if err := ensureVirtUseNFSBoolean(); err != nil {
		return err
	}
	owners := []string{"qemu:kvm", "qemu:qemu", "libvirt-qemu:kvm", "libvirt-qemu:libvirt-qemu"}
	var applied string
	for _, o := range owners {
		parts := strings.SplitN(o, ":", 2)
		if len(parts) != 2 {
			continue
		}
		user, group := parts[0], parts[1]
		check := fmt.Sprintf("id -u '%s' >/dev/null 2>&1 && getent group '%s' >/dev/null 2>&1", escapeForSingleQuotes(user), escapeForSingleQuotes(group))
		if err := exec.Command("bash", "-lc", check).Run(); err != nil {
			continue
		}
		if err := runCommand("set ownership for libvirt/qemu", "sudo", "chown", "-R", o, path); err == nil {
			applied = o
			break
		} else {
			logger.Warn("failed to set ownership:", o, err)
		}
	}
	if applied != "" {
		if err := runCommand("restrict share permissions", "sudo", "chmod", "-R", "770", path); err != nil {
			return err
		}
	}
	if commandExists("getsebool") {
		_ = runCommand("check virt_use_nfs", "getsebool", "virt_use_nfs")
	}
	logger.Info("Permissions set for local FS:", path)
	return nil
}

// MountSharedFolder mounts an exported folder from the network.
func MountSharedFolder(folder FolderMount) error {
	source := strings.TrimSpace(folder.Source)
	target := strings.TrimSpace(folder.Target)
	if source == "" || target == "" {
		return fmt.Errorf("source and target are required")
	}
	if err := runCommand("ensure mount directory", "sudo", "mkdir", "-p", target); err != nil {
		return err
	}

	optsFast := []string{
		"rw", "hard", "proto=tcp", "vers=4.2",
		"rsize=1048576", "wsize=1048576",
		"nconnect=4",
		"noatime", "nodiratime", "_netdev",
	}
	if err := runCommand("mount nfs share",
		"sudo", "mount", "-t", "nfs4", "-o", strings.Join(optsFast, ","), source, target); err != nil {
		return err
	}

	if err := givePermissionsToEveryone(folder); err != nil {
		return err
	}
	CurrentMountsLock.Lock()
	CurrentMounts = append(CurrentMounts, folder)
	CurrentMountsLock.Unlock()
	logger.Info("NFS share mounted: " + source + " -> " + target)
	return nil
}

func UnmountSharedFolder(folder FolderMount) error {
	target := strings.TrimSpace(folder.Target)
	if target == "" {
		return fmt.Errorf("target is required")
	}

	if err := runCommand("unmount nfs share", "sudo", "umount", target); err != nil {
		return err
	}
	logger.Info("NFS share unmounted: " + target)
	CurrentMountsLock.Lock()
	defer CurrentMountsLock.Unlock()
	for i, m := range CurrentMounts {
		if m.Target == target {
			CurrentMounts = append(CurrentMounts[:i], CurrentMounts[i+1:]...)
			break
		}
	}
	return nil
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

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func ensureNFSSELinuxBoolean() error {
	return enableSELinuxBoolean("nfs_export_all_rw")
}

func ensureVirtUseNFSBoolean() error {
	return enableSELinuxBoolean("virt_use_nfs")
}

func enableSELinuxBoolean(name string) error {
	if !commandExists("setsebool") {
		logger.Warn("setsebool binary not available, skipping SELinux boolean:", name)
		return nil
	}
	desc := fmt.Sprintf("enable %s", name)
	if err := runCommand(desc, "sudo", "setsebool", "-P", name, "on"); err != nil {
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

func ensureNFSGeneratorPolicy() error {
	// Module names must begin with a letter per SELinux policy syntax rules.
	const moduleName = "svman_nfs_generator"
	if !commandExists("semodule") || !commandExists("checkmodule") || !commandExists("semodule_package") {
		logger.Warn("SELinux policy tools missing, skipping custom module installation")
		return nil
	}

	listCmd := exec.Command("semodule", "-l")
	out, err := listCmd.Output()
	if err == nil && strings.Contains(string(out), moduleName) {
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "selinux-module-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tePath := filepath.Join(tmpDir, moduleName+".te")
	teContent := fmt.Sprintf(`module %s 1.0;

require {
    type systemd_nfs_generator_t;
    class capability dac_read_search;
}

allow systemd_nfs_generator_t systemd_nfs_generator_t:capability dac_read_search;
`, moduleName)
	if err := os.WriteFile(tePath, []byte(teContent), 0644); err != nil {
		return err
	}

	modPath := filepath.Join(tmpDir, moduleName+".mod")
	ppPath := filepath.Join(tmpDir, moduleName+".pp")

	if err := runCommand("compile selinux policy module", "sudo", "checkmodule", "-M", "-m", "-o", modPath, tePath); err != nil {
		return err
	}
	if err := runCommand("package selinux policy module", "sudo", "semodule_package", "-o", ppPath, "-m", modPath); err != nil {
		return err
	}
	if err := runCommand("install selinux policy module", "sudo", "semodule", "-X", "300", "-i", ppPath); err != nil {
		return err
	}
	return nil
}

// check if folder exists, if not return error
func DownloadISO(url, isoName, downloadFolder string) (string, error) {
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
	if _, err := os.Stat(isoPath); err == nil {
		logger.Info("ISO already exists, skipping download:", isoPath)
		return isoPath, nil
	}

	if !commandExists("curl") {
		return "", fmt.Errorf("curl is not installed")
	}

	cmd := exec.Command("curl", "-L", "-o", isoPath, url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
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
