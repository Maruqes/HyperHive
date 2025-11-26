package protocol

import (
	"512SvMan/env512"
	"512SvMan/extra"
	"512SvMan/logs512"
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
	logsGrpc "github.com/Maruqes/512SvMan/api/proto/logsserve"
	pb "github.com/Maruqes/512SvMan/api/proto/protocol"
	"github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type ConnectionsStruct struct {
	Addr        string
	MachineName string
	Connection  *grpc.ClientConn
	LastSeen    time.Time
	EntryTime   time.Time
}

var recievedNewSlaveFunc func(addr, machineName string, conn *grpc.ClientConn) error

var (
	connections   []*ConnectionsStruct
	connectionsMu sync.RWMutex
)

func init() {
	connections = make([]*ConnectionsStruct, 0)
}
func tryRestoreConnection(addr, machine string) bool {
	for attempt := 0; attempt < 3; attempt++ {
		if err := NewSlaveConnection(addr, machine); err == nil {
			log.Printf("reconnected slave %s (%s) on attempt %d", machine, addr, attempt+1)
			return true
		} else {
			log.Printf("reconnect attempt %d for slave %s failed: %v", attempt+1, addr, err)
			if attempt < 2 {
				time.Sleep(2 * time.Second)
			}
		}
	}
	return false
}

// removes conn if it is really down
func CheckConnectionStateRemove(connection ConnectionsStruct) {
	if connection.Connection == nil {
		log.Printf("connection for slave %s is nil, removing", connection.Addr)
		if removed := removeConnection(connection.Addr, connection.MachineName); removed != nil && removed.Connection != nil {
			_ = removed.Connection.Close()
		}
		if !tryRestoreConnection(connection.Addr, connection.MachineName) {
			log.Printf("failed to recreate connection for slave %s after 3 attempts", connection.Addr)
		}
		return
	}

	// 3 tentativas de ping
	for attempt := 0; attempt < 3; attempt++ {
		if err := PingSlave(connection.Connection, connection.MachineName); err == nil {
			markSlaveHealthy(connection.Addr)
			return
		} else {
			log.Printf("ping slave %s attempt %d failed: %v", connection.Addr, attempt+1, err)
			if attempt < 2 {
				time.Sleep(2 * time.Second)
			}
		}
	}

	log.Printf("removing slave %s from connections", connection.Addr)
	if removed := removeConnection(connection.Addr, connection.MachineName); removed != nil && removed.Connection != nil {
		_ = removed.Connection.Close()
	}
	if !tryRestoreConnection(connection.Addr, connection.MachineName) {
		log.Printf("failed to recreate connection for slave %s after 3 attempts", connection.Addr)
	}
}

// pinga todas as conexoes master -> slave (slave server)
func PingAllSlaves() {
	for _, c := range GetConnectionsSnapshot() {
		if c.Connection == nil {
			CheckConnectionStateRemove(c)
			continue
		}
		err := PingSlave(c.Connection, c.MachineName)
		if err != nil {
			log.Printf("could not notify slave %s: %v", c.Addr, err)
			CheckConnectionStateRemove(c)
			continue
		}
		markSlaveHealthy(c.Addr)
	}
}

func NewSlaveConnection(addr, machineName string) error {
	if addr == "" {
		return fmt.Errorf("addr cannot be empty")
	}
	if machineName == "" {
		return fmt.Errorf("machineName cannot be empty")
	}

	target := net.JoinHostPort(addr, "50052")

	ka := keepalive.ClientParameters{
		Time: 15 * time.Second, Timeout: 10 * time.Second, PermitWithoutStream: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithKeepaliveParams(ka),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(50*1024*1024),
			grpc.MaxCallSendMsgSize(50*1024*1024),
		),
	)
	if err != nil {
		return fmt.Errorf("dial slave %s: %w", target, err)
	}

	if err := PingSlave(conn, machineName); err != nil {
		_ = conn.Close()
		return fmt.Errorf("initial ping to slave %s failed: %w", machineName, err)
	}

	entry := &ConnectionsStruct{
		Addr: addr, MachineName: machineName, Connection: conn, LastSeen: time.Now(), EntryTime: time.Now(),
	}

	replaced, err := addOrReplaceConnection(entry)
	if err != nil {
		_ = conn.Close()
		return err
	}
	if replaced != nil && replaced.Connection != nil {
		_ = replaced.Connection.Close()
	}

	if recievedNewSlaveFunc != nil {
		if err := recievedNewSlaveFunc(addr, machineName, conn); err != nil {
			if removed := removeConnection(addr, machineName); removed != nil && removed.Connection != nil {
				_ = removed.Connection.Close()
			}
			_ = conn.Close()
			return err
		}
	}

	logger.Info("Nova conexao com slave:", addr, machineName)
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
	PingAllSlaves()
	return &pb.SetConnectionResponse{Ok: "OK do Master"}, nil
}

func (s *protocolServer) Notify(ctx context.Context, req *pb.NotifyRequest) (*pb.NotifyResponse, error) {
	return &pb.NotifyResponse{Ok: "OK do Master"}, nil
}

func ListenGRPC(recievedNewConnectionFunction func(addr, machineName string, conn *grpc.ClientConn) error) {
	recievedNewSlaveFunc = recievedNewConnectionFunction

	lis, err := net.Listen("tcp", ":50051")
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
	pb.RegisterProtocolServiceServer(s, &protocolServer{})
	logsGrpc.RegisterLogsServeServer(s, &logs512.LogsServer{})
	extraGrpc.RegisterExtraServiceServer(s, &extra.ExtraServiceServer{})
	logger.Info("Master a ouvir em :50051")
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("serve: %v", err)
		}
	}()

}

func PingAllSlavesLoop() {
	for {
		PingAllSlaves()
		time.Sleep(time.Second * time.Duration(env512.PingInterval))
	}
}
