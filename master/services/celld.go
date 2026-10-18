package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
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

var ErrCelldNotFound = errors.New("fleet not found")
var ErrCelldConflict = errors.New("fleet conflict")
var ErrCelldInvalid = errors.New("invalid fleet request")

type CelldPorts struct {
	HTTP     uint32 `json:"http"`
	Internal uint32 `json:"internal"`
}
type CelldMachine struct {
	Name      string     `json:"name"`
	IP        string     `json:"ip,omitempty"`
	Connected bool       `json:"connected"`
	Running   bool       `json:"running"`
	Healthy   bool       `json:"healthy"`
	Ports     CelldPorts `json:"ports"`
	URL       string     `json:"url,omitempty"`
	Error     string     `json:"error,omitempty"`
}
type CelldCheck struct {
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}
type CelldStorage struct {
	CelldCheck
	BucketCount        *int      `json:"bucket_count"`
	Buckets            []string  `json:"buckets"`
	ManagedBucketCount int       `json:"managed_bucket_count"`
	CheckedAt          time.Time `json:"checked_at"`
}
type CelldFleetHealth struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Healthy   bool      `json:"healthy"`
	Status    string    `json:"status"`
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error,omitempty"`
}
type CelldHealth struct {
	Fleets    []CelldFleetHealth `json:"fleets"`
	Healthy   bool               `json:"healthy"`
	Storage   CelldCheck         `json:"storage"`
	Machines  []CelldMachine     `json:"machines"`
	CheckedAt time.Time          `json:"checked_at"`
}
type CelldLocation struct {
	Bucket   string `json:"bucket"`
	Endpoint string `json:"endpoint"`
	Region   string `json:"region"`
}
type CelldDeployment struct {
	Command        string   `json:"command"`
	CredentialsURL string   `json:"credentials_url"`
	Environment    []string `json:"environment"`
}
type CelldFleetInfo struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	DesiredState string          `json:"desired_state"`
	Status       string          `json:"status"`
	Servers      []CelldMachine  `json:"servers"`
	Ports        CelldPorts      `json:"ports"`
	Storage      CelldLocation   `json:"storage"`
	URLs         []string        `json:"urls"`
	Deploy       CelldDeployment `json:"deploy"`
	CheckedAt    time.Time       `json:"checked_at"`
	Error        string          `json:"error,omitempty"`
}
type CelldFleetInput struct {
	Name    *string     `json:"name"`
	Servers *[]string   `json:"servers"`
	Ports   *CelldPorts `json:"ports"`
}
type CelldCredentials struct {
	AccessKey string `json:"aws_access_key_id"`
	SecretKey string `json:"aws_secret_access_key"`
}
type CelldList struct {
	Fleets       []CelldFleetInfo `json:"fleets"`
	Storage      CelldStorage     `json:"storage"`
	ResetPending bool             `json:"reset_pending"`
	Error        string           `json:"error,omitempty"`
}

type celldRecord struct {
	CelldFleetInfo
	AccessKey      string          `json:"access_key"`
	SecretKey      string          `json:"secret_key"`
	Provisioned    bool            `json:"provisioned"`
	Operation      string          `json:"operation,omitempty"`
	Removed        []CelldMachine  `json:"removed,omitempty"`
	Cleaned        map[string]bool `json:"cleaned,omitempty"`
	RestartIndex   int             `json:"restart_index,omitempty"`
	RestartStopped bool            `json:"restart_stopped,omitempty"`
}
type celldFleetState struct {
	Schema       int    `json:"schema"`
	MasterIP     string `json:"master_ip"`
	RootUser     string `json:"root_user"`
	RootPassword string `json:"root_password"`
	// Legacy singleton credentials/inventory are retained only until reset completes.
	AccessKey    string                  `json:"access_key,omitempty"`
	SecretKey    string                  `json:"secret_key,omitempty"`
	Provisioned  bool                    `json:"provisioned,omitempty"`
	Machines     []CelldMachine          `json:"machines,omitempty"`
	ResetPending bool                    `json:"reset_pending"`
	ResetCleaned map[string]bool         `json:"reset_cleaned,omitempty"`
	Fleets       map[string]*celldRecord `json:"fleets"`
}
type CelldService struct {
	observations  *celldObservability
	lastError     string
	mu            sync.RWMutex
	state         celldFleetState
	loaded        bool
	path          string
	wake          chan struct{}
	storage       CelldStorage
	busy          map[string]bool
	connections   func() []protocol.ConnectionsStruct
	ready         func(context.Context, *pb.Config, string) error
	agentBusy     map[string]bool
	removeStorage func(context.Context, celldFleetState, string, string) error
}

func NewCelldService(path string) *CelldService {
	return &CelldService{path: path, wake: make(chan struct{}, 1), observations: newCelldObservability()}
}

