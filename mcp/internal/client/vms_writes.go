package client

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type VMPinningSettings struct {
	RangeStart     int32 `json:"range_start"`
	RangeEnd       int32 `json:"range_end"`
	HyperThreading bool  `json:"hyper_threading"`
	SocketID       int32 `json:"socket_id"`
}
type VMPinningInput struct {
	VMName string `json:"vm_name"`
	VMPinningSettings
}
type VMXMLSettings struct {
	MachineName string `json:"machine_name,omitempty"`
	VMXML       string `json:"vm_xml"`
}
type VMUpdateXMLInput struct {
	VMName string `json:"vm_name"`
	VMXMLSettings
}
type VMCPUXMLSettings struct {
	MachineName string `json:"machine_name"`
	CPUXML      string `json:"cpu_xml"`
}
type VMUpdateCPUXMLInput struct {
	VMName string `json:"vm_name"`
	VMCPUXMLSettings
}
type VMEnableSettings struct {
	Enable bool `json:"enable"`
}
type VMEnableInput struct {
	VMName string `json:"vm_name"`
	VMEnableSettings
}
type VMKVMSettings struct {
	Hidden bool `json:"hidden"`
}
type VMKVMInput struct {
	VMName string `json:"vm_name"`
	VMKVMSettings
}
type VMHyperVSettings struct {
	HyperV bool `json:"hyperv"`
}
type VMHyperVInput struct {
	VMName string `json:"vm_name"`
	VMHyperVSettings
}
type VMMachineTypeSettings struct {
	MachineType string `json:"machine_type"`
}
type VMMachineTypeInput struct {
	VMName string `json:"vm_name"`
	VMMachineTypeSettings
}
type VMNetworkSettings struct {
	NewNetwork string `json:"new_network"`
}
type VMNetworkInput struct {
	VMName string `json:"vm_name"`
	VMNetworkSettings
}
type VMVNCPasswordSettings struct {
	NewPassword string `json:"new_password"`
}
type VMVNCPasswordInput struct {
	VMName string `json:"vm_name"`
	VMVNCPasswordSettings
}
type VMEditSettings struct {
	Memory     int32 `json:"memory,omitempty"`
	VCPU       int32 `json:"vcpu,omitempty"`
	DiskSizeGB int32 `json:"disk_sizeGB,omitempty"`
}
type VMEditInput struct {
	VMName string `json:"vm_name"`
	VMEditSettings
}
type VMTunedSettings struct {
	Profile string `json:"profile"`
}
type VMTunedInput struct {
	MachineName string `json:"machine_name"`
	VMTunedSettings
}
type VMIrqSettings struct {
	Enabled bool `json:"enabled"`
}
type VMIrqInput struct {
	MachineName string `json:"machine_name"`
	VMIrqSettings
}
type VMTemplateInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	XML         string `json:"xml"`
}
type VMUpdateTemplateInput struct {
	ID int64 `json:"id"`
	VMTemplateInput
}
type VMGrowDiskSettings struct {
	SizeGB int64 `json:"size_gb"`
}
type VMGrowDiskInput struct {
	ID int64 `json:"id"`
	VMGrowDiskSettings
}

