package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"512SvMan/protocol"
	pb "github.com/Maruqes/512SvMan/api/proto/celld"
)

func StartCelld(ctx context.Context) {
	go CelldFleet.observationLoop(ctx)
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		slots := make(chan struct{}, 4)
		for {
			if ctx.Err() != nil {
				return
			}
			err := CelldFleet.reconcile(ctx, slots)
			CelldFleet.mu.Lock()
			CelldFleet.lastError = ""
			if err != nil {
				CelldFleet.lastError = err.Error()
			}
			CelldFleet.mu.Unlock()
			if err != nil {
				log.Printf("celld coordination pending: %v", err)
			}

			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			case <-CelldFleet.wake:
			}
		}
	}()
}

func (s *CelldService) reconcile(ctx context.Context, slots chan struct{}) error {
	if err := s.load(); err != nil {
		return err
	}
	connections := s.snapshotConnections()
	s.mu.Lock()
	previous := s.copyLocked()
	changed := false
	for _, c := range connections {
		found := false
		for i := range s.state.Machines {
			if s.state.Machines[i].Name == c.MachineName {
				found = true
				if s.state.Machines[i].IP != c.Addr {
					s.state.Machines[i].IP = c.Addr
					changed = true
				}
				break
			}
		}
		if !found {
			s.state.Machines = append(s.state.Machines, CelldMachine{Name: c.MachineName, IP: c.Addr})
			changed = true
		}
	}
	var err error
	if changed {
		err = s.commitLocked(previous)
	}
	st := s.copyLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.scheduleFleets(ctx, slots, true)
	s.syncAgents(ctx, connections)
	if st.MasterIP != "" {
		s.observeStorage(ctx, st)
	}
	// Find the storage host without requiring it to be a fleet member.
	var master *protocol.ConnectionsStruct
	for i, c := range connections {
		if c.Connection == nil {
			continue
		}
		call, cancel := context.WithTimeout(ctx, 3*time.Second)
		status, err := pb.NewCelldServiceClient(c.Connection).GetStatus(call, &pb.Empty{})
		cancel()
		if err == nil && status.Master && status.NodeIp == c.Addr {
			master = &connections[i]
			break
		}
	}
	if master == nil {
		return fmt.Errorf("waiting for the master slave agent with fleet synchronization")
	}
	ip := net.ParseIP(master.Addr)
	if ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
		return fmt.Errorf("SILO requires a private management IP")
	}
	if st.MasterIP != "" && st.MasterIP != master.Addr {
		return fmt.Errorf("refusing to move existing SILO storage")
	}
	if st.MasterIP == "" {
		s.mu.Lock()
		previous = s.copyLocked()
		s.state.MasterIP = master.Addr
		err = s.commitLocked(previous)
		st = s.copyLocked()
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	if err = ensureSiloContainer(ctx, st); err != nil {
		s.mu.Lock()
		s.storage = CelldStorage{CelldCheck: CelldCheck{Error: err.Error()}, CheckedAt: time.Now().UTC()}
		s.mu.Unlock()
		return err
	}
	s.observeStorage(ctx, st)
	prepare, cancel := context.WithTimeout(ctx, 5*time.Minute)
	_, err = pb.NewCelldServiceClient(master.Connection).Prepare(prepare, &pb.PrepareRequest{Version: celldVersion()})
	cancel()
	if err != nil {
		return err
	}
	if st.ResetPending {
		if err = s.resetLegacy(ctx); err != nil {
			return err
		}
	}
	if st.ResetPending {
		s.observeStorage(ctx, st)
	}
	s.scheduleFleets(ctx, slots, false)
	return nil
}

func (s *CelldService) observeStorage(ctx context.Context, st celldFleetState) {
	storage := observeCelldStorage(ctx, &pb.Config{Endpoint: "http://" + net.JoinHostPort(st.MasterIP, "9000"), Region: "us-east-1", AccessKey: st.RootUser, SecretKey: st.RootPassword})
	s.mu.Lock()
	s.storage = storage
	s.mu.Unlock()
}

