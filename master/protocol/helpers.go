package protocol

import (
	"context"
	"fmt"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/protocol"
	"google.golang.org/grpc"
)

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

func PingSlave(conn *grpc.ClientConn, name string) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}

	client := pb.NewClientServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Notify(ctx, &pb.NotifyRequest{Text: "Ping to slave " + name})
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	return nil
}
