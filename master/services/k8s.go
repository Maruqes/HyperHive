package services

import (
	"512SvMan/k8s"
	"512SvMan/protocol"
	"fmt"
	"strings"
	"time"

	k8sGrpc "github.com/Maruqes/512SvMan/api/proto/k8s"
	"github.com/Maruqes/512SvMan/logger"
)

// ClusterNodeStatus describes cluster connectivity of a slave.
type ClusterNodeStatus struct {
	MachineName string    `json:"machine"`
	Addr        string    `json:"addr"`
	Connected   bool      `json:"connected"`
	LastSeen    time.Time `json:"lastSeen"`
	TLSSANIps   []string  `json:"tlsSANs,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// ClusterStatus aggregates all slaves and their cluster state.
type ClusterStatus struct {
	Connected    []ClusterNodeStatus `json:"connected"`
	Disconnected []ClusterNodeStatus `json:"disconnected"`
}

type K8sService struct{}

func (s *K8sService) GetToken(machineName string) (*k8sGrpc.Token, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	return k8s.GetToken(conn.Connection)
}

func (s *K8sService) GetConnectionFile(machineName, ip string) (*k8sGrpc.ConnectionFile, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	return k8s.GetConnectionFile(conn.Connection, ip)
}

func (s *K8sService) GetTLSSANIps(machineName string) (*k8sGrpc.TLSSANSIps, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return nil, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	return k8s.GetTLSSANIps(conn.Connection)
}

func (s *K8sService) GetTLSSANIpsAny() (*k8sGrpc.TLSSANSIps, error) {
	conns := protocol.GetConnectionsSnapshot()
	if len(conns) == 0 {
		return nil, fmt.Errorf("no connected machines available")
	}

	var lastErr error
	for _, c := range conns {
		resp, err := s.GetTLSSANIps(c.MachineName)
		if err != nil {
			logger.Debugf("k8s tls sans failed for %s: %v", c.MachineName, err)
			lastErr = err
			continue
		}
		if resp == nil || len(resp.Ips) == 0 {
			lastErr = fmt.Errorf("empty tls sans from %s", c.MachineName)
			continue
		}
		return resp, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no tls sans available")
}

// GetClusterStatus returns cluster connectivity for every known slave.
func (s *K8sService) GetClusterStatus() (*ClusterStatus, error) {
	conns := protocol.GetConnectionsSnapshot()
	status := &ClusterStatus{Connected: []ClusterNodeStatus{}, Disconnected: []ClusterNodeStatus{}}
	if len(conns) == 0 {
		return status, fmt.Errorf("no connected machines available")
	}

	var lastErr error
	for _, c := range conns {
		node := ClusterNodeStatus{
			MachineName: c.MachineName,
			Addr:        c.Addr,
			LastSeen:    c.LastSeen,
		}

		resp, err := s.GetTLSSANIps(c.MachineName)
		if err != nil {
			node.Error = err.Error()
			status.Disconnected = append(status.Disconnected, node)
			lastErr = err
			continue
		}

		if resp == nil || len(resp.Ips) == 0 {
			node.Error = "not connected to cluster"
			if lastErr == nil {
				lastErr = fmt.Errorf("%s: not connected to cluster", c.MachineName)
			}
			status.Disconnected = append(status.Disconnected, node)
			continue
		}

		node.Connected = true
		node.TLSSANIps = resp.Ips
		status.Connected = append(status.Connected, node)
	}

	if len(status.Connected) == 0 && lastErr != nil {
		return status, fmt.Errorf("no nodes connected to cluster: %w", lastErr)
	}

	return status, lastErr
}

func (s *K8sService) GetConnectionFileAny(ip string) (*k8sGrpc.ConnectionFile, error) {
	conns := protocol.GetConnectionsSnapshot()
	if len(conns) == 0 {
		return nil, fmt.Errorf("no connected machines available")
	}

	var lastErr error
	for _, c := range conns {
		resp, err := s.GetConnectionFile(c.MachineName, ip)
		if err != nil {
			logger.Debugf("k8s connection file failed for %s: %v", c.MachineName, err)
			lastErr = err
			continue
		}
		if resp == nil || resp.File == "" {
			lastErr = fmt.Errorf("empty connection file from %s", c.MachineName)
			continue
		}
		resp.File = strings.ReplaceAll(resp.File, "\\n", "\n")
		return resp, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no connection file available")
}

func (s *K8sService) IsMasterSlave(machineName string) (bool, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return false, fmt.Errorf("no connection found for machine: %s", machineName)
	}

	areWeCrl, err := k8s.IsMasterSlave(conn.Connection)
	if err != nil {
		return false, err
	}

	return areWeCrl.WeAreMasterSlave, nil
}

func (s *K8sService) SetConnectionToCluster(machineName string, serverIp, token string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil {
		return fmt.Errorf("no connection found for machine: %s", machineName)
	}

	_, err := k8s.SetConnectionToCluster(conn.Connection, &k8sGrpc.ConnectionToCluster{ServerIp: serverIp, Token: token})
	return err
}

// funcoes para ver status do cluster, algumas infos basicas talvez
var ErrSlaveMasterNotConnected error = fmt.Errorf("slave master is not connected yet or there are bugs on the damm code")

// master slave ja esta conectado (porque é server) temos de confirmar se é slave normal ou master slave
func (s *K8sService) ConnectSlaveToCluster() error {
	allCons := protocol.GetConnectionsSnapshot()

	//ir buscar a token
	var tokenFound *k8sGrpc.Token
	tokenFound = nil
	var slaveMaster protocol.ConnectionsStruct

	for _, con := range allCons {
		tok, err := s.GetToken(con.MachineName)
		if err != nil {
			logger.Errorf("%v", err)
			continue
		}
		if tok.Token != "" {
			tokenFound = tok
			slaveMaster = con
			break
		}
	}

	if tokenFound == nil {
		return ErrSlaveMasterNotConnected
	}

	//temos token
	for _, con := range allCons {
		//se for o slave master continuamos porque é server
		if slaveMaster.MachineName == con.MachineName {
			continue
		}
		err := s.SetConnectionToCluster(con.MachineName, slaveMaster.Addr, tokenFound.Token)
		if err != nil {
			logger.Errorf("%v", err)
		}
	}

	return nil
}
