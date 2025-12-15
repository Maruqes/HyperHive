package ourk8s

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"slave/env512"

	k8sGrpc "github.com/Maruqes/512SvMan/api/proto/k8s"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type K8sService struct {
	k8sGrpc.UnimplementedK8SServiceServer
}

func (s *K8sService) GetToken(ctx context.Context, req *k8sGrpc.Empty) (*k8sGrpc.Token, error) {
	if TOKEN != "" && AreWeMasterSlave() {
		//somos o master slave e temos token
		return &k8sGrpc.Token{Token: TOKEN, NodeIp: env512.SlaveIP}, nil
	}
	return &k8sGrpc.Token{Token: "", NodeIp: env512.SlaveIP}, nil
}

func (s *K8sService) GetConnectionFile(ctx context.Context, req *k8sGrpc.ConnectionFileIp) (*k8sGrpc.ConnectionFile, error) {
	if TOKEN == "" && !AreWeMasterSlave() {
		return &k8sGrpc.ConnectionFile{File: ""}, nil
	}
	cmd := exec.CommandContext(ctx, "sudo", "k3s", "kubectl", "config", "view", "--raw")
	out, err := cmd.Output()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetch kubeconfig: %v", err)
	}

	// replace 0.0.0.0 with req.Ip and fix spacing in server URL
	kubeconfig := strings.ReplaceAll(string(out), "0.0.0.0", req.GetIp())
	return &k8sGrpc.ConnectionFile{File: kubeconfig}, nil
}

func (s *K8sService) GetTLSSANIps(ctx context.Context, req *k8sGrpc.Empty) (*k8sGrpc.TLSSANSIps, error) {
	if TOKEN == "" && !AreWeMasterSlave() {
		return &k8sGrpc.TLSSANSIps{Ips: []string{}}, nil
	}

	tlsSANs := []string{env512.SlaveIP}
	for _, ip := range env512.ExtraK8sIPs {
		if ip == "" || ip == env512.SlaveIP {
			continue
		}
		tlsSANs = append(tlsSANs, ip)
	}

	return &k8sGrpc.TLSSANSIps{Ips: tlsSANs}, nil
}

func (s *K8sService) IsMasterSlave(ctx context.Context, req *k8sGrpc.Empty) (*k8sGrpc.IsMasterSlaveRes, error) {
	return &k8sGrpc.IsMasterSlaveRes{WeAreMasterSlave: AreWeMasterSlave()}, nil
}

func (s *K8sService) GetClusterStatus(ctx context.Context, req *k8sGrpc.Empty) (*k8sGrpc.ClusterStatus, error) {
	status := &k8sGrpc.ClusterStatus{NodeIp: env512.SlaveIP}

	ready, serverURL, err := clusterReadyWithKubeconfig(ctx)
	if err != nil {
		status.Error = err.Error()
		return status, nil
	}

	if !ready {
		status.Error = "not connected to cluster"
		return status, nil
	}

	status.Connected = true
	status.ServerUrl = serverURL
	return status, nil
}

func (s *K8sService) SetConnectionToCluster(ctx context.Context, req *k8sGrpc.ConnectionToCluster) (*k8sGrpc.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	token := strings.TrimSpace(req.GetToken())
	if token == "" {
		return nil, status.Error(codes.InvalidArgument, "token is required")
	}

	var serverURL string
	serverIP := strings.TrimSpace(req.GetServerIp())
	if serverURL == "" {
		if serverIP == "" {
			return nil, status.Error(codes.InvalidArgument, "server IP or URL is required")
		}
		serverURL = fmt.Sprintf("https://%s:6443", serverIP)
	}

	opts := JoinClusterOptions{
		ServerURL: serverURL,
		Token:     token,
		NodeIP:    env512.SlaveIP,
	}

	if err := JoinExistingCluster(ctx, opts); err != nil {
		if errors.Is(err, ErrNodeAlreadyInCluster) {
			return &k8sGrpc.Empty{}, nil
		}
		return nil, status.Errorf(codes.Internal, "join k3s cluster: %v", err)
	}

	return &k8sGrpc.Empty{}, nil
}