var CelldFleet = NewCelldService(celldFleetPath)

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
func celldVersion() string {
	if v := strings.TrimSpace(os.Getenv("CELLD_VERSION")); v != "" {
		return v
	}
	return "v0.6.2"
}
func (s *CelldService) snapshotConnections() []protocol.ConnectionsStruct {
	if s.connections != nil {
		return s.connections()
	}
	return protocol.GetConnectionsSnapshot()
}
func (s *CelldService) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	if s.path == "" {
		s.path = celldFleetPath
	}
	data, err := os.ReadFile(s.path)
	if err == nil {
		if err = json.Unmarshal(data, &s.state); err != nil {
			return err
		}
		if s.state.RootUser == "" || s.state.RootPassword == "" {
			return fmt.Errorf("incomplete persisted SILO credentials")
		}
		if s.state.Schema == 0 {
			if s.state.AccessKey == "" || s.state.SecretKey == "" {
				return fmt.Errorf("incomplete persisted legacy credentials")
			}
			s.state.ResetPending = true
			s.state.Schema = 2
		}
		if s.state.Schema != 2 {
			return fmt.Errorf("unsupported celld state schema")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		password, err := celldSecret()
		if err != nil {
			return err
		}
		s.state = celldFleetState{Schema: 2, RootUser: "hyperhive-admin", RootPassword: password}
	} else {
		return err
	}
	if s.state.Fleets == nil {
		s.state.Fleets = make(map[string]*celldRecord)
	}
	if s.state.ResetCleaned == nil {
		s.state.ResetCleaned = make(map[string]bool)
	}
	if err = s.saveLocked(); err != nil {
		return err
	}
	s.loaded = true
	return nil
}

// Publish only after syncing both the file and its directory.
func (s *CelldService) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(s.path+".tmp", s.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (s *CelldService) copyLocked() celldFleetState {
	data, _ := json.Marshal(s.state)
	var st celldFleetState
	_ = json.Unmarshal(data, &st)
	return st
}
func (s *CelldService) commitLocked(previous celldFleetState) error {
	if err := s.saveLocked(); err != nil {
		s.state = previous
		return err
	}
	s.Wake()
	return nil
}
func recordConfig(st celldFleetState, f *celldRecord) *pb.Config {
	return &pb.Config{Version: celldVersion(), FleetId: f.ID, Bucket: "s3://" + f.Storage.Bucket, Endpoint: "http://" + net.JoinHostPort(st.MasterIP, "9000"), Region: "us-east-1", AccessKey: f.AccessKey, SecretKey: f.SecretKey, HttpPort: f.Ports.HTTP, InternalPort: f.Ports.Internal}
}
func (s *CelldService) infoLocked(f *celldRecord) CelldFleetInfo {
	info := f.CelldFleetInfo
	info.Servers = append([]CelldMachine{}, f.Servers...)
	info.URLs = append([]string{}, f.URLs...)
	if s.state.MasterIP != "" {
		info.Storage.Endpoint = "http://" + net.JoinHostPort(s.state.MasterIP, "9000")
	}
	info.Deploy = CelldDeployment{Command: fmt.Sprintf("celld deploy . --bucket s3://%s --endpoint %s --region %s", info.Storage.Bucket, info.Storage.Endpoint, info.Storage.Region), CredentialsURL: "/celld/fleets/" + f.ID + "/credentials", Environment: []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_REGION=us-east-1"}}
	return info
}
func (s *CelldService) List() (CelldList, error) {
	if err := s.load(); err != nil {
		return CelldList{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := CelldList{Fleets: []CelldFleetInfo{}, Storage: s.storage, ResetPending: s.state.ResetPending, Error: s.lastError}
	result.Storage.Buckets = append([]string(nil), s.storage.Buckets...)
	if result.Storage.CheckedAt.IsZero() {
		result.Storage.Error = "storage has not been checked"
	}
	result.Storage.ManagedBucketCount = len(s.state.Fleets)
	for _, f := range s.state.Fleets {
		result.Fleets = append(result.Fleets, s.infoLocked(f))
	}
	sort.Slice(result.Fleets, func(i, j int) bool { return result.Fleets[i].Name < result.Fleets[j].Name })
	return result, nil
}
func (s *CelldService) Get(id string) (CelldFleetInfo, error) {
	if err := s.load(); err != nil {
		return CelldFleetInfo{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	f := s.state.Fleets[id]
	if f == nil {
		return CelldFleetInfo{}, ErrCelldNotFound
	}
	return s.infoLocked(f), nil
}
func (s *CelldService) Credentials(id string) (CelldCredentials, error) {
	if err := s.load(); err != nil {
		return CelldCredentials{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	f := s.state.Fleets[id]
	if f == nil {
		return CelldCredentials{}, ErrCelldNotFound
	}
	if f.DesiredState == "deleting" {
		return CelldCredentials{}, ErrCelldConflict
	}
	return CelldCredentials{f.AccessKey, f.SecretKey}, nil
}
func (s *CelldService) Health(context.Context) CelldHealth {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := CelldHealth{Storage: s.storage.CelldCheck, Machines: []CelldMachine{}, Fleets: []CelldFleetHealth{}, CheckedAt: s.storage.CheckedAt}
	if result.CheckedAt.IsZero() {
		result.Storage.Error = "storage has not been checked"
	}
	result.Healthy = result.Storage.Healthy && !s.state.ResetPending
	for _, f := range s.state.Fleets {
		if f.DesiredState != "running" {
			continue
		}
		health := CelldFleetHealth{ID: f.ID, Name: f.Name, Healthy: f.Status == "running", Status: f.Status, CheckedAt: f.CheckedAt, Error: f.Error}
		for _, n := range f.Servers {
			if !n.Running || !n.Healthy {
				health.Healthy = false
			}
		}
		result.Machines = append(result.Machines, f.Servers...)
		result.Fleets = append(result.Fleets, health)
		if !health.Healthy {
			result.Healthy = false
		}
	}
	sort.Slice(result.Fleets, func(i, j int) bool { return result.Fleets[i].Name < result.Fleets[j].Name })
	return result
}
