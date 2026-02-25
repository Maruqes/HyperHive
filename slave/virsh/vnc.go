package virsh

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
	libvirt "libvirt.org/go/libvirt"
)

const (
	virtXMLBinary  = "virt-xml"
	virtXMLPackage = "virt-install"
)

type NoVNCVideoInfo struct {
	Enabled    bool
	VideoCount int32
	ModelType  string
	VideoXML   string
}

const (
	defaultSpiceChannelDeviceXML = `  <channel type='spicevmc'>
    <target type='virtio' name='com.redhat.spice.0'/>
  </channel>`

	defaultVNCGraphicsDeviceXML = `  <graphics type='vnc' autoport='yes' port='-1' listen='127.0.0.1'/>`

	defaultSpiceGraphicsDeviceXML = `  <graphics type='spice' autoport='yes' port='-1' listen='127.0.0.1'>
    <listen type='address' address='127.0.0.1'/>
    <image compression='auto_glz'/>
    <jpeg compression='auto'/>
    <zlib compression='auto'/>
    <playback compression='on'/>
    <streaming mode='all'/>
    <clipboard copypaste='yes'/>
    <filetransfer enable='yes'/>
    <mouse mode='client'/>
  </graphics>`

	defaultVideoDeviceXML = `  <video>
    <model type='virtio' heads='1'/>
  </video>`

	defaultSoundDeviceXML = `  <sound model='ich9'/>`
)

const noVNCDeviceModifyFlags = libvirt.DOMAIN_DEVICE_MODIFY_CONFIG

type noVNCDeviceRemovalSummary struct {
	VNCGraphics     int
	SpiceGraphics   int
	Videos          int
	Sounds          int
	Audios          int
	SpiceChannels   int
	SpiceRedirdevs  int
	SpiceSmartcards int
}

type noVNCDeviceAddSummary struct {
	SpiceChannels int
	VNCGraphics   int
	SpiceGraphics int
	Videos        int
	Sounds        int
}

type rawDomainDevicesContainer struct {
	Devices struct {
		InnerXML string `xml:",innerxml"`
	} `xml:"devices"`
}

type rawDomainDeviceElement struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	InnerXML string     `xml:",innerxml"`
}

type noVNCManagedDevice struct {
	kind string
	xml  string
}

func getDomainXMLInactiveOrCurrent(dom *libvirt.Domain) (string, error) {
	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err == nil {
		return xmlDesc, nil
	}
	xmlDesc, err = dom.GetXMLDesc(0)
	if err != nil {
		return "", fmt.Errorf("get xml: %w", err)
	}
	return xmlDesc, nil
}

func xmlAttrValue(attrs []xml.Attr, attrName string) string {
	for _, attr := range attrs {
		if strings.EqualFold(strings.TrimSpace(attr.Name.Local), attrName) {
			return strings.TrimSpace(attr.Value)
		}
	}
	return ""
}

func classifyNoVNCManagedDevice(elem rawDomainDeviceElement) (string, bool) {
	name := strings.ToLower(strings.TrimSpace(elem.XMLName.Local))
	switch name {
	case "graphics":
		switch strings.ToLower(xmlAttrValue(elem.Attrs, "type")) {
		case "vnc":
			return "vnc_graphics", true
		case "spice":
			return "spice_graphics", true
		default:
			return "", false
		}
	case "video":
		return "video", true
	case "sound":
		return "sound", true
	case "audio":
		return "audio", true
	case "channel":
		if strings.EqualFold(xmlAttrValue(elem.Attrs, "type"), "spicevmc") {
			return "spice_channel", true
		}
	case "redirdev":
		if strings.EqualFold(xmlAttrValue(elem.Attrs, "type"), "spicevmc") {
			return "spice_redirdev", true
		}
	case "smartcard":
		if strings.EqualFold(xmlAttrValue(elem.Attrs, "type"), "spicevmc") {
			return "spice_smartcard", true
		}
	}

	return "", false
}

