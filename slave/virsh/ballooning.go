package virsh

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
	libvirt "libvirt.org/go/libvirt"
)

type MemoryBallooningInfo struct {
	Enabled         bool
	MemoryLocked    bool
	HasMemballoon   bool
	MemballoonModel string
}

const (
	defaultEnabledMemballoonXML = `  <memballoon model='virtio'>
    <stats period='10'/>
  </memballoon>`

	defaultDisabledMemballoonXML = `  <memballoon model='none'/>`

	defaultLockedMemoryBackingXML = `  <memoryBacking>
    <locked/>
  </memoryBacking>`
)

func elementLocalName(elem rawDomainDeviceElement) string {
	return strings.ToLower(strings.TrimSpace(elem.XMLName.Local))
}

func parseSingleFragmentElement(fragment string) (rawDomainDeviceElement, error) {
	elems, err := parseXMLFragmentElements(fragment)
	if err != nil {
		return rawDomainDeviceElement{}, err
	}
	if len(elems) != 1 {
		return rawDomainDeviceElement{}, fmt.Errorf("expected 1 element, got %d", len(elems))
	}
	return elems[0], nil
}

func inspectMemoryBallooningFromDomainXML(xmlDesc string) (*MemoryBallooningInfo, error) {
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

	info := &MemoryBallooningInfo{}

	for _, child := range topLevel {
		switch elementLocalName(child) {
		case "devices":
			deviceElems, err := parseXMLFragmentElements(child.InnerXML)
			if err != nil {
				return nil, fmt.Errorf("parse devices children: %w", err)
			}
			for _, dev := range deviceElems {
				if elementLocalName(dev) != "memballoon" {
					continue
				}
				info.HasMemballoon = true
				info.MemballoonModel = strings.TrimSpace(xmlAttrValue(dev.Attrs, "model"))
				modelLower := strings.ToLower(info.MemballoonModel)
				info.Enabled = modelLower != "" && modelLower != "none"
				break
			}
		case "memorybacking":
			mbChildren, err := parseXMLFragmentElements(child.InnerXML)
			if err != nil {
				return nil, fmt.Errorf("parse memoryBacking children: %w", err)
			}
			for _, mbChild := range mbChildren {
				if elementLocalName(mbChild) == "locked" {
					info.MemoryLocked = true
					break
				}
			}
		}
	}

	return info, nil
}

func rewriteMemoryBallooningInDomainXML(xmlDesc string, enable bool) (string, *MemoryBallooningInfo, error) {
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

	devicesIdx := -1
	for i := range topLevel {
		if elementLocalName(topLevel[i]) == "devices" {
			devicesIdx = i
			break
		}
	}
	if devicesIdx < 0 {
		return "", nil, fmt.Errorf("domain XML does not contain <devices>")
	}

	deviceElems, err := parseXMLFragmentElements(topLevel[devicesIdx].InnerXML)
	if err != nil {
		return "", nil, fmt.Errorf("parse devices children: %w", err)
	}

	filteredDevices := make([]rawDomainDeviceElement, 0, len(deviceElems)+1)
	for _, dev := range deviceElems {
		if elementLocalName(dev) == "memballoon" {
			continue
		}
		filteredDevices = append(filteredDevices, dev)
	}

	balloonXML := defaultEnabledMemballoonXML
	if !enable {
		balloonXML = defaultDisabledMemballoonXML
	}
	balloonElem, err := parseSingleFragmentElement(balloonXML)
	if err != nil {
		return "", nil, fmt.Errorf("parse memballoon xml: %w", err)
	}
	filteredDevices = append(filteredDevices, balloonElem)

	rebuiltDevicesInner, err := marshalXMLFragmentElements(filteredDevices)
	if err != nil {
		return "", nil, fmt.Errorf("marshal devices children: %w", err)
	}
	topLevel[devicesIdx].InnerXML = rebuiltDevicesInner

	memoryBackingIdx := -1
	for i := range topLevel {
		if elementLocalName(topLevel[i]) == "memorybacking" {
			memoryBackingIdx = i
			break
		}
	}

	if enable {
		if memoryBackingIdx >= 0 {
			mbChildren, err := parseXMLFragmentElements(topLevel[memoryBackingIdx].InnerXML)
			if err != nil {
				return "", nil, fmt.Errorf("parse memoryBacking children: %w", err)
			}

			filteredMB := make([]rawDomainDeviceElement, 0, len(mbChildren))
			for _, mbChild := range mbChildren {
				if elementLocalName(mbChild) == "locked" {
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
	} else {
		if memoryBackingIdx >= 0 {
			mbChildren, err := parseXMLFragmentElements(topLevel[memoryBackingIdx].InnerXML)
			if err != nil {
				return "", nil, fmt.Errorf("parse memoryBacking children: %w", err)
			}

			hasLocked := false
			for _, mbChild := range mbChildren {
				if elementLocalName(mbChild) == "locked" {
					hasLocked = true
					break
				}
			}

			if !hasLocked {
				lockedElem, err := parseSingleFragmentElement("<locked/>")
				if err != nil {
					return "", nil, fmt.Errorf("parse locked xml: %w", err)
				}
				mbChildren = append(mbChildren, lockedElem)
				rebuiltMBInner, err := marshalXMLFragmentElements(mbChildren)
				if err != nil {
					return "", nil, fmt.Errorf("marshal memoryBacking children: %w", err)
				}
				topLevel[memoryBackingIdx].InnerXML = rebuiltMBInner
			}
		} else {
			memoryBackingElem, err := parseSingleFragmentElement(defaultLockedMemoryBackingXML)
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

	info, err := inspectMemoryBallooningFromDomainXML(string(out))
	if err != nil {
		return "", nil, fmt.Errorf("inspect rebuilt domain xml: %w", err)
	}

	return string(out), info, nil
}

func GetMemoryBallooning(vmName string) (*MemoryBallooningInfo, error) {
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

	return inspectMemoryBallooningFromDomainXML(xmlDesc)
}

func SetMemoryBallooning(vmName string, enable bool) error {
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
		return fmt.Errorf("vm %s must be shut off before editing ballooning settings (current state: %s)", vmName, domainStateToString(state).String())
	}

	originalXML, err := getDomainXMLInactiveOrCurrent(dom)
	if err != nil {
		return err
	}

	updatedXML, info, err := rewriteMemoryBallooningInDomainXML(originalXML, enable)
	if err != nil {
		return fmt.Errorf("rewrite memory ballooning in domain XML: %w", err)
	}
	if updatedXML == "" {
		return fmt.Errorf("rewritten domain XML is empty")
	}
	if updatedXML == originalXML {
		logger.Info("memory ballooning already configured", "vm", vmName, "enabled", info.Enabled, "memory_locked", info.MemoryLocked, "model", info.MemballoonModel)
		return nil
	}

	newDom, err := conn.DomainDefineXML(updatedXML)
	if err != nil {
		return fmt.Errorf("define updated domain xml: %w", err)
	}
	defer newDom.Free()

	logger.Info("updated memory ballooning", "vm", vmName, "enabled", info.Enabled, "memory_locked", info.MemoryLocked, "model", info.MemballoonModel)
	return nil
}
