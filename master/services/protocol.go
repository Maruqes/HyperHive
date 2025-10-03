package services

import (
	"512SvMan/protocol"
)

type ProtocolService struct{}

func (s *ProtocolService) GetAllConnections() []protocol.ConnectionsStruct {
	return protocol.GetConnectionsSnapshot()
}
