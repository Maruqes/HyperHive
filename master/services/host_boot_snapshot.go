package services

import (
	"512SvMan/protocol"
	"512SvMan/virsh"
	"context"
	"errors"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

func (v *VirshService) GetHostBootSnapshot(ctx context.Context, machine string) (*grpcVirsh.HostBootSnapshotResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn := protocol.GetConnectionByMachineName(machine)
	if conn == nil || conn.Connection == nil {
		return nil, errors.New("host snapshot unavailable")
	}
	result, err := virsh.GetHostBootSnapshot(ctx, conn.Connection)
	if err != nil || result == nil {
		return nil, errors.New("host snapshot unavailable")
	}
	return result, nil
}
