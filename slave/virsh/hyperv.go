package virsh

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	libvirt "libvirt.org/go/libvirt"
)

type hyperVFeature struct {
	Name  string
	Attrs []xml.Attr
}

type hyperVConfig struct {
	Mode        string
	Features    []hyperVFeature
	EnsureClock bool
}

const (
	hyperVModeCustom      = "custom"
	hyperVModeHostModel   = "host-model"
	hyperVModePassthrough = "passthrough"

	libvirtHyperVPassthroughVersion = uint32(8_000_000)
	libvirtHyperVHostModelVersion   = uint32(11_009_000)
)

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

var legacyHyperVFeatures = []hyperVFeature{
	defaultHyperVFeatures[0],
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

	config := hyperVConfig{}
	if enabled {
		config, err = getHyperVConfigForDomainXML(currentXML)
		if err != nil {
			return false, err
		}
	}

	updatedXML, err := rewriteHyperVInDomainXML(currentXML, enabled, config)
	if err != nil {
		return false, err
	}
	if enabled && config.EnsureClock {
		updatedXML, err = rewriteHyperVClockInDomainXML(updatedXML)
		if err != nil {
			return false, err
		}
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

type hyperVCapabilityRequest struct {
	Emulator string
	Arch     string
	Machine  string
	VirtType string
}

func getHyperVConfigForDomainXML(domainXML string) (hyperVConfig, error) {
	request, err := extractHyperVCapabilityRequestFromDomainXML(domainXML)
	if err != nil {
		return hyperVConfig{}, err
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return hyperVConfig{}, fmt.Errorf("connect to libvirt for Hyper-V capabilities: %w", err)
	}
	defer conn.Close()

	libvirtVersion, err := conn.GetLibVersion()
	if err != nil {
		return hyperVConfig{}, fmt.Errorf("get libvirt version for Hyper-V: %w", err)
	}
	if config, ok := selectHostAwareHyperVConfig(libvirtVersion); ok {
		return config, nil
	}

	capsXML, err := conn.GetDomainCapabilities(
		request.Emulator,
		request.Arch,
		request.Machine,
		request.VirtType,
		0,
	)
	if err == nil {
		supported, parseErr := parseSupportedHyperVFeaturesFromDomainCapabilities(capsXML)
		if parseErr == nil {
			return customHyperVConfig(selectSupportedHyperVFeatures(supported)), nil
		}
	}

	return customHyperVConfig(legacyHyperVFeatures), nil
}

func extractHyperVCapabilityRequestFromDomainXML(domainXML string) (hyperVCapabilityRequest, error) {
	type capabilityDomainXML struct {
		XMLName  xml.Name `xml:"domain"`
		VirtType string   `xml:"type,attr"`
		OS       struct {
			Type struct {
				Arch    string `xml:"arch,attr"`
				Machine string `xml:"machine,attr"`
			} `xml:"type"`
		} `xml:"os"`
		Devices struct {
			Emulator string `xml:"emulator"`
		} `xml:"devices"`
	}

	domainXML = strings.TrimSpace(domainXML)
	if domainXML == "" {
		return hyperVCapabilityRequest{}, fmt.Errorf("domain XML is empty")
	}

	var parsed capabilityDomainXML
	if err := xml.Unmarshal([]byte(domainXML), &parsed); err != nil {
		return hyperVCapabilityRequest{}, fmt.Errorf("parse domain XML for Hyper-V capabilities: %w", err)
	}
	if !strings.EqualFold(parsed.XMLName.Local, "domain") {
		return hyperVCapabilityRequest{}, fmt.Errorf("domain XML has no <domain> root element")
	}

	return hyperVCapabilityRequest{
		Emulator: strings.TrimSpace(parsed.Devices.Emulator),
		Arch:     strings.TrimSpace(parsed.OS.Type.Arch),
		Machine:  strings.TrimSpace(parsed.OS.Type.Machine),
		VirtType: strings.TrimSpace(parsed.VirtType),
	}, nil
}

func parseSupportedHyperVFeaturesFromDomainCapabilities(capsXML string) (map[string]struct{}, error) {
	type enumXML struct {
		Name   string   `xml:"name,attr"`
		Values []string `xml:"value"`
	}
	type domainCapabilitiesXML struct {
		XMLName  xml.Name `xml:"domainCapabilities"`
		Features struct {
			HyperV struct {
				Supported string    `xml:"supported,attr"`
				Enums     []enumXML `xml:"enum"`
			} `xml:"hyperv"`
		} `xml:"features"`
	}

	var parsed domainCapabilitiesXML
	if err := xml.Unmarshal([]byte(capsXML), &parsed); err != nil {
		return nil, fmt.Errorf("parse Hyper-V domain capabilities: %w", err)
	}
	if !strings.EqualFold(parsed.XMLName.Local, "domainCapabilities") {
		return nil, fmt.Errorf("domain capabilities XML has no <domainCapabilities> root element")
	}
	if !isEnabledXMLState(parsed.Features.HyperV.Supported) {
		return nil, fmt.Errorf("host does not report Hyper-V domain capability support")
	}

	supported := make(map[string]struct{})
	for _, enum := range parsed.Features.HyperV.Enums {
		if !strings.EqualFold(strings.TrimSpace(enum.Name), "features") {
			continue
		}
		for _, value := range enum.Values {
			name := strings.ToLower(strings.TrimSpace(value))
			if name != "" {
				supported[name] = struct{}{}
			}
		}
	}
	if len(supported) == 0 {
		return nil, fmt.Errorf("host reports no supported Hyper-V features")
	}
	return supported, nil
}

func selectSupportedHyperVFeatures(supported map[string]struct{}) []hyperVFeature {
	selected := make([]hyperVFeature, 0, len(defaultHyperVFeatures))
	selectedNames := make(map[string]struct{}, len(defaultHyperVFeatures))

	for _, feature := range defaultHyperVFeatures {
		if _, ok := supported[feature.Name]; !ok {
			continue
		}
		if !hyperVFeatureDependenciesSelected(feature.Name, selectedNames) {
			continue
		}
		selected = append(selected, feature)
		selectedNames[feature.Name] = struct{}{}
	}
	return selected
}

func selectHostAwareHyperVConfig(libvirtVersion uint32) (hyperVConfig, bool) {
	switch {
	case libvirtVersion >= libvirtHyperVHostModelVersion:
		return hyperVConfig{
			Mode:        hyperVModeHostModel,
			EnsureClock: true,
		}, true
	case libvirtVersion >= libvirtHyperVPassthroughVersion:
		return hyperVConfig{
			Mode:        hyperVModePassthrough,
			EnsureClock: true,
		}, true
	default:
		return hyperVConfig{}, false
	}
}

func customHyperVConfig(features []hyperVFeature) hyperVConfig {
	return hyperVConfig{
		Mode:        hyperVModeCustom,
		Features:    features,
		EnsureClock: hasHyperVFeature(features, "stimer"),
	}
}

func hyperVFeatureDependenciesSelected(name string, selected map[string]struct{}) bool {
	switch name {
	case "synic":
		return hasHyperVFeatureName(selected, "vpindex")
	case "stimer":
		return hasHyperVFeatureName(selected, "vpindex") &&
			hasHyperVFeatureName(selected, "synic")
	default:
		return true
	}
}

func hasHyperVFeature(features []hyperVFeature, name string) bool {
	for _, feature := range features {
		if strings.EqualFold(feature.Name, name) {
			return true
		}
	}
	return false
}

func hasHyperVFeatureName(features map[string]struct{}, name string) bool {
	_, ok := features[name]
	return ok
}

func rewriteHyperVInDomainXML(domainXML string, enabled bool, config hyperVConfig) (string, error) {
	domainXML = strings.TrimSpace(domainXML)
	if domainXML == "" {
		return "", fmt.Errorf("domain XML is empty")
	}
	if enabled {
		if strings.TrimSpace(config.Mode) == "" {
			return "", fmt.Errorf("cannot enable Hyper-V without a configuration mode")
		}
		if config.Mode == hyperVModeCustom && len(config.Features) == 0 {
			return "", fmt.Errorf("cannot enable custom Hyper-V without host-supported features")
		}
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
					if err := writeHyperVBlock(encoder, config); err != nil {
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
				if err := writeHyperVBlock(encoder, config); err != nil {
					return "", err
				}
			}

			if err := encoder.EncodeToken(t); err != nil {
				return "", err
			}

			if !hasFeatures && enabled && depth == 2 && strings.EqualFold(t.Name.Local, "os") {
				if err := writeFeaturesHyperVBlock(encoder, config); err != nil {
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

func rewriteHyperVClockInDomainXML(domainXML string) (string, error) {
	domainXML = strings.TrimSpace(domainXML)
	if domainXML == "" {
		return "", fmt.Errorf("domain XML is empty")
	}

	hasClock, err := hasRootDomainElement(domainXML, "clock")
	if err != nil {
		return "", err
	}
	hasCPU, err := hasRootDomainElement(domainXML, "cpu")
	if err != nil {
		return "", err
	}

	decoder := xml.NewDecoder(strings.NewReader(domainXML))
	var buf bytes.Buffer
	encoder := xml.NewEncoder(&buf)

	depth := 0
	inClock := false
	clockDepth := -1
	clockHadHyperVClock := false
	injectedClock := false

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
			if depth == 1 && strings.EqualFold(t.Name.Local, "clock") {
				inClock = true
				clockDepth = depth + 1
				clockHadHyperVClock = false
			}
			if inClock &&
				depth == clockDepth &&
				strings.EqualFold(t.Name.Local, "timer") &&
				hasXMLAttrValue(t.Attr, "name", "hypervclock") {
				t.Attr = setXMLAttr(t.Attr, "present", "yes")
				clockHadHyperVClock = true
			}

			if err := encoder.EncodeToken(t); err != nil {
				return "", err
			}
			depth++
		case xml.EndElement:
			if inClock && depth == clockDepth && strings.EqualFold(t.Name.Local, "clock") && !clockHadHyperVClock {
				if err := writeHyperVClockTimer(encoder); err != nil {
					return "", err
				}
			}

			if err := encoder.EncodeToken(t); err != nil {
				return "", err
			}

			if !hasClock && !injectedClock && depth == 2 {
				switch {
				case hasCPU && strings.EqualFold(t.Name.Local, "cpu"):
					if err := writeHyperVClockBlock(encoder); err != nil {
						return "", err
					}
					injectedClock = true
				case !hasCPU && strings.EqualFold(t.Name.Local, "features"):
					if err := writeHyperVClockBlock(encoder); err != nil {
						return "", err
					}
					injectedClock = true
				}
			}
			if inClock && depth == clockDepth && strings.EqualFold(t.Name.Local, "clock") {
				inClock = false
				clockDepth = -1
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

	if !hasClock && !injectedClock {
		return "", fmt.Errorf("domain XML has no insertion point for Hyper-V clock")
	}
	if err := encoder.Flush(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func writeHyperVClockBlock(encoder *xml.Encoder) error {
	clockStart := xml.StartElement{
		Name: xml.Name{Local: "clock"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "offset"}, Value: "utc"}},
	}
	if err := encoder.EncodeToken(clockStart); err != nil {
		return err
	}
	if err := writeHyperVClockTimer(encoder); err != nil {
		return err
	}
	return encoder.EncodeToken(xml.EndElement{Name: clockStart.Name})
}

func writeHyperVClockTimer(encoder *xml.Encoder) error {
	timerStart := xml.StartElement{
		Name: xml.Name{Local: "timer"},
		Attr: []xml.Attr{
			{Name: xml.Name{Local: "name"}, Value: "hypervclock"},
			{Name: xml.Name{Local: "present"}, Value: "yes"},
		},
	}
	if err := encoder.EncodeToken(timerStart); err != nil {
		return err
	}
	return encoder.EncodeToken(xml.EndElement{Name: timerStart.Name})
}

func hasXMLAttrValue(attrs []xml.Attr, name, value string) bool {
	for _, attr := range attrs {
		if strings.EqualFold(attr.Name.Local, name) &&
			strings.EqualFold(strings.TrimSpace(attr.Value), value) {
			return true
		}
	}
	return false
}

func writeFeaturesHyperVBlock(encoder *xml.Encoder, config hyperVConfig) error {
	if err := encoder.EncodeToken(xml.StartElement{Name: xml.Name{Local: "features"}}); err != nil {
		return err
	}
	if err := writeHyperVBlock(encoder, config); err != nil {
		return err
	}
	return encoder.EncodeToken(xml.EndElement{Name: xml.Name{Local: "features"}})
}

func writeHyperVBlock(encoder *xml.Encoder, config hyperVConfig) error {
	hypervStart := xml.StartElement{
		Name: xml.Name{Local: "hyperv"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "mode"}, Value: config.Mode}},
	}
	if err := encoder.EncodeToken(hypervStart); err != nil {
		return err
	}
	for _, feature := range config.Features {
		start := xml.StartElement{Name: xml.Name{Local: feature.Name}, Attr: feature.Attrs}
		if err := encoder.EncodeToken(start); err != nil {
			return err
		}
		if err := encoder.EncodeToken(xml.EndElement{Name: start.Name}); err != nil {
			return err
		}
	}
	return encoder.EncodeToken(xml.EndElement{Name: hypervStart.Name})
}
