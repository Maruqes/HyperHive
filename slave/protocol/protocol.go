package protocol

import (
	"context"
	"fmt"
	"net"
	"os"
	"slave/btrfs"
	"slave/docker"
	"slave/env512"
	"slave/extra"
	"slave/info"
	"slave/logs512"
	nfsservice "slave/nfs"
	ourk8s "slave/our_k8s"
	pciservice "slave/pci"
	smartdisk "slave/smartdisk"
	"slave/virsh"
	"sync"
	"syscall"
	"time"

	btrfsGrpc "github.com/Maruqes/512SvMan/api/proto/btrfs"
	dockerGRPC "github.com/Maruqes/512SvMan/api/proto/docker"
	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
	k8s "github.com/Maruqes/512SvMan/api/proto/k8s"
	nfsproto "github.com/Maruqes/512SvMan/api/proto/nfs"
	pciGrpc "github.com/Maruqes/512SvMan/api/proto/pci"
	pb "github.com/Maruqes/512SvMan/api/proto/protocol"
	smartdiskGrpc "github.com/Maruqes/512SvMan/api/proto/smartdisk"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"

	"github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	args := os.Args
	env := os.Environ()

	logger.Info("Restarting process...")
	return syscall.Exec(exe, args, env)
}

func restartOnConnectionLoss(conn *grpc.ClientConn, reason string, err error) {
	restartSlaveOnce.Do(func() {
		fields := []interface{}{"reason", reason}
		if conn != nil {
			fields = append(fields, "state", conn.GetState().String())
		}
		if err != nil {
			fields = append(fields, "error", err)
		}

		logger.Error("lost connection to master; restarting slave", fields...)

		if conn != nil {
			_ = conn.Close()
		}

		if execErr := restartSelf(); execErr != nil {
			logger.Error("failed to restart slave process", "error", execErr)
		}
		os.Exit(1)
	})
}

// === Servidor do CLIENTE (ClientService) ===
type clientServer struct {
	pb.UnimplementedClientServiceServer
}

// serve para ser pingado e ver se esta vivo
func (s *clientServer) Notify(ctx context.Context, req *pb.NotifyRequest) (*pb.NotifyResponse, error) {
	// logger.Debug(req.Text)
	return &pb.NotifyResponse{Ok: "OK do Cliente"}, nil
}