func (s *CelldService) scheduleFleets(ctx context.Context, slots chan struct{}, preparedOnly bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy == nil {
		s.busy = make(map[string]bool)
	}
	for id, f := range s.state.Fleets {
		if s.busy[id] || (preparedOnly && f.DesiredState == "running" && !f.Provisioned) {
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			continue
		}
		s.busy[id] = true
		go func(id string) {
			defer func() { <-slots; s.mu.Lock(); delete(s.busy, id); s.mu.Unlock() }()
			work, cancel := context.WithTimeout(ctx, 15*time.Minute)
			defer cancel()
			if err := s.reconcileFleet(work, id); err != nil {
				s.fleetError(id, err)
			}
		}(id)
	}
}

func (s *CelldService) syncAgents(ctx context.Context, connections []protocol.ConnectionsStruct) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentBusy == nil {
		s.agentBusy = make(map[string]bool)
	}
	for _, c := range connections {
		if c.Connection == nil || s.agentBusy[c.MachineName] || len(s.agentBusy) >= 4 {
			continue
		}
		name := c.MachineName
		s.agentBusy[name] = true
		go func() {
			defer func() { s.mu.Lock(); delete(s.agentBusy, name); s.mu.Unlock() }()
			client, _, err := s.synchronizedClient(ctx, name)
			if err == nil {
				call, cancel := context.WithTimeout(ctx, 70*time.Second)
				_, err = client.Remove(call, &pb.Empty{})
				cancel()
			}
			if err != nil {
				log.Printf("celld agent %s synchronization pending: %v", name, err)
			}
		}()
	}
}

