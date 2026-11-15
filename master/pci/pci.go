package pci

import (
	"context"
	"fmt"
	"strings"
	"time"

	pciGrpc "github.com/Maruqes/512SvMan/api/proto/pci"
	"google.golang.org/grpc"
)

const (
	defaultRPCTimeout = 20 * time.Second
	setGPUModeTimeout = 3 * time.Minute
)

func withDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, defaultRPCTimeout)
}

func ListHostGPUs(ctx context.Context, conn *grpc.ClientConn) (*pciGrpc.HostGPUList, error) {
	if conn == nil {
		return nil, fmt.Errorf("grpc connection is nil")
	}

	client := pciGrpc.NewSlavePCIServiceClient(conn)
	rpcCtx, cancel := withDefaultTimeout(ctx)
	defer cancel()

	return client.ListHostGPUs(rpcCtx, &pciGrpc.Empty{})
}

func ListHostGPUsWithIOMMU(ctx context.Context, conn *grpc.ClientConn) (*pciGrpc.HostGPUList, error) {
	if conn == nil {
		return nil, fmt.Errorf("grpc connection is nil")
	}

	client := pciGrpc.NewSlavePCIServiceClient(conn)
	rpcCtx, cancel := withDefaultTimeout(ctx)
	defer cancel()

	return client.ListHostGPUsWithIOMMU(rpcCtx, &pciGrpc.Empty{})
}

func ListVMGPUs(ctx context.Context, conn *grpc.ClientConn, vmName string) (*pciGrpc.VMGPUList, error) {
	if conn == nil {
		return nil, fmt.Errorf("grpc connection is nil")
	}

	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return nil, fmt.Errorf("vm name is required")
	}

	client := pciGrpc.NewSlavePCIServiceClient(conn)
	rpcCtx, cancel := withDefaultTimeout(ctx)
	defer cancel()

	return client.ListVMGPUs(rpcCtx, &pciGrpc.VmNameRequest{VmName: vmName})
}

// SetGPUMode switches a GPU between none, host and vm mode. Loading a vendor driver and
// stopping nvidia-persistenced can take a while, hence the longer bound.
func SetGPUMode(ctx context.Context, conn *grpc.ClientConn, gpuRef, mode, vmName string) (*pciGrpc.OkResponse, error) {
	if conn == nil {
		return nil, fmt.Errorf("grpc connection is nil")
	}

	gpuRef = strings.TrimSpace(gpuRef)
	if gpuRef == "" {
		return nil, fmt.Errorf("gpu reference is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	client := pciGrpc.NewSlavePCIServiceClient(conn)
	rpcCtx, cancel := context.WithTimeout(ctx, setGPUModeTimeout)
	defer cancel()

	return client.SetGPUMode(rpcCtx, &pciGrpc.SetGPUModeRequest{
		GpuRef: gpuRef,
		Mode:   mode,
		VmName: vmName,
	})
}
