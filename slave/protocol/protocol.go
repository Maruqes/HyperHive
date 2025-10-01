package protocol

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"slave/env512"
	nfsservice "slave/nfs"
	"syscall"
	"time"

	nfsproto "github.com/Maruqes/512SvMan/api/proto/nfs"
	pb "github.com/Maruqes/512SvMan/api/proto/protocol"
	"github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	args := os.Args
	env := os.Environ()

	log.Println("Restarting process...")
	return syscall.Exec(exe, args, env)
}

// === Servidor do CLIENTE (ClientService) ===
type clientServer struct {
	pb.UnimplementedClientServiceServer
}

// serve para ser pingado e ver se esta vivo
func (s *clientServer) Notify(ctx context.Context, req *pb.NotifyRequest) (*pb.NotifyResponse, error) {
	return &pb.NotifyResponse{Ok: "OK do Cliente"}, nil
}

func listenGRPC() {
	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	s := grpc.NewServer()

	//registar services
	pb.RegisterClientServiceServer(s, &clientServer{})
	nfsproto.RegisterNFSServiceServer(s, &nfsservice.NFSService{})

	log.Println("Cliente a ouvir em :50052")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func monitorConnection(conn *grpc.ClientConn) {
	ctx := context.Background()
	for {
		state := conn.GetState()
		switch state {
		case connectivity.Shutdown, connectivity.TransientFailure:
			log.Printf("connection to master lost (state: %s), exiting", state)
			//restart the program
			restartSelf()
			os.Exit(1)
			return
		}
		if !conn.WaitForStateChange(ctx, state) {
			return
		}
	}
}

func PingMaster(conn *grpc.ClientConn) {
	for {
		h := pb.NewProtocolServiceClient(conn)
		_, err := h.Notify(context.Background(), &pb.NotifyRequest{Text: "Ping do Slave"})
		if err != nil {
			logger.Error("PingMaster: %v", err)
		}
		//ping every 30 seconds
		time.Sleep(time.Duration(env512.PingInterval) * time.Second)
	}
}
func getMachineName() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return name
}

func ConnectGRPC() *grpc.ClientConn {

	target := fmt.Sprintf("%s:50051", env512.MasterIP)
	go listenGRPC()

	for {
		log.Println("Connecting to master at", target)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		conn, err := grpc.DialContext(ctx, target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
		cancel()
		if err != nil {
			log.Printf("dial master failed: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}

		h := pb.NewProtocolServiceClient(conn)
		reqCtx, reqCancel := context.WithTimeout(context.Background(), 5*time.Second)
		outR, err := h.SetConnection(reqCtx, &pb.SetConnectionRequest{Addr: env512.SlaveIP, MachineName: getMachineName()})
		reqCancel()
		if err != nil {
			log.Printf("SetConnection failed: %v", err)
			conn.Close()
			time.Sleep(3 * time.Second)
			continue
		}

		log.Printf("Resposta do master: %s", outR.GetOk())
		go monitorConnection(conn)
		go PingMaster(conn)
		return conn
	}
}