func listenGRPC() {
	for {
		lis, err := net.Listen("tcp", ":50052")
		if err != nil {
			logger.Error("failed to start client listener", "addr", ":50052", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		enf := keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second, // aceita pings >= 5s de intervalo (mais agressivo)
			PermitWithoutStream: true,
		}

		srvParams := keepalive.ServerParameters{
			MaxConnectionIdle:     0,                // 0 ⇒ não fecha por idle
			MaxConnectionAge:      0,                // 0 ⇒ não recicla conexão por idade
			MaxConnectionAgeGrace: 0,                // sem período de graça
			Time:                  20 * time.Second, // servidor pinga a cada 20s (mais frequente)
			Timeout:               10 * time.Second, // espera 10s pela resposta
		}

		s := grpc.NewServer(
			grpc.KeepaliveEnforcementPolicy(enf),
			grpc.KeepaliveParams(srvParams),
		)

		//registar services
		pb.RegisterClientServiceServer(s, &clientServer{})
		nfsproto.RegisterNFSServiceServer(s, &nfsservice.NFSService{})
		grpcVirsh.RegisterSlaveVirshServiceServer(s, &virsh.SlaveVirshService{})
		extraGrpc.RegisterExtraServiceServer(s, &extra.ExtraService{})
		pciGrpc.RegisterSlavePCIServiceServer(s, &pciservice.PCIService{})
		infoGrpc.RegisterInfoServer(s, &info.INFOService{})
		smartdiskGrpc.RegisterSmartDiskServiceServer(s, &smartdisk.Service{})
		btrfsGrpc.RegisterBtrFSServiceServer(s, &btrfs.BTRFSService{})
		dockerGRPC.RegisterDockerServiceServer(s, &docker.DockerService{})
		k8s.RegisterK8SServiceServer(s, &ourk8s.K8sService{})
		logger.Info("client services listening", "port", 50052)
		if err := s.Serve(lis); err != nil {
			logger.Error("client gRPC serve failed", "error", err)
		}
		_ = lis.Close()
		time.Sleep(2 * time.Second)
	}
}

func monitorConnection(conn *grpc.ClientConn) {
	ctx := context.Background()

	for {
		state := conn.GetState()
		switch state {
		case connectivity.Ready:
		case connectivity.Connecting:
			restartOnConnectionLoss(conn, "connection_state_connecting", nil)
			return
		case connectivity.Idle:
			logger.Info("connection state idle; forcing connect", "state", state.String())
			conn.Connect()
		case connectivity.TransientFailure:
			restartOnConnectionLoss(conn, "connection_state_transient_failure", nil)
			return
		case connectivity.Shutdown:
			restartOnConnectionLoss(conn, "connection_state_shutdown", nil)
			return
		default:
			restartOnConnectionLoss(conn, "connection_state_unknown", nil)
			return
		}
		if !conn.WaitForStateChange(ctx, state) {
			restartOnConnectionLoss(conn, "monitor_wait_for_state_change_stopped", nil)
			return
		}
	}
}

// pinga todas as conexoes slave -> master (master server)
func PingMaster(conn *grpc.ClientConn) {
	ticker := time.NewTicker(time.Duration(env512.PingInterval) * time.Second)
	defer ticker.Stop()

	for {
		h := pb.NewProtocolServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := h.Notify(ctx, &pb.NotifyRequest{Text: "Ping do Slave"})
		cancel()
		if err != nil {
			restartOnConnectionLoss(conn, "ping_master_failed", err)
			return
		}
		<-ticker.C
	}
}

var startSlaveServerOnce sync.Once
var restartSlaveOnce sync.Once

func ConnectGRPC() *grpc.ClientConn {

	target := fmt.Sprintf("%s:50051", env512.MasterIP)
	startSlaveServerOnce.Do(func() {
		go listenGRPC()
	})

	const (
		minRetryDelay = 5 * time.Second
		maxRetryDelay = 1 * time.Minute
	)
	retryDelay := minRetryDelay

	for {
		logger.Info("connecting to master", "target", target)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

		ka := keepalive.ClientParameters{
			Time:                15 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}

		conn, err := grpc.DialContext(ctx, target,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
			grpc.WithKeepaliveParams(ka),
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(50*1024*1024), grpc.MaxCallSendMsgSize(50*1024*1024)),
		)
		cancel()
		if err != nil {
			logger.Error("failed to dial master", "target", target, "error", err)
			time.Sleep(retryDelay)
			if retryDelay < maxRetryDelay {
				retryDelay *= 2
				if retryDelay > maxRetryDelay {
					retryDelay = maxRetryDelay
				}
			}
			continue
		}

		logs512.StartLogs(conn)
		h := pb.NewProtocolServiceClient(conn)
		reqCtx, reqCancel := context.WithTimeout(context.Background(), 300*time.Second)
		outR, err := h.SetConnection(reqCtx, &pb.SetConnectionRequest{Addr: env512.SlaveIP, MachineName: env512.MachineName})
		reqCancel()
		if err != nil {
			logger.Error("SetConnection request failed", "error", err)
			conn.Close()
			time.Sleep(retryDelay)
			if retryDelay < maxRetryDelay {
				retryDelay *= 2
				if retryDelay > maxRetryDelay {
					retryDelay = maxRetryDelay
				}
			}
			continue
		}

		retryDelay = minRetryDelay
		logger.Info("master acknowledged slave", "message", outR.GetOk())
		go monitorConnection(conn)
		go PingMaster(conn)
		return conn
	}
}
