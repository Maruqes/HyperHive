package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slave/btrfs"
	"slave/env512"
	"slave/extra"
	"slave/logs512"
	"slave/nfs"
	"slave/protocol"
	"slave/virsh"
	"strings"
	"time"

	"github.com/Maruqes/512SvMan/logger"
)

const (
	virtioISOURL          = "https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/latest-virtio/virtio-win.iso"
	virtioISOName         = "virtio-win.iso"
	virtioDownloadDir     = "downloads"
	virtioTempFilePattern = "virtio-*.iso"

	vmDirtyRatioPath           = "/proc/sys/vm/dirty_ratio"
	vmDirtyBackgroundRatioPath = "/proc/sys/vm/dirty_background_ratio"
	defaultDirtyRatio          = 15
)

func askForSudo() {
	//if current program is not sudo terminate
	if os.Geteuid() != 0 {
		fmt.Println("This program needs to be run as root.")
		os.Exit(0)
	}
}

func applyDirtyRatioSettings(ratio, background int) error {
	setRatio := ratio
	if setRatio <= 0 {
		setRatio = defaultDirtyRatio
	}
	if setRatio > 100 {
		return fmt.Errorf("dirty ratio percent %d must be between 1 and 100", setRatio)
	}

	if err := writeSysctlValue(vmDirtyRatioPath, setRatio); err != nil {
		return err
	}

	setBackground := background
	if setBackground <= 0 {
		setBackground = setRatio / 4
		if setBackground < 1 {
			setBackground = 1
		}
	}
	if setBackground > 100 {
		return fmt.Errorf("dirty background ratio %d must be between 1 and 100", setBackground)
	}

	return writeSysctlValue(vmDirtyBackgroundRatioPath, setBackground)
}

func writeSysctlValue(path string, value int) error {
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d", value)), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

/*
sudo ssh-keygen -t rsa -b 4096 -f /root/.ssh/id_rsa_512svman -N ""
sudo virsh -c qemu:///system migrate --persistent --verbose --undefinesource --p2p --tunnelled --live plsfunfa qemu+ssh://root@192.168.1.125/system
sudo ssh -i /root/.ssh/id_rsa_512svman -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null root@192.168.1.125 "echo 'SSH funcionando'"

sudo bash -c 'cat > /root/.ssh/config << EOF
Host 192.168.1.125

	IdentityFile /root/.ssh/id_rsa_512svman
	StrictHostKeyChecking no
	UserKnownHostsFile /dev/null

EOF'

sudo chmod 600 /root/.ssh/config
*/
func setupSSHKeys() error {
	const (
		keyFile      = "/root/.ssh/id_rsa_512svman"
		configPath   = "/root/.ssh/config"
		maxAttempts  = 5
		attemptDelay = 2 * time.Second
	)

	if err := ensureSSHKey(keyFile); err != nil {
		return fmt.Errorf("ensure ssh key: %w", err)
	}

	pubKeyFile := keyFile + ".pub"

	for _, ip := range env512.OTHER_SLAVES {
		success := false
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if err := ensureAuthorizedKey(ip, keyFile, pubKeyFile); err != nil {
				logger.Error(fmt.Sprintf("ensure authorized key for %s (attempt %d/%d): %v", ip, attempt, maxAttempts, err))
			} else if err := ensureHostConfig(configPath, ip, keyFile); err != nil {
				logger.Error(fmt.Sprintf("ensure ssh config for %s (attempt %d/%d): %v", ip, attempt, maxAttempts, err))
			} else {
				success = true
				break
			}

			if attempt < maxAttempts {
				time.Sleep(attemptDelay)
			}
		}

		if !success {
			logger.Error(fmt.Sprintf("skipping SSH setup for %s after %d attempts", ip, maxAttempts))
		}
	}
	return nil
}

func ensureSSHKey(keyFile string) error {
	pubKeyPath := keyFile + ".pub"

	info, err := os.Stat(keyFile)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return fmt.Errorf("ssh key path is not a regular file: %s", keyFile)
		}

		valid, err := isValidPrivateKey(keyFile)
		if err != nil {
			return err
		}
		if !valid {
			if err := os.Remove(keyFile); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove invalid ssh key: %w", err)
			}
			if err := os.Remove(pubKeyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove invalid public ssh key: %w", err)
			}
			info = nil
		}
	case errors.Is(err, os.ErrNotExist):
		info = nil
	default:
		return fmt.Errorf("stat ssh key: %w", err)
	}

	if info == nil {
		if err := exec.Command("ssh-keygen", "-t", "rsa", "-b", "4096", "-f", keyFile, "-N", "").Run(); err != nil {
			return fmt.Errorf("generate ssh key: %w", err)
		}
	}

	if ok, err := isValidPublicKey(pubKeyPath); err != nil {
		return err
	} else if !ok {
		if err := regeneratePublicKey(keyFile); err != nil {
			return err
		}
	}

	return nil
}

