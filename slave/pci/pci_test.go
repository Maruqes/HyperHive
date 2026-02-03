package pci

import (
	"strings"
	"testing"
)

func TestParsePCIAddress(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "full bdf", input: "0000:65:00.0", want: "0000:65:00.0"},
		{name: "short bdf", input: "65:00.0", want: "0000:65:00.0"},
		{name: "node name", input: "pci_0000_65_00_1", want: "0000:65:00.1"},
		{name: "raw node name", input: "0000_65_00_2", want: "0000:65:00.2"},
		{name: "sysfs path", input: "/sys/bus/pci/devices/0000:03:00.0", want: "0000:03:00.0"},
		{name: "invalid", input: "gpu0", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := ParsePCIAddress(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if got := addr.String(); got != tt.want {
				t.Fatalf("unexpected parsed address for %q: got %s want %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseHostPCIDevice(t *testing.T) {
	const xmlDesc = `
<device>
  <name>pci_0000_65_00_0</name>
  <path>/sys/devices/pci0000:00/0000:65:00.0</path>
  <driver><name>vfio-pci</name></driver>
  <capability type='pci'>
    <class>0x030000</class>
    <domain>0</domain>
    <bus>101</bus>
    <slot>0</slot>
    <function>0</function>
    <product id='0x1b80'>GeForce GTX</product>
    <vendor id='0x10de'>NVIDIA</vendor>
    <iommuGroup number='53'/>
    <numa node='1'/>
  </capability>
</device>`

	dev, err := parseHostPCIDevice(xmlDesc)
	if err != nil {
		t.Fatalf("parseHostPCIDevice failed: %v", err)
	}

	if dev.Address != "0000:65:00.0" {
		t.Fatalf("unexpected address: %s", dev.Address)
	}
	if !dev.IsGPU {
		t.Fatalf("expected IsGPU=true")
	}
	if !dev.ManagedByVFIO {
		t.Fatalf("expected ManagedByVFIO=true")
	}
	if dev.IOMMUGroup != 53 {
		t.Fatalf("unexpected iommu group: %d", dev.IOMMUGroup)
	}
}

func TestParseVMPCIDevices(t *testing.T) {
	const xmlDesc = `
<domain>
  <devices>
    <hostdev mode='subsystem' type='pci' managed='yes'>
      <source>
        <address domain='0x0000' bus='0x65' slot='0x00' function='0x0'/>
      </source>
      <alias name='hostdev0'/>
    </hostdev>
    <hostdev mode='subsystem' type='usb' managed='yes'>
      <source/>
    </hostdev>
  </devices>
</domain>`

	devs, err := parseVMPCIDevices(xmlDesc)
	if err != nil {
		t.Fatalf("parseVMPCIDevices failed: %v", err)
	}
	if len(devs) != 1 {
		t.Fatalf("expected 1 pci device, got %d", len(devs))
	}
	if devs[0].Address != "0000:65:00.0" {
		t.Fatalf("unexpected vm pci address: %s", devs[0].Address)
	}
	if !devs[0].Managed {
		t.Fatalf("expected managed hostdev")
	}
	if devs[0].Alias != "hostdev0" {
		t.Fatalf("unexpected alias: %s", devs[0].Alias)
	}
}

func TestBuildHostDevXML(t *testing.T) {
	addr, err := ParsePCIAddress("0000:65:00.0")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	xmlDesc := buildHostDevXML(addr, true)
	for _, token := range []string{
		"type='pci'",
		"managed='yes'",
		"domain='0x0000'",
		"bus='0x65'",
		"slot='0x00'",
		"function='0x0'",
	} {
		if !strings.Contains(xmlDesc, token) {
			t.Fatalf("expected %q in xml: %s", token, xmlDesc)
		}
	}
}

func TestFilterPCIDevicesWithIOMMU(t *testing.T) {
	input := []HostPCIDevice{
		{Address: "0000:01:00.0", IOMMUGroup: -1},
		{Address: "0000:02:00.0", IOMMUGroup: 1},
		{Address: "0000:03:00.0", IOMMUGroup: 7},
	}

	out := filterPCIDevicesWithIOMMU(input)
	if len(out) != 2 {
		t.Fatalf("expected 2 devices with iommu, got %d", len(out))
	}
	if out[0].Address != "0000:02:00.0" {
		t.Fatalf("unexpected first device: %s", out[0].Address)
	}
	if out[1].Address != "0000:03:00.0" {
		t.Fatalf("unexpected second device: %s", out[1].Address)
	}
}

func TestPartitionPCIAttachments(t *testing.T) {
	tests := []struct {
		name              string
		attachedVMs       []string
		targetVM          string
		wantAttached      bool
		wantAttachedOther []string
	}{
		{
			name:              "attached to target only",
			attachedVMs:       []string{"win10"},
			targetVM:          "win10",
			wantAttached:      true,
			wantAttachedOther: []string{},
		},
		{
			name:              "attached elsewhere only",
			attachedVMs:       []string{"ubuntu", "fedora"},
			targetVM:          "win10",
			wantAttached:      false,
			wantAttachedOther: []string{"fedora", "ubuntu"},
		},
		{
			name:              "attached target and elsewhere with spaces and duplicates",
			attachedVMs:       []string{" win10 ", "ubuntu", "ubuntu", ""},
			targetVM:          "win10",
			wantAttached:      true,
			wantAttachedOther: []string{"ubuntu"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAttached, gotOther := partitionPCIAttachments(tt.attachedVMs, tt.targetVM)
			if gotAttached != tt.wantAttached {
				t.Fatalf("unexpected attached flag: got %v want %v", gotAttached, tt.wantAttached)
			}
			if len(gotOther) != len(tt.wantAttachedOther) {
				t.Fatalf("unexpected attached-other len: got %d want %d (%v)", len(gotOther), len(tt.wantAttachedOther), gotOther)
			}
			for i := range gotOther {
				if gotOther[i] != tt.wantAttachedOther[i] {
					t.Fatalf("unexpected attached-other at index %d: got %q want %q", i, gotOther[i], tt.wantAttachedOther[i])
				}
			}
		})
	}
}

func TestMakeAddressSetFromHostDevices(t *testing.T) {
	input := []HostPCIDevice{
		{Address: "0000:01:00.0"},
		{Address: " 0000:02:00.0 "},
		{Address: ""},
		{Address: "0000:01:00.0"},
	}

	set := makeAddressSetFromHostDevices(input)
	if len(set) != 2 {
		t.Fatalf("unexpected set size: got %d want 2", len(set))
	}
	if _, ok := set["0000:01:00.0"]; !ok {
		t.Fatalf("expected address 0000:01:00.0 in set")
	}
	if _, ok := set["0000:02:00.0"]; !ok {
		t.Fatalf("expected address 0000:02:00.0 in set")
	}
}

func TestFilterVMPCIDevicesByAddress(t *testing.T) {
	devices := []VMPCIDevice{
		{Address: "0000:01:00.0", Alias: "hostdev0"},
		{Address: "0000:02:00.0", Alias: "hostdev1"},
		{Address: "0000:03:00.0", Alias: "hostdev2"},
	}
	allowed := map[string]struct{}{
		"0000:01:00.0": {},
		"0000:03:00.0": {},
	}

	out := filterVMPCIDevicesByAddress(devices, allowed)
	if len(out) != 2 {
		t.Fatalf("unexpected filtered len: got %d want 2", len(out))
	}
	if out[0].Address != "0000:01:00.0" || out[1].Address != "0000:03:00.0" {
		t.Fatalf("unexpected filtered addresses: %+v", out)
	}
}
