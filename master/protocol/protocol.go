package protocol

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ConnectionsStruct struct {
	Addr        string
	MachineName string
	Connection  *grpc.ClientConn
}

var Connections []ConnectionsStruct

//should listen on prt and recieve ips on SetConnection from slaves
//and connect to the slaves on their ClientService

func GetConnectionByAddr(addr string) *ConnectionsStruct {
	for _, c := range Connections {
		if c.Addr == addr {
			return &c
		}
	}
	return nil
}

func removeConnection(addr string) {
	for i, c := range Connections {
		if c.Addr == addr {
			Connections = append(Connections[:i], Connections[i+1:]...)
			return
		}
	}
}

func addConnection(conn ConnectionsStruct) error {
	if GetConnectionByAddr(conn.Addr) != nil {
		return fmt.Errorf("connection already exists")
	}
	Connections = append(Connections, conn)
	return nil
}

func CheckConnection(connection *ConnectionsStruct) {
	if connection == nil {
		return
	}
	//ping 3 times if not remove em from Connections
	h := pb.NewClientServiceClient(connection.Connection)
	for i := 0; i < 3; i++ {
		_, err := h.Notify(context.Background(), &pb.NotifyRequest{Text: "Ping do Master"})
		if err == nil {
			return
		}
		time.Sleep(2 * time.Second)
	}
	log.Printf("removing slave %s from connections", connection.Addr)
	removeConnection(connection.Addr)
}

func PingAllSlaves(ctx context.Context) {
	for _, c := range Connections {
		h := pb.NewClientServiceClient(c.Connection)
		_, err := h.Notify(ctx, &pb.NotifyRequest{Text: "Ping do Master"})
		if err != nil {
			log.Printf("could not notify slave %s: %v", c.Addr, err)
			CheckConnection(&c)
			continue
		}
	}
}

func NewSlaveConnection(addr, machineName string) error {
	//with grpc connect to the slave's ClientService
	conn, err := grpc.Dial(addr+":50052", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("dial slave %s: %v", addr, err)
	}

	err = addConnection(ConnectionsStruct{Addr: addr, MachineName: machineName, Connection: conn})
	if err != nil {
		conn.Close()
		return err
	}

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
	PingAllSlaves(ctx)
	err := NewSlaveConnection(req.GetAddr(), req.GetMachineName())
	if err != nil {
		return &pb.SetConnectionResponse{Ok: "Erro ao conectar ao slave"}, err
	}
	return &pb.SetConnectionResponse{Ok: "OK do Master"}, nil
}

func (s *protocolServer) Notify(ctx context.Context, req *pb.NotifyRequest) (*pb.NotifyResponse, error) {
	log.Printf("Master recebeu Notify: %s", req.GetText())
	return &pb.NotifyResponse{Ok: "OK do Master"}, nil
}

func ListenGRPC() {
	go func() {
		for {
			PingAllSlaves(context.Background())
			time.Sleep(30 * time.Second)
		}
	}()

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterProtocolServiceServer(s, &protocolServer{})
	log.Println("Master a ouvir em :50051")
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("serve: %v", err)
		}
	}()

}
