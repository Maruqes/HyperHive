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

	noVNCEnabledVideoSpec  = "model.type=virtio,model.heads=1"
	noVNCDisabledVideoSpec = "model.type=none"
)

type NoVNCVideoInfo struct {
	Enabled    bool
	VideoCount int32
	ModelType  string
	VideoXML   string
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

	if err := EnsureVirtXMLInstalled(); err != nil {
		return err
	}
	if err := ensureVMShutOff(vmName); err != nil {
		return err
	}

	videoInfo, err := GetNoVNCVideo(vmName)
	if err != nil {
		return err
	}

	args := []string{"--connect", "qemu:///system", vmName}
	if videoInfo.VideoCount == 0 {
		args = append(args, "--add-device")
	} else {
		args = append(args, "--edit", "all")
	}
	args = append(args, "--video", noVNCEnabledVideoSpec)

	if err := runCmdDiscardOutput(virtXMLBinary, args...); err != nil {
		return fmt.Errorf("enable noVNC video: %w", err)
	}

	logger.Info("enabled noVNC video", "vm", vmName)
	return nil
}

func RemoveNoVNCVideo(vmName string) error {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return fmt.Errorf("vm name is empty")
	}

	if err := EnsureVirtXMLInstalled(); err != nil {
		return err
	}
	if err := ensureVMShutOff(vmName); err != nil {
		return err
	}

	videoInfo, err := GetNoVNCVideo(vmName)
	if err != nil {
		return err
	}

	args := []string{"--connect", "qemu:///system", vmName}
	if videoInfo.VideoCount == 0 {
		args = append(args, "--add-device")
	} else {
		args = append(args, "--edit", "all")
	}
	args = append(args, "--video", noVNCDisabledVideoSpec)

	if err := runCmdDiscardOutput(virtXMLBinary, args...); err != nil {
		return fmt.Errorf("disable noVNC video: %w", err)
	}

	logger.Info("disabled noVNC video", "vm", vmName)
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
