package virsh

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"

	libvirt "libvirt.org/go/libvirt"
)

type machineTypeCapabilities struct {
	Guests []machineTypeGuest `xml:"guest"`
}

type machineTypeGuest struct {
	OSType string          `xml:"os_type"`
	Arch   machineTypeArch `xml:"arch"`
}

type machineTypeArch struct {
	Name     string               `xml:"name,attr"`
	Machines []machineTypeMachine `xml:"machine"`
}

type machineTypeMachine struct {
	Name      string `xml:",chardata"`
	Canonical string `xml:"canonical,attr"`
}

func ListMachineTypes() ([]string, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	capabilitiesXML, err := conn.GetCapabilities()
	if err != nil {
		return nil, fmt.Errorf("get capabilities: %w", err)
	}

	return parseMachineTypesFromCapabilities(capabilitiesXML)
}

func parseMachineTypesFromCapabilities(capabilitiesXML string) ([]string, error) {
	var caps machineTypeCapabilities
	if err := xml.Unmarshal([]byte(capabilitiesXML), &caps); err != nil {
		return nil, fmt.Errorf("parse capabilities xml: %w", err)
	}

	machineTypes := collectMachineTypes(caps.Guests, func(guest machineTypeGuest) bool {
		return strings.EqualFold(strings.TrimSpace(guest.OSType), "hvm") &&
			strings.EqualFold(strings.TrimSpace(guest.Arch.Name), "x86_64")
	})
	if len(machineTypes) == 0 {
		machineTypes = collectMachineTypes(caps.Guests, func(guest machineTypeGuest) bool {
			return strings.EqualFold(strings.TrimSpace(guest.OSType), "hvm")
		})
	}
	if len(machineTypes) == 0 {
		return nil, fmt.Errorf("no hvm machine types found in libvirt capabilities")
	}
	return machineTypes, nil
}

func collectMachineTypes(guests []machineTypeGuest, include func(machineTypeGuest) bool) []string {
	seen := make(map[string]struct{})
	for _, guest := range guests {
		if !include(guest) {
			continue
		}
		for _, machine := range guest.Arch.Machines {
			for _, name := range []string{machine.Name, machine.Canonical} {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				seen[name] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func normalizeMachineType(machineType string, supported []string) (string, bool) {
	machineType = strings.TrimSpace(machineType)
	for _, supportedType := range supported {
		if strings.TrimSpace(supportedType) == machineType {
			return strings.TrimSpace(supportedType), true
		}
	}
	for _, supportedType := range supported {
		supportedType = strings.TrimSpace(supportedType)
		if strings.EqualFold(supportedType, machineType) {
			return supportedType, true
		}
	}
	return "", false
}

func SetMachineType(vmName, machineType string) (string, error) {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return "", fmt.Errorf("vm name is empty")
	}
	machineType = strings.TrimSpace(machineType)
	if machineType == "" {
		return "", fmt.Errorf("machine type is empty")
	}

	supportedTypes, err := ListMachineTypes()
	if err != nil {
		return "", err
	}
	requestedMachineType := machineType
	machineType, ok := normalizeMachineType(machineType, supportedTypes)
	if !ok {
		return "", fmt.Errorf("machine type %q is not supported by this host", requestedMachineType)
	}

	currentXML, err := GetVMXML(vmName)
	if err != nil {
		return "", err
	}
	if extractMachineTypeFromDomainXML(currentXML) == machineType {
		return machineType, nil
	}

	updatedXML, err := rewriteMachineTypeInDomainXML(currentXML, machineType)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(updatedXML) == "" {
		return "", fmt.Errorf("rewritten domain XML is empty")
	}

	if err := UpdateVMXml(vmName, updatedXML); err != nil {
		return "", err
	}
	return machineType, nil
}

func extractMachineTypeFromDomainXML(domainXML string) string {
	type domainMachineTypeXML struct {
		OS struct {
			Type struct {
				Machine string `xml:"machine,attr"`
			} `xml:"type"`
		} `xml:"os"`
	}

	var parsed domainMachineTypeXML
	if err := xml.Unmarshal([]byte(domainXML), &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.OS.Type.Machine)
}

func rewriteMachineTypeInDomainXML(domainXML, machineType string) (string, error) {
	domainXML = strings.TrimSpace(domainXML)
	if domainXML == "" {
		return "", fmt.Errorf("domain XML is empty")
	}
	machineType = strings.TrimSpace(machineType)
	if machineType == "" {
		return "", fmt.Errorf("machine type is empty")
	}

	decoder := xml.NewDecoder(strings.NewReader(domainXML))
	var buf bytes.Buffer
	encoder := xml.NewEncoder(&buf)

	depth := 0
	inOS := false
	osDepth := -1
	skipDepth := 0
	machineTypeSet := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}

		if skipDepth > 0 {
			switch tok.(type) {
			case xml.StartElement:
				skipDepth++
			case xml.EndElement:
				skipDepth--
			}
			continue
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if shouldDropForMachineTypeRewrite(t) {
				skipDepth = 1
				continue
			}

			if depth == 1 && strings.EqualFold(t.Name.Local, "os") {
				inOS = true
				osDepth = depth + 1
			}
			if inOS && depth == osDepth && strings.EqualFold(t.Name.Local, "type") {
				t.Attr = setXMLAttr(t.Attr, "machine", machineType)
				machineTypeSet = true
			}

			if err := encoder.EncodeToken(t); err != nil {
				return "", err
			}
			depth++
		case xml.EndElement:
			if err := encoder.EncodeToken(t); err != nil {
				return "", err
			}
			if inOS && depth == osDepth && strings.EqualFold(t.Name.Local, "os") {
				inOS = false
				osDepth = -1
			}
			if depth > 0 {
				depth--
			}
		default:
			if err := encoder.EncodeToken(tok); err != nil {
				return "", err
			}
		}
	}

	if !machineTypeSet {
		return "", fmt.Errorf("domain XML has no <os><type> element to update")
	}
	if err := encoder.Flush(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func shouldDropForMachineTypeRewrite(start xml.StartElement) bool {
	local := strings.ToLower(strings.TrimSpace(start.Name.Local))
	switch local {
	case "controller":
		return strings.EqualFold(xmlAttrValue(start.Attr, "type"), "pci")
	case "address":
		return strings.EqualFold(xmlAttrValue(start.Attr, "type"), "pci")
	default:
		return false
	}
}

func setXMLAttr(attrs []xml.Attr, name, value string) []xml.Attr {
	for i := range attrs {
		if attrs[i].Name.Space == "" && strings.EqualFold(attrs[i].Name.Local, name) {
			attrs[i].Value = value
			return attrs
		}
	}
	return append(attrs, xml.Attr{Name: xml.Name{Local: name}, Value: value})
}
