package virsh

import (
	"strings"
	"testing"
)

func TestRewriteMachineTypeConvertsITCOWatchdogForI440FX(t *testing.T) {
	input := `<domain type='kvm'>
  <name>mine-live</name>
  <os>
    <type arch='x86_64' machine='pc-q35-10.1'>hvm</type>
  </os>
  <devices>
    <watchdog model='itco' action='reset'/>
  </devices>
</domain>`

	output, err := rewriteMachineTypeInDomainXML(input, "pc-i440fx-10.1")
	if err != nil {
		t.Fatalf("rewriteMachineTypeInDomainXML returned error: %v", err)
	}

	if !strings.Contains(output, `machine="pc-i440fx-10.1"`) {
		t.Fatalf("expected machine type to be rewritten, got:\n%s", output)
	}
	if strings.Contains(output, `model="itco"`) {
		t.Fatalf("expected itco watchdog to be rewritten, got:\n%s", output)
	}
	if !strings.Contains(output, `model="i6300esb"`) {
		t.Fatalf("expected i6300esb watchdog, got:\n%s", output)
	}
	if !strings.Contains(output, `action="reset"`) {
		t.Fatalf("expected watchdog action to be preserved, got:\n%s", output)
	}
}

func TestRewriteMachineTypeKeepsITCOWatchdogForQ35(t *testing.T) {
	input := `<domain type='kvm'>
  <name>mine-live</name>
  <os>
    <type arch='x86_64' machine='pc-i440fx-10.1'>hvm</type>
  </os>
  <devices>
    <watchdog model='itco' action='reset'/>
  </devices>
</domain>`

	output, err := rewriteMachineTypeInDomainXML(input, "pc-q35-10.1")
	if err != nil {
		t.Fatalf("rewriteMachineTypeInDomainXML returned error: %v", err)
	}

	if !strings.Contains(output, `machine="pc-q35-10.1"`) {
		t.Fatalf("expected machine type to be rewritten, got:\n%s", output)
	}
	if !strings.Contains(output, `model="itco"`) {
		t.Fatalf("expected itco watchdog to be preserved, got:\n%s", output)
	}
}
