package pci

import (
	"context"
	"fmt"
	"strings"
	"time"

	pciGrpc "github.com/Maruqes/512SvMan/api/proto/pci"
	"google.golang.org/grpc"
)

const defaultRPCTimeout = 20 * time.Second

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

func AttachGPUToVM(ctx context.Context, conn *grpc.ClientConn, vmName, gpuRef string) (*pciGrpc.OkResponse, error) {
	if conn == nil {
		return nil, fmt.Errorf("grpc connection is nil")
	}

	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return nil, fmt.Errorf("vm name is required")
	}

	gpuRef = strings.TrimSpace(gpuRef)
	if gpuRef == "" {
		return nil, fmt.Errorf("gpu reference is required")
	}

	client := pciGrpc.NewSlavePCIServiceClient(conn)
	rpcCtx, cancel := withDefaultTimeout(ctx)
	defer cancel()

	return client.AttachGPUToVM(rpcCtx, &pciGrpc.VMGPURequest{
		VmName: vmName,
		GpuRef: gpuRef,
	})
}

func DetachGPUFromVM(ctx context.Context, conn *grpc.ClientConn, vmName, gpuRef string) (*pciGrpc.OkResponse, error) {
	if conn == nil {
		return nil, fmt.Errorf("grpc connection is nil")
	}

	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return nil, fmt.Errorf("vm name is required")
	}

	gpuRef = strings.TrimSpace(gpuRef)
	if gpuRef == "" {
		return nil, fmt.Errorf("gpu reference is required")
	}

	client := pciGrpc.NewSlavePCIServiceClient(conn)
	rpcCtx, cancel := withDefaultTimeout(ctx)
	defer cancel()

	return client.DetachGPUFromVM(rpcCtx, &pciGrpc.VMGPURequest{
		VmName: vmName,
		GpuRef: gpuRef,
	})
}

func ReturnGPUToHost(ctx context.Context, conn *grpc.ClientConn, gpuRef string) (*pciGrpc.OkResponse, error) {
	if conn == nil {
		return nil, fmt.Errorf("grpc connection is nil")
	}

	gpuRef = strings.TrimSpace(gpuRef)
	if gpuRef == "" {
		return nil, fmt.Errorf("gpu reference is required")
	}

	client := pciGrpc.NewSlavePCIServiceClient(conn)
	rpcCtx, cancel := withDefaultTimeout(ctx)
	defer cancel()

	return client.ReturnGPUToHost(rpcCtx, &pciGrpc.GPUReferenceRequest{
		GpuRef: gpuRef,
	})
}
