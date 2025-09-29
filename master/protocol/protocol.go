package protocol

import (
	"context"
	"log"
	"net"

	pb "github.com/Maruqes/512SvMan/api/proto/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Connections struct {
	Addr        string
	MachineName string
	Connection  *grpc.ClientConn
}

var connections []Connections

//should listen on prt and recieve ips on SetConnection from slaves
//and connect to the slaves on their ClientService

func NewSlaveConnection(addr, machineName string) error {
	//with grpc connect to the slave's ClientService
	conn, err := grpc.Dial(addr+":50052", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("dial slave %s: %v", addr, err)
	}

	connections = append(connections, Connections{
		Addr:        addr,
		MachineName: machineName,
		Connection:  conn,
	})

	h := pb.NewClientServiceClient(conn)
	h.Notify(context.Background(), &pb.NotifyRequest{Text: "Ping do Master"})

	
	log.Println("Nova conexao com slave:", addr, machineName)
	return nil
}

// === Servidor do MASTER (HelloService) ===
type protocolServer struct {
	pb.UnimplementedProtocolServiceServer
}

func (s *protocolServer) SetConnection(ctx context.Context, req *pb.SetConnectionRequest) (*pb.SetConnectionResponse, error) {
	log.Printf("Master recebeu SetConnection: %s", req.GetAddr())

	err := NewSlaveConnection(req.GetAddr(), req.GetMachineName())
	if err != nil {
		return &pb.SetConnectionResponse{Ok: "Erro ao conectar ao slave"}, err
	}
	return &pb.SetConnectionResponse{Ok: "OK do Master"}, nil
}

func ListenGRPC() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterProtocolServiceServer(s, &protocolServer{})
	log.Println("Master a ouvir em :50051")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
