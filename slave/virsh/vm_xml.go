package virsh

import (
	"encoding/xml"
	"fmt"
	"strings"

	libvirt "libvirt.org/go/libvirt"
)

func GetVMXML(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("vm name is empty")
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return "", fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	xmlDesc, err := getDomainXMLInactiveOrCurrent(dom)
	if err != nil {
		return "", err
	}
	return xmlDesc, nil
}

type domainNameOnly struct {
	XMLName xml.Name `xml:"domain"`
	Name    string   `xml:"name"`
}

func UpdateVMXml(vmName, vmXml string) error {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return fmt.Errorf("vm name is empty")
	}

	vmXml = strings.TrimSpace(vmXml)
	if vmXml == "" {
		return fmt.Errorf("vm xml is empty")
	}

	var parsed domainNameOnly
	if err := xml.Unmarshal([]byte(vmXml), &parsed); err != nil {
		return fmt.Errorf("parse domain xml: %w", err)
	}
	if !strings.EqualFold(parsed.XMLName.Local, "domain") {
		return fmt.Errorf("invalid domain xml: missing <domain> root element")
	}
	parsedName := strings.TrimSpace(parsed.Name)
	if parsedName == "" {
		return fmt.Errorf("invalid domain xml: missing <name>")
	}
	if parsedName != vmName {
		return fmt.Errorf("domain xml name %q must match target VM %q", parsedName, vmName)
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

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}
	if state != libvirt.DOMAIN_SHUTOFF {
		return fmt.Errorf("domain %s must be shut off to update XML", vmName)
	}

	newDom, err := conn.DomainDefineXMLFlags(vmXml, libvirt.DOMAIN_DEFINE_VALIDATE)
	if err != nil {
		return fmt.Errorf("define domain with new xml: %w", err)
	}
	defer newDom.Free()

	return nil
}
