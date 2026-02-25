package virsh

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os/exec"
	"regexp"
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

var (
	vncGraphicsDevicePattern    = regexp.MustCompile(`(?is)\n?[ \t]*<graphics\b[^>]*\btype\s*=\s*['"]vnc['"][^>]*(?:/>|>.*?</graphics>)\n?`)
	spiceGraphicsDevicePattern  = regexp.MustCompile(`(?is)\n?[ \t]*<graphics\b[^>]*\btype\s*=\s*['"]spice['"][^>]*(?:/>|>.*?</graphics>)\n?`)
	videoDevicePattern          = regexp.MustCompile(`(?is)\n?[ \t]*<video\b[^>]*(?:/>|>.*?</video>)\n?`)
	soundDevicePattern          = regexp.MustCompile(`(?is)\n?[ \t]*<sound\b[^>]*(?:/>|>.*?</sound>)\n?`)
	audioDevicePattern          = regexp.MustCompile(`(?is)\n?[ \t]*<audio\b[^>]*(?:/>|>.*?</audio>)\n?`)
	spiceChannelDevicePattern   = regexp.MustCompile(`(?is)\n?[ \t]*<channel\b[^>]*\btype\s*=\s*['"]spicevmc['"][^>]*(?:/>|>.*?</channel>)\n?`)
	spiceRedirdevDevicePattern  = regexp.MustCompile(`(?is)\n?[ \t]*<redirdev\b[^>]*\btype\s*=\s*['"]spicevmc['"][^>]*(?:/>|>.*?</redirdev>)\n?`)
	spiceSmartcardDevicePattern = regexp.MustCompile(`(?is)\n?[ \t]*<smartcard\b[^>]*\btype\s*=\s*['"]spicevmc['"][^>]*(?:/>|>.*?</smartcard>)\n?`)
	excessBlankLinesPattern     = regexp.MustCompile(`\n{3,}`)
)

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

func removeXMLNodesByPattern(xmlDesc string, pattern *regexp.Regexp) (string, int) {
	matches := pattern.FindAllStringIndex(xmlDesc, -1)
	if len(matches) == 0 {
		return xmlDesc, 0
	}
	return pattern.ReplaceAllString(xmlDesc, ""), len(matches)
}

func removeNoVNCAndSpiceDevicesFromDomainXML(xmlDesc string) (string, noVNCDeviceRemovalSummary, error) {
	var summary noVNCDeviceRemovalSummary

	updatedXML := xmlDesc
	updatedXML, summary.VNCGraphics = removeXMLNodesByPattern(updatedXML, vncGraphicsDevicePattern)
	updatedXML, summary.SpiceGraphics = removeXMLNodesByPattern(updatedXML, spiceGraphicsDevicePattern)
	updatedXML, summary.Videos = removeXMLNodesByPattern(updatedXML, videoDevicePattern)
	updatedXML, summary.Sounds = removeXMLNodesByPattern(updatedXML, soundDevicePattern)
	updatedXML, summary.Audios = removeXMLNodesByPattern(updatedXML, audioDevicePattern)
	updatedXML, summary.SpiceChannels = removeXMLNodesByPattern(updatedXML, spiceChannelDevicePattern)
	updatedXML, summary.SpiceRedirdevs = removeXMLNodesByPattern(updatedXML, spiceRedirdevDevicePattern)
	updatedXML, summary.SpiceSmartcards = removeXMLNodesByPattern(updatedXML, spiceSmartcardDevicePattern)

	if summary.VNCGraphics == 0 &&
		summary.SpiceGraphics == 0 &&
		summary.Videos == 0 &&
		summary.Sounds == 0 &&
		summary.Audios == 0 &&
		summary.SpiceChannels == 0 &&
		summary.SpiceRedirdevs == 0 &&
		summary.SpiceSmartcards == 0 {
		return xmlDesc, summary, nil
	}

	updatedXML = excessBlankLinesPattern.ReplaceAllString(updatedXML, "\n\n")
	if strings.TrimSpace(updatedXML) == "" {
		return "", summary, fmt.Errorf("removing VNC/SPICE devices produced empty domain XML")
	}

	return updatedXML, summary, nil
}

func insertDevicesBeforeDevicesCloseTag(xmlDesc string, devices ...string) (string, error) {
	if strings.TrimSpace(xmlDesc) == "" {
		return "", fmt.Errorf("domain XML is empty")
	}
	if len(devices) == 0 {
		return xmlDesc, nil
	}

	closeIdx := strings.LastIndex(xmlDesc, "</devices>")
	if closeIdx < 0 {
		return "", fmt.Errorf("domain XML does not contain </devices>")
	}

	var block strings.Builder
	for _, deviceXML := range devices {
		if strings.TrimSpace(deviceXML) == "" {
			continue
		}
		block.WriteByte('\n')
		block.WriteString(deviceXML)
		block.WriteByte('\n')
	}
	if block.Len() == 0 {
		return xmlDesc, nil
	}

	return xmlDesc[:closeIdx] + block.String() + xmlDesc[closeIdx:], nil
}

func addNoVNCAndSpiceDevicesToDomainXML(xmlDesc string) (string, noVNCDeviceAddSummary, error) {
	var summary noVNCDeviceAddSummary

	updatedXML, err := insertDevicesBeforeDevicesCloseTag(
		xmlDesc,
		defaultSpiceChannelDeviceXML,
		defaultVNCGraphicsDeviceXML,
		defaultSpiceGraphicsDeviceXML,
		defaultVideoDeviceXML,
		defaultSoundDeviceXML,
	)
	if err != nil {
		return "", summary, err
	}

	summary.SpiceChannels = 1
	summary.VNCGraphics = 1
	summary.SpiceGraphics = 1
	summary.Videos = 1
	summary.Sounds = 1
	return updatedXML, summary, nil
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

	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		xmlDesc, err = dom.GetXMLDesc(0)
		if err != nil {
			return fmt.Errorf("get xml: %w", err)
		}
	}

	cleanXML, removedSummary, err := removeNoVNCAndSpiceDevicesFromDomainXML(xmlDesc)
	if err != nil {
		return err
	}

	updatedXML, addedSummary, err := addNoVNCAndSpiceDevicesToDomainXML(cleanXML)
	if err != nil {
		return fmt.Errorf("restore noVNC/SPICE devices in domain XML: %w", err)
	}
	if updatedXML == xmlDesc {
		logger.Info("noVNC/SPICE graphics/audio devices already enabled", "vm", vmName)
		return nil
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

	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		xmlDesc, err = dom.GetXMLDesc(0)
		if err != nil {
			return fmt.Errorf("get xml: %w", err)
		}
	}

	updatedXML, summary, err := removeNoVNCAndSpiceDevicesFromDomainXML(xmlDesc)
	if err != nil {
		return err
	}
	if updatedXML == xmlDesc {
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
