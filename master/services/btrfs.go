package services

import (
	"512SvMan/btrfs"
	"512SvMan/protocol"
	"fmt"
	"strings"

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

func (s *BTRFSService) GetNotMountedDisks(machineName string) (*btrfsGrpc.MinDiskArr, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	disks, err := btrfs.GetAllDisks(conn.Connection)
	if err != nil {
		return nil, err
	}

	var ret btrfsGrpc.MinDiskArr
	for _, disk := range disks.Disks {
		if disk.Mounted == false {
			ret.Disks = append(ret.Disks, disk)
		}
	}

	return &ret, nil
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

func (s *BTRFSService) RemoveRaid(machineName string, uuid string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return btrfs.RemoveRaid(conn.Connection, &btrfsGrpc.UUIDReq{Uuid: uuid})
}

const (
	CompressionNone   = ""        // No compression (default)
	CompressionLZO    = "lzo"     // Fast compression, moderate ratio
	CompressionZlib   = "zlib"    // Highest compression ratio, slowest
	CompressionZlib1  = "zlib:1"  // Zlib level 1 (fastest)
	CompressionZlib3  = "zlib:3"  // Zlib level 3 (default)
	CompressionZlib9  = "zlib:9"  // Zlib level 9 (maximum compression)
	CompressionZstd   = "zstd"    // Recommended: best balance of speed/ratio
	CompressionZstd1  = "zstd:1"  // Zstd level 1 (fastest)
	CompressionZstd3  = "zstd:3"  // Zstd level 3 (default, recommended)
	CompressionZstd9  = "zstd:9"  // Zstd level 9 (high compression)
	CompressionZstd15 = "zstd:15" // Zstd level 15 (maximum compression)
)

func (s *BTRFSService) MountRaid(machineName string, uuid, target, compression string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}

	compression = strings.TrimSpace(compression)
	validCompressions := []string{
		CompressionNone,
		CompressionLZO,
		CompressionZlib,
		CompressionZlib1,
		CompressionZlib3,
		CompressionZlib9,
		CompressionZstd,
		CompressionZstd1,
		CompressionZstd3,
		CompressionZstd9,
		CompressionZstd15,
	}

	isValid := false
	for _, valid := range validCompressions {
		if compression == valid {
			isValid = true
			break
		}
	}
	if !isValid && compression != "" {
		return fmt.Errorf("invalid compression type: %s", compression)
	}

	return btrfs.MountRaid(conn.Connection, &btrfsGrpc.MountReq{Uuid: uuid, Target: target, Compression: compression})
}

func (s *BTRFSService) UMountRaid(machineName string, uuid string, force bool) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return btrfs.UMountRaid(conn.Connection, &btrfsGrpc.UMountReq{Uuid: uuid, Force: force})
}

func (s *BTRFSService) AddDiskToRaid(machineName string, uuid string, disk string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return btrfs.AddDiskToRaid(conn.Connection, &btrfsGrpc.AddDiskToRaidReq{Uuid: uuid, DiskPath: disk})
}

func (s *BTRFSService) RemoveDiskFromRaid(machineName string, uuid string, disk string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return btrfs.RemoveDiskFromRaid(conn.Connection, &btrfsGrpc.RemoveDiskFromRaidReq{Uuid: uuid, DiskPath: disk})
}

func (s *BTRFSService) ReplaceDiskInRaid(machineName string, uuid string, oldDisk string, newDisk string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return btrfs.ReplaceDiskInRaid(conn.Connection, &btrfsGrpc.ReplaceDiskToRaidReq{Uuid: uuid, OldDiskPath: oldDisk, NewDiskPath: newDisk})
}

func (s *BTRFSService) ChangeRaidLevel(machineName string, uuid string, newRaidLevel string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}

	raidtype := convertStringToRaidType(newRaidLevel)
	if raidtype == nil {
		return fmt.Errorf("raid type is not valid: %s", newRaidLevel)
	}

	return btrfs.ChangeRaidLevel(conn.Connection, &btrfsGrpc.ChangeRaidLevelReq{Uuid: uuid, RaidType: raidtype.sType})
}

func (s *BTRFSService) BalanceRaid(machineName string, uuid string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return btrfs.BalanceRaid(conn.Connection, &btrfsGrpc.UUIDReq{Uuid: uuid})
}

func (s *BTRFSService) DefragmentRaid(machineName string, uuid string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return btrfs.DefragmentRaid(conn.Connection, &btrfsGrpc.UUIDReq{Uuid: uuid})
}

func (s *BTRFSService) ScrubRaid(machineName string, uuid string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return btrfs.ScrubRaid(conn.Connection, &btrfsGrpc.UUIDReq{Uuid: uuid})
}

func (s *BTRFSService) GetRaidStats(machineName string, uuid string) (*btrfsGrpc.RaidStats, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return btrfs.GetRaidStats(conn.Connection, &btrfsGrpc.UUIDReq{Uuid: uuid})
}

func (s *BTRFSService) PauseBalance(machineName string, uuid string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return btrfs.PauseBalance(conn.Connection, &btrfsGrpc.UUIDReq{Uuid: uuid})
}

func (s *BTRFSService) ResumeBalance(machineName string, uuid string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return btrfs.ResumeBalance(conn.Connection, &btrfsGrpc.UUIDReq{Uuid: uuid})
}

func (s *BTRFSService) CancelBalance(machineName string, uuid string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return btrfs.CancelBalance(conn.Connection, &btrfsGrpc.UUIDReq{Uuid: uuid})
}

func (s *BTRFSService) ScrubStats(machineName string, uuid string) (*btrfsGrpc.ScrubStatus, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}
	return btrfs.ScrubStats(conn.Connection, &btrfsGrpc.UUIDReq{Uuid: uuid})
}
