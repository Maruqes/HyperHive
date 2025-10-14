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

	for _, ip := range env512.OTHER_SLAVES {
		needsConfig, err := hostNeedsConfig(configPath, ip, keyFile)
		if err != nil {
			return fmt.Errorf("check ssh config for %s: %w", ip, err)
		}
		if needsConfig {
			if err := appendHostConfig(configPath, ip, keyFile); err != nil {
				return fmt.Errorf("failed to update ssh config for %s: %w", ip, err)
			}
		}

		if err := verifySSHConnection(ip, keyFile); err != nil {
			return fmt.Errorf("failed to test ssh connection to %s: %w", ip, err)
		}
	}
	return nil
}

func ensureSSHKey(keyFile string) error {
	if _, err := os.Stat(keyFile); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat ssh key: %w", err)
	}

	if err := exec.Command("ssh-keygen", "-t", "rsa", "-b", "4096", "-f", keyFile, "-N", "").Run(); err != nil {
		return fmt.Errorf("generate ssh key: %w", err)
	}

	return nil
}

func hostNeedsConfig(configPath, ip, keyFile string) (bool, error) {
	file, err := os.Open(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("open ssh config: %w", err)
	}
	defer file.Close()

	var inHostBlock bool
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Host ") {
			host := strings.TrimSpace(strings.TrimPrefix(line, "Host "))
			inHostBlock = host == ip
			continue
		}
		if inHostBlock && strings.HasPrefix(line, "IdentityFile") {
			identity := strings.TrimSpace(strings.TrimPrefix(line, "IdentityFile"))
			if identity == keyFile {
				return false, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read ssh config: %w", err)
	}

	return true, nil
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

func verifySSHConnection(ip, keyFile string) error {
	cmd := exec.Command("ssh",
		"-i", keyFile,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"root@"+ip,
		"echo 'SSH funcionando'",
	)
	if err := cmd.Run(); err != nil {
		return err
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
	if err := setupSSHKeys(); err != nil {
		return fmt.Errorf("setup ssh keys: %w", err)
	}

	return nil
}

func main() {
	askForSudo()

	if err := env512.Setup(); err != nil {
		log.Fatalf("env setup: %v", err)
	}

	if err := setupAll(); err != nil {
		log.Fatalf("setup all: %v", err)
	}

	if err := virsh.SetVNCPorts(env512.VNC_MIN_PORT, env512.VNC_MAX_PORT); err != nil {
		log.Fatalf("set vnc ports: %v", err)
	}

	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	if err := nfs.InstallNFS(); err != nil {
		log.Fatalf("failed to install NFS: %v", err)
	}

	conn := protocol.ConnectGRPC()
	defer conn.Close()
	select {}
}
