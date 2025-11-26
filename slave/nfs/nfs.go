package nfs

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slave/env512"
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
	FolderPath      string // shared folder, folder in host that will be shared via nfs
	Source          string // nfs source (ip:/path)
	Target          string // local mount point
	HostNormalMount bool
}

const (
	monitorInterval         = 5 * time.Second
	monitorFailureThreshold = 3
	exportsDir              = "/etc/exports.d"
	exportsFile             = "/etc/exports.d/512svman.exports"
)

// obrigado gpt pela configuraçao
var (
	nfsMountOptions = []string{
		"rw",
		"hard",            // chamadas bloqueiam e re-tentam até o servidor responder (evita corrupção)
		"proto=tcp",       // usa TCP (mais estável e fiável que UDP)
		"vers=4.2",        // força NFS versão 4.2 (melhor suporte e desempenho)
		"nconnect=8",      // múltiplas ligações TCP paralelas para mais throughput (testar 8 ou 16)
		"rsize=1048576",   // tamanho máximo de leitura (1 MiB)
		"wsize=1048576",   // tamanho máximo de escrita (1 MiB)
		"timeo=600",       // timeout base (60s) antes de reintentar
		"retrans=3",       // número de reintentos antes de declarar falha
		"noatime",         // não atualiza tempo de acesso de ficheiros (menos writes)
		"nodiratime",      // não atualiza tempo de acesso de diretórios
		"_netdev",         // indica dependência da rede (ordem de montagem correta)
		"nocto",           // desativa verificação close-to-open (melhor performance, menos coerência)
		"actimeo=30",      // cache de atributos por 30s (menos RPCs; aceitável p/ single-writer)
		"lookupcache=all", // guarda resultados de lookup positivos e negativos
		"fsc",             // ativa cache local em disco (requer cachefilesd ativo)
	}

	nfsServerOptions = []string{
		"rw",               // leitura/escrita
		"async",            // respostas antes de gravar em disco (máxima performance, menor segurança)
		"no_wdelay",        // não atrasa writes pequenos (melhor latência)
		"no_subtree_check", // evita verificações dispendiosas de subdiretórios
		"no_root_squash",   // permite que o root do cliente mantenha privilégios (necessário para libvirt/qemu)
		"insecure",         // aceita conexões de portas não privilegiadas (>1024)
		"sec=sys",          // autenticação simples por UID/GID (rápida, padrão em LAN segura)
	}

	localMountOpts = []string{
		"rw",         // leitura/escrita
		"noatime",    // não atualizar tempo de acesso (reduz IO)
		"nodiratime", // idem para diretórios
		"relatime",   // só atualiza atime se for mais antigo que mtime (compromisso equilibrado)
	}
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
	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()

	for range ticker.C {
		CurrentMountsLock.RLock()
		mounts := append([]FolderMount(nil), CurrentMounts...)
		CurrentMountsLock.RUnlock()

		for _, mount := range mounts {
			// Skip empty targets
			if strings.TrimSpace(mount.Target) == "" {
				continue
			}

			// Check if is mounted
			if !isMounted(mount.Target) {
				logger.Warn("NFS mount lost, attempting to remount:", mount.Target)
				success := false

				for attempt := 1; attempt <= monitorFailureThreshold; attempt++ {
					logger.Info("Remount attempt", "attempt", attempt, "of", monitorFailureThreshold, "target", mount.Target)

					err := MountSharedFolder(mount)
					if err == nil {
						success = true
						logger.Info("Successfully remounted NFS share on attempt", attempt, ":", mount.Target)
						break
					}

					logger.Warn("Remount attempt failed:", "attempt", attempt, "target", mount.Target, "error", err)

					// Don't sleep after last attempt
					if attempt < monitorFailureThreshold {
						time.Sleep(monitorInterval)
					}
				}

				if !success {
					logger.Error("Failed to remount NFS share after", monitorFailureThreshold, "attempts:", mount.Target)
					// Remove from CurrentMounts to avoid constant retry spam
					CurrentMountsLock.Lock()
					for i, m := range CurrentMounts {
						if m.Target == mount.Target {
							CurrentMounts = append(CurrentMounts[:i], CurrentMounts[i+1:]...)
							logger.Warn("Removed failed mount from tracking:", mount.Target)
							break
						}
					}
					CurrentMountsLock.Unlock()
				}
			}
		}
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
	return fmt.Sprintf("%s *(%s)", path, strings.Join(nfsServerOptions, ","))
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

	// Validate path for safety
	if err := IsSafePath(path); err != nil {
		return fmt.Errorf("invalid folder path: %w", err)
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

	// Remove the original folder so stale directories are not left behind.
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

func mountLocalFolder(folder FolderMount) error {
	logger.Info("Mounting local Folder with performance optimizations")

	parts := strings.SplitN(folder.Source, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid source format for local mount: %s (expected host:path)", folder.Source)
	}

	source := strings.TrimSpace(parts[1]) // Get the path part
	target := strings.TrimSpace(folder.Target)

	if source == "" || target == "" {
		return fmt.Errorf("source and target paths are required")
	}

	// Validate paths
	if err := IsSafePath(source); err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}
	if err := IsSafePath(target); err != nil {
		return fmt.Errorf("invalid target path: %w", err)
	}

	// Check if source exists
	if _, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source folder does not exist: %s", source)
		}
		return fmt.Errorf("failed to stat source folder: %w", err)
	}

	if err := runCommand("ensure mount directory", "sudo", "mkdir", "-p", target); err != nil {
		return err
	}

	ensureMountedWithOpts := func(remount bool) error {
		if remount {
			// Try remount in place first
			if err := runCommand("remount local folder with correct opts",
				"sudo", "mount", "-o", "remount,"+strings.Join(localMountOpts, ","), target); err == nil {
				return nil
			}
			// Fall back to full unmount + mount if remount failed
			logger.Warn("In-place remount failed, doing full unmount+mount")
			_ = runCommand("unmount local folder", "sudo", "umount", "-f", target)
		}
		// Fresh mount with bind + options
		if err := runCommand("bind mount local folder",
			"sudo", "mount", "--bind", source, target); err != nil {
			return fmt.Errorf("failed to bind mount: %w", err)
		}
		// Apply mount options on top of the bind mount
		if err := runCommand("apply mount options to local folder",
			"sudo", "mount", "-o", "remount,"+strings.Join(localMountOpts, ","), target); err != nil {
			return fmt.Errorf("failed to apply mount options: %w", err)
		}
		return nil
	}

	if isMounted(target) {
		// Validate current mount options; remount if needed
		mtab, err := os.ReadFile("/proc/mounts")
		if err != nil {
			logger.Warn("Failed to read /proc/mounts, assuming remount needed:", err)
			return ensureMountedWithOpts(true)
		}

		targetEsc := strings.ReplaceAll(target, " ", "\\040")
		lines := strings.Split(string(mtab), "\n")
		needsRemount := false
		foundMount := false

		for _, ln := range lines {
			if ln == "" {
				continue
			}
			fields := strings.Fields(ln)
			if len(fields) < 4 {
				continue
			}
			mp := fields[1]
			if mp == targetEsc {
				foundMount = true
				cur := "," + fields[3] + ","
				// Check against expected localMountOpts
				for _, opt := range localMountOpts {
					if !strings.Contains(cur, ","+opt+",") {
						needsRemount = true
						logger.Warn("Missing expected mount option:", opt)
						break
					}
				}
				break
			}
		}

		if !foundMount {
			logger.Warn("Mount point exists but not found in /proc/mounts, remounting")
			needsRemount = true
		}

		if !needsRemount {
			logger.Info("Local folder already mounted with correct options:", source, "->", target)
			// Ensure it's in CurrentMounts (idempotent)
			CurrentMountsLock.Lock()
			alreadyTracked := false
			for _, m := range CurrentMounts {
				if m.Target == target {
					alreadyTracked = true
					break
				}
			}
			if !alreadyTracked {
				CurrentMounts = append(CurrentMounts, folder)
			}
			CurrentMountsLock.Unlock()
			return nil
		}

		logger.Warn("Remounting local folder with correct options:", target)
		if err := ensureMountedWithOpts(true); err != nil {
			return fmt.Errorf("failed to remount local folder: %w", err)
		}
		// Update CurrentMounts after successful remount
		CurrentMountsLock.Lock()
		alreadyTracked := false
		for _, m := range CurrentMounts {
			if m.Target == target {
				alreadyTracked = true
				break
			}
		}
		if !alreadyTracked {
			CurrentMounts = append(CurrentMounts, folder)
		}
		CurrentMountsLock.Unlock()
		return nil
	}

	// First-time mount with correct options
	if err := ensureMountedWithOpts(false); err != nil {
		return fmt.Errorf("failed to mount local folder: %w", err)
	}

	// Only add to CurrentMounts after successful mount
	CurrentMountsLock.Lock()
	alreadyTracked := false
	for _, m := range CurrentMounts {
		if m.Target == target {
			alreadyTracked = true
			break
		}
	}
	if !alreadyTracked {
		CurrentMounts = append(CurrentMounts, folder)
	}
	CurrentMountsLock.Unlock()

	logger.Info("Local folder mounted successfully with optimizations:", source, "->", target)
	return nil
}

