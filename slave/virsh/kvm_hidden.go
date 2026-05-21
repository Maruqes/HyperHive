package virsh

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

func GetKVMHidden(vmName string) (bool, error) {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return false, fmt.Errorf("vm name is empty")
	}

	vmXML, err := GetVMXML(vmName)
	if err != nil {
		return false, err
	}
	return extractKVMHiddenFromDomainXML(vmXML), nil
}

func SetKVMHidden(vmName string, hidden bool) (bool, error) {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return false, fmt.Errorf("vm name is empty")
	}

	currentXML, err := GetVMXML(vmName)
	if err != nil {
		return false, err
	}

	updatedXML, err := rewriteKVMHiddenInDomainXML(currentXML, hidden)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(updatedXML) == "" {
		return false, fmt.Errorf("rewritten domain XML is empty")
	}

	if err := UpdateVMXml(vmName, updatedXML); err != nil {
		return false, err
	}
	return hidden, nil
}

func extractKVMHiddenFromDomainXML(domainXML string) bool {
	type domainKVMHiddenXML struct {
		Features struct {
			KVM struct {
				Hidden struct {
					State string `xml:"state,attr"`
				} `xml:"hidden"`
			} `xml:"kvm"`
		} `xml:"features"`
	}

	var parsed domainKVMHiddenXML
	if err := xml.Unmarshal([]byte(domainXML), &parsed); err != nil {
		return false
	}
	return isEnabledXMLState(parsed.Features.KVM.Hidden.State)
}

func rewriteKVMHiddenInDomainXML(domainXML string, hidden bool) (string, error) {
	domainXML = strings.TrimSpace(domainXML)
	if domainXML == "" {
		return "", fmt.Errorf("domain XML is empty")
	}

	hasFeatures, err := hasRootDomainElement(domainXML, "features")
	if err != nil {
		return "", err
	}

	decoder := xml.NewDecoder(strings.NewReader(domainXML))
	var buf bytes.Buffer
	encoder := xml.NewEncoder(&buf)

	depth := 0
	inFeatures := false
	featuresDepth := -1
	featuresHadKVM := false
	inKVM := false
	kvmDepth := -1
	kvmHadHidden := false
	injectedFeatures := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 1 && strings.EqualFold(t.Name.Local, "features") {
				inFeatures = true
				featuresDepth = depth + 1
				featuresHadKVM = false
			}
			if inFeatures && depth == featuresDepth && strings.EqualFold(t.Name.Local, "kvm") {
				inKVM = true
				kvmDepth = depth + 1
				kvmHadHidden = false
				featuresHadKVM = true
			}
			if inKVM && depth == kvmDepth && strings.EqualFold(t.Name.Local, "hidden") {
				t.Attr = setXMLAttr(t.Attr, "state", xmlState(hidden))
				kvmHadHidden = true
			}

			if err := encoder.EncodeToken(t); err != nil {
				return "", err
			}
			depth++
		case xml.EndElement:
			if inKVM && depth == kvmDepth && strings.EqualFold(t.Name.Local, "kvm") && !kvmHadHidden {
				if err := writeKVMHiddenElement(encoder, hidden); err != nil {
					return "", err
				}
			}
			if inFeatures && depth == featuresDepth && strings.EqualFold(t.Name.Local, "features") && !featuresHadKVM {
				if err := writeKVMBlock(encoder, hidden); err != nil {
					return "", err
				}
			}

			if err := encoder.EncodeToken(t); err != nil {
				return "", err
			}

			if !hasFeatures && depth == 2 && strings.EqualFold(t.Name.Local, "os") {
				if err := writeFeaturesKVMBlock(encoder, hidden); err != nil {
					return "", err
				}
				injectedFeatures = true
			}
			if inKVM && depth == kvmDepth && strings.EqualFold(t.Name.Local, "kvm") {
				inKVM = false
				kvmDepth = -1
			}
			if inFeatures && depth == featuresDepth && strings.EqualFold(t.Name.Local, "features") {
				inFeatures = false
				featuresDepth = -1
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

	if !hasFeatures && !injectedFeatures {
		return "", fmt.Errorf("domain XML has no root <os> element to insert <features>")
	}
	if err := encoder.Flush(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func hasRootDomainElement(domainXML, elementName string) (bool, error) {
	decoder := xml.NewDecoder(strings.NewReader(domainXML))
	depth := 0
	for {
		tok, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				return false, nil
			}
			return false, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 1 && strings.EqualFold(t.Name.Local, elementName) {
				return true, nil
			}
			depth++
		case xml.EndElement:
			if depth > 0 {
				depth--
			}
		}
	}
}

func writeFeaturesKVMBlock(encoder *xml.Encoder, hidden bool) error {
	if err := encoder.EncodeToken(xml.StartElement{Name: xml.Name{Local: "features"}}); err != nil {
		return err
	}
	if err := writeKVMBlock(encoder, hidden); err != nil {
		return err
	}
	return encoder.EncodeToken(xml.EndElement{Name: xml.Name{Local: "features"}})
}

func writeKVMBlock(encoder *xml.Encoder, hidden bool) error {
	if err := encoder.EncodeToken(xml.StartElement{Name: xml.Name{Local: "kvm"}}); err != nil {
		return err
	}
	if err := writeKVMHiddenElement(encoder, hidden); err != nil {
		return err
	}
	return encoder.EncodeToken(xml.EndElement{Name: xml.Name{Local: "kvm"}})
}

func writeKVMHiddenElement(encoder *xml.Encoder, hidden bool) error {
	start := xml.StartElement{
		Name: xml.Name{Local: "hidden"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "state"}, Value: xmlState(hidden)}},
	}
	if err := encoder.EncodeToken(start); err != nil {
		return err
	}
	return encoder.EncodeToken(xml.EndElement{Name: start.Name})
}

func xmlState(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func isEnabledXMLState(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "on", "yes", "true", "1":
		return true
	default:
		return false
	}
}
