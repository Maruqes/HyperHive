package services

import (
	"512SvMan/btrfs"
	"512SvMan/protocol"
	"fmt"

	btrfsGrpc "github.com/Maruqes/512SvMan/api/proto/btrfs"
)

type BTRFSService struct{}

func (s *BTRFSService) GetAllDisks(machineName string) ([]*btrfsGrpc.MinDisk, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	return btrfs.GetAllDisks(conn.Connection)
}
