package services

import (
	"512SvMan/k8s"
	"512SvMan/protocol"
	"fmt"

	k8sGrpc "github.com/Maruqes/512SvMan/api/proto/k8s"
	"github.com/Maruqes/512SvMan/logger"
)

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
