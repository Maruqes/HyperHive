package protocol

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"slave/btrfs"
	"slave/env512"
	"slave/extra"
	"slave/info"
	"slave/logs512"
	nfsservice "slave/nfs"
	"slave/virsh"
	"syscall"
	"time"

	btrfsGrpc "github.com/Maruqes/512SvMan/api/proto/btrfs"
	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
	nfsproto "github.com/Maruqes/512SvMan/api/proto/nfs"
	pb "github.com/Maruqes/512SvMan/api/proto/protocol"
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

	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("listen: %v", err)
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
	infoGrpc.RegisterInfoServer(s, &info.INFOService{})
	btrfsGrpc.RegisterBtrFSServiceServer(s, &btrfs.BTRFSService{})
	logger.Info("Cliente a ouvir em :50052")
	if err := s.Serve(lis); err != nil {
		logger.Error("serve: %v", err)
	}
}

func monitorConnection(conn *grpc.ClientConn) {
	ctx := context.Background()
	for {
		state := conn.GetState()
		switch state {
		case connectivity.Ready:
			// ok
		case connectivity.Connecting:
			logger.Info("connection to master reconnecting...")
		case connectivity.Idle:
			logger.Info("connection state changed: IDLE -> forcing Connect()")
			conn.Connect()
		case connectivity.Shutdown, connectivity.TransientFailure:
			logger.Info("connection to master lost; restarting")
			_ = conn.Close()
			if err := restartSelf(); err != nil {
				logger.Error(fmt.Sprintf("failed to restart slave process: %v", err))
			}
			os.Exit(1)
			return
		default:
			logger.Info(fmt.Sprintf("connection state changed: %s", state.String()))
		}
		if !conn.WaitForStateChange(ctx, state) {
			logger.Info("monitorConnection: no further state changes, stopping monitor")
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
			logger.Error("PingMaster: %v", err)
			// Check connection state and handle accordingly
			state := conn.GetState()
			switch state {
			case connectivity.Idle:
				logger.Info("Connection idle, attempting to reconnect...")
				conn.Connect()
			case connectivity.TransientFailure, connectivity.Shutdown:
				logger.Error("Connection to master is dead (state: %s), triggering restart", state)
				_ = conn.Close()
				if err := restartSelf(); err != nil {
					logger.Error("failed to restart slave process: %v", err)
				}
				os.Exit(1)
				return
			default:
				logger.Debug("Connection state: %s, continuing...", state)
			}
		} else {
			//ping to master ok
			// logger.Debug("Ping to master successful")
		}
		<-ticker.C
	}
}

func ConnectGRPC() *grpc.ClientConn {

	target := fmt.Sprintf("%s:50051", env512.MasterIP)
	go listenGRPC()

	for {
		logger.Info("Connecting to master at", target)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

		ka := keepalive.ClientParameters{
			Time:                15 * time.Second, // envia ping a cada 15s (mais agressivo)
			Timeout:             10 * time.Second, // espera 10s pelo ACK do ping
			PermitWithoutStream: true,             // pings mesmo sem RPCs ativas
		}

		conn, err := grpc.DialContext(ctx, target,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
			grpc.WithKeepaliveParams(ka),
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(50*1024*1024), grpc.MaxCallSendMsgSize(50*1024*1024)),
		)
		cancel()
		if err != nil {
			logger.Error("dial master failed: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}

		logs512.StartLogs(conn)
		h := pb.NewProtocolServiceClient(conn)
		reqCtx, reqCancel := context.WithTimeout(context.Background(), 60*time.Second)
		outR, err := h.SetConnection(reqCtx, &pb.SetConnectionRequest{Addr: env512.SlaveIP, MachineName: env512.MachineName})
		reqCancel()
		if err != nil {
			logger.Error("SetConnection failed: %v", err)
			conn.Close()
			time.Sleep(3 * time.Second)
			continue
		}

		logger.Info("Resposta do master: %s", outR.GetOk())
		go monitorConnection(conn)
		go PingMaster(conn)
		return conn
	}
}
