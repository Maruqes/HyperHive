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

type rawDomainDeviceElement struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	InnerXML string     `xml:",innerxml"`
}

func parseXMLFragmentElements(fragment string) ([]rawDomainDeviceElement, error) {
	wrapped := "<root>" + fragment + "</root>"
	decoder := xml.NewDecoder(strings.NewReader(wrapped))
	elements := make([]rawDomainDeviceElement, 0)

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if strings.EqualFold(start.Name.Local, "root") {
			continue
		}

		var elem rawDomainDeviceElement
		if err := decoder.DecodeElement(&elem, &start); err != nil {
			return nil, err
		}
		elements = append(elements, elem)
	}

	return elements, nil
}

func marshalXMLFragmentElements(elements []rawDomainDeviceElement) (string, error) {
	var b strings.Builder
	for _, elem := range elements {
		raw, err := xml.Marshal(elem)
		if err != nil {
			return "", err
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.Write(raw)
	}
	return b.String(), nil
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

func addToNoVNCAddSummary(summary *noVNCDeviceAddSummary, kind string) {
	switch kind {
	case "vnc_graphics":
		summary.VNCGraphics++
	case "spice_graphics":
		summary.SpiceGraphics++
	case "video":
		summary.Videos++
	case "sound":
		summary.Sounds++
	case "spice_channel":
		summary.SpiceChannels++
	}
}

func defaultNoVNCManagedDeviceElements() ([]rawDomainDeviceElement, noVNCDeviceAddSummary, error) {
	specs := []struct {
		kind string
		xml  string
	}{
		{kind: "spice_channel", xml: defaultSpiceChannelDeviceXML},
		{kind: "vnc_graphics", xml: defaultVNCGraphicsDeviceXML},
		{kind: "spice_graphics", xml: defaultSpiceGraphicsDeviceXML},
		{kind: "video", xml: defaultVideoDeviceXML},
		{kind: "sound", xml: defaultSoundDeviceXML},
	}

	result := make([]rawDomainDeviceElement, 0, len(specs))
	var summary noVNCDeviceAddSummary
	for _, spec := range specs {
		elems, err := parseXMLFragmentElements(spec.xml)
		if err != nil {
			return nil, summary, fmt.Errorf("parse default %s xml: %w", spec.kind, err)
		}
		if len(elems) != 1 {
			return nil, summary, fmt.Errorf("default %s xml did not produce exactly one element", spec.kind)
		}
		result = append(result, elems[0])
		addToNoVNCAddSummary(&summary, spec.kind)
	}
	return result, summary, nil
}

func rewriteNoVNCManagedDevicesInDomainXML(xmlDesc string, enable bool) (string, noVNCDeviceRemovalSummary, noVNCDeviceAddSummary, error) {
	var root rawDomainDeviceElement
	if err := xml.Unmarshal([]byte(xmlDesc), &root); err != nil {
		return "", noVNCDeviceRemovalSummary{}, noVNCDeviceAddSummary{}, fmt.Errorf("parse domain xml: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(root.XMLName.Local), "domain") {
		return "", noVNCDeviceRemovalSummary{}, noVNCDeviceAddSummary{}, fmt.Errorf("unexpected root element %q", root.XMLName.Local)
	}

	topLevel, err := parseXMLFragmentElements(root.InnerXML)
	if err != nil {
		return "", noVNCDeviceRemovalSummary{}, noVNCDeviceAddSummary{}, fmt.Errorf("parse domain children: %w", err)
	}

	var removalSummary noVNCDeviceRemovalSummary
	var addSummary noVNCDeviceAddSummary
	foundDevices := false

	for i := range topLevel {
		if !strings.EqualFold(strings.TrimSpace(topLevel[i].XMLName.Local), "devices") {
			continue
		}
		foundDevices = true

		deviceElems, err := parseXMLFragmentElements(topLevel[i].InnerXML)
		if err != nil {
			return "", removalSummary, addSummary, fmt.Errorf("parse devices children: %w", err)
		}

		filtered := make([]rawDomainDeviceElement, 0, len(deviceElems))
		for _, devElem := range deviceElems {
			kind, managed := classifyNoVNCManagedDevice(devElem)
			if managed {
				addToNoVNCRemovalSummary(&removalSummary, kind)
				continue
			}
			filtered = append(filtered, devElem)
		}

		if enable {
			defaultElems, summary, err := defaultNoVNCManagedDeviceElements()
			if err != nil {
				return "", removalSummary, addSummary, err
			}
			filtered = append(filtered, defaultElems...)
			addSummary = summary
		}

		rebuiltDevicesInner, err := marshalXMLFragmentElements(filtered)
		if err != nil {
			return "", removalSummary, addSummary, fmt.Errorf("marshal devices children: %w", err)
		}
		topLevel[i].InnerXML = rebuiltDevicesInner
		break
	}

	if !foundDevices {
		return "", removalSummary, addSummary, fmt.Errorf("domain XML does not contain <devices>")
	}

	rebuiltDomainInner, err := marshalXMLFragmentElements(topLevel)
	if err != nil {
		return "", removalSummary, addSummary, fmt.Errorf("marshal domain children: %w", err)
	}
	root.InnerXML = rebuiltDomainInner

	out, err := xml.Marshal(root)
	if err != nil {
		return "", removalSummary, addSummary, fmt.Errorf("marshal domain xml: %w", err)
	}
	return string(out), removalSummary, addSummary, nil
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

	updatedXML, removedSummary, addedSummary, err := rewriteNoVNCManagedDevicesInDomainXML(originalXML, true)
	if err != nil {
		return fmt.Errorf("rebuild noVNC/SPICE devices in domain XML: %w", err)
	}
	if updatedXML == "" {
		return fmt.Errorf("rebuild noVNC/SPICE devices in domain XML produced empty XML")
	}

	newDom, err := conn.DomainDefineXML(updatedXML)
	if err != nil {
		return fmt.Errorf("define updated domain xml: %w", err)
	}
	defer newDom.Free()

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

	updatedXML, summary, _, err := rewriteNoVNCManagedDevicesInDomainXML(originalXML, false)
	if err != nil {
		return fmt.Errorf("remove noVNC/SPICE devices from domain XML: %w", err)
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

	newDom, err := conn.DomainDefineXML(updatedXML)
	if err != nil {
		return fmt.Errorf("define updated domain xml: %w", err)
	}
	defer newDom.Free()

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
