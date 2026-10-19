package virsh

import (
	"context"
	"encoding/xml"
	"errors"
	"strings"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	libvirt "libvirt.org/go/libvirt"
)

// snapshotDomain is deliberately narrow so secure XML flags and all fail-closed
// paths can be verified without a hypervisor or any real guest.
type snapshotDomain interface {
	GetName() (string, error)
	GetUUIDString() (string, error)
	IsActive() (bool, error)
	GetXMLDesc(libvirt.DomainXMLFlags) (string, error)
}

func readVMSnapshot(ctx context.Context, dom snapshotDomain, name string) (*grpcVirsh.VMSnapshotResponse, error) {
	failure := errors.New("secure VM snapshot unavailable")
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	actual, err := dom.GetName()
	if err != nil || actual != name {
		return nil, failure
	}
	uuid, err := dom.GetUUIDString()
	if err != nil || strings.TrimSpace(uuid) == "" {
		return nil, failure
	}
	active, err := dom.IsActive()
	if err != nil {
		return nil, failure
	}
	inactive, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_SECURE | libvirt.DOMAIN_XML_INACTIVE)
	if err != nil || !snapshotXMLIdentity(inactive, name, uuid) {
		return nil, failure
	}
	current := ""
	if active {
		current, err = dom.GetXMLDesc(libvirt.DOMAIN_XML_SECURE)
		if err != nil || !snapshotXMLIdentity(current, name, uuid) {
			return nil, failure
		}
	}
	// A changing domain cannot yield a reliable paired inactive/current record.
	activeAfter, err := dom.IsActive()
	if err != nil || activeAfter != active {
		return nil, failure
	}
	uuidAfter, err := dom.GetUUIDString()
	if err != nil || uuidAfter != uuid {
		return nil, failure
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &grpcVirsh.VMSnapshotResponse{Name: name, Uuid: uuid, Active: active, InactiveXml: inactive, CurrentXml: current}, nil
}

func snapshotXMLIdentity(value, name, uuid string) bool {
	var doc struct {
		XMLName xml.Name `xml:"domain"`
		Name    string   `xml:"name"`
		UUID    string   `xml:"uuid"`
	}
	return xml.Unmarshal([]byte(value), &doc) == nil && doc.XMLName.Local == "domain" && doc.Name == name && doc.UUID == uuid
}

func (s *SlaveVirshService) GetVMSnapshot(ctx context.Context, req *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.VMSnapshotResponse, error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("VM name is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return nil, errors.New("secure VM snapshot unavailable")
	}
	defer conn.Close()
	dom, err := conn.LookupDomainByName(req.Name)
	if err != nil {
		return nil, errors.New("secure VM snapshot unavailable")
	}
	defer dom.Free()
	return readVMSnapshot(ctx, dom, req.Name)
}
