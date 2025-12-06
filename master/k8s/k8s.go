package k8s

import (
	"context"

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

func SetConnectionToCluster(conn *grpc.ClientConn, con *k8sGrpc.ConnectionToCluster) (*k8sGrpc.Empty, error) {
	client := k8sGrpc.NewK8SServiceClient(conn)
	return client.SetConnectionToCluster(context.Background(), con)
}

func IsMasterSlave(conn *grpc.ClientConn) (*k8sGrpc.IsMasterSlaveRes, error) {
	client := k8sGrpc.NewK8SServiceClient(conn)
	return client.IsMasterSlave(context.Background(), &k8sGrpc.Empty{})
}
