package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"512SvMan/protocol"
	pb "github.com/Maruqes/512SvMan/api/proto/celld"
)

const celldFleetPath = "/var/lib/hyperhive/celld-fleet.json"
const celldBinary = "/usr/local/bin/celld"

type CelldMachine struct {
	Name      string `json:"name"`
	IP        string `json:"ip"`
	Connected bool   `json:"connected"`
	Healthy   bool   `json:"healthy"`
	Error     string `json:"error,omitempty"`
}

type CelldCheck struct {
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

type CelldHealth struct {
	Healthy  bool           `json:"healthy"`
	Storage  CelldCheck     `json:"storage"`
	Machines []CelldMachine `json:"machines"`
}

type celldFleetState struct {
	MasterIP     string         `json:"master_ip"`
	RootUser     string         `json:"root_user"`
	RootPassword string         `json:"root_password"`
	AccessKey    string         `json:"access_key"`
	SecretKey    string         `json:"secret_key"`
	Provisioned  bool           `json:"provisioned"`
	Machines     []CelldMachine `json:"machines"`
}

type CelldService struct {
	mu               sync.RWMutex
	state            celldFleetState
	loaded, prepared bool
	wake             chan struct{}
	lastError        string
}

var CelldFleet = &CelldService{wake: make(chan struct{}, 1)}

func StartCelld(ctx context.Context) {
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			if err := CelldFleet.reconcile(ctx); err != nil {
				CelldFleet.mu.Lock()
				CelldFleet.lastError = err.Error()
				CelldFleet.mu.Unlock()
				log.Printf("celld fleet setup pending: %v", err)
			} else {
				CelldFleet.mu.Lock()
				CelldFleet.lastError = ""
				CelldFleet.mu.Unlock()
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

func (s *CelldService) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func celldSecret() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *CelldService) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	data, err := os.ReadFile(celldFleetPath)
	if err == nil {
		if err = json.Unmarshal(data, &s.state); err != nil {
			return err
		}
		if s.state.RootUser == "" || s.state.RootPassword == "" || s.state.AccessKey == "" || s.state.SecretKey == "" {
			return fmt.Errorf("incomplete persisted celld credentials")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		s.state.RootUser = "hyperhive-admin"
		if s.state.RootPassword, err = celldSecret(); err != nil {
			return err
		}
		key, err := celldSecret()
		if err != nil {
			return err
		}
		s.state.AccessKey = "celld-" + key[:16]
		if s.state.SecretKey, err = celldSecret(); err != nil {
			return err
		}
		if err = s.saveLocked(); err != nil {
			return err
		}
	} else {
		return err
	}
	s.loaded = true
	return nil
}

func (s *CelldService) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(celldFleetPath), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(celldFleetPath+".tmp", data, 0600); err != nil {
		return err
	}
	return os.Rename(celldFleetPath+".tmp", celldFleetPath)
}

func fleetConfig(st celldFleetState) *pb.Config {
	v := strings.TrimSpace(os.Getenv("CELLD_VERSION"))
	if v == "" {
		v = "v0.6.2"
	}
	return &pb.Config{Version: v, Bucket: "s3://hyperhive-celld", Endpoint: "http://" + net.JoinHostPort(st.MasterIP, "9000"), Region: "us-east-1", AccessKey: st.AccessKey, SecretKey: st.SecretKey}
}

func (s *CelldService) reconcile(ctx context.Context) error {
	if err := s.load(); err != nil {
		return err
	}
	connections := protocol.GetConnectionsSnapshot()
	s.mu.Lock()
	for _, c := range connections {
		found := false
		for i := range s.state.Machines {
			if s.state.Machines[i].Name == c.MachineName {
				s.state.Machines[i].IP = c.Addr
				found = true
				break
			}
		}
		if !found {
			s.state.Machines = append(s.state.Machines, CelldMachine{Name: c.MachineName, IP: c.Addr})
		}
	}
	sort.Slice(s.state.Machines, func(i, j int) bool { return s.state.Machines[i].Name < s.state.Machines[j].Name })
	err := s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}

	var master *protocol.ConnectionsStruct
	for i := range connections {
		c := &connections[i]
		if c.Connection == nil {
			continue
		}
		call, cancel := context.WithTimeout(ctx, 5*time.Second)
		status, err := pb.NewCelldServiceClient(c.Connection).GetStatus(call, &pb.Empty{})
		cancel()
		if err == nil && status.Master && status.NodeIp == c.Addr {
			master = c
			break
		}
	}
	if master == nil {
		return fmt.Errorf("waiting for the master slave agent")
	}
	ip := net.ParseIP(master.Addr)
	if ip == nil || !ip.IsPrivate() {
		return fmt.Errorf("master celld storage requires a private management IP")
	}
	s.mu.Lock()
	if s.state.MasterIP != "" && s.state.MasterIP != master.Addr {
		s.mu.Unlock()
		return fmt.Errorf("refusing to move existing SILO storage to another master IP")
	}
	s.state.MasterIP = master.Addr
	err = s.saveLocked()
	st := s.state
	s.mu.Unlock()
	if err != nil {
		return err
	}
	cfg := fleetConfig(st)
	// Prepare the master's binary before using its deploy/diagnostic commands.
	if !s.prepared {
		if _, err = configureCelld(ctx, *master, cfg, false); err != nil {
			return err
		}
		if err = ensureCelldStorage(ctx, st); err != nil {
			return err
		}
		if err = validateCelldStorage(ctx, cfg); err != nil {
			return fmt.Errorf("SILO storage compatibility: %w", err)
		}
		if err = bootstrapCelld(ctx, cfg); err != nil {
			return err
		}
		s.mu.Lock()
		s.state.Provisioned = true
		err = s.saveLocked()
		s.mu.Unlock()
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.prepared = true
		s.mu.Unlock()
	} else if err = ensureSiloContainer(ctx, st); err != nil {
		return err
	}

	// Finish each replacement before moving to the next machine.
	ordered := []protocol.ConnectionsStruct{*master}
	for _, c := range connections {
		if c.MachineName != master.MachineName {
			ordered = append(ordered, c)
		}
	}
	for _, c := range ordered {
		if c.Connection == nil {
			continue
		}
		if _, err = configureCelld(ctx, c, cfg, true); err != nil {
			return fmt.Errorf("%s: %w", c.MachineName, err)
		}
		if err = waitCelldReady(ctx, cfg, c.Addr); err != nil {
			return fmt.Errorf("waiting for %s before continuing rollout: %w", c.MachineName, err)
		}
	}
	return nil
}

