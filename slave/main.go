package main

import (
	"context"
	"log"
	"net"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/hello"
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

func main() {
	// 1) Arranca gRPC server do CLIENTE
	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterClientServiceServer(s, &clientServer{})
	go func() {
		log.Println("Cliente a ouvir em :50052")
		if err := s.Serve(lis); err != nil {
			log.Fatalf("serve: %v", err)
		}
	}()

	// 2) (Exemplo) Cliente chama o MASTER (HelloService) em :50051
	time.Sleep(300 * time.Millisecond) // só para dar tempo ao master
	conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial master: %v", err)
	}
	defer conn.Close()
	h := pb.NewHelloServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := h.SayHello(ctx, &pb.HelloRequest{Name: "Marques"})
	if err != nil {
		log.Fatalf("SayHello: %v", err)
	}
	log.Printf("Resposta do master: %s", out.GetMessage())
}
