package protocol

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ConnectionsStruct struct {
	Addr        string
	MachineName string
	Connection  *grpc.ClientConn
	LastSeen    time.Time
}

var recievedNewSlaveFunc func(addr, machineName string, conn *grpc.ClientConn) error

var (
	connections   []*ConnectionsStruct
	connectionsMu sync.RWMutex
)

func init() {
	connections = make([]*ConnectionsStruct, 0)
}

func GetAllGRPCConnections() []*grpc.ClientConn {
	connectionsMu.RLock()
	defer connectionsMu.RUnlock()

	conns := make([]*grpc.ClientConn, 0, len(connections))
	for _, c := range connections {
		if c.Connection != nil {
			conns = append(conns, c.Connection)
		}
	}
	return conns
}

func GetAllMachineNames() []string {
	connectionsMu.RLock()
	defer connectionsMu.RUnlock()

	names := make([]string, 0, len(connections))
	for _, c := range connections {
		names = append(names, c.MachineName)
	}
	return names
}

func GetConnectionsSnapshot() []ConnectionsStruct {
	connectionsMu.RLock()
	defer connectionsMu.RUnlock()

	snapshot := make([]ConnectionsStruct, len(connections))
	for i, c := range connections {
		snapshot[i] = *c
	}
	return snapshot
}

//should listen on prt and recieve ips on SetConnection from slaves
//and connect to the slaves on their ClientService

func GetConnectionByAddr(addr string) *ConnectionsStruct {
	connectionsMu.RLock()
	defer connectionsMu.RUnlock()

	for _, c := range connections {
		if c.Addr == addr {
			return c
		}
	}
	return nil
}

func GetConnectionByMachineName(machineName string) *ConnectionsStruct {
	connectionsMu.RLock()
	defer connectionsMu.RUnlock()

	for _, c := range connections {
		if c.MachineName == machineName {
			return c
		}
	}
	return nil
}

func removeConnection(addr string) *ConnectionsStruct {
	connectionsMu.Lock()
	defer connectionsMu.Unlock()

	for i, c := range connections {
		if c.Addr == addr {
			removed := c
			connections = append(connections[:i], connections[i+1:]...)
			return removed
		}
	}
	return nil
}

func addOrReplaceConnection(conn *ConnectionsStruct) (*ConnectionsStruct, error) {
	var replaced *ConnectionsStruct

	if conn == nil {
		return nil, fmt.Errorf("nil connection provided")
	}

	connectionsMu.Lock()
	defer connectionsMu.Unlock()

	for i, existing := range connections {
		if existing.Addr == conn.Addr || existing.MachineName == conn.MachineName {
			replaced = existing
			connections[i] = conn
			return replaced, nil
		}
	}

	connections = append(connections, conn)
	return nil, nil
}

func markSlaveHealthy(addr string) {
	connectionsMu.Lock()
	defer connectionsMu.Unlock()

	for _, c := range connections {
		if c.Addr == addr {
			c.LastSeen = time.Now()
			return
		}
	}
}

func CheckConnection(connection ConnectionsStruct) {
	if connection.Connection == nil {
		log.Printf("connection for slave %s is nil, removing", connection.Addr)
		if removed := removeConnection(connection.Addr); removed != nil && removed.Connection != nil {
			_ = removed.Connection.Close()
		}
		return
	}

	h := pb.NewClientServiceClient(connection.Connection)
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := h.Notify(ctx, &pb.NotifyRequest{Text: "Ping do Master"})
		cancel()
		if err == nil {
			markSlaveHealthy(connection.Addr)
			return
		}
		log.Printf("ping slave %s attempt %d failed: %v", connection.Addr, i+1, err)
		time.Sleep(2 * time.Second)
	}

	log.Printf("removing slave %s from connections", connection.Addr)
	if removed := removeConnection(connection.Addr); removed != nil && removed.Connection != nil {
		_ = removed.Connection.Close()
	}
}

func PingAllSlaves(ctx context.Context) {
	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	for _, c := range GetConnectionsSnapshot() {
		if c.Connection == nil {
			CheckConnection(c)
			continue
		}
		pingCtx, cancel := context.WithTimeout(baseCtx, 5*time.Second)
		h := pb.NewClientServiceClient(c.Connection)
		_, err := h.Notify(pingCtx, &pb.NotifyRequest{Text: "Ping do Master"})
		cancel()
		if err != nil {
			log.Printf("could not notify slave %s: %v", c.Addr, err)
			CheckConnection(c)
			continue
		}
		markSlaveHealthy(c.Addr)
	}
}

func NewSlaveConnection(addr, machineName string) error {

	target := addr + ":50052"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	conn, err := grpc.DialContext(ctx, target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	cancel()
	if err != nil {
		return fmt.Errorf("dial slave %s: %w", target, err)
	}

	client := pb.NewClientServiceClient(conn)
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if _, err := client.Notify(pingCtx, &pb.NotifyRequest{Text: "Ping do Master"}); err != nil {
		pingCancel()
		_ = conn.Close()
		return fmt.Errorf("initial ping slave %s: %w", target, err)
	}
	pingCancel()

	entry := &ConnectionsStruct{
		Addr:        addr,
		MachineName: machineName,
		Connection:  conn,
		LastSeen:    time.Now(),
	}

	replaced, err := addOrReplaceConnection(entry)
	if err != nil {
		_ = conn.Close()
		return err
	}
	if replaced != nil && replaced.Connection != nil {
		_ = replaced.Connection.Close()
	}

	if err := recievedNewSlaveFunc(addr, machineName, conn); err != nil {
		if removed := removeConnection(addr); removed != nil && removed.Connection != nil {
			_ = removed.Connection.Close()
		}
		_ = conn.Close()
		return err
	}

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
	return &pb.NotifyResponse{Ok: "OK do Master"}, nil
}

func ListenGRPC(recievedNewConnectionFunction func(addr, machineName string, conn *grpc.ClientConn) error) {
	recievedNewSlaveFunc = recievedNewConnectionFunction
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
