package services

import (
	"512SvMan/protocol"
	"fmt"
	"strings"
)

func (s *CelldService) validateInputLocked(in CelldFleetInput, id string, connections []protocol.ConnectionsStruct) error {
	if in.Name != nil {
		if *in.Name == "" || *in.Name != strings.TrimSpace(*in.Name) || len(*in.Name) > 128 {
			return fmt.Errorf("%w: name must contain 1–128 characters without surrounding whitespace", ErrCelldInvalid)
		}
		for other, f := range s.state.Fleets {
			if other != id && f.Name == *in.Name {
				return fmt.Errorf("%w: name already exists", ErrCelldConflict)
			}
		}
	}
	if in.Servers != nil {
		known := make(map[string]bool)
		for _, n := range s.state.Machines {
			known[n.Name] = true
		}
		for _, c := range connections {
			known[c.MachineName] = true
		}
		if len(*in.Servers) == 0 {
			return fmt.Errorf("%w: servers cannot be empty", ErrCelldInvalid)
		}
		seen := make(map[string]bool)
		for _, name := range *in.Servers {
			if !known[name] || seen[name] {
				return fmt.Errorf("%w: unknown or duplicate server %q", ErrCelldInvalid, name)
			}
			seen[name] = true
		}
	}
	return nil
}
func validCelldPorts(ports CelldPorts) bool {
	return ports.HTTP > 0 && ports.HTTP <= 65535 && ports.Internal > 0 && ports.Internal <= 65535 && ports.HTTP != ports.Internal
}
func (s *CelldService) reserveLocked(id string, servers []CelldMachine, ports CelldPorts) bool {
	if !validCelldPorts(ports) {
		return false
	}
	selected := make(map[string]bool, len(servers))
	for _, n := range servers {
		selected[n.Name] = true
	}
	for other, f := range s.state.Fleets {
		if other == id {
			continue
		}
		if ports.HTTP != f.Ports.HTTP && ports.HTTP != f.Ports.Internal && ports.Internal != f.Ports.HTTP && ports.Internal != f.Ports.Internal {
			continue
		}
		for _, n := range f.Servers {
			if selected[n.Name] {
				return false
			}
		}
		for _, n := range f.Removed {
			if selected[n.Name] {
				return false
			}
		}
	}
	return true
}
func celldNodes(names []string, known []CelldMachine, conns []protocol.ConnectionsStruct) []CelldMachine {
	inventory := make(map[string]CelldMachine, len(known)+len(conns))
	for _, n := range known {
		inventory[n.Name] = n
	}
	for _, c := range conns {
		n := inventory[c.MachineName]
		n.Name = c.MachineName
		n.IP = c.Addr
		inventory[c.MachineName] = n
	}
	nodes := make([]CelldMachine, 0, len(names))
	for _, name := range names {
		n := inventory[name]
		n.Name = name
		nodes = append(nodes, n)
	}
	return nodes
}
func (s *CelldService) Create(in CelldFleetInput) (CelldFleetInfo, error) {
	if err := s.load(); err != nil {
		return CelldFleetInfo{}, err
	}
	conns := s.snapshotConnections()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.ResetPending {
		return CelldFleetInfo{}, fmt.Errorf("%w: legacy reset is pending", ErrCelldConflict)
	}
	if in.Name == nil || in.Servers == nil {
		return CelldFleetInfo{}, fmt.Errorf("%w: name and servers are required", ErrCelldInvalid)
	}
	if err := s.validateInputLocked(in, "", conns); err != nil {
		return CelldFleetInfo{}, err
	}
	key, err := celldSecret()
	if err != nil {
		return CelldFleetInfo{}, err
	}
	secret, err := celldSecret()
	if err != nil {
		return CelldFleetInfo{}, err
	}
	id := "f" + key[:24]
	nodes := celldNodes(*in.Servers, s.state.Machines, conns)
	ports := CelldPorts{}
	if in.Ports != nil {
		if !validCelldPorts(*in.Ports) {
			return CelldFleetInfo{}, fmt.Errorf("%w: invalid ports", ErrCelldInvalid)
		}
		ports = *in.Ports
	} else {
		for p := uint32(18080); p < 65000; p += 2 {
			pair := CelldPorts{p, p + 1}
			if s.reserveLocked(id, nodes, pair) {
				ports = pair
				break
			}
		}
	}
	if !s.reserveLocked(id, nodes, ports) {
		return CelldFleetInfo{}, fmt.Errorf("%w: ports are invalid or reserved", ErrCelldConflict)
	}
	previous := s.copyLocked()
	f := &celldRecord{CelldFleetInfo: CelldFleetInfo{ID: id, Name: *in.Name, DesiredState: "running", Status: "provisioning", Servers: nodes, Ports: ports, Storage: CelldLocation{Bucket: "hyperhive-celld-" + id, Region: "us-east-1"}, URLs: []string{}}, AccessKey: "celld-" + key[:16], SecretKey: secret, Operation: "create"}
	s.state.Fleets[id] = f
	if err = s.commitLocked(previous); err != nil {
		return CelldFleetInfo{}, err
	}
	return s.infoLocked(f), nil
}
func (s *CelldService) Update(id string, in CelldFleetInput) (CelldFleetInfo, error) {
	if err := s.load(); err != nil {
		return CelldFleetInfo{}, err
	}
	conns := s.snapshotConnections()
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.state.Fleets[id]
	if f == nil {
		return CelldFleetInfo{}, ErrCelldNotFound
	}
	if f.Operation != "" || s.busy[id] {
		return CelldFleetInfo{}, ErrCelldConflict
	}
	if err := s.validateInputLocked(in, id, conns); err != nil {
		return CelldFleetInfo{}, err
	}
	if in.Ports != nil && !validCelldPorts(*in.Ports) {
		return CelldFleetInfo{}, fmt.Errorf("%w: invalid ports", ErrCelldInvalid)
	}
	if in.Ports != nil && f.DesiredState != "stopped" {
		return CelldFleetInfo{}, fmt.Errorf("%w: ports can only change while stopped", ErrCelldConflict)
	}
	previous := s.copyLocked()
	if in.Name != nil {
		f.Name = *in.Name
	}
	if in.Servers != nil {
		nodes := celldNodes(*in.Servers, f.Servers, conns)
		keep := make(map[string]bool)
		for _, n := range nodes {
			keep[n.Name] = true
		}
		for _, n := range f.Servers {
			if !keep[n.Name] {
				f.Removed = append(f.Removed, n)
			}
		}
		f.Servers = nodes
	}
	if in.Ports != nil {
		f.Ports = *in.Ports
	}
	if !s.reserveLocked(id, append(append([]CelldMachine{}, f.Servers...), f.Removed...), f.Ports) {
		s.state = previous
		return CelldFleetInfo{}, fmt.Errorf("%w: ports are invalid or reserved", ErrCelldConflict)
	}
	f.Operation = "edit"
	f.Status = "provisioning"
	f.Error = ""
	if err := s.commitLocked(previous); err != nil {
		return CelldFleetInfo{}, err
	}
	return s.infoLocked(f), nil
}
func (s *CelldService) Action(id, action string) (CelldFleetInfo, error) {
	if err := s.load(); err != nil {
		return CelldFleetInfo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.state.Fleets[id]
	if f == nil {
		return CelldFleetInfo{}, ErrCelldNotFound
	}
	if s.busy[id] || (f.Operation != "" && (f.Error == "" || (action != "stop" && action != "delete"))) {
		return CelldFleetInfo{}, ErrCelldConflict
	}
	previous := s.copyLocked()
	switch action {
	case "start", "restart":
		f.DesiredState = "running"
		f.Status = "provisioning"
	case "stop":
		f.DesiredState = "stopped"
		f.Status = "provisioning"
	case "delete":
		f.DesiredState = "deleting"
		f.Status = "deleting"
		f.Cleaned = make(map[string]bool)
	default:
		return CelldFleetInfo{}, ErrCelldInvalid
	}
	f.Operation = action
	f.RestartIndex = 0
	f.RestartStopped = false
	f.Error = ""
	if err := s.commitLocked(previous); err != nil {
		return CelldFleetInfo{}, err
	}
	return s.infoLocked(f), nil
}
