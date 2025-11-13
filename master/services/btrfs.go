package services

import (
	"512SvMan/btrfs"
	"512SvMan/protocol"
	"fmt"

	btrfsGrpc "github.com/Maruqes/512SvMan/api/proto/btrfs"
)

type BTRFSService struct{}

func (s *BTRFSService) GetAllDisks(machineName string) (*btrfsGrpc.MinDiskArr, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	return btrfs.GetAllDisks(conn.Connection)
}

func (s *BTRFSService) GetAllFileSystems(machineName string) (*btrfsGrpc.FindMntOutput, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	return btrfs.GetAllFileSystems(conn.Connection)
}

// DUPLICATE ON SLAVE
// DUPLICATE ON SLAVE
// DUPLICATE ON SLAVE
type raidType struct {
	sType string // perfil de dados (-d)
	sMeta string // perfil de metadados (-m)
	c     int    // numero minimo de discos
}

// DUPLICATE ON SLAVE
// DUPLICATE ON SLAVE
// DUPLICATE ON SLAVE
var (
	Raid0 = raidType{
		sType: "raid0",
		sMeta: "single",
		c:     2,
	}

	Raid1 = raidType{
		sType: "raid1",
		sMeta: "raid1",
		c:     2,
	}

	Raid1c3 = raidType{
		sType: "raid1c3",
		sMeta: "raid1c3",
		c:     3,
	}

	Raid1c4 = raidType{
		sType: "raid1c4",
		sMeta: "raid1c4",
		c:     4,
	}
)

func convertStringToRaidType(s string) *raidType {
	switch s {
	case Raid0.sType:
		return &Raid0
	case Raid1.sType:
		return &Raid1
	case Raid1c3.sType:
		return &Raid1c3
	case Raid1c4.sType:
		return &Raid1c4
	default:
		return nil
	}
}

func (s *BTRFSService) CreateRaid(machineName string, name string, raid string, disks ...string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}

	raidtype := convertStringToRaidType(raid)
	if raidtype == nil {
		return fmt.Errorf("raid type is not valid")
	}

	req := btrfsGrpc.CreateRaidReq{Name: name, Raid: raidtype.sType, Disks: disks}
	return btrfs.CreateRaid(conn.Connection, &req)
}
