package services

import (
	"512SvMan/btrfs"
	"512SvMan/db"
	"512SvMan/nots"
	"512SvMan/protocol"
	"context"
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
		if !disk.Mounted {
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
		sType: "raid0",  // data em striping (sem redundancia)
		sMeta: "single", // metadados sem redundancia
		c:     2,        // minimo 2 discos
	}

	Raid1 = raidType{
		sType: "raid1", // data espelhada (raid1c2)
		sMeta: "raid1", // metadados espelhados
		c:     2,       // minimo 2 discos
	}

	Raid1c3 = raidType{
		sType: "raid1c3", // data com 3 copias
		sMeta: "raid1c3", // metadados com 3 copias
		c:     3,         // minimo 3 discos
	}

	Raid1c4 = raidType{
		sType: "raid1c4", // data com 4 copias
		sMeta: "raid1c4", // metadados com 4 copias
		c:     4,         // minimo 4 discos
	}

	Single = raidType{
		sType: "single", // data sem redundancia
		sMeta: "single", // metadados sem redundancia
		c:     1,        // minimo 1 disco
	}

	Dup = raidType{
		sType: "dup", // data duplicada no mesmo disco (single-device)
		sMeta: "dup", // metadados duplicados no mesmo disco
		c:     1,     // minimo 1 disco
	}

	Raid5 = raidType{
		sType: "raid5",  // data com paridade (1 disco)
		sMeta: "single", // metadados sem redundancia
		c:     3,        // minimo 3 discos
	}

	Raid6 = raidType{
		sType: "raid6",  // data com paridade dupla (2 discos)
		sMeta: "single", // metadados sem redundancia
		c:     4,        // minimo 4 discos
	}
)

func convertStringToRaidType(s string) *raidType {
	rt := strings.ToLower(strings.TrimSpace(s))
	if rt == "" {
		return nil
	}

	switch rt {
	case Raid0.sType:
		return &Raid0
	case Raid1.sType, "raid1c2":
		return &Raid1
	case Raid1c3.sType:
		return &Raid1c3
	case Raid1c4.sType:
		return &Raid1c4
	case Single.sType:
		return &Single
	case Dup.sType:
		return &Dup
	case Raid5.sType:
		return &Raid5
	case Raid6.sType:
		return &Raid6
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
	err := btrfs.RemoveRaid(conn.Connection, &btrfsGrpc.UUIDReq{Uuid: uuid})
	if err != nil {
		return err
	}

	_, err = db.DeleteBtrfsByUUID(context.Background(), uuid, machineName)
	if err != nil {
		return err
	}

	return nil
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
		Force:                force,
		ConvertToCurrentRaid: convertToCurrentRaid,
	}

	return btrfs.BalanceRaid(conn.Connection, req)
}

func (s *BTRFSService) DefragmentRaid(machineName string, uuid string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}
	go func() {
		err := btrfs.DefragmentRaid(conn.Connection, &btrfsGrpc.UUIDReq{Uuid: uuid})
		if err != nil {
			nots.SendGlobalNotification("Defragment Failed", "BTRFS defragment failed for "+uuid, err.Error(), true)
		} else {
			nots.SendGlobalNotification("Defragment done", "BTRFS defragment done for "+uuid, "Defragment operation is done", false)
		}
	}()
	return nil
}

func (s *BTRFSService) ScrubRaid(machineName string, uuid string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}
	go func() {
		err := btrfs.ScrubRaid(conn.Connection, &btrfsGrpc.UUIDReq{Uuid: uuid})
		if err != nil {
			nots.SendGlobalNotification("Scrub Failed", "BTRFS scrub failed for "+uuid, err.Error(), true)
		} else {
			nots.SendGlobalNotification("Scrub done", "BTRFS scrub done for "+uuid, "Scrub operation in done", false)
		}
	}()
	return nil
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

	existing, err := db.GetBtrfsByUUIDAndMount(context.Background(), machineName, uuid, mountPoint)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return 0, fmt.Errorf("automatic mount already exists for machine %s uuid %s at %s", machineName, uuid, mountPoint)
	}

	return db.InsertBtrfs(context.Background(), uuid, mountPoint, compression, machineName)
}

func (s *BTRFSService) RemoveAutomaticMount(id int) (int64, error) {
	if id <= 0 {
		return 0, fmt.Errorf("invalid id %d", id)
	}

	rows, err := db.DeleteBtrfs(context.Background(), id)
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
		return db.GetAllBtrfs(context.Background())
	}
	return db.GetBtrfsByMachineName(context.Background(), machineName)
}

func (s *BTRFSService) AutoMountRaid(machineName string) error {
	raids, err := db.GetBtrfsByMachineName(context.Background(), machineName)
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
