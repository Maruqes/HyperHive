package virsh

import (
	"context"
	"errors"
	libvirt "libvirt.org/go/libvirt"
	"testing"
)

type snapshotFake struct {
	active      bool
	fail        string
	flags       []libvirt.DomainXMLFlags
	activeCalls int
}

func (f *snapshotFake) GetName() (string, error) {
	if f.fail == "name" {
		return "", errors.New("PRIVATE")
	}
	return "guest", nil
}
func (f *snapshotFake) GetUUIDString() (string, error) {
	if f.fail == "uuid" {
		return "", errors.New("PRIVATE")
	}
	return "fixture-uuid", nil
}
func (f *snapshotFake) IsActive() (bool, error) {
	f.activeCalls++
	if f.fail == "active" {
		return false, errors.New("PRIVATE")
	}
	if f.fail == "changed" && f.activeCalls > 1 {
		return !f.active, nil
	}
	return f.active, nil
}
func (f *snapshotFake) GetXMLDesc(flags libvirt.DomainXMLFlags) (string, error) {
	f.flags = append(f.flags, flags)
	if f.fail == "xml" || f.fail == "current" && flags == libvirt.DOMAIN_XML_SECURE {
		return "", errors.New("PRIVATE xml")
	}
	name := "guest"
	if f.fail == "identity" {
		name = "other"
	}
	return "<domain><name>" + name + "</name><uuid>fixture-uuid</uuid><devices><graphics type='vnc' passwd='actual-secret'/></devices></domain>", nil
}
func TestVMSnapshotSecureFlags(t *testing.T) {
	for _, active := range []bool{false, true} {
		fake := &snapshotFake{active: active}
		result, err := readVMSnapshot(context.Background(), fake, "guest")
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if active {
			want = 2
		}
		if len(fake.flags) != want || fake.flags[0] != libvirt.DOMAIN_XML_SECURE|libvirt.DOMAIN_XML_INACTIVE {
			t.Fatalf("unsafe flags %v", fake.flags)
		}
		if active && fake.flags[1] != libvirt.DOMAIN_XML_SECURE {
			t.Fatal("current XML not secure")
		}
		if result.Active != active || result.InactiveXml == "" || active && result.CurrentXml == "" {
			t.Fatal("incomplete snapshot")
		}
	}
}
func TestVMSnapshotFailsClosed(t *testing.T) {
	for _, failure := range []string{"name", "uuid", "active", "xml", "current", "identity", "changed"} {
		fake := &snapshotFake{active: true, fail: failure}
		result, err := readVMSnapshot(context.Background(), fake, "guest")
		if err == nil || result != nil || err.Error() != "secure VM snapshot unavailable" {
			t.Fatalf("%s: leaked or incomplete snapshot %#v %v", failure, result, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &snapshotFake{}
	if result, err := readVMSnapshot(ctx, fake, "guest"); err == nil || result != nil || len(fake.flags) > 0 {
		t.Fatal("canceled snapshot read")
	}
}
