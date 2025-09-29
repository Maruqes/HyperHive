package protocol

import (
	"context"
	"log"
	"net"

	pb "github.com/Maruqes/512SvMan/api/proto/protocol"
	"google.golang.org/grpc"
)

//should listen on prt and recieve ips on SetConnection from slaves
//and connect to the slaves on their ClientService

// === Servidor do MASTER (HelloService) ===
type helloServer struct {
	pb.UnimplementedHelloServiceServer
}

func (s *helloServer) SayHello(ctx context.Context, req *pb.HelloRequest) (*pb.HelloResponse, error) {
	log.Printf("Master recebeu: %s", req.GetName())
	return &pb.HelloResponse{Message: "Olá, " + req.GetName()}, nil
}

func (s *helloServer) SetConnection(ctx context.Context, req *pb.SetConnectionRequest) (*pb.SetConnectionResponse, error) {
	log.Printf("Master recebeu SetConnection: %s", req.GetAddr())

	//conectar e guardar em algum array de conexoes
	return &pb.SetConnectionResponse{Ok: "OK do Master"}, nil
}

func ListenGRPC() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterHelloServiceServer(s, &helloServer{})
	log.Println("Master a ouvir em :50051")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