func ensureHostConfig(configPath, ip, keyFile string) error {
	configured, err := isHostConfigured(configPath, ip)
	if err != nil {
		return fmt.Errorf("check ssh config: %w", err)
	}
	if configured {
		return nil
	}

	if err := appendHostConfig(configPath, ip, keyFile); err != nil {
		return fmt.Errorf("update ssh config: %w", err)
	}
	return nil
}

func isHostConfigured(configPath, ip string) (bool, error) {
	file, err := os.Open(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open ssh config: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Host ") {
			host := strings.TrimSpace(strings.TrimPrefix(line, "Host "))
			if host == ip {
				return true, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read ssh config: %w", err)
	}

	return false, nil
}

func isValidPrivateKey(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read private ssh key: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return false, nil
	}

	if !strings.Contains(trimmed, "BEGIN OPENSSH PRIVATE KEY") && !strings.Contains(trimmed, "BEGIN RSA PRIVATE KEY") {
		return false, nil
	}

	return true, nil
}

func isValidPublicKey(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read public ssh key: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return false, nil
	}

	if !strings.HasPrefix(trimmed, "ssh-") {
		return false, nil
	}

	return true, nil
}

func ensureAuthorizedKey(ip, keyFile, pubKeyFile string) error {
	if err := testSSHConnection(ip, keyFile); err == nil {
		return nil
	}

	if err := copyPublicKeyToHost(ip, pubKeyFile); err != nil {
		return err
	}

	if err := testSSHConnection(ip, keyFile); err != nil {
		return fmt.Errorf("ssh test after installing key: %w", err)
	}

	return nil
}

func testSSHConnection(ip, keyFile string) error {
	cmd := exec.Command(
		"ssh",
		"-i", keyFile,
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"root@"+ip,
		"echo ok",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		out := strings.TrimSpace(string(output))
		if out != "" {
			return fmt.Errorf("ssh root@%s: %w (output: %s)", ip, err, out)
		}
		return fmt.Errorf("ssh root@%s: %w", ip, err)
	}
	return nil
}

func copyPublicKeyToHost(ip, pubKeyFile string) error {
	if _, err := os.Stat(pubKeyFile); err != nil {
		return fmt.Errorf("stat public ssh key: %w", err)
	}

	if _, err := exec.LookPath("ssh-copy-id"); err != nil {
		return fmt.Errorf("ssh-copy-id not found in PATH: %w", err)
	}

	fmt.Printf("Installing SSH key on %s (root). You may be prompted for the password.\n", ip)
	cmd := exec.Command(
		"ssh-copy-id",
		"-i", pubKeyFile,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"root@"+ip,
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh-copy-id root@%s: %w", ip, err)
	}

	return nil
}

func appendHostConfig(configPath, ip, keyFile string) error {
	hostBlock := fmt.Sprintf("Host %s\n    IdentityFile %s\n    StrictHostKeyChecking no\n    UserKnownHostsFile /dev/null\n", ip, keyFile)

	info, err := os.Stat(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat ssh config: %w", err)
	}

	file, err := os.OpenFile(configPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open ssh config for write: %w", err)
	}
	defer file.Close()

	if info != nil && info.Size() > 0 {
		if _, err := file.WriteString("\n"); err != nil {
			return fmt.Errorf("write newline to ssh config: %w", err)
		}
	}

	if _, err := file.WriteString(hostBlock); err != nil {
		return fmt.Errorf("append ssh config block: %w", err)
	}

	if err := os.Chmod(configPath, 0600); err != nil {
		return fmt.Errorf("chmod ssh config: %w", err)
	}

	return nil
}