func MountSharedFolder(folder FolderMount) error {
	// Validate and trim inputs first
	source := strings.TrimSpace(folder.Source)
	target := strings.TrimSpace(folder.Target)
	if source == "" || target == "" {
		return fmt.Errorf("source and target are required")
	}

	// Handle local mount if HostNormalMount is enabled and source IP matches this slave
	if folder.HostNormalMount {
		// Validate source format (must contain ":")
		if !strings.Contains(source, ":") {
			return fmt.Errorf("invalid NFS source format: %s (expected host:path)", source)
		}

		ip := strings.Split(source, ":")[0]
		if ip == env512.SlaveIP || ip == "localhost" || ip == "127.0.0.1" {
			logger.Info("Local mount detected (IP matches this slave):", ip)
			return mountLocalFolder(folder)
		}
	}

	opts := append([]string(nil), nfsMountOptions...)

	// Ensure mount point exists BEFORE checking if it's mounted
	if err := IsSafePath(target); err != nil {
		return fmt.Errorf("invalid target path: %w", err)
	}
	if err := runCommand("ensure mount directory", "sudo", "mkdir", "-p", target); err != nil {
		return err
	}

	ensureMountedWithOpts := func(remount bool) error {
		if remount {
			// Try remount in place first
			if err := runCommand("remount nfs share with correct opts",
				"sudo", "mount", "-t", "nfs4", "-o", "remount,"+strings.Join(opts, ","), source, target); err == nil {
				return nil
			}
			// Fall back to full unmount + mount if remount failed
			logger.Warn("In-place remount failed, doing full unmount+mount")
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
		mtab, err := os.ReadFile("/proc/mounts")
		if err != nil {
			logger.Warn("Failed to read /proc/mounts, assuming remount needed:", err)
			return ensureMountedWithOpts(true)
		}

		targetEsc := strings.ReplaceAll(target, " ", "\\040") // how /proc/mounts escapes spaces
		lines := strings.Split(string(mtab), "\n")
		needsRemount := false
		foundMount := false

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
				foundMount = true
				cur := "," + fields[3] + ","
				// Check against expected nfsMountOptions
				for _, opt := range nfsMountOptions {
					// Handle options with values (e.g., "rsize=1048576")
					if strings.Contains(opt, "=") {
						optKey := strings.Split(opt, "=")[0]
						// Check if the option key exists in current mounts
						if !strings.Contains(cur, ","+optKey+"=") {
							needsRemount = true
							logger.Warn("Missing expected mount option:", opt)
							break
						}
					} else {
						// Simple option without value
						if !strings.Contains(cur, ","+opt+",") {
							needsRemount = true
							logger.Warn("Missing expected mount option:", opt)
							break
						}
					}
				}
				break
			}
		}

		if !foundMount {
			logger.Warn("Mount point exists but not found in /proc/mounts, remounting")
			needsRemount = true
		}

		if !needsRemount {
			logger.Info("NFS share already mounted with correct options:", source, "->", target)
			// Ensure it's in CurrentMounts (idempotent)
			CurrentMountsLock.Lock()
			alreadyTracked := false
			for _, m := range CurrentMounts {
				if m.Target == target {
					alreadyTracked = true
					break
				}
			}
			if !alreadyTracked {
				CurrentMounts = append(CurrentMounts, folder)
			}
			CurrentMountsLock.Unlock()
			return nil
		}

		logger.Warn("Remounting NFS with correct options:", target)
		if err := ensureMountedWithOpts(true); err != nil {
			return fmt.Errorf("failed to remount NFS share: %w", err)
		}
		// Update CurrentMounts after successful remount
		CurrentMountsLock.Lock()
		alreadyTracked := false
		for _, m := range CurrentMounts {
			if m.Target == target {
				alreadyTracked = true
				break
			}
		}
		if !alreadyTracked {
			CurrentMounts = append(CurrentMounts, folder)
		}
		CurrentMountsLock.Unlock()
		return nil
	}

	// First-time mount with correct options
	if err := ensureMountedWithOpts(false); err != nil {
		return fmt.Errorf("failed to mount NFS share: %w", err)
	}

	// Only add to CurrentMounts after successful mount
	CurrentMountsLock.Lock()
	alreadyTracked := false
	for _, m := range CurrentMounts {
		if m.Target == target {
			alreadyTracked = true
			break
		}
	}
	if !alreadyTracked {
		CurrentMounts = append(CurrentMounts, folder)
	}
	CurrentMountsLock.Unlock()

	logger.Info("NFS share mounted successfully:", source, "->", target)
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

	//delete target folder if it can be deleted
	_ = os.Remove(folder.Target)
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

