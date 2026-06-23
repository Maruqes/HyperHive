package services

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrVMNotFound = errors.New("vm not found")
	ErrVMRunning  = errors.New("vm is running")
)

const AllVMsTarget = "all-hyperhive-all"

var supportedSSHKeyTypes = map[string]bool{
	"ssh-rsa":             true,
	"ssh-ed25519":         true,
	"ecdsa-sha2-nistp256": true,
	"ecdsa-sha2-nistp384": true,
	"ecdsa-sha2-nistp521": true,
}

type AddSSHKeyResult struct {
	VMName string `json:"vm_name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type AddSSHKeyBatchResult struct {
	Total     int               `json:"total"`
	Succeeded int               `json:"succeeded"`
	Skipped   int               `json:"skipped"`
	Failed    int               `json:"failed"`
	Results   []AddSSHKeyResult `json:"results"`
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

	return keyType + " " + keyData, nil
}