func (s *CelldService) fleetError(id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.state.Fleets[id]
	if f == nil {
		return
	}
	previous := s.copyLocked()
	f.Error = err.Error()
	f.CheckedAt = time.Now().UTC()
	f.Status = "error"
	if f.DesiredState == "deleting" {
		f.Status = "deleting"
	} else {
		for _, n := range f.Servers {
			if n.Healthy {
				f.Status = "degraded"
				break
			}
		}
	}
	if saveErr := s.saveLocked(); saveErr != nil {
		s.state = previous
		log.Printf("celld progress save failed: %v", saveErr)
	}
}
func (s *CelldService) saveRecord(f *celldRecord) error {
	f.CheckedAt = time.Now().UTC()
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	var saved celldRecord
	if err = json.Unmarshal(data, &saved); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.state.Fleets[f.ID]
	s.state.Fleets[f.ID] = &saved
	if err := s.saveLocked(); err != nil {
		s.state.Fleets[f.ID] = previous
		return err
	}
	return nil
}
func (s *CelldService) synchronizedClient(ctx context.Context, name string) (pb.CelldServiceClient, string, error) {
	var conn *protocol.ConnectionsStruct
	for _, c := range s.snapshotConnections() {
		if c.MachineName == name && c.Connection != nil {
			copy := c
			conn = &copy
			break
		}
	}
	if conn == nil {
		return nil, "", fmt.Errorf("%s: agent is offline", name)
	}
	s.mu.RLock()
	ids := []string{}
	assigned := []string{}
	for id, f := range s.state.Fleets {
		member := false
		for _, n := range f.Servers {
			if n.Name == name {
				member = true
				break
			}
		}
		if f.Operation == "edit" {
			for _, n := range f.Removed {
				if n.Name == name {
					member = true
					break
				}
			}
		}
		if member && f.DesiredState != "deleting" {
			assigned = append(assigned, id)
			if f.DesiredState == "running" {
				ids = append(ids, id)
			}
		}
	}
	s.mu.RUnlock()
	client := pb.NewCelldServiceClient(conn.Connection)
	call, cancel := context.WithTimeout(ctx, 70*time.Second)
	defer cancel()
	if _, err := client.Sync(call, &pb.SyncRequest{RunningFleetIds: ids, AssignedFleetIds: assigned}); err != nil {
		return nil, conn.Addr, fmt.Errorf("%s: synchronization failed: %w", name, err)
	}
	return client, conn.Addr, nil
}
func (s *CelldService) nodeAction(ctx context.Context, name, id, action string) error {
	client, _, err := s.synchronizedClient(ctx, name)
	if err != nil {
		return err
	}
	call, cancel := context.WithTimeout(ctx, 70*time.Second)
	defer cancel()
	if action == "remove" {
		_, err = client.Remove(call, &pb.Empty{FleetId: id})
	} else {
		_, err = client.Stop(call, &pb.Empty{FleetId: id})
	}
	if err != nil {
		return fmt.Errorf("%s: %s failed: %w", name, action, err)
	}
	return nil
}
func (s *CelldService) startNode(ctx context.Context, f *celldRecord, cfg *pb.Config, i int) error {
	node := &f.Servers[i]
	client, ip, err := s.synchronizedClient(ctx, node.Name)
	if ip != "" {
		node.IP = ip
	}
	if err != nil {
		node.Running = false
		node.Healthy = false
		node.Connected = false
		node.Error = err.Error()
		_ = s.saveRecord(f)
		return err
	}
	call, cancel := context.WithTimeout(ctx, 5*time.Minute)
	status, err := client.Configure(call, &pb.ConfigureRequest{Config: cfg, Start: true})
	cancel()
	if err == nil {
		node.Running = status.Running
		node.Ports = CelldPorts{HTTP: status.HttpPort, Internal: status.InternalPort}
		if node.Ports.HTTP == 0 {
			node.Ports = f.Ports
		}
		node.Connected = true
		if s.ready != nil {
			err = s.ready(ctx, cfg, ip)
		} else {
			err = waitCelldReady(ctx, cfg, ip)
		}
	}
	node.Healthy = err == nil
	node.Error = ""
	node.URL = ""
	if err != nil {
		node.Error = err.Error()
	} else {
		node.Running = true
		node.URL = "http://" + net.JoinHostPort(ip, strconv.Itoa(int(f.Ports.HTTP)))
	}
	if saveErr := s.saveRecord(f); saveErr != nil {
		return saveErr
	}
	return err
}