func addToNoVNCRemovalSummary(summary *noVNCDeviceRemovalSummary, kind string) {
	switch kind {
	case "vnc_graphics":
		summary.VNCGraphics++
	case "spice_graphics":
		summary.SpiceGraphics++
	case "video":
		summary.Videos++
	case "sound":
		summary.Sounds++
	case "audio":
		summary.Audios++
	case "spice_channel":
		summary.SpiceChannels++
	case "spice_redirdev":
		summary.SpiceRedirdevs++
	case "spice_smartcard":
		summary.SpiceSmartcards++
	}
}

func collectNoVNCManagedDevicesFromDomainXML(xmlDesc string) ([]noVNCManagedDevice, noVNCDeviceRemovalSummary, error) {
	var parsed rawDomainDevicesContainer
	if err := xml.Unmarshal([]byte(xmlDesc), &parsed); err != nil {
		return nil, noVNCDeviceRemovalSummary{}, fmt.Errorf("parse domain xml: %w", err)
	}

	inner := parsed.Devices.InnerXML
	if strings.TrimSpace(inner) == "" {
		return nil, noVNCDeviceRemovalSummary{}, nil
	}

	decoder := xml.NewDecoder(strings.NewReader("<devices>" + inner + "</devices>"))
	devices := make([]noVNCManagedDevice, 0)
	var summary noVNCDeviceRemovalSummary

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, summary, fmt.Errorf("decode devices xml: %w", err)
		}

		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if strings.EqualFold(start.Name.Local, "devices") {
			continue
		}

		var elem rawDomainDeviceElement
		if err := decoder.DecodeElement(&elem, &start); err != nil {
			return nil, summary, fmt.Errorf("decode device element %s: %w", start.Name.Local, err)
		}

		kind, managed := classifyNoVNCManagedDevice(elem)
		if !managed {
			continue
		}

		deviceXML, err := xml.Marshal(elem)
		if err != nil {
			return nil, summary, fmt.Errorf("marshal device element %s: %w", start.Name.Local, err)
		}

		devices = append(devices, noVNCManagedDevice{
			kind: kind,
			xml:  string(deviceXML),
		})
		addToNoVNCRemovalSummary(&summary, kind)
	}

	return devices, summary, nil
}

func detachNoVNCManagedDevices(dom *libvirt.Domain) (noVNCDeviceRemovalSummary, error) {
	xmlDesc, err := getDomainXMLInactiveOrCurrent(dom)
	if err != nil {
		return noVNCDeviceRemovalSummary{}, err
	}

	devices, summary, err := collectNoVNCManagedDevicesFromDomainXML(xmlDesc)
	if err != nil {
		return noVNCDeviceRemovalSummary{}, err
	}

	for _, device := range devices {
		if err := dom.DetachDeviceFlags(device.xml, noVNCDeviceModifyFlags); err != nil {
			return summary, fmt.Errorf("detach %s: %w", device.kind, err)
		}
	}

	return summary, nil
}

func attachNoVNCDefaultDevices(dom *libvirt.Domain) (noVNCDeviceAddSummary, error) {
	devices := []struct {
		kind string
		xml  string
	}{
		{kind: "spice_channel", xml: defaultSpiceChannelDeviceXML},
		{kind: "vnc_graphics", xml: defaultVNCGraphicsDeviceXML},
		{kind: "spice_graphics", xml: defaultSpiceGraphicsDeviceXML},
		{kind: "video", xml: defaultVideoDeviceXML},
		{kind: "sound", xml: defaultSoundDeviceXML},
	}

	var summary noVNCDeviceAddSummary
	for _, device := range devices {
		if err := dom.AttachDeviceFlags(device.xml, noVNCDeviceModifyFlags); err != nil {
			return summary, fmt.Errorf("attach %s: %w", device.kind, err)
		}

		switch device.kind {
		case "spice_channel":
			summary.SpiceChannels++
		case "vnc_graphics":
			summary.VNCGraphics++
		case "spice_graphics":
			summary.SpiceGraphics++
		case "video":
			summary.Videos++
		case "sound":
			summary.Sounds++
		}
	}

	return summary, nil
}

func restoreDomainConfigXML(conn *libvirt.Connect, originalXML string) error {
	if strings.TrimSpace(originalXML) == "" {
		return fmt.Errorf("original domain XML is empty")
	}

	newDom, err := conn.DomainDefineXML(originalXML)
	if err != nil {
		return fmt.Errorf("restore domain xml: %w", err)
	}
	defer newDom.Free()
	return nil
}