func regeneratePublicKey(keyFile string) error {
	cmd := exec.Command("ssh-keygen", "-y", "-f", keyFile)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("regenerate public ssh key: %w", err)
	}

	pubKeyPath := keyFile + ".pub"
	if len(output) == 0 || output[len(output)-1] != '\n' {
		output = append(output, '\n')
	}
	if err := os.Chmod(keyFile, 0600); err != nil {
		return fmt.Errorf("chmod private ssh key: %w", err)
	}
	if err := os.WriteFile(pubKeyPath, output, 0644); err != nil {
		return fmt.Errorf("write public ssh key: %w", err)
	}
	return nil
}

/*
INSTALL THINGS THAT ARE NEEDED TO THE FULL APP FUNCTIONALITY
sudo dnf install -y xmlstarlet
*/
func setupAll() error {
	if err := btrfs.InstallBTRFS(); err != nil {
		log.Fatal("Error installing zfs: ", err)
	}

	// Install xmlstarlet
	if err := exec.Command("dnf", "install", "-y", "xmlstarlet").Run(); err != nil {
		return fmt.Errorf("failed to install xmlstarlet: %w", err)
	}
	if err := exec.Command("sudo", "setenforce", "0").Run(); err != nil {
		return fmt.Errorf("failed to setenforce 0: %w", err)
	}
	if err := setupSSHKeys(); err != nil {
		return fmt.Errorf("setup ssh keys: %w", err)
	}

	//allow port 500511

	// with firewalld, open VNC port range only for this boot (runtime only)
	portRange := fmt.Sprintf("%d-%d/tcp", env512.VNC_MIN_PORT, env512.VNC_MAX_PORT)
	if err := exec.Command("firewall-cmd", "--add-port="+portRange).Run(); err != nil {
		return fmt.Errorf("failed to add runtime firewall rule for vnc ports: %w", err)
	}

	//adicionar porta 50051 e 50052
	if err := exec.Command("firewall-cmd", "--add-port=50051/tcp").Run(); err != nil {
		return fmt.Errorf("failed to add runtime firewall rule for port 50051: %w", err)
	}
	if err := exec.Command("firewall-cmd", "--add-port=50052/tcp").Run(); err != nil {
		return fmt.Errorf("failed to add runtime firewall rule for port 50052: %w", err)
	}

	return nil
}
func set_host_uuid_source() error {
	const path = "/etc/libvirt/virtqemud.conf"

	current, _ := os.ReadFile(path)
	text := string(current)

	reSrc := regexp.MustCompile(`(?m)^\s*#?\s*host_uuid_source\s*=.*$`)
	if reSrc.MatchString(text) {
		text = reSrc.ReplaceAllString(text, `host_uuid_source = "machine-id"`)
	} else {
		if len(text) > 0 && text[len(text)-1] != '\n' {
			text += "\n"
		}
		text += `host_uuid_source = "machine-id"` + "\n"
	}

	reHost := regexp.MustCompile(`(?m)^\s*host_uuid\s*=.*$`)
	text = reHost.ReplaceAllStringFunc(text, func(line string) string {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "#") {
			return line
		}
		return "# " + line
	})

	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		return fmt.Errorf("escrever %s: %w", path, err)
	}
	if err := exec.Command("systemctl", "reload", "virtqemud").Run(); err != nil {
		if err2 := exec.Command("systemctl", "restart", "virtqemud").Run(); err2 != nil {
			return fmt.Errorf("reload/restart virtqemud falhou: %v / %v", err, err2)
		}
	}
	return nil
}