func configureCelld(ctx context.Context, c protocol.ConnectionsStruct, cfg *pb.Config, start bool) (*pb.Status, error) {
	call, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return pb.NewCelldServiceClient(c.Connection).Configure(call, &pb.ConfigureRequest{Config: cfg, Start: start})
}

type celldDiagnostic struct {
	Verdict string `json:"verdict"`
	Check   string `json:"check"`
	Detail  string `json:"detail"`
}

func diagnoseCelld(ctx context.Context, cfg *pb.Config) (map[string]celldDiagnostic, error) {
	cmd := exec.CommandContext(ctx, celldBinary, "diagnose", "--read-only", "--json", "--bucket", cfg.Bucket, "--endpoint", cfg.Endpoint, "--region", cfg.Region,
		"--listen", "127.0.0.1:0", "--internal-listen", "127.0.0.1:0")
	cmd.Env = append(os.Environ(), "AWS_ACCESS_KEY_ID="+cfg.AccessKey, "AWS_SECRET_ACCESS_KEY="+cfg.SecretKey, "AWS_REGION="+cfg.Region, "AWS_SESSION_TOKEN=")
	out, runErr := cmd.Output()
	peers, parseErr := parseCelldPeers(strings.NewReader(string(out)))
	return peers, errors.Join(runErr, parseErr)
}

func parseCelldPeers(reader io.Reader) (map[string]celldDiagnostic, error) {
	peers := make(map[string]celldDiagnostic)
	d := json.NewDecoder(reader)
	for {
		var row celldDiagnostic
		if err := d.Decode(&row); err != nil {
			if errors.Is(err, io.EOF) {
				return peers, nil
			}
			return peers, err
		}
		if strings.HasPrefix(row.Check, "peer ") {
			_, addr, ok := strings.Cut(row.Check, " at ")
			if ok {
				// A restarted node can share an address with its previous, unexpired lease.
				if prior, exists := peers[addr]; !exists || prior.Verdict != "ok" || row.Verdict == "ok" {
					peers[addr] = row
				}
			}
		}
	}
}

