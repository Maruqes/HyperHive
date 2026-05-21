package virsh

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

type hyperVFeature struct {
	Name  string
	Attrs []xml.Attr
}

var defaultHyperVFeatures = []hyperVFeature{
	{Name: "relaxed", Attrs: []xml.Attr{{Name: xml.Name{Local: "state"}, Value: "on"}}},
	{Name: "vapic", Attrs: []xml.Attr{{Name: xml.Name{Local: "state"}, Value: "on"}}},
	{Name: "spinlocks", Attrs: []xml.Attr{
		{Name: xml.Name{Local: "state"}, Value: "on"},
		{Name: xml.Name{Local: "retries"}, Value: "8191"},
	}},
	{Name: "vpindex", Attrs: []xml.Attr{{Name: xml.Name{Local: "state"}, Value: "on"}}},
	{Name: "runtime", Attrs: []xml.Attr{{Name: xml.Name{Local: "state"}, Value: "on"}}},
	{Name: "synic", Attrs: []xml.Attr{{Name: xml.Name{Local: "state"}, Value: "on"}}},
	{Name: "stimer", Attrs: []xml.Attr{{Name: xml.Name{Local: "state"}, Value: "on"}}},
	{Name: "reset", Attrs: []xml.Attr{{Name: xml.Name{Local: "state"}, Value: "on"}}},
	{Name: "frequencies", Attrs: []xml.Attr{{Name: xml.Name{Local: "state"}, Value: "on"}}},
}

func GetHyperV(vmName string) (bool, error) {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return false, fmt.Errorf("vm name is empty")
	}

	vmXML, err := GetVMXML(vmName)
	if err != nil {
		return false, err
	}
	return extractHyperVEnabledFromDomainXML(vmXML), nil
}

func SetHyperV(vmName string, enabled bool) (bool, error) {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return false, fmt.Errorf("vm name is empty")
	}

	currentXML, err := GetVMXML(vmName)
	if err != nil {
		return false, err
	}

	updatedXML, err := rewriteHyperVInDomainXML(currentXML, enabled)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(updatedXML) == "" {
		return false, fmt.Errorf("rewritten domain XML is empty")
	}

	if err := UpdateVMXml(vmName, updatedXML); err != nil {
		return false, err
	}
	return enabled, nil
}

func extractHyperVEnabledFromDomainXML(domainXML string) bool {
	type domainHyperVXML struct {
		Features struct {
			HyperV struct {
				Mode      string   `xml:"mode,attr"`
				Relaxed   stateXML `xml:"relaxed"`
				VAPIC     stateXML `xml:"vapic"`
				Spinlocks stateXML `xml:"spinlocks"`
				VPIndex   stateXML `xml:"vpindex"`
				Runtime   stateXML `xml:"runtime"`
				SynIC     stateXML `xml:"synic"`
				STimer    stateXML `xml:"stimer"`
			} `xml:"hyperv"`
		} `xml:"features"`
	}

	var parsed domainHyperVXML
	if err := xml.Unmarshal([]byte(domainXML), &parsed); err != nil {
		return false
	}
	hyperv := parsed.Features.HyperV
	if strings.TrimSpace(hyperv.Mode) != "" {
		return true
	}
	return isEnabledXMLState(hyperv.Relaxed.State) ||
		isEnabledXMLState(hyperv.VAPIC.State) ||
		isEnabledXMLState(hyperv.Spinlocks.State) ||
		isEnabledXMLState(hyperv.VPIndex.State) ||
		isEnabledXMLState(hyperv.Runtime.State) ||
		isEnabledXMLState(hyperv.SynIC.State) ||
		isEnabledXMLState(hyperv.STimer.State)
}

type stateXML struct {
	State string `xml:"state,attr"`
}

func rewriteHyperVInDomainXML(domainXML string, enabled bool) (string, error) {
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
	featuresHadHyperV := false
	skipDepth := 0
	injectedFeatures := false

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
			if depth == 1 && strings.EqualFold(t.Name.Local, "features") {
				inFeatures = true
				featuresDepth = depth + 1
				featuresHadHyperV = false
			}
			if inFeatures && depth == featuresDepth && strings.EqualFold(t.Name.Local, "hyperv") {
				featuresHadHyperV = true
				skipDepth = 1
				if enabled {
					if err := writeHyperVBlock(encoder); err != nil {
						return "", err
					}
				}
				continue
			}

			if err := encoder.EncodeToken(t); err != nil {
				return "", err
			}
			depth++
		case xml.EndElement:
			if inFeatures && depth == featuresDepth && strings.EqualFold(t.Name.Local, "features") && enabled && !featuresHadHyperV {
				if err := writeHyperVBlock(encoder); err != nil {
					return "", err
				}
			}

			if err := encoder.EncodeToken(t); err != nil {
				return "", err
			}

			if !hasFeatures && enabled && depth == 2 && strings.EqualFold(t.Name.Local, "os") {
				if err := writeFeaturesHyperVBlock(encoder); err != nil {
					return "", err
				}
				injectedFeatures = true
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

	if !hasFeatures && enabled && !injectedFeatures {
		return "", fmt.Errorf("domain XML has no root <os> element to insert <features>")
	}
	if err := encoder.Flush(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func writeFeaturesHyperVBlock(encoder *xml.Encoder) error {
	if err := encoder.EncodeToken(xml.StartElement{Name: xml.Name{Local: "features"}}); err != nil {
		return err
	}
	if err := writeHyperVBlock(encoder); err != nil {
		return err
	}
	return encoder.EncodeToken(xml.EndElement{Name: xml.Name{Local: "features"}})
}

func writeHyperVBlock(encoder *xml.Encoder) error {
	hypervStart := xml.StartElement{
		Name: xml.Name{Local: "hyperv"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "mode"}, Value: "custom"}},
	}
	if err := encoder.EncodeToken(hypervStart); err != nil {
		return err
	}
	for _, feature := range defaultHyperVFeatures {
		start := xml.StartElement{Name: xml.Name{Local: feature.Name}, Attr: feature.Attrs}
		if err := encoder.EncodeToken(start); err != nil {
			return err
		}
		if feature.Name == "stimer" {
			directStart := xml.StartElement{
				Name: xml.Name{Local: "direct"},
				Attr: []xml.Attr{{Name: xml.Name{Local: "state"}, Value: "on"}},
			}
			if err := encoder.EncodeToken(directStart); err != nil {
				return err
			}
			if err := encoder.EncodeToken(xml.EndElement{Name: directStart.Name}); err != nil {
				return err
			}
		}
		if err := encoder.EncodeToken(xml.EndElement{Name: start.Name}); err != nil {
			return err
		}
	}
	return encoder.EncodeToken(xml.EndElement{Name: hypervStart.Name})
}
