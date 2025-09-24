package virsh

import (
	"errors"
	"os"
	"os/exec"
)

type VM struct {
	Name     string `yaml:"name"`
	IP       string `yaml:"ip"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

var vmVault *VaultStore

func MigrateVMs(vmName string) error {
	toMigrate := "sv2@sv2"

	if vmVault == nil {
		return errors.New("vmVault is not initialized")
	}

	df, err := vmVault.GetVM(vmName)
	if err != nil {
		return err
	}
	if df == nil {
		return errors.New("vm not found")
	}

	// NOTE: embedding passwords on the command line is insecure; done here only for testing as requested.
	cmd := "sshpass -p 'sv2' virsh migrate --live " + df.Name + " qemu+ssh://" + toMigrate + "/system"
	out, err := exec.Command("bash", "-c", cmd).CombinedOutput()
	if err != nil {
		return errors.New("migration failed: " + err.Error() + ": " + string(out))
	}
	println("Migration command executed")
	return nil
}

func SetupVMs() error {
	p := os.Getenv("VAULT_PASS")
	if p == "" {
		return errors.New("VAULT_PASS is not set")
	}

	vmVault = NewVaultStore("vms.yaml", func() (string, error) {
		return p, nil
	})

	// Ensure the vault file exists and can be decrypted with the provided passphrase.
	if err := vmVault.Setup(); err != nil {
		return err
	}

	//default
	vmVault.AddVM(VM{
		Name:     "debian-kde-nat",
		IP:       "192.168.122.69",
		User:     "user",
		Password: "live",
	}, false)

	return nil
}
