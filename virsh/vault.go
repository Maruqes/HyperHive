package virsh

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	vault "github.com/sosedoff/ansible-vault-go"
	"gopkg.in/yaml.v3"
)

// dataFile is the root YAML structure stored inside the vault-encrypted file.
// Example (plaintext before encryption):
// vms:
//   - name: vm1
//     ip: 192.0.2.10
//     user: admin
//     password: secret
//
// Once encrypted, the file will begin with: "$ANSIBLE_VAULT;1.1;AES256".
// This package reads/writes the encrypted form transparently.
type dataFile struct {
	VMs []VM `yaml:"vms"`
}

// VaultStore manages an Ansible Vault file that contains your VMs list.
// It never writes decrypted content to disk.
type VaultStore struct {
	Path         string
	PassProvider func() (string, error)
}

// NewVaultStore creates a store bound to a given path. The PassProvider should
// return the Ansible Vault passphrase on demand (e.g., read from env, stdin, or a
// password file with 0600 permissions).
func NewVaultStore(path string, passProvider func() (string, error)) *VaultStore {
	return &VaultStore{Path: path, PassProvider: passProvider}
}

// Setup ensures the vault file exists and is a valid Ansible Vault file.
// If the file doesn't exist, it creates a minimal encrypted document with an empty list of VMs.
func (s *VaultStore) Setup() error {
	if s.PassProvider == nil {
		return errors.New("PassProvider is nil")
	}
	if s.Path == "" {
		return errors.New("Path is empty")
	}
	if _, err := os.Stat(s.Path); errors.Is(err, fs.ErrNotExist) {
		// Create directory if needed
		if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
		// Write an empty structure, encrypted.
		pass, err := s.PassProvider()
		if err != nil {
			return err
		}
		plain := &dataFile{VMs: []VM{}}
		b, err := yaml.Marshal(plain)
		if err != nil {
			return err
		}
		enc, err := vault.Encrypt(string(b), pass)
		if err != nil {
			return err
		}
		// Write atomically with 0600 permissions
		return writeAtomic(s.Path, []byte(enc), 0o600)
	}
	// If the file exists, do a light sanity check by attempting to decrypt.
	_, err := s.readData()
	return err
}

// ReadAllVMs returns all VMs from the vault file.
func (s *VaultStore) ReadAllVMs() ([]VM, error) {
	df, err := s.readData()
	if err != nil {
		return nil, err
	}
	return df.VMs, nil
}

// GetVM returns a VM by name (case-insensitive). If you prefer lookup by IP,
// call GetVMByIP.
func (s *VaultStore) GetVM(name string) (*VM, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return nil, errors.New("empty name")
	}
	df, err := s.readData()
	if err != nil {
		return nil, err
	}
	for _, vm := range df.VMs {
		if strings.ToLower(vm.Name) == name {
			v := vm
			return &v, nil
		}
	}
	return nil, fs.ErrNotExist
}

// GetVMByIP returns a VM by exact IP match.
func (s *VaultStore) GetVMByIP(ip string) (*VM, error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil, errors.New("empty ip")
	}
	df, err := s.readData()
	if err != nil {
		return nil, err
	}
	for _, vm := range df.VMs {
		if vm.IP == ip {
			v := vm
			return &v, nil
		}
	}
	return nil, fs.ErrNotExist
}

// AddVM adds a new VM. If a VM with the same name already exists:
// - if upsert is true, it will update it; otherwise returns an error.
func (s *VaultStore) AddVM(newVM VM, upsert bool) error {
	if strings.TrimSpace(newVM.Name) == "" {
		return errors.New("vm.name is required")
	}
	if strings.TrimSpace(newVM.IP) == "" {
		return errors.New("vm.ip is required")
	}
	if strings.TrimSpace(newVM.User) == "" {
		return errors.New("vm.user is required")
	}

	df, err := s.readData()
	if err != nil {
		return err
	}

	updated := false
	for i, vm := range df.VMs {
		if strings.EqualFold(vm.Name, newVM.Name) {
			if !upsert {
				return fmt.Errorf("vm with name %q already exists", newVM.Name)
			}
			df.VMs[i] = newVM
			updated = true
			break
		}
	}
	if !updated {
		df.VMs = append(df.VMs, newVM)
	}
	return s.writeData(df)
}

// --- internal helpers ---

func (s *VaultStore) readData() (*dataFile, error) {
	pass, err := s.PassProvider()
	if err != nil {
		return nil, err
	}

	cipherText, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}

	plain, err := vault.Decrypt(string(cipherText), pass)
	if err != nil {
		return nil, fmt.Errorf("vault decrypt: %w", err)
	}

	var df dataFile
	if err := yaml.Unmarshal([]byte(plain), &df); err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %w", err)
	}
	if df.VMs == nil {
		df.VMs = []VM{}
	}
	return &df, nil
}

func (s *VaultStore) writeData(df *dataFile) error {
	pass, err := s.PassProvider()
	if err != nil {
		return err
	}
	b, err := yaml.Marshal(df)
	if err != nil {
		return err
	}
	enc, err := vault.Encrypt(string(b), pass)
	if err != nil {
		return fmt.Errorf("vault encrypt: %w", err)
	}
	return writeAtomic(s.Path, []byte(enc), 0o600)
}

// writeAtomic writes to a temp file in the same dir then renames over the target,
// preserving 0600 permissions to avoid accidental world-readable secrets.
func writeAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.tmp.%d", base, time.Now().UnixNano()))
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// --- Example usage ---
// Provide a passphrase from VAULT_PASS env var or a file path in VAULT_PASS_FILE.
func defaultPassProvider() func() (string, error) {
	return func() (string, error) {
		
		if f := os.Getenv("VAULT_PASS_FILE"); f != "" {
			b, err := os.ReadFile(f)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(b)), nil
		}
		return "", errors.New("set VAULT_PASS or VAULT_PASS_FILE for the vault passphrase")
	}
}