var celldHTTP = &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func celldReady(ctx context.Context, ip string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+net.JoinHostPort(ip, "8080")+"/.well-known/celld/health", nil)
	if err != nil {
		return err
	}
	resp, err := celldHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func waitCelldReady(ctx context.Context, cfg *pb.Config, ip string) error {
	wait, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for {
		if err := celldReady(wait, ip); err == nil {
			probe, stop := context.WithTimeout(wait, 20*time.Second)
			peers, _ := diagnoseCelld(probe, cfg)
			stop()
			settled := true
			for _, peer := range peers {
				if peer.Verdict == "ok" && !strings.Contains(" "+peer.Detail+" ", " restoring=0 ") {
					settled = false
				}
			}
			if peer, ok := peers[net.JoinHostPort(ip, "8081")]; ok && peer.Verdict == "ok" && settled {
				return nil
			}
		}
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-wait.Done():
			timer.Stop()
			return wait.Err()
		case <-timer.C:
		}
	}
}

func (s *CelldService) Health(ctx context.Context) CelldHealth {
	s.mu.RLock()
	st := s.state
	machines := append([]CelldMachine{}, st.Machines...)
	setupError := s.lastError
	prepared := s.prepared
	s.mu.RUnlock()
	// Include connections received since the most recent reconciliation, without writing.
	for _, c := range protocol.GetConnectionsSnapshot() {
		found := false
		for i := range machines {
			if machines[i].Name == c.MachineName {
				machines[i].IP = c.Addr
				found = true
				break
			}
		}
		if !found {
			machines = append(machines, CelldMachine{Name: c.MachineName, IP: c.Addr})
		}
	}
	result := CelldHealth{Machines: machines}
	if st.MasterIP == "" {
		result.Storage.Error = "fleet storage is not configured"
		if setupError != "" {
			result.Storage.Error = setupError
		}
		for i := range result.Machines {
			result.Machines[i].Error = "fleet is not configured"
		}
		return result
	}
	check, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cfg := fleetConfig(st)
	resp, err := celldS3(check, cfg, "GET", "?list-type=2&max-keys=0", nil, "")
	if err != nil {
		result.Storage.Error = err.Error()
	} else if resp.code != 200 {
		result.Storage.Error = fmt.Sprintf("SILO returned HTTP %d", resp.code)
	} else {
		result.Storage.Healthy = true
	}
	if !prepared {
		result.Storage.Healthy = false
		result.Storage.Error = "fleet storage has not passed initialization"
		if setupError != "" {
			result.Storage.Error = setupError
		}
	}
	peers, peerErr := diagnoseCelld(check, cfg)
	return finishCelldHealth(check, result, peers, peerErr)
}

func finishCelldHealth(ctx context.Context, result CelldHealth, peers map[string]celldDiagnostic, peerErr error) CelldHealth {
	result.Healthy = result.Storage.Healthy && len(result.Machines) > 0
	var wg sync.WaitGroup
	for i := range result.Machines {
		wg.Add(1)
		go func(node *CelldMachine) {
			defer wg.Done()
			node.Connected, node.Healthy, node.Error = false, false, ""
			peer, ok := peers[net.JoinHostPort(node.IP, "8081")]
			if !ok || peer.Verdict != "ok" {
				node.Error = "no live reachable celld peer"
				if ok {
					node.Error = peer.Detail
				} else if peerErr != nil {
					node.Error += ": " + peerErr.Error()
				}
				return
			}
			node.Connected = true
			if err := celldReady(ctx, node.IP); err != nil {
				node.Error = err.Error()
				return
			}
			node.Healthy = true
		}(&result.Machines[i])
	}
	wg.Wait()
	for _, node := range result.Machines {
		if !node.Connected || !node.Healthy {
			result.Healthy = false
		}
	}
	sort.Slice(result.Machines, func(i, j int) bool { return result.Machines[i].Name < result.Machines[j].Name })
	return result
}