func ensureVirtioISO() (destPath string, err error) {
	start := time.Now()
	var didDownload bool
	var resultNote string
	defer func() {
		title := "Ensure VirtIO ISO"
		success := err == nil
		elapsed := time.Since(start).Round(time.Millisecond)
		if elapsed == 0 {
			elapsed = time.Since(start)
		}
		var msg string
		if success {
			state := resultNote
			if state == "" {
				if didDownload {
					state = "downloaded"
				} else {
					state = "ready"
				}
			}
			msg = fmt.Sprintf("VirtIO ISO %s at %s (took %s)", state, destPath, elapsed)
		} else {
			msg = fmt.Sprintf("Failed to ensure VirtIO ISO: %v", err)
		}
		extra.SendNotifications(title, msg, "/", !success)
	}()
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working dir: %w", err)
	}

	downloadDir := filepath.Join(wd, virtioDownloadDir)
	absDownloadDir, err := filepath.Abs(downloadDir)
	if err != nil {
		return "", fmt.Errorf("resolve download dir: %w", err)
	}
	if err := os.MkdirAll(absDownloadDir, 0o755); err != nil {
		return "", fmt.Errorf("create download dir: %w", err)
	}

	target := filepath.Join(absDownloadDir, virtioISOName)
	libvirtDir := "/var/lib/libvirt/boot"
	if err := os.MkdirAll(libvirtDir, 0o755); err != nil {
		return "", fmt.Errorf("create libvirt boot dir: %w", err)
	}
	dest := filepath.Join(libvirtDir, virtioISOName)
	destPath = dest
	metaPath := dest + ".etag"

	remoteInfo, headErr := fetchRemoteVirtioInfo()

	destInfo, destErr := os.Stat(dest)
	needDownload := errors.Is(destErr, os.ErrNotExist)
	if destErr != nil && !errors.Is(destErr, os.ErrNotExist) {
		return "", fmt.Errorf("stat virtio iso dest: %w", destErr)
	}

	if !needDownload && headErr == nil {
		if remoteInfo.ETag != "" {
			localETag := strings.TrimSpace(readETag(metaPath))
			if localETag != remoteInfo.ETag {
				needDownload = true
			}
		} else if remoteInfo.ContentLength > 0 && destInfo.Size() != remoteInfo.ContentLength {
			needDownload = true
		}
	}

	if needDownload {
		logger.Info("Starting VirtIO ISO download from " + virtioISOURL)

		tmpFile, err := os.CreateTemp(absDownloadDir, virtioTempFilePattern)
		if err != nil {
			return "", fmt.Errorf("create temp virtio iso: %w", err)
		}
		defer os.Remove(tmpFile.Name())

		resp, err := http.Get(virtioISOURL)
		if err != nil {
			return "", fmt.Errorf("download virtio iso: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("download virtio iso: unexpected status %s", resp.Status)
		}

		totalSize := resp.ContentLength
		if totalSize > 0 {
			logger.Info(fmt.Sprintf("VirtIO ISO size: %.2f MB", float64(totalSize)/(1024*1024)))
		}

		// Download with progress tracking
		startTime := time.Now()
		var downloadedBytes int64
		lastLogTime := startTime
		lastLogBytes := int64(0)

		buffer := make([]byte, 32*1024) // 32KB buffer
		for {
			n, err := resp.Body.Read(buffer)
			if n > 0 {
				if _, writeErr := tmpFile.Write(buffer[:n]); writeErr != nil {
					tmpFile.Close()
					return "", fmt.Errorf("save virtio iso: %w", writeErr)
				}
				downloadedBytes += int64(n)

				// Log progress every 2 seconds or at completion
				now := time.Now()
				if now.Sub(lastLogTime) >= 2*time.Second || err == io.EOF {
					elapsed := now.Sub(startTime).Seconds()
					if elapsed > 0 {
						avgSpeed := float64(downloadedBytes) / elapsed / (1024 * 1024)                                         // MB/s
						instantSpeed := float64(downloadedBytes-lastLogBytes) / now.Sub(lastLogTime).Seconds() / (1024 * 1024) // MB/s

						if totalSize > 0 {
							percentage := float64(downloadedBytes) / float64(totalSize) * 100
							logger.Info(fmt.Sprintf("Downloading VirtIO ISO: %.1f%% (%.2f/%.2f MB) - Speed: %.2f MB/s (avg: %.2f MB/s)",
								percentage,
								float64(downloadedBytes)/(1024*1024),
								float64(totalSize)/(1024*1024),
								instantSpeed,
								avgSpeed))
						} else {
							logger.Info(fmt.Sprintf("Downloading VirtIO ISO: %.2f MB - Speed: %.2f MB/s (avg: %.2f MB/s)",
								float64(downloadedBytes)/(1024*1024),
								instantSpeed,
								avgSpeed))
						}
						lastLogTime = now
						lastLogBytes = downloadedBytes
					}
				}
			}

			if err == io.EOF {
				break
			}
			if err != nil {
				tmpFile.Close()
				return "", fmt.Errorf("read virtio iso during download: %w", err)
			}
		}

		totalTime := time.Since(startTime).Seconds()
		avgSpeed := float64(downloadedBytes) / totalTime / (1024 * 1024)
		logger.Info(fmt.Sprintf("VirtIO ISO download completed: %.2f MB in %.1fs (avg: %.2f MB/s)",
			float64(downloadedBytes)/(1024*1024), totalTime, avgSpeed))

		if err := tmpFile.Close(); err != nil {
			return "", fmt.Errorf("finalize virtio iso: %w", err)
		}

		if err := os.Rename(tmpFile.Name(), target); err != nil {
			return "", fmt.Errorf("place virtio iso: %w", err)
		}
		if err := os.Chmod(target, 0o644); err != nil {
			return "", fmt.Errorf("chmod virtio iso: %w", err)
		}

		logger.Info("VirtIO ISO saved to " + target)
		didDownload = true
		resultNote = "downloaded"
	} else if errors.Is(destErr, os.ErrNotExist) {
		return "", fmt.Errorf("virtio iso missing and remote head failed")
	} else {
		logger.Info("VirtIO ISO already up to date at " + dest)
		resultNote = "already up to date"
	}

	// ensure cache copy exists for troubleshooting
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) && destErr == nil {
		logger.Info("Syncing VirtIO ISO to cache directory")
		if err := copyFile(dest, target); err != nil {
			return "", fmt.Errorf("sync virtio cache: %w", err)
		}
	}

	// Copy to libvirt directory if needed
	if needDownload || errors.Is(destErr, os.ErrNotExist) {
		logger.Info("Copying VirtIO ISO to libvirt boot directory")
		srcFile, err := os.Open(target)
		if err != nil {
			return "", fmt.Errorf("open virtio iso for copy: %w", err)
		}
		defer srcFile.Close()

		destFile, err := os.Create(dest)
		if err != nil {
			return "", fmt.Errorf("create virtio iso at libvirt dir: %w", err)
		}

		if _, err := io.Copy(destFile, srcFile); err != nil {
			destFile.Close()
			return "", fmt.Errorf("copy virtio iso to libvirt dir: %w", err)
		}
		if err := destFile.Close(); err != nil {
			return "", fmt.Errorf("finalize virtio iso at libvirt dir: %w", err)
		}
		if err := os.Chmod(dest, 0o644); err != nil {
			return "", fmt.Errorf("chmod virtio iso at libvirt dir: %w", err)
		}
		logger.Info("VirtIO ISO copied to " + dest)
	}

	if headErr == nil && remoteInfo.ETag != "" {
		_ = os.WriteFile(metaPath, []byte(remoteInfo.ETag+"\n"), 0o644)
	}

	return dest, nil
}

