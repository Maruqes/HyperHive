package virsh

import (
	"strings"
	"testing"
)

func TestBuildCPUPinningPlanDoesNotReserveCoreForSmallHTAllocation(t *testing.T) {
	sockets := simulatedHTSockets(4)
	config := CPUPinningConfig{
		RangeStart:     0,
		RangeEnd:       1,
		HyperThreading: true,
		SocketID:       0,
	}

	plan, err := buildCPUPinningPlan(config, sockets)
	if err != nil {
		t.Fatalf("buildCPUPinningPlan() error = %v", err)
	}

	if plan.GuestPhysicalCores != 2 {
		t.Fatalf("GuestPhysicalCores = %d, want 2", plan.GuestPhysicalCores)
	}
	if got, want := len(plan.GuestPins), 4; got != want {
		t.Fatalf("len(GuestPins) = %d, want %d", got, want)
	}
	if got, want := VCPUCount(config), 4; got != want {
		t.Fatalf("VCPUCount() = %d, want %d", got, want)
	}
}

func TestBuildCPUPinningPlanReservesOneCoreForLargerAllocations(t *testing.T) {
	sockets := simulatedHTSockets(6)
	config := CPUPinningConfig{
		RangeStart:     0,
		RangeEnd:       3,
		HyperThreading: true,
		SocketID:       0,
	}

	plan, err := buildCPUPinningPlan(config, sockets)
	if err != nil {
		t.Fatalf("buildCPUPinningPlan() error = %v", err)
	}

	if plan.GuestPhysicalCores != 3 {
		t.Fatalf("GuestPhysicalCores = %d, want 3", plan.GuestPhysicalCores)
	}
	if got, want := len(plan.GuestPins), 6; got != want {
		t.Fatalf("len(GuestPins) = %d, want %d", got, want)
	}
	if got, want := formatCPUSet(plan.EmulatorCPUs), "3,9"; got != want {
		t.Fatalf("EmulatorCPUs = %q, want %q", got, want)
	}
}

func TestBuildCPUTuneXMLPinsIOThreadsWithEmulator(t *testing.T) {
	pins := []VCPUPin{
		{VCPU: 0, CPUSet: "0"},
		{VCPU: 1, CPUSet: "6"},
	}

	xml := buildCPUTuneXML(pins, []int{3, 9}, []int{1, 2}, "  ")

	for _, want := range []string{
		"<emulatorpin cpuset='3,9'/>",
		"<iothreadpin iothread='1' cpuset='3,9'/>",
		"<iothreadpin iothread='2' cpuset='3,9'/>",
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("cputune XML missing %q:\n%s", want, xml)
		}
	}
}

func TestRewriteCPUPinningInDomainXMLPinsIOThreadsWithoutHT(t *testing.T) {
	sockets := simulatedHTSockets(4)
	config := CPUPinningConfig{
		RangeStart:     0,
		RangeEnd:       3,
		HyperThreading: false,
		SocketID:       0,
	}

	xml, err := rewriteCPUPinningInDomainXML(testDomainXMLWithIOThreads(), config, sockets)
	if err != nil {
		t.Fatalf("rewriteCPUPinningInDomainXML() error = %v", err)
	}

	for _, want := range []string{
		"<vcpu placement='static' current='3'>3</vcpu>",
		"<topology sockets='1' cores='3' threads='1'/>",
		"<emulatorpin cpuset='3'/>",
		"<iothreadpin iothread='1' cpuset='3'/>",
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("rewritten XML missing %q:\n%s", want, xml)
		}
	}
}

func TestRewriteCPUPinningInDomainXMLPinsIOThreadsWithHT(t *testing.T) {
	sockets := simulatedHTSockets(4)
	config := CPUPinningConfig{
		RangeStart:     0,
		RangeEnd:       3,
		HyperThreading: true,
		SocketID:       0,
	}

	xml, err := rewriteCPUPinningInDomainXML(testDomainXMLWithIOThreads(), config, sockets)
	if err != nil {
		t.Fatalf("rewriteCPUPinningInDomainXML() error = %v", err)
	}

	for _, want := range []string{
		"<vcpu placement='static' current='6'>6</vcpu>",
		"<topology sockets='1' cores='3' threads='2'/>",
		"<emulatorpin cpuset='3,7'/>",
		"<iothreadpin iothread='1' cpuset='3,7'/>",
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("rewritten XML missing %q:\n%s", want, xml)
		}
	}
}

func TestParseIOThreadIDsFromXML(t *testing.T) {
	tests := []struct {
		name string
		xml  string
		want string
	}{
		{
			name: "count",
			xml:  "<domain><iothreads>2</iothreads></domain>",
			want: "1-2",
		},
		{
			name: "explicit ids",
			xml:  `<domain><iothreadids><iothread id="4"/><iothread id='2'/></iothreadids></domain>`,
			want: "2,4",
		},
		{
			name: "none",
			xml:  "<domain/>",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCPUSet(parseIOThreadIDsFromXML(tt.xml))
			if got != tt.want {
				t.Fatalf("parseIOThreadIDsFromXML() = %q, want %q", got, tt.want)
			}
		})
	}
}

func simulatedHTSockets(physicalCores int) []CPUSocket {
	cpus := make([]CPUInfo, 0, physicalCores*2)
	for i := 0; i < physicalCores; i++ {
		cpus = append(cpus, CPUInfo{
			CPUID:    i,
			Siblings: []int{i, i + physicalCores},
		})
	}
	for i := physicalCores; i < physicalCores*2; i++ {
		cpus = append(cpus, CPUInfo{
			CPUID:    i,
			Siblings: []int{i - physicalCores, i},
		})
	}
	return []CPUSocket{{SocketID: 0, CPUs: cpus}}
}

func testDomainXMLWithIOThreads() string {
	return `<domain type='kvm'>
  <name>test-vm</name>
  <memory unit='KiB'>1048576</memory>
  <vcpu placement='static'>2</vcpu>
  <iothreads>1</iothreads>
  <cpu mode='host-passthrough' check='none'>
    <topology sockets='1' cores='2' threads='1'/>
  </cpu>
  <devices>
    <emulator>/usr/bin/qemu-system-x86_64</emulator>
  </devices>
</domain>`
}
