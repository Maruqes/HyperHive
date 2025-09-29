package main

import (
	"context"
	"log"
	"net"
	"os"

	pb "github.com/Maruqes/512SvMan/api/proto/protocol"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// === Servidor do CLIENTE (ClientService) ===
type clientServer struct {
	pb.UnimplementedClientServiceServer
}

func (s *clientServer) Notify(ctx context.Context, req *pb.NotifyRequest) (*pb.NotifyResponse, error) {
	log.Printf("Cliente recebeu Notify: %s", req.GetText())
	return &pb.NotifyResponse{Ok: "OK do Cliente"}, nil
}

func listenGRPC() {
	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterClientServiceServer(s, &clientServer{})
	log.Println("Cliente a ouvir em :50052")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func connectGRPC() {
	//gte MASTER_IP from .env
	godotenv.Load()
	masterIP := os.Getenv("MASTER_IP")
	if masterIP == "" {
		log.Fatalln("MASTER_IP not set in .env")
	}

	conn, err := grpc.Dial(masterIP+":50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial master: %v", err)
	}
	defer conn.Close()
	h := pb.NewProtocolServiceClient(conn)

	go listenGRPC()


	slaveIp := os.Getenv("SLAVE_IP")
	if slaveIp == "" {
		log.Fatalln("SLAVE_IP not set in .env")
	}

	outR, err := h.SetConnection(context.Background(), &pb.SetConnectionRequest{Addr: slaveIp, MachineName: "slave1"})
	if err != nil {
		log.Fatalf("SetConnection: %v", err)
	}
	log.Printf("Resposta do master: %s", outR.GetOk())

}

func main() {
	connectGRPC()
	select {}
}