type virtioRemoteInfo struct {
	ETag          string
	ContentLength int64
}

func fetchRemoteVirtioInfo() (virtioRemoteInfo, error) {
	resp, err := http.Head(virtioISOURL)
	if err != nil {
		return virtioRemoteInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return virtioRemoteInfo{}, fmt.Errorf("head virtio iso: status %s", resp.Status)
	}

	etag := strings.TrimSpace(resp.Header.Get("ETag"))
	etag = strings.Trim(etag, "\"")
	return virtioRemoteInfo{
		ETag:          etag,
		ContentLength: resp.ContentLength,
	}, nil
}

func readETag(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, 0o644)
}

func main() {
	askForSudo()

	//varsc
	if err := env512.Setup(); err != nil {
		log.Fatalf("env setup: %v", err)
	}

	if err := applyDirtyRatioSettings(env512.DirtyRatioPercent, env512.DirtyBackgroundRatioPercent); err != nil {
		log.Fatalf("apply dirty ratio settings: %v", err)
	}

	if err := virsh.SetVNCPorts(env512.VNC_MIN_PORT, env512.VNC_MAX_PORT); err != nil {
		log.Fatalf("set vnc ports: %v", err)
	}

	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	if err := nfs.InstallNFS(); err != nil {
		log.Fatalf("failed to install NFS: %v", err)
	}

	if err := setupAll(); err != nil {
		log.Fatalf("setup all: %v", err)
	}

	isoPath, err := ensureVirtioISO()
	if err != nil {
		log.Fatalf("ensure virtio iso: %v", err)
	}
	env512.VirtioISOPath = isoPath

	err = set_host_uuid_source()
	if err != nil {
		logger.Error(err.Error())
	}

	conn := protocol.ConnectGRPC()
	env512.SetConn(conn)
	extra.SendNotifications(fmt.Sprintf("%s connected", env512.MachineName), "Machine connected", "/", false)
	defer conn.Close()
	select {}
}
