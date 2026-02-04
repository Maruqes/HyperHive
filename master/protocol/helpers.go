package protocol

import (
	"512SvMan/nots"
	"context"
	"fmt"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

// IsConnectionHealthy checks if a gRPC connection is in a healthy state
func IsConnectionHealthy(conn *grpc.ClientConn) bool {
	if conn == nil {
		return false
	}
	state := conn.GetState()
	return state == connectivity.Ready || state == connectivity.Idle
}

// EnsureConnectionReady ensures a connection is ready, attempting to reconnect if needed
// Returns the current connection state and any error
func EnsureConnectionReady(conn *grpc.ClientConn, timeout time.Duration) (connectivity.State, error) {
	if conn == nil {
		return connectivity.Shutdown, fmt.Errorf("connection is nil")
	}

	state := conn.GetState()
	if state == connectivity.Ready {
		return state, nil
	}

	if state == connectivity.Shutdown {
		return state, fmt.Errorf("connection is shutdown")
	}

	// Try to connect if in idle or transient failure state
	if state == connectivity.Idle || state == connectivity.TransientFailure {
		conn.Connect()
	}

	// Wait for the connection to become ready
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		if conn.WaitForStateChange(ctx, state) {
			newState := conn.GetState()
			if newState == connectivity.Ready {
				return newState, nil
			}
			if newState == connectivity.Shutdown {
				return newState, fmt.Errorf("connection is shutdown")
			}
			state = newState
		} else {
			// Context timeout
			return conn.GetState(), fmt.Errorf("timeout waiting for connection to be ready")
		}
	}
}

// GetHealthyConnectionByMachineName returns a healthy connection for the given machine name
// It validates the connection state before returning
func GetHealthyConnectionByMachineName(machineName string) *ConnectionsStruct {
	conn := GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return nil
	}

	// Check if connection is healthy
	if !IsConnectionHealthy(conn.Connection) {
		// Try to trigger reconnection check
		go CheckConnectionStateRemove(*conn)
		return nil
	}

	return conn
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

func removeConnection(addr string, machineName string, expectedEntry time.Time) *ConnectionsStruct {
	connectionsMu.Lock()
	defer connectionsMu.Unlock()
	for i, c := range connections {
		if c.Addr == addr && (expectedEntry.IsZero() || c.EntryTime.Equal(expectedEntry)) {
			removed := c
			connections = append(connections[:i], connections[i+1:]...)
			nots.SendGlobalNotification(fmt.Sprintf("Lost connection to %s", machineName), "Lost connection after many attempts", "/", true)
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
