package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"slave/env512"
	"slave/logs512"
	"slave/nfs"
	"slave/protocol"
	"slave/virsh"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
)

func askForSudo() {
	//if current program is not sudo terminate
	if os.Geteuid() != 0 {
		fmt.Println("This program needs to be run as root.")
		os.Exit(0)
	}
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
		keyFile    = "/root/.ssh/id_rsa_512svman"
		configPath = "/root/.ssh/config"
	)

	if err := ensureSSHKey(keyFile); err != nil {
		return fmt.Errorf("ensure ssh key: %w", err)
	}

	pubKeyFile := keyFile + ".pub"

	for _, ip := range env512.OTHER_SLAVES {
		for {
			if err := ensureAuthorizedKey(ip, keyFile, pubKeyFile); err != nil {
				logger.Error(fmt.Sprintf("ensure authorized key for %s: %v", ip, err))
				continue
			}

			if err := ensureHostConfig(configPath, ip, keyFile); err != nil {
				logger.Error(fmt.Sprintf("ensure ssh config for %s: %v", ip, err))
				continue
			}
			break
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

func main() {
	askForSudo()


	//varsc
	if err := env512.Setup(); err != nil {
		log.Fatalf("env setup: %v", err)
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

	conn := protocol.ConnectGRPC()
	env512.SetConn(conn)
	defer conn.Close()
	select {}
}
