package services

import (
	"strings"
	"testing"
)

func TestRelocateDiskXMLPreservesDevices(t *testing.T) {
	input := `<domain><name>guest</name><devices><disk type="file" device="disk"><driver name="qemu" type="raw"/><source file="/one/data.raw"/><target dev="vdb" bus="virtio"/><serial>keep-me</serial></disk><disk><source file="/one/boot.qcow2"/><target dev="vda"/></disk><interface type="network"><source network="default"/></interface></devices></domain>`
	output, err := relocateDiskXML(input, "/one/data.raw", "/two/a&b.raw")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{`file="/two/a&amp;b.raw"`, `file="/one/boot.qcow2"`, `dev="vdb"`, `<serial>keep-me</serial>`, `network="default"`} {
		if !strings.Contains(output, text) {
			t.Fatal("lost device metadata", text)
		}
	}
	if err = verifyDiskXML(output, "/one/data.raw", "/two/a&b.raw"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{input + `<broken>`, strings.ReplaceAll(input, "/one/boot.qcow2", "/one/data.raw"), strings.ReplaceAll(input, "/one/data.raw", "/missing")} {
		if _, err = relocateDiskXML(bad, "/one/data.raw", "/two/data.raw"); err == nil {
			t.Fatal("accepted ambiguous/missing/malformed disk")
		}
	}
}
func TestDiskWorkerRejectsOverlap(t *testing.T) {
	lock, err := diskWorker("test-worker")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = diskWorker("test-worker"); err == nil {
		t.Fatal("overlapping operation")
	}
	lock.Unlock()
	lock, err = diskWorker("test-worker")
	if err != nil {
		t.Fatal(err)
	}
	lock.Unlock()
}
