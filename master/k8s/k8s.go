package k8s

import (
	"context"
	"time"

	k8sGrpc "github.com/Maruqes/512SvMan/api/proto/k8s"
	"google.golang.org/grpc"
)

func GetToken(conn *grpc.ClientConn) (*k8sGrpc.Token, error) {
	client := k8sGrpc.NewK8SServiceClient(conn)
	return client.GetToken(context.Background(), &k8sGrpc.Empty{})
}

func GetConnectionFile(conn *grpc.ClientConn, ip string) (*k8sGrpc.ConnectionFile, error) {
	client := k8sGrpc.NewK8SServiceClient(conn)
	return client.GetConnectionFile(context.Background(), &k8sGrpc.ConnectionFileIp{Ip: ip})
}

func GetTLSSANIps(conn *grpc.ClientConn) (*k8sGrpc.TLSSANSIps, error) {
	client := k8sGrpc.NewK8SServiceClient(conn)
	return client.GetTLSSANIps(context.Background(), &k8sGrpc.Empty{})
}

func GetClusterStatus(conn *grpc.ClientConn) (*k8sGrpc.ClusterStatus, error) {
	client := k8sGrpc.NewK8SServiceClient(conn)
	return client.GetClusterStatus(context.Background(), &k8sGrpc.Empty{})
}

func SetConnectionToCluster(conn *grpc.ClientConn, con *k8sGrpc.ConnectionToCluster) (*k8sGrpc.Empty, error) {
	client := k8sGrpc.NewK8SServiceClient(conn)
	return client.SetConnectionToCluster(context.Background(), con)
}

func IsMasterSlave(conn *grpc.ClientConn) (*k8sGrpc.IsMasterSlaveRes, error) {
	client := k8sGrpc.NewK8SServiceClient(conn)
	return client.IsMasterSlave(context.Background(), &k8sGrpc.Empty{})
}

const isMasterSlaveTimeout = 15 * time.Second

// IsMasterSlaveCtx is IsMasterSlave with ctx and its own timeout.
func IsMasterSlaveCtx(ctx context.Context, conn *grpc.ClientConn) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, isMasterSlaveTimeout)
	defer cancel()
	res, err := k8sGrpc.NewK8SServiceClient(conn).IsMasterSlave(ctx, &k8sGrpc.Empty{})
	if err != nil {
		return false, err
	}
	return res.GetWeAreMasterSlave(), nil
}

const clusterResourcesTimeout = 6 * time.Second

func GetClusterResources(ctx context.Context, conn *grpc.ClientConn) (*k8sGrpc.ClusterResources, error) {
	ctx, cancel := context.WithTimeout(ctx, clusterResourcesTimeout)
	defer cancel()
	return k8sGrpc.NewK8SServiceClient(conn).GetClusterResources(ctx, &k8sGrpc.Empty{})
}
