package celld

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	"google.golang.org/protobuf/proto"
)

var fleetIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// No child resumes from disk. Only the master's current membership can enable it.
type Registry struct {
	gate       sync.RWMutex
	mu         sync.Mutex
	legacy     *Manager
	fleets     map[string]*Manager
	allowed    map[string]bool
	running    map[string]bool
	startAfter time.Time
	configs    map[string]*pb.Config
	life       context.Context
	cancel     context.CancelFunc
	closed     bool
}

func NewRegistry(dir, binary, ip string) (*Registry, error) {
	legacy, err := New(dir, binary, ip)
	if err != nil {
		return nil, err
	}
	life, cancel := context.WithCancel(context.Background())
	r := &Registry{legacy: legacy, fleets: make(map[string]*Manager), allowed: make(map[string]bool), configs: make(map[string]*pb.Config), life: life, cancel: cancel, startAfter: time.Now().Add(10 * time.Second)}
	entries, err := os.ReadDir(filepath.Join(dir, "fleets"))
	if err != nil && !os.IsNotExist(err) {
		r.Close()
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !fleetIDPattern.MatchString(entry.Name()) {
			continue
		}
		m, err := New(filepath.Join(dir, "fleets", entry.Name()), binary, ip)
		if err != nil {
			r.Close()
			return nil, err
		}
		r.fleets[entry.Name()] = m
		data, err := os.ReadFile(filepath.Join(m.dir, "config.json"))
		if os.IsNotExist(err) {
			continue
		}
		var c pb.Config
		if err == nil {
			err = json.Unmarshal(data, &c)
		}
		if err != nil {
			r.Close()
			return nil, err
		}
		r.configs[entry.Name()] = &c
	}
	return r, nil
}

func (r *Registry) Sync(ids, runningIDs []string) error {
	r.gate.Lock()
	defer r.gate.Unlock()
	if r.closed {
		return fmt.Errorf("celld registry is shutting down")
	}
	allowed := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !fleetIDPattern.MatchString(id) {
			return fmt.Errorf("invalid fleet ID")
		}
		allowed[id] = true
	}
	running := make(map[string]bool, len(runningIDs))
	for _, id := range runningIDs {
		if !allowed[id] {
			return fmt.Errorf("running fleet must be assigned")
		}
		running[id] = true
	}
	r.allowed = allowed
	r.running = running
	var stop []*Manager
	stop = append(stop, r.legacy)
	for id, m := range r.fleets {
		if !running[id] {
			stop = append(stop, m)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(stop))
	for _, m := range stop {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.Stop(); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	return nil
}

func (r *Registry) Configure(ctx context.Context, c *pb.Config, start bool) (*pb.Status, error) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.life, cancel)
	defer stop()
	defer cancel()
	r.gate.RLock()
	defer r.gate.RUnlock()
	if err := validate(c); err != nil {
		return nil, err
	}
	if r.closed || c.FleetId == "" || !r.allowed[c.FleetId] || (start && !r.running[c.FleetId]) {
		return nil, fmt.Errorf("fleet is not authorized by master synchronization")
	}
	if start {
		timer := time.NewTimer(time.Until(r.startAfter))
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	r.mu.Lock()
	// Include persisted reservations, including disabled fleets awaiting removal.
	for id, other := range r.configs {
		if id == c.FleetId {
			continue
		}
		if c.HttpPort == other.HttpPort || c.HttpPort == other.InternalPort || c.InternalPort == other.HttpPort || c.InternalPort == other.InternalPort {
			r.mu.Unlock()
			return nil, fmt.Errorf("ports are reserved by fleet %s", id)
		}

	}
	m := r.fleets[c.FleetId]
	if m == nil {
		var err error
		m, err = New(filepath.Join(r.legacy.dir, "fleets", c.FleetId), r.legacy.binary, r.legacy.ip)
		if err != nil {
			r.mu.Unlock()
			return nil, err
		}
		r.fleets[c.FleetId] = m
	}
	r.configs[c.FleetId] = proto.Clone(c).(*pb.Config)
	r.mu.Unlock()
	err := m.Configure(ctx, c, start)
	return m.Status(), err
}

func (r *Registry) Status(id string) *pb.Status {
	r.gate.RLock()
	defer r.gate.RUnlock()
	r.mu.Lock()
	m := r.fleets[id]
	r.mu.Unlock()
	if m != nil {
		return m.Status()
	}
	return &pb.Status{FleetId: id, NodeIp: r.legacy.ip}
}

func (r *Registry) Stop(id string, remove bool) error {
	r.gate.Lock()
	defer r.gate.Unlock()
	if r.closed {
		return fmt.Errorf("celld registry is shutting down")
	}
	if id != "" && !fleetIDPattern.MatchString(id) {
		return fmt.Errorf("invalid fleet ID")
	}
	m := r.fleets[id]
	if id == "" {
		m = r.legacy
	}
	if m == nil {
		return nil
	}
	if err := m.Stop(); err != nil {
		return err
	}
	if !remove {
		return nil
	}
	if id == "" {
		for _, name := range []string{"enabled", "config.json", "config.json.tmp", "data", "logs"} {
			if err := os.RemoveAll(filepath.Join(m.dir, name)); err != nil {
				return err
			}
		}
		return nil
	}
	// Keep ownership while purging. A failed removal stays retryable in the registry.
	for _, name := range []string{"enabled", "config.json", "config.json.tmp", "data", "logs"} {
		if err := os.RemoveAll(filepath.Join(m.dir, name)); err != nil {
			return err
		}
	}
	m.Close()
	if err := os.RemoveAll(m.dir); err != nil {
		return err
	}
	r.mu.Lock()
	delete(r.fleets, id)
	delete(r.configs, id)
	delete(r.allowed, id)
	r.mu.Unlock()
	return nil
}

func (r *Registry) Prepare(ctx context.Context, version string) error {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.life, cancel)
	defer stop()
	defer cancel()
	r.gate.RLock()
	defer r.gate.RUnlock()
	if r.closed || !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid release or closed registry")
	}
	return r.legacy.install(ctx, version)
}

func (r *Registry) Close() {
	r.cancel()
	r.mu.Lock()
	r.legacy.stopLife()
	for _, m := range r.fleets {
		m.stopLife()
	}
	r.mu.Unlock()
	r.gate.Lock()
	defer r.gate.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	var wg sync.WaitGroup
	for _, m := range r.fleets {
		wg.Add(1)
		go func() { defer wg.Done(); m.Close() }()
	}
	wg.Add(1)
	go func() { defer wg.Done(); r.legacy.Close() }()
	wg.Wait()
}