func (s *CelldService) reconcileFleet(ctx context.Context, id string) error {
	s.mu.RLock()
	st := s.copyLocked()
	s.mu.RUnlock()
	f := st.Fleets[id]
	if f == nil {
		return nil
	}
	if f.Cleaned == nil {
		f.Cleaned = make(map[string]bool)
	}
	cfg := recordConfig(st, f)
	if f.DesiredState == "deleting" {
		all := append(append([]CelldMachine{}, f.Servers...), f.Removed...)
		var pending error
		for _, n := range all {
			if f.Cleaned[n.Name] {
				continue
			}
			if err := s.nodeAction(ctx, n.Name, id, "remove"); err != nil {
				pending = err
				continue
			}
			f.Cleaned[n.Name] = true
			for i := range f.Servers {
				if f.Servers[i].Name == n.Name {
					f.Servers[i].Running = false
					f.Servers[i].Healthy = false
					f.Servers[i].URL = ""
					f.Servers[i].Error = ""
				}
			}
			if err := s.saveRecord(f); err != nil {
				return err
			}
		}
		if pending != nil {
			return pending
		}
		remove := s.removeStorage
		if remove == nil {
			remove = removeCelldStorage
		}
		if err := remove(ctx, st, f.Storage.Bucket, f.AccessKey); err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		previous := s.copyLocked()
		delete(s.state.Fleets, id)
		return s.commitLocked(previous)
	}
	if f.DesiredState == "stopped" {
		var pending error
		for i := range f.Servers {
			n := &f.Servers[i]
			if err := s.nodeAction(ctx, n.Name, id, "stop"); err != nil {
				n.Error = err.Error()
				n.Connected = false
				n.Healthy = false
				pending = err
				continue
			}
			client, _, err := s.synchronizedClient(ctx, n.Name)
			if err == nil {
				call, cancel := context.WithTimeout(ctx, 5*time.Minute)
				_, err = client.Configure(call, &pb.ConfigureRequest{Config: cfg})
				cancel()
			}
			if err != nil {
				n.Error = err.Error()
				pending = err
				continue
			}
			n.Running = false
			n.Healthy = false
			n.Connected = true
			n.Error = ""
			n.URL = ""
			n.Ports = f.Ports
		}
		if err := s.saveRecord(f); err != nil {
			return err
		}
		if pending != nil {
			return pending
		}
	} else {
		if !f.Provisioned {
			if err := ensureCelldStorage(ctx, st, f); err != nil {
				return err
			}
			if err := validateCelldStorage(ctx, cfg); err != nil {
				return fmt.Errorf("storage compatibility: %w", err)
			}
			if err := bootstrapCelld(ctx, cfg); err != nil {
				return err
			}
			f.Provisioned = true
			if err := s.saveRecord(f); err != nil {
				return err
			}
		}
		if f.Operation == "restart" {
			for f.RestartIndex < len(f.Servers) {
				i := f.RestartIndex
				if !f.RestartStopped {
					if err := s.nodeAction(ctx, f.Servers[i].Name, id, "stop"); err != nil {
						return err
					}
					f.RestartStopped = true
					if err := s.saveRecord(f); err != nil {
						return err
					}
				}
				if err := s.startNode(ctx, f, cfg, i); err != nil {
					return err
				}
				f.RestartIndex++
				f.RestartStopped = false
				if err := s.saveRecord(f); err != nil {
					return err
				}
			}
		} else {
			var pending error
			for i := range f.Servers {
				if err := s.startNode(ctx, f, cfg, i); err != nil {
					pending = err
				}
			}
			if pending != nil {
				return pending
			}
		}
	}
	// New members are ready (or the entire fleet is stopped) before old data drains.
	var pending error
	for i := 0; i < len(f.Removed); {
		if err := s.nodeAction(ctx, f.Removed[i].Name, id, "remove"); err != nil {
			pending = err
			i++
			continue
		}
		f.Removed = append(f.Removed[:i], f.Removed[i+1:]...)
		if err := s.saveRecord(f); err != nil {
			return err
		}
	}
	if pending != nil {
		return pending
	}
	f.Operation = ""
	f.Error = ""
	f.Status = "stopped"
	f.CheckedAt = time.Now().UTC()
	f.URLs = []string{}
	if f.DesiredState == "running" {
		f.Status = "running"
		for _, n := range f.Servers {
			if n.URL != "" {
				f.URLs = append(f.URLs, n.URL)
			}
		}
	}
	return s.saveRecord(f)
}

func (s *CelldService) resetLegacy(ctx context.Context) error {
	s.mu.RLock()
	st := s.copyLocked()
	s.mu.RUnlock()
	// Include agents learned after migration; Sync always disables legacy children.
	var pending error
	for _, n := range st.Machines {
		if st.ResetCleaned[n.Name] {
			continue
		}
		if err := s.nodeAction(ctx, n.Name, "", "remove"); err != nil {
			pending = err
			continue
		}
		s.mu.Lock()
		previous := s.copyLocked()
		s.state.ResetCleaned[n.Name] = true
		err := s.saveLocked()
		if err != nil {
			s.state = previous
		}
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	if pending != nil {
		return fmt.Errorf("legacy reset pending: %w", pending)
	}
	if err := removeCelldStorage(ctx, st, "hyperhive-celld", st.AccessKey); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.copyLocked()
	s.state.ResetPending = false
	s.state.AccessKey = ""
	s.state.SecretKey = ""
	s.state.Provisioned = false
	return s.commitLocked(previous)
}
