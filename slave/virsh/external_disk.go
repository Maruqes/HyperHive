package virsh

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	libvirt "libvirt.org/go/libvirt"
)

func AttachExternalDisk(vmName, diskPath, format string) (string, error) {
	vmName = strings.TrimSpace(vmName)
	diskPath = strings.TrimSpace(diskPath)
	if vmName == "" {
		return "", fmt.Errorf("vm name is empty")
	}
	if diskPath == "" {
		return "", fmt.Errorf("disk path is empty")
	}
	if err := ensureExternalDiskFile(diskPath); err != nil {
		return "", err
	}

	format = normalizeExternalDiskFormat(format, diskPath)

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return "", fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return "", fmt.Errorf("get inactive xml: %w", err)
	}
	if domainXMLHasDiskSource(xmlDesc, diskPath) {
		return "", fmt.Errorf("disk %s is already attached to vm %s", diskPath, vmName)
	}

	targetDev, err := nextVirtioDiskTarget(xmlDesc)
	if err != nil {
		return "", err
	}
	deviceXML := externalDiskXML(diskPath, format, targetDev)

	flags, err := diskModifyFlags(dom)
	if err != nil {
		return "", err
	}
	if err := dom.AttachDeviceFlags(deviceXML, flags); err != nil {
		return "", fmt.Errorf("attach external disk: %w", err)
	}

	return targetDev, nil
}

func DetachExternalDisk(vmName, diskPath string) (string, error) {
	vmName = strings.TrimSpace(vmName)
	diskPath = strings.TrimSpace(diskPath)
	if vmName == "" {
		return "", fmt.Errorf("vm name is empty")
	}
	if diskPath == "" {
		return "", fmt.Errorf("disk path is empty")
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return "", fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return "", fmt.Errorf("get inactive xml: %w", err)
	}
	deviceXML, targetDev, err := findDiskDeviceXMLBySource(xmlDesc, diskPath)
	if err != nil {
		return "", err
	}

	flags, err := diskModifyFlags(dom)
	if err != nil {
		return "", err
	}
	if err := dom.DetachDeviceFlags(deviceXML, flags); err != nil {
		return "", fmt.Errorf("detach external disk: %w", err)
	}

	return targetDev, nil
}

func ensureExternalDiskFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat disk %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("disk path is a directory: %s", path)
	}
	return nil
}

func normalizeExternalDiskFormat(format, path string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "raw":
		return "raw"
	case "qcow", "qcow2":
		return "qcow2"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".raw":
		return "raw"
	default:
		return "qcow2"
	}
}

func diskModifyFlags(dom *libvirt.Domain) (libvirt.DomainDeviceModifyFlags, error) {
	state, _, err := dom.GetState()
	if err != nil {
		return 0, fmt.Errorf("get state: %w", err)
	}
	flags := libvirt.DOMAIN_DEVICE_MODIFY_CONFIG
	if state == libvirt.DOMAIN_RUNNING || state == libvirt.DOMAIN_BLOCKED || state == libvirt.DOMAIN_PAUSED {
		flags |= libvirt.DOMAIN_DEVICE_MODIFY_LIVE
	}
	return flags, nil
}

func externalDiskXML(path, format, targetDev string) string {
	return fmt.Sprintf(
		"<disk type='file' device='disk'><driver name='qemu' type='%s' cache='none' io='native'/><source file='%s'/><target dev='%s' bus='virtio'/></disk>",
		xmlAttrEscape(format),
		xmlAttrEscape(path),
		xmlAttrEscape(targetDev),
	)
}

func xmlAttrEscape(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}

func domainXMLHasDiskSource(xmlDesc, diskPath string) bool {
	_, _, err := findDiskDeviceXMLBySource(xmlDesc, diskPath)
	return err == nil
}

func nextVirtioDiskTarget(xmlDesc string) (string, error) {
	used := make(map[string]struct{})
	re := regexp.MustCompile(`(?is)<target\b[^>]*\bdev\s*=\s*['"]([^'"]+)['"][^>]*/?>`)
	for _, match := range re.FindAllStringSubmatch(xmlDesc, -1) {
		if len(match) > 1 {
			used[strings.TrimSpace(match[1])] = struct{}{}
		}
	}

	for ch := 'b'; ch <= 'z'; ch++ {
		dev := fmt.Sprintf("vd%c", ch)
		if _, ok := used[dev]; !ok {
			return dev, nil
		}
	}
	return "", fmt.Errorf("no free virtio disk target available")
}

func findDiskDeviceXMLBySource(xmlDesc, diskPath string) (string, string, error) {
	blocks := regexp.MustCompile(`(?is)<disk\b[^>]*device\s*=\s*['"]disk['"][^>]*>.*?</disk>`).FindAllString(xmlDesc, -1)
	for _, block := range blocks {
		source := sourceFileFromDiskBlock(block)
		if source != diskPath {
			continue
		}
		target := targetDevFromDiskBlock(block)
		if target == "" {
			return "", "", fmt.Errorf("disk %s has no target dev", diskPath)
		}
		return strings.TrimSpace(block), target, nil
	}
	return "", "", fmt.Errorf("disk %s is not attached", diskPath)
}

func sourceFileFromDiskBlock(block string) string {
	re := regexp.MustCompile(`(?is)<source\b[^>]*\bfile\s*=\s*['"]([^'"]+)['"][^>]*/?>`)
	match := re.FindStringSubmatch(block)
	if len(match) < 2 {
		return ""
	}
	value := strings.TrimSpace(match[1])
	if unescaped, err := xmlUnescape(value); err == nil {
		return unescaped
	}
	return value
}

func targetDevFromDiskBlock(block string) string {
	re := regexp.MustCompile(`(?is)<target\b[^>]*\bdev\s*=\s*['"]([^'"]+)['"][^>]*/?>`)
	match := re.FindStringSubmatch(block)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func xmlUnescape(value string) (string, error) {
	var decoded string
	if err := xml.Unmarshal([]byte("<v>"+value+"</v>"), &decoded); err != nil {
		return "", err
	}
	return decoded, nil
}
