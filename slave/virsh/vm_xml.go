package virsh

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
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
	UUID    string   `xml:"uuid"`
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

	currentUUID, err := dom.GetUUIDString()
	if err != nil {
		return fmt.Errorf("get uuid: %w", err)
	}

	if strings.TrimSpace(parsed.UUID) != strings.TrimSpace(currentUUID) {
		vmXml, err = ensureDomainXMLUUID(vmXml, currentUUID)
		if err != nil {
			return fmt.Errorf("normalize domain xml uuid: %w", err)
		}
	}

	newDom, err := conn.DomainDefineXMLFlags(vmXml, libvirt.DOMAIN_DEFINE_VALIDATE)
	if err != nil {
		return fmt.Errorf("define domain with new xml: %w", err)
	}
	defer newDom.Free()

	return nil
}

func ensureDomainXMLUUID(vmXML, uuid string) (string, error) {
	vmXML = strings.TrimSpace(vmXML)
	uuid = strings.TrimSpace(uuid)
	if vmXML == "" {
		return "", fmt.Errorf("vm xml is empty")
	}
	if uuid == "" {
		return "", fmt.Errorf("uuid is empty")
	}

	dec := xml.NewDecoder(strings.NewReader(vmXML))
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)

	depth := 0
	inRootName := false
	rootUUIDSeen := false
	uuidSkipDepth := 0
	injectedUUIDAfterName := false

	writeUUIDElement := func() error {
		if err := enc.EncodeToken(xml.StartElement{Name: xml.Name{Local: "uuid"}}); err != nil {
			return err
		}
		if err := enc.EncodeToken(xml.CharData([]byte(uuid))); err != nil {
			return err
		}
		if err := enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: "uuid"}}); err != nil {
			return err
		}
		rootUUIDSeen = true
		return nil
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		if uuidSkipDepth > 0 {
			switch t := tok.(type) {
			case xml.StartElement:
				uuidSkipDepth++
				_ = t
			case xml.EndElement:
				uuidSkipDepth--
				if uuidSkipDepth == 0 {
					if err := enc.EncodeToken(t); err != nil {
						return "", err
					}
				}
			}
			continue
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 1 && strings.EqualFold(t.Name.Local, "uuid") {
				rootUUIDSeen = true
				if err := enc.EncodeToken(t); err != nil {
					return "", err
				}
				if err := enc.EncodeToken(xml.CharData([]byte(uuid))); err != nil {
					return "", err
				}
				uuidSkipDepth = 1
				continue
			}
			if depth == 1 && strings.EqualFold(t.Name.Local, "name") {
				inRootName = true
			}
			if err := enc.EncodeToken(t); err != nil {
				return "", err
			}
			depth++
		case xml.EndElement:
			// depth here is current nesting before consuming the end token
			if inRootName && depth == 2 && strings.EqualFold(t.Name.Local, "name") {
				inRootName = false
				if err := enc.EncodeToken(t); err != nil {
					return "", err
				}
				if !rootUUIDSeen && !injectedUUIDAfterName {
					if err := writeUUIDElement(); err != nil {
						return "", err
					}
					injectedUUIDAfterName = true
				}
				if depth > 0 {
					depth--
				}
				continue
			}
			// Fallback: inject uuid before closing root if <name> handling didn't inject it.
			if depth == 1 && strings.EqualFold(t.Name.Local, "domain") && !rootUUIDSeen {
				if err := writeUUIDElement(); err != nil {
					return "", err
				}
			}
			if err := enc.EncodeToken(t); err != nil {
				return "", err
			}
			if depth > 0 {
				depth--
			}
		case xml.CharData:
			if inRootName {
				// preserve incoming root <name> content; caller already validates matching name
				if err := enc.EncodeToken(t); err != nil {
					return "", err
				}
				continue
			}
			if err := enc.EncodeToken(t); err != nil {
				return "", err
			}
		default:
			if err := enc.EncodeToken(tok); err != nil {
				return "", err
			}
		}
	}

	if err := enc.Flush(); err != nil {
		return "", err
	}
	return buf.String(), nil
}