// EnsureVirtXMLInstalled verifies that virt-xml is available and installs it if missing.
func EnsureVirtXMLInstalled() error {
	if _, err := exec.LookPath(virtXMLBinary); err == nil {
		return nil
	}

	logger.Info("virt-xml not found, installing package", "package", virtXMLPackage)
	if err := installVirtXML(); err != nil {
		return fmt.Errorf("install virt-xml: %w", err)
	}
	if _, err := exec.LookPath(virtXMLBinary); err != nil {
		return fmt.Errorf("virt-xml binary still missing after install: %w", err)
	}
	return nil
}

func installVirtXML() error {
	if _, err := exec.LookPath("dnf"); err != nil {
		return fmt.Errorf("dnf is required to install virt-xml: %w", err)
	}
	if err := runCmdDiscardOutput("dnf", "-y", "install", virtXMLPackage); err != nil {
		return err
	}
	return nil
}

func runCmdDiscardOutput(name string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, stderr.String())
	}
	return nil
}

// ChangeVNCPassword updates the VNC password of a libvirt VM using virt-xml.
func ChangeVNCPassword(vmName, newPassword string) error {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return fmt.Errorf("vm name is empty")
	}

	newPassword = strings.TrimSpace(newPassword)
	if newPassword == "" {
		return fmt.Errorf("new password cannot be empty")
	}

	if !isValidPassword(newPassword) {
		return fmt.Errorf("password contains invalid characters (avoid commas, equals signs, and special shell characters)")
	}

	if err := EnsureVirtXMLInstalled(); err != nil {
		return err
	}

	if err := ensureVMShutOff(vmName); err != nil {
		return err
	}

	args := []string{
		"--connect", "qemu:///system",
		vmName,
		"--edit",
		"--graphics", fmt.Sprintf("vnc,password=%s", newPassword),
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(virtXMLBinary, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("virt-xml command failed: %w: %s", err, stderr.String())
	}

	logger.Info("updated VNC password", "vm", vmName)
	return nil
}

func AddNoVNCVideo(vmName string) error {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return fmt.Errorf("vm name is empty")
	}

	if err := ensureVMShutOff(vmName); err != nil {
		return err
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("lookup domain: %w", err)
	}
	defer dom.Free()

	originalXML, err := getDomainXMLInactiveOrCurrent(dom)
	if err != nil {
		return err
	}

	removedSummary, err := detachNoVNCManagedDevices(dom)
	if err != nil {
		if restoreErr := restoreDomainConfigXML(conn, originalXML); restoreErr != nil {
			return fmt.Errorf("clear existing noVNC/SPICE devices before add: %w (rollback failed: %v)", err, restoreErr)
		}
		return fmt.Errorf("clear existing noVNC/SPICE devices before add: %w", err)
	}

	addedSummary, err := attachNoVNCDefaultDevices(dom)
	if err != nil {
		if restoreErr := restoreDomainConfigXML(conn, originalXML); restoreErr != nil {
			return fmt.Errorf("attach noVNC/SPICE default devices: %w (rollback failed: %v)", err, restoreErr)
		}
		return fmt.Errorf("attach noVNC/SPICE default devices: %w", err)
	}

	logger.Info(
		"restored noVNC/SPICE graphics/audio devices",
		"vm", vmName,
		"removed_vnc_graphics", removedSummary.VNCGraphics,
		"removed_spice_graphics", removedSummary.SpiceGraphics,
		"removed_videos", removedSummary.Videos,
		"removed_sounds", removedSummary.Sounds,
		"removed_audios", removedSummary.Audios,
		"removed_spice_channels", removedSummary.SpiceChannels,
		"removed_spice_redirdevs", removedSummary.SpiceRedirdevs,
		"removed_spice_smartcards", removedSummary.SpiceSmartcards,
		"added_vnc_graphics", addedSummary.VNCGraphics,
		"added_spice_graphics", addedSummary.SpiceGraphics,
		"added_videos", addedSummary.Videos,
		"added_sounds", addedSummary.Sounds,
		"added_spice_channels", addedSummary.SpiceChannels,
	)
	return nil
}

