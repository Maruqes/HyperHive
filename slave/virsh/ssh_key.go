package virsh

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	libvirt "libvirt.org/go/libvirt"
)

var supportedSSHKeyTypes = map[string]bool{
	"ssh-rsa":             true,
	"ssh-ed25519":         true,
	"ecdsa-sha2-nistp256": true,
	"ecdsa-sha2-nistp384": true,
	"ecdsa-sha2-nistp521": true,
}

func AddSSHKey(vmName, sshKey string) error {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return fmt.Errorf("vm name is empty")
	}

	normalizedKey, err := normalizeSSHPublicKey(sshKey)
	if err != nil {
		return err
	}

	if _, err := exec.LookPath("virt-customize"); err != nil {
		return fmt.Errorf("virt-customize not found: %w", err)
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}
	if state != libvirt.DOMAIN_SHUTOFF {
		return fmt.Errorf("domain %s must be shut off to customize disk", vmName)
	}

	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		xmlDesc, err = dom.GetXMLDesc(0)
		if err != nil {
			return fmt.Errorf("get domain xml: %w", err)
		}
	}

	diskPath, err := diskPathFromDomainXML(xmlDesc)
	if err != nil {
		return fmt.Errorf("detect disk path: %w", err)
	}
	if diskPath == "" {
		return fmt.Errorf("no primary file-backed disk found for vm %s", vmName)
	}
	if err := ensureFileExists(diskPath); err != nil {
		return fmt.Errorf("validate disk path: %w", err)
	}

	scriptPath, err := writeAddSSHKeyScript(normalizedKey)
	if err != nil {
		return err
	}
	defer os.Remove(scriptPath)

	cmd := exec.Command("virt-customize", "-a", diskPath, "--no-network", "--run", scriptPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("virt-customize add ssh key: %s", msg)
		}
		return fmt.Errorf("virt-customize add ssh key: %w", err)
	}

	return nil
}

func normalizeSSHPublicKey(sshKey string) (string, error) {
	sshKey = strings.TrimSpace(sshKey)
	if sshKey == "" {
		return "", fmt.Errorf("ssh public key is empty")
	}
	if strings.ContainsAny(sshKey, "\r\n\x00") {
		return "", fmt.Errorf("ssh public key must be a single line")
	}

	fields := strings.Fields(sshKey)
	if len(fields) < 2 {
		return "", fmt.Errorf("ssh public key must contain key type and key data")
	}

	keyType := fields[0]
	keyData := fields[1]
	if !supportedSSHKeyTypes[keyType] {
		return "", fmt.Errorf("unsupported ssh public key type %q", keyType)
	}
	if keyData == "" {
		return "", fmt.Errorf("ssh public key data is empty")
	}
	for _, r := range keyData {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=') {
			return "", fmt.Errorf("ssh public key data contains invalid characters")
		}
	}

	// Drop the optional comment to keep the generated guest script shell-safe and deterministic.
	return keyType + " " + keyData, nil
}

func writeAddSSHKeyScript(sshKey string) (string, error) {
	f, err := os.CreateTemp("", "add-ssh-key-*.sh")
	if err != nil {
		return "", fmt.Errorf("create virt-customize script: %w", err)
	}
	path := f.Name()

	script := fmt.Sprintf(`#!/bin/sh
set -eu

key=%s

add_key() {
	home="$1"
	uid="$2"
	gid="$3"

	[ -n "$home" ] || return 0
	[ -d "$home" ] || return 0

	mkdir -p "$home/.ssh"
	touch "$home/.ssh/authorized_keys"
	grep -qxF -- "$key" "$home/.ssh/authorized_keys" || echo "$key" >> "$home/.ssh/authorized_keys"
	chown -R "$uid:$gid" "$home/.ssh"
	chmod 700 "$home/.ssh"
	chmod 600 "$home/.ssh/authorized_keys"
}

while IFS=: read -r user _ uid gid _ home shell; do
	case "$shell" in
		""|*/nologin|*/false)
			continue
			;;
	esac

	if [ "$user" = "root" ] || [ "$uid" -ge 1000 ] 2>/dev/null; then
		add_key "$home" "$uid" "$gid"
	fi
done < /etc/passwd

mkdir -p /etc/skel/.ssh
touch /etc/skel/.ssh/authorized_keys
grep -qxF -- "$key" /etc/skel/.ssh/authorized_keys || echo "$key" >> /etc/skel/.ssh/authorized_keys
chmod 700 /etc/skel/.ssh
chmod 600 /etc/skel/.ssh/authorized_keys
`, shellSingleQuote(sshKey))

	if _, err := f.WriteString(script); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write virt-customize script: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close virt-customize script: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("chmod virt-customize script: %w", err)
	}

	return path, nil
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
