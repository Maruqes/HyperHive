package services

import (
	"512SvMan/protocol"
	"512SvMan/virsh"
	"context"
	"errors"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

func (v *VirshService) GetVMSnapshot(ctx context.Context, machine, name string) (*grpcVirsh.VMSnapshotResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn := protocol.GetConnectionByMachineName(machine)
	if conn == nil || conn.Connection == nil {
		return nil, errors.New("VM snapshot host unavailable")
	}
	result, err := virsh.GetVMSnapshot(ctx, conn.Connection, name)
	if err != nil || result == nil {
		return nil, errors.New("secure VM snapshot unavailable")
	}
	return result, nil
}
