package services

import (
	"512SvMan/extra"
	"512SvMan/protocol"
	"fmt"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
)

type ExtraService struct{}

func (s *ExtraService) CheckForUpdates(machineName string) (*extraGrpc.AllUpdates, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	return extra.CheckForUpdates(conn.Connection, &extraGrpc.Empty{})
}

func (s *ExtraService) PerformUpdate(machineName, pkgName string, reboot bool) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}

	_, err := extra.PerformUpdate(conn.Connection, &extraGrpc.UpdateRequest{
		Name:         pkgName,
		RestartAfter: reboot,
	})
	return err
}