func RemoveNoVNCVideo(vmName string) error {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return fmt.Errorf("vm name is empty")
	}

	if err := ensureVMShutOff(vmName); err != nil {
		return err
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("lookup domain: %w", err)
	}
	defer dom.Free()

	originalXML, err := getDomainXMLInactiveOrCurrent(dom)
	if err != nil {
		return err
	}

	summary, err := detachNoVNCManagedDevices(dom)
	if err != nil {
		if restoreErr := restoreDomainConfigXML(conn, originalXML); restoreErr != nil {
			return fmt.Errorf("remove noVNC/SPICE devices: %w (rollback failed: %v)", err, restoreErr)
		}
		return fmt.Errorf("remove noVNC/SPICE devices: %w", err)
	}
	if summary.VNCGraphics == 0 &&
		summary.SpiceGraphics == 0 &&
		summary.Videos == 0 &&
		summary.Sounds == 0 &&
		summary.Audios == 0 &&
		summary.SpiceChannels == 0 &&
		summary.SpiceRedirdevs == 0 &&
		summary.SpiceSmartcards == 0 {
		logger.Info("no noVNC/SPICE graphics/audio devices found to remove", "vm", vmName)
		return nil
	}

	logger.Info(
		"removed noVNC/SPICE graphics/audio devices",
		"vm", vmName,
		"vnc_graphics", summary.VNCGraphics,
		"spice_graphics", summary.SpiceGraphics,
		"videos", summary.Videos,
		"sounds", summary.Sounds,
		"audios", summary.Audios,
		"spice_channels", summary.SpiceChannels,
		"spice_redirdevs", summary.SpiceRedirdevs,
		"spice_smartcards", summary.SpiceSmartcards,
	)
	return nil
}

func GetNoVNCVideo(vmName string) (*NoVNCVideoInfo, error) {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return nil, fmt.Errorf("vm name is empty")
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return nil, fmt.Errorf("lookup domain: %w", err)
	}
	defer dom.Free()

	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		xmlDesc, err = dom.GetXMLDesc(0)
		if err != nil {
			return nil, fmt.Errorf("get xml: %w", err)
		}
	}

	type videoDevice struct {
		XMLName xml.Name `xml:"video"`
		Model   struct {
			Type string `xml:"type,attr"`
		} `xml:"model"`
	}
	type domainVideoXML struct {
		Devices struct {
			Videos []videoDevice `xml:"video"`
		} `xml:"devices"`
	}

	var parsed domainVideoXML
	if err := xml.Unmarshal([]byte(xmlDesc), &parsed); err != nil {
		return nil, fmt.Errorf("parse domain xml: %w", err)
	}

	info := &NoVNCVideoInfo{
		VideoCount: int32(len(parsed.Devices.Videos)),
	}
	if len(parsed.Devices.Videos) == 0 {
		return info, nil
	}

	info.ModelType = strings.TrimSpace(parsed.Devices.Videos[0].Model.Type)
	if raw, err := xml.Marshal(parsed.Devices.Videos[0]); err == nil {
		info.VideoXML = string(raw)
	}

	for _, video := range parsed.Devices.Videos {
		modelType := strings.TrimSpace(video.Model.Type)
		lowerModelType := strings.ToLower(modelType)
		if lowerModelType != "" && lowerModelType != "none" {
			info.Enabled = true
			info.ModelType = modelType
			break
		}
	}

	return info, nil
}

func isValidPassword(password string) bool {
	for _, r := range password {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("!@#$%^&*()_+-[]{}|;:'\"<>?/~", r)) {
			return false
		}
	}
	return true
}

func ensureVMShutOff(vmName string) error {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("lookup domain: %w", err)
	}
	defer dom.Free()

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}

	if state != libvirt.DOMAIN_SHUTOFF && state != libvirt.DOMAIN_SHUTDOWN {
		return fmt.Errorf("vm %s must be shut off before editing VNC settings (current state: %s)", vmName, domainStateToString(state).String())
	}
	return nil
}
