package client

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"golang.org/x/crypto/ssh"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// VMCreateInput preserves every writable API field, including explicit false
// flags, empty CPU XML and zero template/ISO selectors.
type VMCreateInput struct {
	MachineName string `json:"machine_name"`
	Name        string `json:"name"`
	Memory      int32  `json:"memory" jsonschema:"RAM in MiB; zero permits template defaults"`
	VCPU        int32  `json:"vcpu"`
	DiskSizeGB  int32  `json:"disk_sizeGB"`
	TemplateID  int64  `json:"template_id"`
	ISOID       int64  `json:"iso_id"`
	NFSShareID  int64  `json:"nfs_share_id"`
	Network     string `json:"network"`
	VNCPassword string `json:"VNC_password"`
	CPUXML      string `json:"cpu_xml"`
	Live        bool   `json:"live"`
	AutoStart   bool   `json:"auto_start"`
	IsWindows   bool   `json:"is_windows"`
}
type VMAutostartSettings struct {
	AutoStart bool `json:"auto_start"`
}
type VMAutostartInput struct {
	VMName string `json:"vm_name"`
	VMAutostartSettings
}
type VMScheduleSettings struct {
	VMName           string `json:"vm_name"`
	FrequencyDays    int64  `json:"frequency_days"`
	MinTime          string `json:"min_time" jsonschema:"HH:MM in the server timezone; window may cross midnight"`
	MaxTime          string `json:"max_time" jsonschema:"HH:MM; window must be at least 45 minutes"`
	NFSMountID       int64  `json:"nfs_mount_id"`
	MaxBackupsRetain int64  `json:"max_backups_retain" jsonschema:"Positive retention count; older backups can be deleted automatically"`
}
type VMUpdateScheduleInput struct {
	ID int64 `json:"id"`
	VMScheduleSettings
}
type VMSSHSettings struct {
	SSHKey string `json:"ssh_key" jsonschema:"One OpenSSH public key; private keys and authorized_keys options are rejected"`
}
type VMSSHInput struct {
	VMName string `json:"vm_name"`
	VMSSHSettings
}

func validateVMIncrement(input any) error {
	switch in := input.(type) {
	case VMCreateInput:
		if ValidateVMName(in.Name) != nil || in.Name == "." || in.Name == ".." || strings.ContainsAny(in.Name, "/\\") {
			return errors.New("name must be a safe VM name and directory component")
		}
		for _, r := range in.Name {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
				return errors.New("new VM names must contain only letters, numbers and hyphens")
			}
		}
		if err := ValidateVMName(in.MachineName); err != nil {
			return err
		}
		if in.TemplateID < 0 || in.ISOID < 0 || in.NFSShareID <= 0 || in.DiskSizeGB <= 0 || in.Memory < 0 || in.VCPU < 0 {
			return errors.New("invalid VM resource values or selectors")
		}
		if in.TemplateID == 0 && (in.ISOID == 0 || in.Memory == 0 || in.VCPU == 0 || in.Network == "") {
			return errors.New("creation without a template needs ISO, memory, vcpu and network")
		}
		if in.CPUXML != "" {
			var doc struct {
				XMLName xml.Name `xml:"cpu"`
			}
			if xml.Unmarshal([]byte(in.CPUXML), &doc) != nil {
				return errors.New("cpu_xml must contain a CPU XML element")
			}
		}
		return nil
	case VMAutostartInput:
		return ValidateVMName(in.VMName)
	case VMScheduleSettings:
		if err := ValidateVMName(in.VMName); err != nil {
			return err
		}
		if in.FrequencyDays < 1 || in.NFSMountID < 1 || in.MaxBackupsRetain < 1 {
			return errors.New("frequency, NFS ID and retention must be positive")
		}
		min, e1 := time.Parse("15:04", in.MinTime)
		max, e2 := time.Parse("15:04", in.MaxTime)
		if e1 != nil || e2 != nil || min.Format("15:04") != in.MinTime || max.Format("15:04") != in.MaxTime {
			return errors.New("schedule clocks must be HH:MM")
		}
		window := max.Sub(min)
		if window <= 0 {
			window += 24 * time.Hour
		}
		if window < 45*time.Minute {
			return errors.New("schedule window must be at least 45 minutes")
		}
		return nil
	case VMUpdateScheduleInput:
		if err := ValidateVMID(in.ID); err != nil {
			return err
		}
		return validateVMIncrement(in.VMScheduleSettings)
	case VMSSHInput:
		if err := ValidateVMName(in.VMName); err != nil {
			return err
		}
		key := strings.TrimSpace(in.SSHKey)
		if strings.ContainsAny(key, "\r\n\x00") {
			return errors.New("ssh_key must be a single public key line")
		}
		fields := strings.Fields(key)
		if len(fields) < 2 {
			return errors.New("ssh_key must contain type and base64 data")
		}
		switch fields[0] {
		case "ssh-rsa", "ssh-ed25519", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521":
		default:
			return errors.New("unsupported SSH public key type")
		}
		_, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(key))
		if err != nil || len(options) != 0 || len(strings.TrimSpace(string(rest))) != 0 {
			return errors.New("invalid OpenSSH public key")
		}
		return nil
	}
	return errors.New("unsupported VM increment input")
}

func (c *Client) CreateVM(ctx context.Context, in VMCreateInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.withVMProvisioningTimeout().request(ctx, "POST", "/virsh/createvm", in, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) SetVMAutostart(ctx context.Context, in VMAutostartInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/autostart/"+url.PathEscape(in.VMName), in.VMAutostartSettings, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) SetVMLive(ctx context.Context, in VMEnableInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/vmlive/"+url.PathEscape(in.VMName), in.VMEnableSettings, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) CreateVMAutomaticBackup(ctx context.Context, in VMScheduleSettings) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/autobak", in, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true,"enabled":true}`), nil
}
func (c *Client) UpdateVMAutomaticBackup(ctx context.Context, in VMUpdateScheduleInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "PUT", "/virsh/autobak/"+strconv.FormatInt(in.ID, 10), in.VMScheduleSettings, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true,"enabled":true}`), nil
}
func (c *Client) EnableVMAutomaticBackup(ctx context.Context, in VMIDInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "PATCH", "/virsh/autopak/enable/"+strconv.FormatInt(in.ID, 10), nil, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true,"enabled":true}`), nil
}
func (c *Client) AddVMSSHKey(ctx context.Context, in VMSSHInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.withVMProvisioningTimeout().request(ctx, "POST", "/virsh/add_ssh_key/"+url.PathEscape(in.VMName), in.VMSSHSettings, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}

// Provisioning and offline guest customization may exceed ordinary JSON calls.
// Earlier caller deadlines still apply. A timeout is not proof of rollback.
func (c *Client) withVMProvisioningTimeout() *Client {
	copyClient := *c
	transport := *c.http
	transport.Timeout = 15 * time.Minute
	copyClient.http = &transport
	return &copyClient
}

// RemoveVMISOs removes every CD-ROM device; ISO files remain in storage.
func (c *Client) RemoveVMISOs(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/removeiso/"+url.PathEscape(in.VMName), nil, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
