package virsh

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
	libvirt "libvirt.org/go/libvirt"
)

type HugePagesInfo struct {
	Enabled      bool
	HasHugepages bool
	MemoryLocked bool
}

const defaultHugePagesMemoryBackingXML = `  <memoryBacking>
    <hugepages/>
  </memoryBacking>`

func inspectHugePagesFromDomainXML(xmlDesc string) (*HugePagesInfo, error) {
	var root rawDomainDeviceElement
	if err := xml.Unmarshal([]byte(xmlDesc), &root); err != nil {
		return nil, fmt.Errorf("parse domain xml: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(root.XMLName.Local), "domain") {
		return nil, fmt.Errorf("unexpected root element %q", root.XMLName.Local)
	}

	topLevel, err := parseXMLFragmentElements(root.InnerXML)
	if err != nil {
		return nil, fmt.Errorf("parse domain children: %w", err)
	}

	info := &HugePagesInfo{}
	for _, child := range topLevel {
		if elementLocalName(child) != "memorybacking" {
			continue
		}

		mbChildren, err := parseXMLFragmentElements(child.InnerXML)
		if err != nil {
			return nil, fmt.Errorf("parse memoryBacking children: %w", err)
		}
		for _, mbChild := range mbChildren {
			switch elementLocalName(mbChild) {
			case "hugepages":
				info.HasHugepages = true
				info.Enabled = true
			case "locked":
				info.MemoryLocked = true
			}
		}
		break
	}

	return info, nil
}

func rewriteHugePagesInDomainXML(xmlDesc string, enable bool) (string, *HugePagesInfo, error) {
	var root rawDomainDeviceElement
	if err := xml.Unmarshal([]byte(xmlDesc), &root); err != nil {
		return "", nil, fmt.Errorf("parse domain xml: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(root.XMLName.Local), "domain") {
		return "", nil, fmt.Errorf("unexpected root element %q", root.XMLName.Local)
	}

	topLevel, err := parseXMLFragmentElements(root.InnerXML)
	if err != nil {
		return "", nil, fmt.Errorf("parse domain children: %w", err)
	}

	memoryBackingIdx := -1
	devicesIdx := -1
	for i := range topLevel {
		switch elementLocalName(topLevel[i]) {
		case "memorybacking":
			memoryBackingIdx = i
		case "devices":
			if devicesIdx < 0 {
				devicesIdx = i
			}
		}
	}

	if enable {
		if memoryBackingIdx >= 0 {
			mbChildren, err := parseXMLFragmentElements(topLevel[memoryBackingIdx].InnerXML)
			if err != nil {
				return "", nil, fmt.Errorf("parse memoryBacking children: %w", err)
			}

			hasHugepages := false
			for _, mbChild := range mbChildren {
				if elementLocalName(mbChild) == "hugepages" {
					hasHugepages = true
					break
				}
			}

			if !hasHugepages {
				hugepagesElem, err := parseSingleFragmentElement("<hugepages/>")
				if err != nil {
					return "", nil, fmt.Errorf("parse hugepages xml: %w", err)
				}
				mbChildren = append(mbChildren, hugepagesElem)

				rebuiltMBInner, err := marshalXMLFragmentElements(mbChildren)
				if err != nil {
					return "", nil, fmt.Errorf("marshal memoryBacking children: %w", err)
				}
				topLevel[memoryBackingIdx].InnerXML = rebuiltMBInner
			}
		} else {
			memoryBackingElem, err := parseSingleFragmentElement(defaultHugePagesMemoryBackingXML)
			if err != nil {
				return "", nil, fmt.Errorf("parse memoryBacking xml: %w", err)
			}

			insertAt := devicesIdx
			if insertAt < 0 || insertAt > len(topLevel) {
				insertAt = len(topLevel)
			}
			topLevel = append(topLevel, rawDomainDeviceElement{})
			copy(topLevel[insertAt+1:], topLevel[insertAt:])
			topLevel[insertAt] = memoryBackingElem
		}
	} else if memoryBackingIdx >= 0 {
		mbChildren, err := parseXMLFragmentElements(topLevel[memoryBackingIdx].InnerXML)
		if err != nil {
			return "", nil, fmt.Errorf("parse memoryBacking children: %w", err)
		}

		filteredMB := make([]rawDomainDeviceElement, 0, len(mbChildren))
		for _, mbChild := range mbChildren {
			if elementLocalName(mbChild) == "hugepages" {
				continue
			}
			filteredMB = append(filteredMB, mbChild)
		}

		if len(filteredMB) == 0 {
			topLevel = append(topLevel[:memoryBackingIdx], topLevel[memoryBackingIdx+1:]...)
		} else {
			rebuiltMBInner, err := marshalXMLFragmentElements(filteredMB)
			if err != nil {
				return "", nil, fmt.Errorf("marshal memoryBacking children: %w", err)
			}
			topLevel[memoryBackingIdx].InnerXML = rebuiltMBInner
		}
	}

	rebuiltDomainInner, err := marshalXMLFragmentElements(topLevel)
	if err != nil {
		return "", nil, fmt.Errorf("marshal domain children: %w", err)
	}
	root.InnerXML = rebuiltDomainInner

	out, err := xml.Marshal(root)
	if err != nil {
		return "", nil, fmt.Errorf("marshal domain xml: %w", err)
	}

	info, err := inspectHugePagesFromDomainXML(string(out))
	if err != nil {
		return "", nil, fmt.Errorf("inspect rebuilt domain xml: %w", err)
	}

	return string(out), info, nil
}

func GetHugePages(vmName string) (*HugePagesInfo, error) {
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

	xmlDesc, err := getDomainXMLInactiveOrCurrent(dom)
	if err != nil {
		return nil, err
	}

	return inspectHugePagesFromDomainXML(xmlDesc)
}

func SetHugePages(vmName string, enable bool) error {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return fmt.Errorf("vm name is empty")
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
	if state != libvirt.DOMAIN_SHUTOFF && state != libvirt.DOMAIN_SHUTDOWN {
		return fmt.Errorf("vm %s must be shut off before editing hugepages settings (current state: %s)", vmName, domainStateToString(state).String())
	}

	originalXML, err := getDomainXMLInactiveOrCurrent(dom)
	if err != nil {
		return err
	}

	updatedXML, info, err := rewriteHugePagesInDomainXML(originalXML, enable)
	if err != nil {
		return fmt.Errorf("rewrite hugepages in domain XML: %w", err)
	}
	if updatedXML == "" {
		return fmt.Errorf("rewritten domain XML is empty")
	}
	if updatedXML == originalXML {
		logger.Info("hugepages already configured", "vm", vmName, "enabled", info.Enabled, "memory_locked", info.MemoryLocked)
		return nil
	}

	newDom, err := conn.DomainDefineXML(updatedXML)
	if err != nil {
		return fmt.Errorf("define updated domain xml: %w", err)
	}
	defer newDom.Free()

	logger.Info("updated hugepages", "vm", vmName, "enabled", info.Enabled, "memory_locked", info.MemoryLocked)
	return nil
}
