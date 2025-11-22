package services

import (
	"512SvMan/btrfs"
	"512SvMan/db"
	"512SvMan/protocol"
	"fmt"
	"strings"

	btrfsGrpc "github.com/Maruqes/512SvMan/api/proto/btrfs"
	"github.com/Maruqes/512SvMan/logger"
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

var allowedCompressions = []string{
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

func normalizeCompression(compression string) (string, error) {
	compression = strings.TrimSpace(compression)
	for _, valid := range allowedCompressions {
		if compression == valid {
			return compression, nil
		}
	}
	if compression != "" {
		return "", fmt.Errorf("invalid compression type: %s", compression)
	}
	return compression, nil
}

func (s *BTRFSService) MountRaid(machineName string, uuid, target, compression string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}

	var err error
	compression, err = normalizeCompression(compression)
	if err != nil {
		return err
	}

	mountRes, err := btrfs.MountRaid(conn.Connection, &btrfsGrpc.MountReq{Uuid: uuid, Target: target, Compression: compression})
	if err != nil {
		return err
	}
	if mountRes.Degraded {
		for _, prob := range mountRes.Problems {
			logger.Error(prob)
		}
	}
	return nil
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
func (s *BTRFSService) BalanceRaid(machineName string, uuid string, dataUsageMax, metadataUsageMax int32, force, convertToCurrentRaid bool) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}

	req := &btrfsGrpc.BalanceRaidReq{
		Uuid: uuid,
		Filters: &btrfsGrpc.BalanceRaidReq_Filters{
			DataUsageMax:     dataUsageMax,
			MetadataUsageMax: metadataUsageMax,
		},
		Force: force,
		ConvertToCurrentRaid: convertToCurrentRaid,
	}

	return btrfs.BalanceRaid(conn.Connection, req)
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

func (s *BTRFSService) AddAutomaticMount(machineName, uuid, mountPoint, compression string) (int64, error) {
	machineName = strings.TrimSpace(machineName)
	uuid = strings.TrimSpace(uuid)
	mountPoint = strings.TrimSpace(mountPoint)

	if machineName == "" {
		return 0, fmt.Errorf("machine name cannot be empty")
	}
	if uuid == "" {
		return 0, fmt.Errorf("uuid cannot be empty")
	}
	if mountPoint == "" {
		return 0, fmt.Errorf("mount point cannot be empty")
	}

	var err error
	compression, err = normalizeCompression(compression)
	if err != nil {
		return 0, err
	}

	existing, err := db.GetBtrfsByUUIDAndMount(machineName, uuid, mountPoint)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return 0, fmt.Errorf("automatic mount already exists for machine %s uuid %s at %s", machineName, uuid, mountPoint)
	}

	return db.InsertBtrfs(uuid, mountPoint, compression, machineName)
}

func (s *BTRFSService) RemoveAutomaticMount(id int) (int64, error) {
	if id <= 0 {
		return 0, fmt.Errorf("invalid id %d", id)
	}

	rows, err := db.DeleteBtrfs(id)
	if err != nil {
		return 0, err
	}
	if rows == 0 {
		return 0, fmt.Errorf("automatic mount with id %d not found", id)
	}
	return rows, nil
}

func (s *BTRFSService) ListAutomaticMounts(machineName string) ([]db.Btrfs, error) {
	machineName = strings.TrimSpace(machineName)
	if machineName == "" {
		return db.GetAllBtrfs()
	}
	return db.GetBtrfsByMachineName(machineName)
}

func (s *BTRFSService) AutoMountRaid(machineName string) error {
	raids, err := db.GetBtrfsByMachineName(machineName)
	if err != nil {
		return err
	}
	if len(raids) == 0 {
		return nil
	}

	for _, raid := range raids {
		err := s.MountRaid(machineName, raid.RaidUUID, raid.MountPoint, raid.Compression)
		if err != nil {
			logger.Error(err.Error())
		}
	}
	return nil
}