func ValidateVMWrite(input any) error {
	nonblank := func(value string) error {
		if strings.TrimSpace(value) == "" {
			return errors.New("setting must not be blank")
		}
		return nil
	}
	switch in := input.(type) {
	case VMCoreIsolationInput:
		if err := ValidateVMName(in.MachineName); err != nil {
			return err
		}
		if len(in.Sockets) == 0 {
			return errors.New("sockets cannot be empty")
		}
		seen := map[int32]bool{}
		for _, socket := range in.Sockets {
			if socket.SocketID < 0 || seen[socket.SocketID] || len(socket.CoreIndices) == 0 {
				return errors.New("socket IDs must be unique/nonnegative and core_indices nonempty")
			}
			seen[socket.SocketID] = true
			for _, core := range socket.CoreIndices {
				if core < 0 {
					return errors.New("core_indices must be nonnegative")
				}
			}
		}
		return nil
	case VMHostHugePagesInput:
		if err := ValidateVMName(in.MachineName); err != nil {
			return err
		}
		if in.PageCount <= 0 || strings.TrimSpace(in.PageSize) == "" {
			return errors.New("page_size must be nonblank and page_count positive")
		}
		return nil
	case VMAttachDiskInput:
		if err := ValidateVMName(in.VMName); err != nil {
			return err
		}
		return ValidateVMID(in.VMDiskID)
	case VMBackupInput:
		if err := ValidateVMName(in.VMName); err != nil {
			return err
		}
		return ValidateVMID(in.NFSID)
	case VMCloneInput:
		if err := ValidateVMName(in.VMName); err != nil {
			return err
		}
		if err := ValidateVMName(in.NewName); err != nil {
			return err
		}
		if in.NewName == in.VMName {
			return errors.New("new_name must differ from source VM")
		}
		if err := ValidateVMName(in.DestinationMachine); err != nil {
			return err
		}
		if in.TemplateID < 0 {
			return errors.New("template_id must be nonnegative")
		}
		return ValidateVMID(in.DestinationNFS)
	case VMIDInput:
		return ValidateVMID(in.ID)
	case VMNameInput:
		return ValidateVMName(in.VMName)
	case VMPinningInput:
		if in.RangeStart < 0 || in.RangeEnd < in.RangeStart || in.SocketID < 0 {
			return errors.New("CPU core range and socket must be nonnegative and ordered")
		}
		return ValidateVMName(in.VMName)
	case VMUpdateXMLInput:
		if in.MachineName != "" {
			if err := ValidateVMName(in.MachineName); err != nil {
				return err
			}
		}
		if err := ValidateVMName(in.VMName); err != nil {
			return err
		}
		var doc struct {
			XMLName xml.Name `xml:"domain"`
			Name    string   `xml:"name"`
		}
		if xml.Unmarshal([]byte(in.VMXML), &doc) != nil || doc.XMLName.Local != "domain" || doc.Name != in.VMName {
			return errors.New("domain XML must name the target VM")
		}
		return nil
	case VMUpdateCPUXMLInput:
		if err := ValidateVMName(in.VMName); err != nil {
			return err
		}
		if err := ValidateVMName(in.MachineName); err != nil {
			return err
		}
		var doc struct{ XMLName xml.Name }
		if xml.Unmarshal([]byte(in.CPUXML), &doc) != nil || doc.XMLName.Local != "cpu" {
			return errors.New("cpu_xml must contain a cpu element")
		}
		return nil
	case VMEnableInput:
		return ValidateVMName(in.VMName)
	case VMKVMInput:
		return ValidateVMName(in.VMName)
	case VMHyperVInput:
		return ValidateVMName(in.VMName)
	case VMMachineTypeInput:
		if err := nonblank(in.MachineType); err != nil {
			return err
		}
		return ValidateVMName(in.VMName)
	case VMNetworkInput:
		if in.NewNetwork != "default" && in.NewNetwork != "512rede" {
			return errors.New("new_network must be default or 512rede")
		}
		return ValidateVMName(in.VMName)
	case VMVNCPasswordInput:
		if err := nonblank(in.NewPassword); err != nil {
			return err
		}
		return ValidateVMName(in.VMName)
	case VMEditInput:
		if in.Memory < 0 || in.VCPU < 0 || in.DiskSizeGB < 0 {
			return errors.New("resource values must be nonnegative")
		}
		return ValidateVMName(in.VMName)
	case VMTunedInput:
		if err := nonblank(in.Profile); err != nil {
			return err
		}
		return ValidateVMName(in.MachineName)
	case VMIrqInput:
		return ValidateVMName(in.MachineName)
	case VMTemplateInput:
		if err := nonblank(in.Name); err != nil {
			return err
		}
		return nonblank(in.XML)
	case VMUpdateTemplateInput:
		if err := ValidateVMID(in.ID); err != nil {
			return err
		}
		return ValidateVMWrite(in.VMTemplateInput)
	case VMGrowDiskInput:
		if in.SizeGB <= 0 {
			return errors.New("size_gb must be positive")
		}
		return ValidateVMID(in.ID)
	default:
		return fmt.Errorf("unsupported VM input")
	}
}
func (c *Client) StartVM(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/startvm/"+url.PathEscape(in.VMName)+"", nil, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) ShutdownVM(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/shutdownvm/"+url.PathEscape(in.VMName)+"", nil, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) PauseVM(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/pausevm/"+url.PathEscape(in.VMName)+"", nil, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) ResumeVM(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/resumevm/"+url.PathEscape(in.VMName)+"", nil, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) ForceShutdownVM(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/forceshutdownvm/"+url.PathEscape(in.VMName)+"", nil, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) RestartVM(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/restartvm/"+url.PathEscape(in.VMName)+"", nil, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) SetVMCPUPinning(ctx context.Context, in VMPinningInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/cpupinning/"+url.PathEscape(in.VMName)+"", in.VMPinningSettings, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) UpdateVMXML(ctx context.Context, in VMUpdateXMLInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/updatevmxml/"+url.PathEscape(in.VMName)+"", in.VMXMLSettings, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) UpdateVMCPUXML(ctx context.Context, in VMUpdateCPUXMLInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/updatecpuxml/"+url.PathEscape(in.VMName)+"", in.VMCPUXMLSettings, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) SetVMBallooning(ctx context.Context, in VMEnableInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/ballooning/"+url.PathEscape(in.VMName)+"", in.VMEnableSettings, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) SetVMHugePages(ctx context.Context, in VMEnableInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/hugepages/"+url.PathEscape(in.VMName)+"", in.VMEnableSettings, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) SetVMKVM(ctx context.Context, in VMKVMInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	raw, err := c.vmJSON(ctx, "POST", "/virsh/kvm/"+url.PathEscape(in.VMName)+"", in.VMKVMSettings)
	if err != nil {
		return nil, err
	}
	if err = ValidateVMResponse("GetVMKVM", raw); err != nil {
		return nil, err
	}
	return raw, nil
}
func (c *Client) SetVMHyperV(ctx context.Context, in VMHyperVInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	raw, err := c.vmJSON(ctx, "POST", "/virsh/hyperv/"+url.PathEscape(in.VMName)+"", in.VMHyperVSettings)
	if err != nil {
		return nil, err
	}
	if err = ValidateVMResponse("GetVMHyperV", raw); err != nil {
		return nil, err
	}
	return raw, nil
}
func (c *Client) SetVMMachineType(ctx context.Context, in VMMachineTypeInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	raw, err := c.vmJSON(ctx, "POST", "/virsh/machine_type/"+url.PathEscape(in.VMName)+"", in.VMMachineTypeSettings)
	if err != nil {
		return nil, err
	}
	if err = ValidateVMResponse("MachineType", raw); err != nil {
		return nil, err
	}
	return raw, nil
}
func (c *Client) SetVMNetwork(ctx context.Context, in VMNetworkInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/change_vm_network/"+url.PathEscape(in.VMName)+"", in.VMNetworkSettings, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) SetVMVNCPassword(ctx context.Context, in VMVNCPasswordInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/change_vnc_password/"+url.PathEscape(in.VMName)+"", in.VMVNCPasswordSettings, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) AddVMVideo(ctx context.Context, in VMNameInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/novncvideo/add/"+url.PathEscape(in.VMName)+"", nil, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) EditVM(ctx context.Context, in VMEditInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/editvm/"+url.PathEscape(in.VMName)+"", in.VMEditSettings, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}
func (c *Client) SetVMTunedProfile(ctx context.Context, in VMTunedInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	raw, err := c.vmJSON(ctx, "POST", "/virsh/tunedadm/"+url.PathEscape(in.MachineName)+"", in.VMTunedSettings)
	if err != nil {
		return nil, err
	}
	if err = ValidateVMResponse("TunedResult", raw); err != nil {
		return nil, err
	}
	return raw, nil
}
func (c *Client) SetVMIrqBalance(ctx context.Context, in VMIrqInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	raw, err := c.vmJSON(ctx, "POST", "/virsh/irqbalance/"+url.PathEscape(in.MachineName)+"", in.VMIrqSettings)
	if err != nil {
		return nil, err
	}
	if err = ValidateVMResponse("IrqResult", raw); err != nil {
		return nil, err
	}
	return raw, nil
}
func (c *Client) CreateVMXMLTemplate(ctx context.Context, in VMTemplateInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	raw, err := c.vmJSON(ctx, "POST", "/virsh/xmltemplates/", in)
	if err != nil {
		return nil, err
	}
	if err = ValidateVMResponse("GetVMXMLTemplate", raw); err != nil {
		return nil, err
	}
	return raw, nil
}
func (c *Client) UpdateVMXMLTemplate(ctx context.Context, in VMUpdateTemplateInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	raw, err := c.vmJSON(ctx, "PUT", "/virsh/xmltemplates/"+strconv.FormatInt(in.ID, 10)+"", in.VMTemplateInput)
	if err != nil {
		return nil, err
	}
	if err = ValidateVMResponse("GetVMXMLTemplate", raw); err != nil {
		return nil, err
	}
	return raw, nil
}
func (c *Client) GrowVMDisk(ctx context.Context, in VMGrowDiskInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	raw, err := c.vmJSON(ctx, "POST", "/vm-disk/"+strconv.FormatInt(in.ID, 10)+"/grow", in.VMGrowDiskSettings)
	if err != nil {
		return nil, err
	}
	if err = ValidateVMResponse("VMDisk", raw); err != nil {
		return nil, err
	}
	return raw, nil
}
