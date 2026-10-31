package virsh

import "testing"

func TestVMXMLHasDiskPath(t *testing.T) {
	const path = "/pool/disk & data.qcow2"
	xml := `<domain><devices><disk><source file="/pool/other.qcow2"/></disk><disk><source file="/pool/disk &amp; data.qcow2"/></disk></devices></domain>`
	matched, err := VMXMLHasDiskPath(xml, path)
	if err != nil || !matched {
		t.Fatalf("matched = %v, err = %v", matched, err)
	}
	if _, err := VMXMLHasDiskPath("<invalid>", path); err == nil {
		t.Fatal("accepted malformed VM XML")
	}
}