func fastHTTPClient() *http.Client {
	tr := &http.Transport{
		// Dial rápido com keep-alives agressivos
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 60 * time.Second,
			// Buffers do SO (deixa o kernel auto-tunar; se quiseres, ajusta aqui)
		}).DialContext,
		ForceAttemptHTTP2:     true, // HTTPS → tenta HTTP/2 (melhor multiplexing)
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   200,
		MaxConnsPerHost:       0, // 0 = sem limite (deixa o cliente gerir)
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true, // não interessa para binários
		ExpectContinueTimeout: 0,    // evita espera extra
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// HTTP/3 não é suportado por net/http puro; para isso usar quic-go (extra).
		},
	}
	return &http.Client{
		Transport: tr,
		Timeout:   0, // sem timeout global; usa o ctx do pedido
	}
}

func downloadFile(ctx context.Context, url, destPath string) (err error) {
	start := time.Now()
	fileName := filepath.Base(destPath)
	defer func() {
		elapsed := time.Since(start).Round(time.Second)
		if elapsed == 0 {
			elapsed = time.Since(start)
		}
		if err != nil {
			extra.SendNotifications(
				"ISO download failed",
				fmt.Sprintf("Failed to download %s from %s to %s: %v", fileName, url, destPath, err),
				"/",
				true,
			)
		} else {
			extra.SendNotifications(
				"ISO download succeeded",
				fmt.Sprintf("Downloaded %s to %s in %s", fileName, destPath, elapsed),
				"/",
				false,
			)
		}
	}()

	// FAIL-FAST se o ficheiro já existe
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("file already exists: %s", destPath)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	client := grab.NewClient()
	client.HTTPClient = fastHTTPClient()

	req, err := grab.NewRequest(destPath, url)
	if err != nil {
		return err
	}
	req.HTTPRequest.Header.Set("User-Agent",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "+
			"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	// Evita encodings/negociações esquisitas
	req.HTTPRequest.Header.Set("Accept", "*/*")
	req.HTTPRequest.Header.Del("Accept-Encoding") // n/comprimir

	// Contexto para cancelamento
	req = req.WithContext(ctx)

	resp := client.Do(req)
	if resp.HTTPResponse != nil {
		fmt.Printf("HTTP %s\n", resp.HTTPResponse.Status)
	}

	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "Download canceled")
			return ctx.Err()
		case <-t.C:
			// progresso
			extra.SendWebsocketMessage(
				fmt.Sprintf("Download: %d / %d (%.2f%%) - %.2f MB/s",
					resp.BytesComplete(),
					resp.Size(),
					100*resp.Progress(),
					float64(resp.BytesPerSecond())/1024/1024),
				extraGrpc.WebSocketsMessageType_DownloadIso,
			)
		case <-resp.Done:
			if err := resp.Err(); err != nil {
				return fmt.Errorf("download failed: %w", err)
			}
			fmt.Printf("Saved to %s\n", resp.Filename)
			return nil
		}
	}
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
	extra.SendNotifications(
		"ISO download started",
		fmt.Sprintf("Downloading %s from %s to %s", isoName, url, isoPath),
		"/",
		false,
	)

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
	target := strings.TrimSpace(folder.Target)
	if target == "" {
		return nil, fmt.Errorf("target path is required")
	}

	var status SharedFolderStatus

	// Check if the target is mounted (not the source FolderPath)
	if !isMounted(target) {
		status.Working = false
		status.SpaceOccupiedGB = -1
		status.SpaceFreeGB = -1
		status.SpaceTotalGB = -1
		return &status, nil
	}
	status.Working = true

	// Use target (mount point) for filesystem stats, not the source path
	var statfs syscall.Statfs_t
	if err := syscall.Statfs(target, &statfs); err != nil {
		return nil, fmt.Errorf("failed to get filesystem stats for %s: %w", target, err)
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

func Sync() error {
	return runCommand("sync filesystem", "sync")
}
